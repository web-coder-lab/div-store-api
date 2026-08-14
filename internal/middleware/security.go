package middleware

import (
	"crypto/subtle"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// ─── Stealth 404 (no stack, no path, no host brand) ──────────────────

const stealth404HTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width,initial-scale=1"/>
<meta name="robots" content="noindex,nofollow,noarchive"/>
<title>Not Found</title>
<style>
*{box-sizing:border-box;margin:0}
body{min-height:100vh;display:flex;align-items:center;justify-content:center;
font-family:system-ui,-apple-system,sans-serif;background:#0B0D10;color:#F7F8FA}
.box{text-align:center;padding:32px}
h1{font-size:3rem;font-weight:800;letter-spacing:.04em}
p{margin-top:10px;color:#6B7280;font-size:.95rem}
</style>
</head>
<body>
<div class="box"><h1>404</h1><p>Not Found</p></div>
</body>
</html>`

func Stealth404(w http.ResponseWriter, r *http.Request) {
	// Strip identifying headers
	w.Header().Del("X-Powered-By")
	w.Header().Del("X-Render-Origin-Server")
	w.Header().Set("Server", "web")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "text/html") || !strings.HasPrefix(r.URL.Path, "/api/") {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(stealth404HTML))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(`{"error":"not_found"}`))
}

func writeJSON(w http.ResponseWriter, code int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Server", "web")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(body))
}

// ─── Security headers ───────────────────────────────────────────────

func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-XSS-Protection", "0")
		w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=(), payment=()")
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		w.Header().Set("Cross-Origin-Resource-Policy", "same-site")
		w.Header().Set("X-Permitted-Cross-Domain-Policies", "none")
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https:; connect-src 'self' https:; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains; preload")
		}
		w.Header().Set("Server", "web")
		w.Header().Del("X-Powered-By")
		next.ServeHTTP(w, r)
	})
}

// ─── Body size ──────────────────────────────────────────────────────

func MaxBody(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil && r.Method != http.MethodGet && r.Method != http.MethodHead {
				limit := maxBytes
				if !isUploadPath(r.URL.Path) {
					limit = 2 << 20
				}
				r.Body = http.MaxBytesReader(w, r.Body, limit)
			}
			next.ServeHTTP(w, r)
		})
	}
}

func isUploadPath(path string) bool {
	return strings.Contains(path, "/submit") ||
		strings.Contains(path, "/upload") ||
		strings.Contains(path, "/developers/register")
}

// ─── IP helpers ─────────────────────────────────────────────────────

func clientIP(r *http.Request) string {
	for _, h := range []string{"CF-Connecting-IP", "True-Client-IP", "X-Real-IP"} {
		if v := strings.TrimSpace(r.Header.Get(h)); v != "" {
			return v
		}
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ─── Rate limit + ban ───────────────────────────────────────────────

type visitor struct {
	count       int
	reset       time.Time
	strikes     int
	bannedUntil time.Time
}

var (
	rlMu        sync.Mutex
	rlMap       = map[string]*visitor{}
	rlRate      = 60
	rlWin       = time.Minute
	adminRL     = map[string]*visitor{}
	adminRate   = 20
	submitRL    = map[string]*visitor{}
	submitRate  = 6
	banDuration = 30 * time.Minute
	maxStrikes  = 2
)

func getVisitor(m map[string]*visitor, ip string, now time.Time) *visitor {
	v, ok := m[ip]
	if !ok || now.After(v.reset) {
		v2 := &visitor{count: 0, reset: now.Add(rlWin)}
		if ok {
			v2.strikes = v.strikes
			v2.bannedUntil = v.bannedUntil
		}
		m[ip] = v2
		return v2
	}
	return v
}

func isBanned(v *visitor, now time.Time) bool { return now.Before(v.bannedUntil) }

func strikeAndMaybeBan(v *visitor, now time.Time) {
	v.strikes++
	if v.strikes >= maxStrikes {
		v.bannedUntil = now.Add(banDuration)
		v.strikes = 0
	}
}

func RateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		now := time.Now()
		rlMu.Lock()
		v := getVisitor(rlMap, ip, now)
		if isBanned(v, now) {
			rlMu.Unlock()
			Stealth404(w, r)
			return
		}
		v.count++
		if v.count > rlRate {
			strikeAndMaybeBan(v, now)
			rlMu.Unlock()
			Stealth404(w, r) // scanners see 404 not 429
			return
		}
		rlMu.Unlock()
		next.ServeHTTP(w, r)
	})
}

func SubmitRateLimit(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		now := time.Now()
		rlMu.Lock()
		v, ok := submitRL[ip]
		if !ok || now.After(v.reset) {
			submitRL[ip] = &visitor{count: 1, reset: now.Add(rlWin)}
			rlMu.Unlock()
			next(w, r)
			return
		}
		v.count++
		if v.count > submitRate {
			rlMu.Unlock()
			Stealth404(w, r)
			return
		}
		rlMu.Unlock()
		next(w, r)
	}
}

func AdminRateLimit(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		now := time.Now()
		rlMu.Lock()
		v, ok := adminRL[ip]
		if !ok || now.After(v.reset) {
			adminRL[ip] = &visitor{count: 1, reset: now.Add(rlWin)}
			rlMu.Unlock()
			next(w, r)
			return
		}
		v.count++
		if v.count > adminRate {
			rlMu.Unlock()
			Stealth404(w, r)
			return
		}
		rlMu.Unlock()
		next(w, r)
	}
}

// ─── Scanner / tool signatures ──────────────────────────────────────

var blockedPaths = []string{
	"/.env", "/.git", "/.svn", "/.hg", "/.DS_Store", "/.aws", "/.ssh",
	"/wp-admin", "/wp-login", "/wp-content", "/wp-includes", "/xmlrpc.php",
	"/phpmyadmin", "/admin.php", "/administrator", "/manager/html",
	"/vendor/phpunit", "/actuator", "/server-status", "/server-info",
	"/cgi-bin", "/shell", "/eval", "/config.json", "/debug", "/trace",
	"/telescope", "/_profiler", "/console", "/solr", "/jenkins",
	"/api/v1/pods", "/api/swagger", "/swagger", "/graphql",
	"/favicon.ico", "/robots.txt", "/sitemap.xml", "/.well-known",
	"/backup", "/dump", "/db.sql", "/web.config", "/crossdomain.xml",
	"/phpinfo", "/info.php", "/test.php", "/adminer", "/elmah",
}

var blockedUA = []string{
	"sqlmap", "nikto", "nmap", "masscan", "zgrab", "dirbuster", "gobuster",
	"wfuzz", "nuclei", "httpx", "feroxbuster", "ffuf", "dirsearch",
	"acunetix", "nessus", "openvas", "burpsuite", "w3af", "skipfish",
	"whatweb", "wpscan", "joomscan", "droopescan", "cmseek",
	"python-requests", "python-urllib", "go-http-client", "java/",
	"libwww-perl", "lwp-trivial", "curl/", "wget/", "scrapy",
	"httrack", "semrush", "ahrefs", "mj12bot", "petalbot", "bytespider",
	"gptbot", "ccbot", "claudebot", "dataforseo", "zgrab",
}

// Legitimate app clients we never treat as scanners
func isTrustedUA(ua string) bool {
	u := strings.ToLower(ua)
	return strings.Contains(u, "okhttp") ||
		strings.Contains(u, "divstore") ||
		strings.Contains(u, "dart") ||
		strings.Contains(u, "mozilla/") // real browsers for admin/privacy
}

func isScanner(r *http.Request) bool {
	ua := strings.ToLower(r.UserAgent())
	if isTrustedUA(ua) {
		return false
	}
	if ua == "" {
		return true
	}
	for _, bad := range blockedUA {
		if strings.Contains(ua, bad) {
			return true
		}
	}
	// Headless / automation hints
	if strings.Contains(ua, "headless") || strings.Contains(ua, "phantom") ||
		strings.Contains(ua, "selenium") || strings.Contains(ua, "puppeteer") ||
		strings.Contains(ua, "playwright") {
		return true
	}
	return false
}

func Firewall(next http.Handler) http.Handler {
	allowList := parseIPList(os.Getenv("IP_ALLOWLIST"))
	denyList := parseIPList(os.Getenv("IP_DENYLIST"))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		path := strings.ToLower(r.URL.Path)

		if ipInList(ip, denyList) {
			Stealth404(w, r)
			return
		}
		if len(allowList) > 0 && !ipInList(ip, allowList) {
			Stealth404(w, r)
			return
		}

		// Path traversal
		raw := r.URL.RawPath
		if raw == "" {
			raw = r.URL.Path
		}
		if strings.Contains(raw, "..") || strings.Contains(raw, "\x00") ||
			strings.Contains(path, "%2e%2e") || strings.Contains(path, "..%2f") ||
			strings.Contains(path, "%00") {
			strikeIP(ip)
			Stealth404(w, r)
			return
		}

		// Known probes → always stealth 404 + strike
		for _, p := range blockedPaths {
			if path == p || strings.HasPrefix(path, p+"/") || strings.Contains(path, p) {
				strikeIP(ip)
				Stealth404(w, r)
				return
			}
		}

		// Scanner tools → 404 everything (no 200)
		if isScanner(r) {
			strikeIP(ip)
			Stealth404(w, r)
			return
		}

		if strings.HasPrefix(path, "/api/") {
			switch r.Method {
			case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch,
				http.MethodDelete, http.MethodOptions, http.MethodHead:
			default:
				Stealth404(w, r)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

func strikeIP(ip string) {
	rlMu.Lock()
	v := getVisitor(rlMap, ip, time.Now())
	strikeAndMaybeBan(v, time.Now())
	rlMu.Unlock()
}

func parseIPList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func ipInList(ip string, list []string) bool {
	for _, item := range list {
		if item == ip {
			return true
		}
		if strings.Contains(item, "/") {
			_, netw, err := net.ParseCIDR(item)
			if err == nil && netw.Contains(net.ParseIP(ip)) {
				return true
			}
		}
	}
	return false
}

// ─── Admin auth + IP lock (failures → 404 stealth) ──────────────────

func adminIPAllowed(ip string) bool {
	list := parseIPList(os.Getenv("ADMIN_IP_ALLOWLIST"))
	if len(list) == 0 {
		return false
	}
	return ipInList(ip, list)
}

func RequireAdminIP(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !adminIPAllowed(clientIP(r)) {
			Stealth404(w, r)
			return
		}
		next(w, r)
	}
}

func ServeAdminHTML(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !adminIPAllowed(ip) {
		Stealth404(w, r)
		return
	}
	http.ServeFile(w, r, "admin/index.html")
}

func RequireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		if !adminIPAllowed(ip) {
			strikeIP(ip)
			Stealth404(w, r)
			return
		}
		key := os.Getenv("ADMIN_API_KEY")
		if key == "" {
			key = "div-store-admin"
		}
		auth := r.Header.Get("Authorization")
		token := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
		if token == "" {
			token = r.Header.Get("X-Api-Key")
		}
		if subtle.ConstantTimeCompare([]byte(token), []byte(key)) != 1 {
			strikeIP(ip)
			Stealth404(w, r) // no "Unauthorized" leak
			return
		}
		AdminRateLimit(next)(w, r)
	}
}

func MyIP(w http.ResponseWriter, r *http.Request) {
	// only useful for allowlisted owner; others still get IP (harmless) or 404 scanners
	if isScanner(r) {
		Stealth404(w, r)
		return
	}
	ip := clientIP(r)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Server", "web")
	_, _ = w.Write([]byte(`{"ip":"` + ip + `"}`))
}
