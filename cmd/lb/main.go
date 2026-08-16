package main

import (
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

// Simple round-robin load balancer for Div Store API instances.
// BACKENDS=https://a.onrender.com,https://b.onrender.com,https://c.onrender.com

func main() {
	raw := os.Getenv("BACKENDS")
	if raw == "" {
		log.Fatal("BACKENDS env required (comma-separated base URLs)")
	}
	var backends []*url.URL
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		u, err := url.Parse(p)
		if err != nil || u.Scheme == "" || u.Host == "" {
			log.Fatalf("bad backend: %s", p)
		}
		backends = append(backends, u)
	}
	if len(backends) == 0 {
		log.Fatal("no backends")
	}

	var idx uint64
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: 120 * time.Second,
	}

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			i := atomic.AddUint64(&idx, 1)
			b := backends[int(i-1)%len(backends)]
			req.URL.Scheme = b.Scheme
			req.URL.Host = b.Host
			req.Host = b.Host
			// Preserve path/query
			if _, ok := req.Header["User-Agent"]; !ok {
				req.Header.Set("User-Agent", "DivStore-LB/1.0")
			}
			// Forward client IP
			if ip := req.Header.Get("X-Forwarded-For"); ip == "" {
				if host, _, err := net.SplitHostPort(req.RemoteAddr); err == nil {
					req.Header.Set("X-Forwarded-For", host)
				}
			}
			req.Header.Set("X-Forwarded-Proto", "https")
		},
		Transport: transport,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Printf("proxy error: %v", err)
			http.Error(w, `{"error":"backend_unavailable"}`, http.StatusBadGateway)
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/lb/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok","role":"load_balancer","backends":` + itoa(len(backends)) + `}`))
	})
	mux.Handle("/", proxy)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Div Store LB → :%s backends=%d", port, len(backends))
	log.Fatal(http.ListenAndServe(":"+port, mux))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
