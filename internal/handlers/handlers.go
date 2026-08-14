package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/husdainshah2-web/div-store/internal/config"
	"github.com/husdainshah2-web/div-store/internal/ghdb"
	"github.com/husdainshah2-web/div-store/internal/storage"
)

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
func db() *ghdb.Store { return ghdb.Global() }

func Health(w http.ResponseWriter, r *http.Request) {
	st := storage.NewFromEnv()
	g := db()
	writeJSON(w, 200, map[string]any{
		"status":  "ok",
		"name":    "Div Store API",
		"version": "2.1.0-typed-ids",
		"engine":  "go",
		"database": map[string]any{
			"engine": "github",
			"status": func() map[string]any {
				if g == nil {
					return map[string]any{"ok": false, "error": "not init"}
				}
				return g.Status()
			}(),
		},
		"repos": config.Layout(),
		"apkStorage": map[string]any{
			"enabled": st.Token != "", "owner": st.Owner, "prefix": st.Prefix, "maxGB": 3,
			"purpose": "apk_only",
		},
		"time": time.Now().UTC().Format(time.RFC3339),
	})
}



type nilCtx struct{}

func (nilCtx) Deadline() (time.Time, bool) { return time.Time{}, false }
func (nilCtx) Done() <-chan struct{}       { return nil }
func (nilCtx) Err() error                  { return nil }
func (nilCtx) Value(any) any               { return nil }

func mapApp(d map[string]any, names map[int64]string) map[string]any {
	id := d["id"]
	if id == nil {
		id = d["apkId"]
	}
	cid := ghdb.ToInt(d["categoryId"])
	return map[string]any{
		"id": id, "apkId": id, "type": "apk", "name": d["name"], "packageName": d["packageName"],
		"description": d["description"], "categoryId": d["categoryId"], "categoryName": names[cid],
		"version": d["version"], "size": d["size"], "iconUrl": d["iconUrl"],
		"screenshotUrls": d["screenshotUrls"], "downloadUrl": d["downloadUrl"],
		"developerSlug": d["developerSlug"], "developerLogoUrl": d["developerLogoUrl"],
		"developerDescription": d["developerDescription"], "scanStatus": d["scanStatus"],
		"scanReport": d["scanReport"], "downloads": ghdb.ToInt(d["downloads"]),
		"rating": d["rating"], "reviewCount": ghdb.ToInt(d["reviewCount"]),
		"isFeatured": asBool(d["isFeatured"]), "isActive": d["isActive"] != false,
		"developer": d["developer"], "createdAt": d["createdAt"],
	}
}

func ListApps(w http.ResponseWriter, r *http.Request) {
	g := db()
	if g == nil || !g.Enabled() {
		writeErr(w, 503, "GitHub database not configured (GITHUB_STORAGE_TOKEN)")
		return
	}
	rows, err := g.ReadAll(r.Context(), "apps")
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	names := map[int64]string{}
	if cats, err := g.ReadAll(r.Context(), "categories"); err == nil {
		for _, c := range cats {
			names[ghdb.ToInt(c["id"])] = asString(c["name"])
		}
	}
	q := r.URL.Query()
	search := strings.ToLower(q.Get("search"))
	category := q.Get("category")
	featured := q.Get("featured")
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	if offset < 0 {
		offset = 0
	}
	var catID int64 = -1
	if category != "" {
		for id, n := range names {
			if n == category {
				catID = id
				break
			}
		}
	}
	out := make([]map[string]any, 0)
	for _, d := range rows {
		if d["isActive"] == false {
			continue
		}
		if catID >= 0 && ghdb.ToInt(d["categoryId"]) != catID {
			continue
		}
		if featured == "true" && !asBool(d["isFeatured"]) {
			continue
		}
		if search != "" {
			n := strings.ToLower(asString(d["name"]))
			p := strings.ToLower(asString(d["packageName"]))
			if !strings.Contains(n, search) && !strings.Contains(p, search) {
				continue
			}
		}
		out = append(out, mapApp(d, names))
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if ghdb.ToInt(out[j]["downloads"]) > ghdb.ToInt(out[i]["downloads"]) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	if offset > len(out) {
		offset = len(out)
	}
	end := offset + limit
	if end > len(out) {
		end = len(out)
	}
	writeJSON(w, 200, out[offset:end])
}

func GetApp(w http.ResponseWriter, r *http.Request) {
	idStr := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/apps/"), "/")[0]
	g := db()
	if g == nil {
		writeErr(w, 503, "GitHub database not configured")
		return
	}
	row, err := g.GetByStringID(r.Context(), "apps", idStr)
	if err != nil {
		writeErr(w, 404, "App not found.")
		return
	}
	names := map[int64]string{}
	if cats, e := g.ReadAll(r.Context(), "categories"); e == nil {
		for _, c := range cats {
			names[ghdb.ToInt(c["id"])] = asString(c["name"])
		}
	}
	writeJSON(w, 200, mapApp(row, names))
}

func ListCategories(w http.ResponseWriter, r *http.Request) {
	g := db()
	if g == nil || !g.Enabled() {
		writeErr(w, 503, "GitHub database not configured")
		return
	}
	cats, err := g.ReadAll(r.Context(), "categories")
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	apps, _ := g.ReadAll(r.Context(), "apps")
	counts := map[int64]int{}
	for _, a := range apps {
		counts[ghdb.ToInt(a["categoryId"])]++
	}
	out := make([]map[string]any, 0, len(cats))
	for _, c := range cats {
		id := ghdb.ToInt(c["id"])
		out = append(out, map[string]any{
			"id": id, "name": c["name"], "icon": c["icon"],
			"createdAt": c["createdAt"], "appCount": counts[id],
		})
	}
	writeJSON(w, 200, out)
}

func CreateCategory(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
		Icon string `json:"icon"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		writeErr(w, 400, "Invalid request body.")
		return
	}
	if body.Icon == "" {
		body.Icon = "Package"
	}
	g := db()
	idStr, idNum, err := g.NextTypedID(r.Context(), "categories", ghdb.PrefixCat)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	row := map[string]any{
		"id": idStr, "idNum": idNum, "name": body.Name, "icon": body.Icon,
		"type": "category", "createdAt": time.Now().UTC().Format(time.RFC3339),
	}
	if err := g.UpsertByStringID(r.Context(), "categories", idStr, row); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	row["appCount"] = 0
	writeJSON(w, 201, row)
}

func DeleteCategory(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/categories/")
	if err := db().DeleteByStringID(r.Context(), "categories", idStr); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	w.WriteHeader(204)
}

func ListReviews(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 4 {
		writeErr(w, 400, "Invalid path.")
		return
	}
	rows, err := db().ReadAll(r.Context(), "reviews")
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	appKey := parts[2]
	out := make([]map[string]any, 0)
	for _, d := range rows {
		if ghdb.IDEquals(d["appId"], appKey) || fmt.Sprint(d["appId"]) == appKey {
			out = append(out, d)
		}
	}
	writeJSON(w, 200, out)
}

func CreateReview(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 4 {
		writeErr(w, 400, "Invalid path.")
		return
	}
	var body struct {
		ReviewerName string `json:"reviewerName"`
		Name         string `json:"name"`
		Rating       int64  `json:"rating"`
		Comment      string `json:"comment"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	name := body.ReviewerName
	if name == "" {
		name = body.Name
	}
	if name == "" {
		name = "Anonymous"
	}
	if body.Rating < 1 || body.Rating > 5 || strings.TrimSpace(body.Comment) == "" {
		writeErr(w, 400, "Rating 1-5 and comment required.")
		return
	}
	idStr, idNum, err := db().NextTypedID(r.Context(), "reviews", ghdb.PrefixReview)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	row := map[string]any{
		"id": idStr, "idNum": idNum, "appId": parts[2], "reviewerName": name,
		"rating": body.Rating, "comment": body.Comment, "type": "review",
		"createdAt": time.Now().UTC().Format(time.RFC3339),
	}
	if err := db().UpsertByStringID(r.Context(), "reviews", idStr, row); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, row)
}

func DownloadApp(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	idStr := parts[2]
	row, err := db().GetByStringID(r.Context(), "apps", idStr)
	if err != nil {
		writeErr(w, 404, "App not found.")
		return
	}
	row["downloads"] = ghdb.ToInt(row["downloads"]) + 1
	_ = db().UpsertByStringID(r.Context(), "apps", idStr, row)
	writeJSON(w, 200, map[string]any{
		"ok": true, "apkId": idStr, "downloadUrl": row["downloadUrl"], "source": "github-release",
	})
}

func AdminStats(w http.ResponseWriter, r *http.Request) {
	g := db()
	if g == nil || !g.Enabled() {
		// still allow login check — zeros + note via health
		writeJSON(w, 200, map[string]any{
			"apps": 0, "categories": 0, "reviews": 0, "submissions": 0,
			"pendingSubmissions": 0, "totalDownloads": 0, "featuredApps": 0,
			"warning": "GITHUB_STORAGE_TOKEN not set on server",
		})
		return
	}
	apps, err1 := g.ReadAll(r.Context(), "apps")
	cats, err2 := g.ReadAll(r.Context(), "categories")
	revs, _ := g.ReadAll(r.Context(), "reviews")
	subs, _ := g.ReadAll(r.Context(), "submissions")
	_ = err1
	_ = err2
	var dl int64
	var feat int
	pending := 0
	for _, a := range apps {
		dl += ghdb.ToInt(a["downloads"])
		if asBool(a["isFeatured"]) {
			feat++
		}
	}
	for _, s := range subs {
		if asString(s["status"]) == "pending" {
			pending++
		}
	}
	writeJSON(w, 200, map[string]any{
		"apps": len(apps), "categories": len(cats), "reviews": len(revs),
		"submissions": len(subs), "pendingSubmissions": pending,
		"totalDownloads": dl, "featuredApps": feat,
	})
}

func AdminListApps(w http.ResponseWriter, r *http.Request) {
	rows, err := db().ReadAll(r.Context(), "apps")
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	names := map[int64]string{}
	if cats, e := db().ReadAll(r.Context(), "categories"); e == nil {
		for _, c := range cats {
			names[ghdb.ToInt(c["id"])] = asString(c["name"])
		}
	}
	out := make([]map[string]any, 0, len(rows))
	for _, d := range rows {
		out = append(out, mapApp(d, names))
	}
	writeJSON(w, 200, out)
}

func AdminCreateApp(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "Invalid body.")
		return
	}
	for _, k := range []string{"name", "packageName", "description", "iconUrl"} {
		if asString(body[k]) == "" {
			writeErr(w, 400, "name, packageName, description, categoryId, iconUrl required.")
			return
		}
	}
	idStr, idNum, err := db().NextTypedID(r.Context(), "apps", ghdb.PrefixAPK)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	row := map[string]any{
		"id": idStr, "apkId": idStr, "idNum": idNum, "type": "apk",
		"name": body["name"], "packageName": body["packageName"],
		"description": body["description"], "categoryId": body["categoryId"],
		"version": strOr(body["version"], "1.0.0"), "size": strOr(body["size"], "10 MB"),
		"iconUrl": body["iconUrl"], "screenshotUrls": body["screenshotUrls"],
		"downloadUrl": body["downloadUrl"], "developerSlug": body["developerSlug"],
		"developerLogoUrl": strOr(body["developerLogoUrl"], ""),
		"developerDescription": strOr(body["developerDescription"], ""),
		"scanStatus": strOr(body["scanStatus"], "pending"),
		"downloads": int64(0), "rating": float64(0), "reviewCount": int64(0),
		"isFeatured": asBool(body["isFeatured"]), "isActive": true,
		"developer": body["developer"], "createdAt": time.Now().UTC().Format(time.RFC3339),
	}
	if row["screenshotUrls"] == nil {
		row["screenshotUrls"] = []any{}
	}
	if err := db().UpsertByStringID(r.Context(), "apps", idStr, row); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, row)
}

func AdminUpdateApp(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/admin/apps/")
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	row, err := db().GetByStringID(r.Context(), "apps", idStr)
	if err != nil {
		writeErr(w, 404, "App not found.")
		return
	}
	for k, v := range body {
		if k == "id" || k == "apkId" || k == "type" {
			continue
		}
		row[k] = v
	}
	if err := db().UpsertByStringID(r.Context(), "apps", idStr, row); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, row)
}

func AdminDeleteApp(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/admin/apps/")
	if err := db().DeleteByStringID(r.Context(), "apps", idStr); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	w.WriteHeader(204)
}

func ListSubmissions(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	rows, err := db().ReadAll(r.Context(), "submissions")
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	out := make([]map[string]any, 0)
	for _, d := range rows {
		if status != "" && asString(d["status"]) != status {
			continue
		}
		out = append(out, d)
	}
	writeJSON(w, 200, out)
}

func SubmitApp(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "Invalid body.")
		return
	}
	appName := asString(body["appName"])
	if appName == "" {
		appName = asString(body["name"])
	}
	devName := asString(body["developerName"])
	if devName == "" {
		devName = asString(body["developer"])
	}
	email := asString(body["contactEmail"])
	if email == "" {
		email = asString(body["email"])
	}
	pkg := asString(body["packageName"])
	desc := asString(body["description"])
	icon := asString(body["iconUrl"])
	apk := asString(body["apkUrl"])
	if apk == "" {
		apk = asString(body["downloadUrl"])
	}
	if appName == "" || devName == "" || email == "" || pkg == "" || desc == "" || icon == "" || apk == "" {
		writeErr(w, 400, "Missing required fields.")
		return
	}
	slug := asString(body["developerSlug"])
	if slug == "" {
		slug = strings.ToLower(strings.ReplaceAll(devName, " ", "-"))
	}
	id, err := db().NextID(r.Context(), "submissions")
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	row := map[string]any{
		"id": id, "appName": appName, "developerName": devName, "developerSlug": slug,
		"developerLogoUrl": strOr(body["developerLogoUrl"], ""),
		"developerDescription": strOr(body["developerDescription"], ""),
		"contactEmail": email, "description": desc, "packageName": pkg,
		"version": strOr(body["version"], "1.0.0"), "size": strOr(body["size"], "Unknown"),
		"categoryName": strOr(body["categoryName"], strOr(body["category"], "Other")),
		"iconUrl": icon, "apkUrl": apk, "screenshotUrls": body["screenshotUrls"],
		"scanStatus": "pending", "status": "pending",
		"submittedAt": time.Now().UTC().Format(time.RFC3339),
	}
	if row["screenshotUrls"] == nil {
		row["screenshotUrls"] = []any{}
	}
	if err := db().UpsertByID(r.Context(), "submissions", id, row); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, row)
}

func ApproveSubmission(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/admin/submissions/")
	idStr = strings.TrimSuffix(idStr, "/approve")
	sub, err := db().GetByStringID(r.Context(), "submissions", idStr)
	if err != nil {
		writeErr(w, 404, "Not found.")
		return
	}
	catName := asString(sub["categoryName"])
	if catName == "" {
		catName = "Other"
	}
	catNames := []string{}
	if arr, ok := sub["categoryNames"].([]any); ok {
		for _, v := range arr {
			if s := asString(v); s != "" {
				catNames = append(catNames, s)
			}
		}
	}
	if len(catNames) == 0 {
		catNames = []string{catName}
	}
	if len(catNames) > 4 {
		catNames = catNames[:4]
	}
	cats, _ := db().ReadAll(r.Context(), "categories")
	var catID int64
	var catIDs []int64
	for _, n := range catNames {
		found := false
		for _, c := range cats {
			if asString(c["name"]) == n {
				id := ghdb.ToInt(c["id"])
				catIDs = append(catIDs, id)
				if catID == 0 {
					catID = id
				}
				found = true
				break
			}
		}
		if !found {
			nid, _ := db().NextID(r.Context(), "categories")
			_ = db().UpsertByID(r.Context(), "categories", nid, map[string]any{
				"id": nid, "name": n, "icon": "Package",
				"createdAt": time.Now().UTC().Format(time.RFC3339),
			})
			catIDs = append(catIDs, nid)
			if catID == 0 {
				catID = nid
			}
		}
	}
	apkID, apkNum, _ := db().NextTypedID(r.Context(), "apps", ghdb.PrefixAPK)
	dl := sub["apkUrl"]
	if asString(dl) == "" {
		dl = sub["downloadUrl"]
	}
	app := map[string]any{
		"id": apkID, "apkId": apkID, "idNum": apkNum, "type": "apk",
		"name": sub["appName"], "packageName": sub["packageName"],
		"description": sub["description"], "categoryId": catID, "categoryIds": catIDs,
		"categoryNames": catNames, "version": sub["version"], "size": sub["size"],
		"iconUrl": sub["iconUrl"], "screenshotUrls": sub["screenshotUrls"],
		"downloadUrl": dl, "developerSlug": sub["developerSlug"], "scanStatus": "safe",
		"downloads": int64(0), "rating": float64(0), "reviewCount": int64(0),
		"isFeatured": false, "isActive": true, "developer": sub["developerName"],
		"fromRequestId": idStr, "createdAt": time.Now().UTC().Format(time.RFC3339),
	}
	_ = db().UpsertByStringID(r.Context(), "apps", apkID, app)
	sub["status"] = "approved"
	sub["reviewedAt"] = time.Now().UTC().Format(time.RFC3339)
	sub["approvedApkId"] = apkID
	_ = db().UpsertByStringID(r.Context(), "submissions", idStr, sub)
	appName := asString(sub["appName"])
	devEmail := asString(sub["contactEmail"])
	pushStoreNotification(r.Context(),
		"New app approved",
		appName+" is now live on DIV STORE.",
		"app_approved",
		devEmail,
	)
	writeJSON(w, 200, map[string]any{
		"submission": "approved", "requestId": idStr, "apkId": apkID, "app": app,
	})
}

func RejectSubmission(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/admin/submissions/")
	idStr = strings.TrimSuffix(idStr, "/reject")
	var body struct {
		Note string `json:"note"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	sub, err := db().GetByStringID(r.Context(), "submissions", idStr)
	if err != nil {
		writeErr(w, 404, "Not found.")
		return
	}
	sub["status"] = "rejected"
	sub["reviewNote"] = body.Note
	sub["reviewedAt"] = time.Now().UTC().Format(time.RFC3339)
	_ = db().UpsertByStringID(r.Context(), "submissions", idStr, sub)
	appName := asString(sub["appName"])
	devEmail := asString(sub["contactEmail"])
	note := strings.TrimSpace(body.Note)
	msg := appName + " was not approved."
	if note != "" {
		msg = msg + " Note: " + note
	}
	pushStoreNotification(r.Context(),
		"Submission update",
		msg,
		"app_rejected",
		devEmail,
	)
	writeJSON(w, 200, map[string]any{"ok": true, "requestId": idStr, "status": "rejected"})
}

func ListSettings(w http.ResponseWriter, r *http.Request) {
	rows, err := db().ReadAll(r.Context(), "settings")
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, rows)
}

func SetSetting(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/api/admin/settings/")
	var body struct {
		Value string `json:"value"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	rows, _ := db().ReadAll(r.Context(), "settings")
	found := false
	for i := range rows {
		if asString(rows[i]["key"]) == key {
			rows[i]["value"] = body.Value
			rows[i]["updatedAt"] = time.Now().UTC().Format(time.RFC3339)
			found = true
			break
		}
	}
	if !found {
		rows = append(rows, map[string]any{
			"key": key, "value": body.Value,
			"updatedAt": time.Now().UTC().Format(time.RFC3339),
		})
	}
	if err := db().WriteAll(r.Context(), "settings", rows, "setting "+key); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"key": key, "value": body.Value})
}

func ListDevelopers(w http.ResponseWriter, r *http.Request) {
	rows, err := db().ReadAll(r.Context(), "developer_profiles")
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, rows)
}

func GetDeveloper(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimPrefix(r.URL.Path, "/api/developers/")
	rows, err := db().ReadAll(r.Context(), "developer_profiles")
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	for _, d := range rows {
		if asString(d["slug"]) == slug {
			apps, _ := db().ReadAll(r.Context(), "apps")
			mine := make([]map[string]any, 0)
			for _, a := range apps {
				if asString(a["developerSlug"]) == slug {
					mine = append(mine, a)
				}
			}
			d["apps"] = mine
			writeJSON(w, 200, d)
			return
		}
	}
	writeErr(w, 404, "Developer not found.")
}

func UpsertDeveloper(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	slug := strings.TrimSpace(asString(body["slug"]))
	name := strings.TrimSpace(asString(body["name"]))
	if slug == "" || name == "" {
		writeErr(w, 400, "slug and name required.")
		return
	}
	rows, _ := db().ReadAll(r.Context(), "developer_profiles")
	now := time.Now().UTC().Format(time.RFC3339)
	body["slug"] = slug
	body["name"] = name
	body["updatedAt"] = now
	found := false
	for i := range rows {
		if asString(rows[i]["slug"]) == slug {
			if rows[i]["createdAt"] == nil {
				body["createdAt"] = now
			} else {
				body["createdAt"] = rows[i]["createdAt"]
			}
			rows[i] = body
			found = true
			break
		}
	}
	if !found {
		body["createdAt"] = now
		rows = append(rows, body)
	}
	if err := db().WriteAll(r.Context(), "developer_profiles", rows, "developer "+slug); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, body)
}

func StorageStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"database": db().Status(),
		"apk":      storage.NewFromEnv().Status(),
	})
}

func SyncDataNow(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"ok": true, "message": "Phase 2: data already lives on GitHub (db/*.json). No separate push needed.",
		"database": db().Status(),
	})
}

func UploadAPK(w http.ResponseWriter, r *http.Request) {
	c := storage.NewFromEnv()
	if c.Token == "" {
		writeErr(w, 503, "GITHUB_STORAGE_TOKEN required")
		return
	}
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeErr(w, 400, "Invalid multipart form")
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		writeErr(w, 400, "file required")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		writeErr(w, 500, "read failed")
		return
	}
	assetName := r.FormValue("name")
	if assetName == "" {
		assetName = hdr.Filename
	}
	if assetName == "" {
		assetName = "app.apk"
	}
	assetName = filepath.Base(assetName)
	tag := r.FormValue("tag")
	if tag == "" {
		tag = "apk-" + time.Now().UTC().Format("20060102-150405")
	}
	repo, err := c.PickRepo(int64(len(data)))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	url, size, err := c.UploadAPK(repo.Name, tag, assetName, data)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	appID := ghdb.ToInt(r.FormValue("appId"))
	if appID > 0 {
		if row, err := db().GetByID(r.Context(), "apps", appID); err == nil {
			row["downloadUrl"] = url
			row["apkRepo"] = repo.FullName
			row["apkTag"] = tag
			_ = db().UpsertByID(r.Context(), "apps", appID, row)
		}
	}
	writeJSON(w, 201, map[string]any{
		"ok": true, "repo": repo.FullName, "tag": tag, "asset": assetName,
		"size": size, "downloadUrl": url, "appId": appID,
	})
}

func asBool(v any) bool { b, _ := v.(bool); return b }
func asString(v any) string {
	s, _ := v.(string)
	return s
}
func strOr(v any, def string) string {
	s := asString(v)
	if s == "" {
		return def
	}
	return s
}

// silence unused
var _ = fmt.Sprintf
