package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/husdainshah2-web/div-store/internal/ghdb"
	"github.com/husdainshah2-web/div-store/internal/handlers"
	"github.com/husdainshah2-web/div-store/internal/middleware"
	"github.com/husdainshah2-web/div-store/internal/storage"
)

func main() {
	store := ghdb.NewFromEnv()
	ghdb.SetGlobal(store)
	if !store.Enabled() {
		log.Printf("[ghdb] WARNING: GITHUB_STORAGE_TOKEN missing — database unavailable")
	} else {
		log.Printf("[ghdb] GitHub DB → %s/%s (db/*.json)", store.Owner, store.Repo)
	}
	storage.SetGlobalAPK(storage.NewFromEnv())

	mux := http.NewServeMux()

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
	mux.HandleFunc("/api/developers", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			handlers.ListDevelopers(w, r)
		case http.MethodPost:
			middleware.RequireAdmin(handlers.UpsertDeveloper)(w, r)
		default:
			http.Error(w, "method not allowed", 405)
		}
	})
	mux.HandleFunc("/api/developers/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			handlers.GetDeveloper(w, r)
			return
		}
		http.Error(w, "method not allowed", 405)
	})
	mux.HandleFunc("/api/admin/settings", middleware.RequireAdmin(handlers.ListSettings))
	mux.HandleFunc("/api/admin/settings/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut || r.Method == http.MethodPost {
			middleware.RequireAdmin(handlers.SetSetting)(w, r)
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
	mux.HandleFunc("/api/admin/storage", middleware.RequireAdmin(handlers.StorageStatus))
	mux.HandleFunc("/api/admin/sync-data", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", 405)
			return
		}
		middleware.RequireAdmin(handlers.SyncDataNow)(w, r)
	})
	mux.HandleFunc("/api/admin/upload-apk", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", 405)
			return
		}
		middleware.RequireAdmin(handlers.UploadAPK)(w, r)
	})
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

	mux.HandleFunc("/terms", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "admin/terms.html")
	})
	mux.HandleFunc("/privacy", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "admin/privacy.html")
	})
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
	log.Printf("Admin → /admin · Terms → /terms · Privacy → /privacy")

	handler := middleware.Chain(mux,
		middleware.CORS,
		middleware.SecurityHeaders,
		middleware.RateLimit,
		middleware.MaxBody(32<<20),
	)
	if err := http.ListenAndServe(addr, handler); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}
