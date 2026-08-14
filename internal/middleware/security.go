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
		// Tight CSP for HTML pages; API JSON ignores it
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https:; connect-src 'self' https:; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains; preload")
		}
		// Hide server fingerprint
		w.Header().Set("Server", "div-store")
		w.Header().Del("X-Powered-By")
		next.ServeHTTP(w, r)
	})
}

// ─── Body size ──────────────────────────────────────────────────────

func MaxBody(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil && r.Method != http.MethodGet && r.Method != http.MethodHead {
				// Tighter limit for non-upload paths
				limit := maxBytes
				path := r.URL.Path
				if !isUploadPath(path) {
					limit = 2 << 20 // 2 MB for normal API
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
	// Prefer Render / proxy real IP
	for _, h := range []string{"CF-Connecting-IP", "True-Client-IP", "X-Real-IP"} {
		if v := strings.TrimSpace(r.Header.Get(h)); v != "" {
			return v
		}
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ─── Rate limit + temporary ban (fail2ban style) ────────────────────

type visitor struct {
	count    int
	reset    time.Time
	strikes  int
	bannedUntil time.Time
}

var (
	rlMu   sync.Mutex
	rlMap  = map[string]*visitor{}
	rlRate = 90  // public req / minute
	rlWin  = time.Minute

	adminRL   = map[string]*visitor{}
	adminRate = 30

	submitRL   = map[string]*visitor{}
	submitRate = 8 // APK submits / minute per IP

	banDuration = 15 * time.Minute
	maxStrikes  = 3
)

func getVisitor(m map[string]*visitor, ip string, now time.Time) *visitor {
	v, ok := m[ip]
	if !ok || now.After(v.reset) {
		// keep ban across windows
		if ok && now.Before(v.bannedUntil) {
			v.count = 0
			v.reset = now.Add(rlWin)
			return v
		}
		v = &visitor{count: 0, reset: now.Add(rlWin)}
		if ok {
			v.strikes = rlMap[ip].strikes
			v.bannedUntil = rlMap[ip].bannedUntil
		}
		m[ip] = v
	}
	return v
}

func isBanned(v *visitor, now time.Time) bool {
	return now.Before(v.bannedUntil)
}

func strikeAndMaybeBan(v *visitor, now time.Time) {
	v.strikes++
	if v.strikes >= maxStrikes {
		v.bannedUntil = now.Add(banDuration)
		v.strikes = 0
	}
}

func writeJSON(w http.ResponseWriter, code int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(body))
}

// RateLimit per-IP fixed window + auto-ban after repeated abuse.
func RateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		now := time.Now()
		rlMu.Lock()
		v := getVisitor(rlMap, ip, now)
		if isBanned(v, now) {
			rlMu.Unlock()
			w.Header().Set("Retry-After", "900")
			writeJSON(w, 403, `{"error":"ip_temporarily_banned"}`)
			return
		}
		v.count++
		if v.count > rlRate {
			strikeAndMaybeBan(v, now)
			rlMu.Unlock()
			w.Header().Set("Retry-After", "60")
			writeJSON(w, 429, `{"error":"rate_limited"}`)
			return
		}
		rlMu.Unlock()
		next.ServeHTTP(w, r)
	})
}

// SubmitRateLimit tighter for APK upload / submit.
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
			w.Header().Set("Retry-After", "60")
			writeJSON(w, 429, `{"error":"submit_rate_limited"}`)
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
			writeJSON(w, 429, `{"error":"admin_rate_limited"}`)
			return
		}
		rlMu.Unlock()
		next(w, r)
	}
}

// ─── Firewall: probes, methods, IP lists, path attacks ──────────────

var blockedPaths = []string{
	"/.env", "/.git", "/wp-admin", "/wp-login", "/xmlrpc.php",
	"/phpmyadmin", "/admin.php", "/.aws", "/vendor/phpunit",
	"/actuator", "/server-status", "/.well-known/security.txt",
	"/cgi-bin", "/shell", "/eval", "/config.json", "/debug",
	"/telescope", "/_profiler", "/manager/html",
}

var blockedUA = []string{
	"sqlmap", "nikto", "nmap", "masscan", "zgrab", "dirbuster",
	"gobuster", "wfuzz", "nuclei", "httpx", "python-requests/",
}

func Firewall(next http.Handler) http.Handler {
	allowList := parseIPList(os.Getenv("IP_ALLOWLIST")) // empty = allow all
	denyList := parseIPList(os.Getenv("IP_DENYLIST"))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		path := strings.ToLower(r.URL.Path)

		// Deny list
		if ipInList(ip, denyList) {
			writeJSON(w, 403, `{"error":"forbidden"}`)
			return
		}
		// Optional allow list (if set, only these IPs)
		if len(allowList) > 0 && !ipInList(ip, allowList) {
			writeJSON(w, 403, `{"error":"ip_not_allowed"}`)
			return
		}

		// Block path traversal / null bytes
		raw := r.URL.RawPath
		if raw == "" {
			raw = r.URL.Path
		}
		if strings.Contains(raw, "..") || strings.Contains(raw, "\x00") ||
			strings.Contains(path, "%2e%2e") || strings.Contains(path, "..%2f") {
			writeJSON(w, 400, `{"error":"bad_path"}`)
			return
		}

		// Known exploit probes
		for _, p := range blockedPaths {
			if path == p || strings.HasPrefix(path, p+"/") || strings.Contains(path, p) {
				// soft-ban probes
				rlMu.Lock()
				v := getVisitor(rlMap, ip, time.Now())
				strikeAndMaybeBan(v, time.Now())
				rlMu.Unlock()
				writeJSON(w, 404, `{"error":"not_found"}`)
				return
			}
		}

		// Suspicious User-Agent (scanners)
		ua := strings.ToLower(r.UserAgent())
		if ua == "" && strings.HasPrefix(path, "/api/") && r.Method != http.MethodGet {
			// empty UA on write API — soft reject
			writeJSON(w, 403, `{"error":"forbidden"}`)
			return
		}
		for _, bad := range blockedUA {
			if strings.Contains(ua, bad) {
				writeJSON(w, 403, `{"error":"forbidden"}`)
				return
			}
		}

		// Method allow for API
		if strings.HasPrefix(path, "/api/") {
			switch r.Method {
			case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions, http.MethodHead:
				// ok
			default:
				writeJSON(w, 405, `{"error":"method_not_allowed"}`)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
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
		// CIDR support
		if strings.Contains(item, "/") {
			_, netw, err := net.ParseCIDR(item)
			if err == nil && netw.Contains(net.ParseIP(ip)) {
				return true
			}
		}
	}
	return false
}

// ─── Admin auth + IP lock ───────────────────────────────────────────

// adminIPAllowed: ADMIN_IP_ALLOWLIST must be set; only listed IPs/CIDRs pass.
// If env empty → deny all admin (force config). Use "0.0.0.0/0" only for emergency open.
func adminIPAllowed(ip string) bool {
	list := parseIPList(os.Getenv("ADMIN_IP_ALLOWLIST"))
	if len(list) == 0 {
		return false
	}
	return ipInList(ip, list)
}

// RequireAdminIP blocks /admin HTML and admin APIs from foreign IPs.
func RequireAdminIP(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		if !adminIPAllowed(ip) {
			writeJSON(w, 403, `{"error":"admin_ip_blocked","ip":"`+ip+`"}`)
			return
		}
		next(w, r)
	}
}

// ServeAdminHTML only from allowlisted IP.
func ServeAdminHTML(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !adminIPAllowed(ip) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(403)
		_, _ = w.Write([]byte(`<!DOCTYPE html><html><body style="font-family:system-ui;background:#F7F8FA;color:#17191C;padding:40px;text-align:center">
<h2>403 — Admin blocked</h2>
<p>Your IP is not allowlisted.</p>
<p style="color:#6B7280;font-size:13px">IP: ` + ip + `</p>
<p style="color:#6B7280;font-size:12px">Open <code>/api/my-ip</code> from your phone/PC and add that IP to <code>ADMIN_IP_ALLOWLIST</code> on Render.</p>
</body></html>`))
		return
	}
	http.ServeFile(w, r, "admin/index.html")
}

func RequireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		// 1) IP lock first
		if !adminIPAllowed(ip) {
			rlMu.Lock()
			v := getVisitor(rlMap, ip, time.Now())
			strikeAndMaybeBan(v, time.Now())
			rlMu.Unlock()
			writeJSON(w, 403, `{"error":"admin_ip_blocked","ip":"`+ip+`"}`)
			return
		}
		// 2) API key
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
			rlMu.Lock()
			v := getVisitor(rlMap, ip, time.Now())
			strikeAndMaybeBan(v, time.Now())
			rlMu.Unlock()
			writeJSON(w, 401, `{"error":"Unauthorized"}`)
			return
		}
		AdminRateLimit(next)(w, r)
	}
}

// MyIP public helper so owner can copy IP into ADMIN_IP_ALLOWLIST.
func MyIP(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ip":"` + ip + `"}`))
}
