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

// SecurityHeaders basic hardening.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-XSS-Protection", "0")
		w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

// MaxBody limits request body size (default 32MB JSON; upload route overrides).
func MaxBody(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil && r.Method != http.MethodGet && r.Method != http.MethodHead {
				r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			}
			next.ServeHTTP(w, r)
		})
	}
}

type visitor struct {
	count int
	reset time.Time
}

var (
	rlMu   sync.Mutex
	rlMap  = map[string]*visitor{}
	rlRate = 120 // requests
	rlWin  = time.Minute
)

func clientIP(r *http.Request) string {
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

// RateLimit simple per-IP fixed window.
func RateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		now := time.Now()
		rlMu.Lock()
		v, ok := rlMap[ip]
		if !ok || now.After(v.reset) {
			rlMap[ip] = &visitor{count: 1, reset: now.Add(rlWin)}
			rlMu.Unlock()
			next.ServeHTTP(w, r)
			return
		}
		v.count++
		if v.count > rlRate {
			rlMu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(429)
			w.Write([]byte(`{"error":"rate_limited"}`))
			return
		}
		rlMu.Unlock()
		next.ServeHTTP(w, r)
	})
}

// AdminRateLimit stricter for write admin routes.
var (
	adminRL   = map[string]*visitor{}
	adminRate = 40
)

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
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(429)
			w.Write([]byte(`{"error":"admin_rate_limited"}`))
			return
		}
		rlMu.Unlock()
		next(w, r)
	}
}

// RequireAdmin constant-time key compare.
func RequireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(401)
			w.Write([]byte(`{"error":"Unauthorized"}`))
			return
		}
		AdminRateLimit(next)(w, r)
	}
}
