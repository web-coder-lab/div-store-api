package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/husdainshah2-web/div-store/internal/firebase"
	"google.golang.org/api/iterator"
)

func ListDevelopers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	it := firebase.Client().Collection("developer_profiles").Documents(ctx)
	var rows []map[string]any
	for {
		doc, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			writeErr(w, 500, "Internal server error.")
			return
		}
		d := doc.Data()
		d["slug"] = strOr(d["slug"], doc.Ref.ID)
		rows = append(rows, d)
	}
	writeJSON(w, 200, rows)
}

func GetDeveloper(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimPrefix(r.URL.Path, "/api/developers/")
	slug = strings.Split(slug, "/")[0]
	if slug == "" {
		writeErr(w, 400, "Invalid slug.")
		return
	}
	ctx := r.Context()
	doc, err := firebase.Client().Collection("developer_profiles").Doc(slug).Get(ctx)
	if err != nil || !doc.Exists() {
		writeErr(w, 404, "Developer not found.")
		return
	}
	d := doc.Data()
	d["slug"] = slug
	// attach apps
	catNames := loadCatNames(ctx)
	var apps []map[string]any
	it := firebase.Client().Collection("apps").Documents(ctx)
	for {
		adoc, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		ad := adoc.Data()
		if asString(ad["developerSlug"]) == slug {
			apps = append(apps, mapApp(ad, catNames))
		}
	}
	d["apps"] = apps
	writeJSON(w, 200, d)
}

func UpsertDeveloper(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "Invalid body.")
		return
	}
	slug := strings.TrimSpace(asString(body["slug"]))
	name := strings.TrimSpace(asString(body["name"]))
	if slug == "" || name == "" {
		writeErr(w, 400, "slug and name required.")
		return
	}
	ctx := r.Context()
	now := time.Now().UTC().Format(time.RFC3339)
	ref := firebase.Client().Collection("developer_profiles").Doc(slug)
	existing, _ := ref.Get(ctx)
	created := now
	if existing != nil && existing.Exists() {
		created = toISO(existing.Data()["createdAt"])
	}
	row := map[string]any{
		"slug": slug, "name": name,
		"logoUrl":        strOr(body["logoUrl"], ""),
		"description":    strOr(body["description"], ""),
		"website":        body["website"],
		"contactEmail":   body["contactEmail"],
		"verified":       asBool(body["verified"]),
		"createdAt":      created,
		"updatedAt":      now,
	}
	_, err := ref.Set(ctx, row, firestore.MergeAll)
	if err != nil {
		writeErr(w, 500, "Internal server error.")
		return
	}
	writeJSON(w, 201, row)
}
