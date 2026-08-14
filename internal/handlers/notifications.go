package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/husdainshah2-web/div-store/internal/ghdb"
)

// GET /api/notifications — public list (newest first)
func ListNotifications(w http.ResponseWriter, r *http.Request) {
	rows, err := db().ReadAll(r.Context(), "notifications")
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
		rows[i], rows[j] = rows[j], rows[i]
	}
	writeJSON(w, 200, rows)
}

// POST /api/admin/notifications — title, message, imageUrl (admin key)
func AdminCreateNotification(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title    string `json:"title"`
		Message  string `json:"message"`
		Body     string `json:"body"`
		ImageUrl string `json:"imageUrl"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	msg := strings.TrimSpace(body.Message)
	if msg == "" {
		msg = strings.TrimSpace(body.Body)
	}
	title := strings.TrimSpace(body.Title)
	if title == "" || msg == "" {
		writeErr(w, 400, "title and message required")
		return
	}
	idStr, idNum, err := db().NextTypedID(r.Context(), "notifications", "ntf_")
	if err != nil {
		n, e2 := db().NextID(r.Context(), "notifications")
		if e2 != nil {
			writeErr(w, 500, err.Error())
			return
		}
		idStr = ghdb.FormatID("ntf_", n)
		idNum = n
	}
	now := time.Now().UTC().Format(time.RFC3339)
	row := map[string]any{
		"id": idStr, "idNum": idNum, "type": "admin_broadcast",
		"title": title, "message": msg, "imageUrl": strings.TrimSpace(body.ImageUrl),
		"createdAt": now,
	}
	if err := db().UpsertByStringID(r.Context(), "notifications", idStr, row); err != nil {
		rows, _ := db().ReadAll(r.Context(), "notifications")
		rows = append(rows, row)
		if err2 := db().WriteAll(r.Context(), "notifications", rows, "notify "+idStr); err2 != nil {
			writeErr(w, 500, err2.Error())
			return
		}
	}
	writeJSON(w, 201, row)
}

// pushStoreNotification — Phase 14: approve/reject → Alerts feed
func pushStoreNotification(ctx context.Context, title, message, nType, audienceEmail string) {
	g := db()
	if g == nil || !g.Enabled() {
		return
	}
	idStr, idNum, err := g.NextTypedID(ctx, "notifications", "ntf_")
	if err != nil {
		n, e2 := g.NextID(ctx, "notifications")
		if e2 != nil {
			return
		}
		idStr = ghdb.FormatID("ntf_", n)
		idNum = n
	}
	now := time.Now().UTC().Format(time.RFC3339)
	row := map[string]any{
		"id": idStr, "idNum": idNum, "type": nType,
		"title": title, "message": message, "body": message,
		"audienceEmail": audienceEmail,
		"createdAt":     now,
	}
	if err := g.UpsertByStringID(ctx, "notifications", idStr, row); err != nil {
		rows, _ := g.ReadAll(ctx, "notifications")
		rows = append(rows, row)
		_ = g.WriteAll(ctx, "notifications", rows, "notify "+idStr)
	}
}
