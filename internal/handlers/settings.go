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

func ListSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	it := firebase.Client().Collection("settings").Documents(ctx)
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
		rows = append(rows, map[string]any{
			"key":       strOr(d["key"], doc.Ref.ID),
			"value":     d["value"],
			"updatedAt": toISO(d["updatedAt"]),
		})
	}
	writeJSON(w, 200, rows)
}

func SetSetting(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/api/admin/settings/")
	key = strings.Split(key, "/")[0]
	if key == "" {
		writeErr(w, 400, "Invalid key.")
		return
	}
	var body struct {
		Value string `json:"value"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	ctx := r.Context()
	_, err := firebase.Client().Collection("settings").Doc(key).Set(ctx, map[string]any{
		"key": key, "value": body.Value,
		"updatedAt": time.Now().UTC().Format(time.RFC3339),
	}, firestore.MergeAll)
	if err != nil {
		writeErr(w, 500, "Internal server error.")
		return
	}
	writeJSON(w, 200, map[string]any{"key": key, "value": body.Value})
}
