package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/husdainshah2-web/div-store/internal/firebase"
	"github.com/husdainshah2-web/div-store/internal/handlers"
	"github.com/husdainshah2-web/div-store/internal/middleware"
)

func main() {
	if err := firebase.Init(); err != nil {
		log.Printf("[firebase] init warning: %v", err)
	} else {
		log.Printf("[firebase] OK project=%s", firebase.ProjectID())
	}

	mux := http.NewServeMux()

	// API
	mux.HandleFunc("/api/health", handlers.Health)
	mux.HandleFunc("/api/apps", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			handlers.ListApps(w, r)
			return
		}
		http.Error(w, "method not allowed", 405)
	})
	mux.HandleFunc("/api/apps/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasSuffix(path, "/reviews") {
			if r.Method == http.MethodGet {
				handlers.ListReviews(w, r)
			} else if r.Method == http.MethodPost {
				handlers.CreateReview(w, r)
			} else {
				http.Error(w, "method not allowed", 405)
			}
			return
		}
		if strings.HasSuffix(path, "/download") && r.Method == http.MethodPost {
			handlers.DownloadApp(w, r)
			return
		}
		if r.Method == http.MethodGet {
			handlers.GetApp(w, r)
			return
		}
		http.Error(w, "method not allowed", 405)
	})
	mux.HandleFunc("/api/categories", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			handlers.ListCategories(w, r)
		case http.MethodPost:
			middleware.RequireAdmin(handlers.CreateCategory)(w, r)
		default:
			http.Error(w, "method not allowed", 405)
		}
	})
	mux.HandleFunc("/api/categories/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			middleware.RequireAdmin(handlers.DeleteCategory)(w, r)
			return
		}
		http.Error(w, "method not allowed", 405)
	})
	mux.HandleFunc("/api/submit", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			handlers.SubmitApp(w, r)
			return
		}
		http.Error(w, "method not allowed", 405)
	})
	mux.HandleFunc("/api/admin/stats", middleware.RequireAdmin(handlers.AdminStats))
	mux.HandleFunc("/api/admin/apps", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			middleware.RequireAdmin(handlers.AdminListApps)(w, r)
		case http.MethodPost:
			middleware.RequireAdmin(handlers.AdminCreateApp)(w, r)
		default:
			http.Error(w, "method not allowed", 405)
		}
	})
	mux.HandleFunc("/api/admin/apps/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPatch, http.MethodPut:
			middleware.RequireAdmin(handlers.AdminUpdateApp)(w, r)
		case http.MethodDelete:
			middleware.RequireAdmin(handlers.AdminDeleteApp)(w, r)
		default:
			http.Error(w, "method not allowed", 405)
		}
	})
	mux.HandleFunc("/api/admin/submissions", middleware.RequireAdmin(handlers.ListSubmissions))
	mux.HandleFunc("/api/admin/submissions/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/approve") && r.Method == http.MethodPost {
			middleware.RequireAdmin(handlers.ApproveSubmission)(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/reject") && r.Method == http.MethodPost {
			middleware.RequireAdmin(handlers.RejectSubmission)(w, r)
			return
		}
		http.Error(w, "method not allowed", 405)
	})

	// Admin panel (mobile UI)
	mux.HandleFunc("/admin", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "admin/index.html")
	})
	mux.HandleFunc("/admin/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "admin/index.html")
	})
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/admin", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	}))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	addr := ":" + port
	log.Printf("Div Store API (Go) → http://0.0.0.0%s", addr)
	log.Printf("Admin panel → /admin")
	if err := http.ListenAndServe(addr, middleware.CORS(mux)); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}
