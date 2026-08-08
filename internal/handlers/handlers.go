package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/husdainshah2-web/div-store/internal/firebase"
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

func toISO(v any) string {
	switch t := v.(type) {
	case time.Time:
		return t.UTC().Format(time.RFC3339)
	case string:
		if t != "" {
			return t
		}
	}
	return time.Now().UTC().Format(time.RFC3339)
}

func Health(w http.ResponseWriter, r *http.Request) {
	st := storage.NewFromEnv()
	writeJSON(w, 200, map[string]any{
		"status":   "ok",
		"name":     "Div Store API",
		"version":  "1.0.2-go",
		"engine":   "go",
		"firebase": firebase.Status(),
		"storage": map[string]any{
			"enabled": st.Token != "",
			"owner":   st.Owner,
			"prefix":  st.Prefix,
			"maxGB":   3,
			"dataRepo": "div-store-data",
			"apkRepos": []string{"div-store-apks-01", "div-store-apks-02"},
			"syncEvery": "3m",
		},
		"time": time.Now().UTC().Format(time.RFC3339),
	})
}

func ListApps(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			writeErr(w, 500, fmt.Sprintf("panic: %v", rec))
		}
	}()
	ctx := r.Context()
	db := firebase.Client()
	if db == nil {
		writeErr(w, 503, "Firebase not ready")
		return
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

	catNames, err := loadCatNames(ctx)
	if err != nil {
		writeErr(w, 500, "categories: "+err.Error())
		return
	}

	var catID int64 = -1
	if category != "" {
		for id, n := range catNames {
			if n == category {
				catID = id
				break
			}
		}
	}

	docs, err := db.Collection("apps").Documents(ctx).GetAll()
	if err != nil {
		writeErr(w, 500, "apps: "+err.Error())
		return
	}

	rows := make([]map[string]any, 0)
	for _, doc := range docs {
		d := doc.Data()
		app := mapApp(d, catNames)
		if v, ok := d["isActive"]; ok && v == false {
			continue
		}
		if catID >= 0 && asInt(d["categoryId"]) != catID {
			continue
		}
		if featured == "true" && !asBool(d["isFeatured"]) {
			continue
		}
		if featured == "false" && asBool(d["isFeatured"]) {
			continue
		}
		if search != "" {
			name := strings.ToLower(asString(d["name"]))
			pkg := strings.ToLower(asString(d["packageName"]))
			if !strings.Contains(name, search) && !strings.Contains(pkg, search) {
				continue
			}
		}
		rows = append(rows, app)
	}
	for i := 0; i < len(rows); i++ {
		for j := i + 1; j < len(rows); j++ {
			if asInt(rows[j]["downloads"]) > asInt(rows[i]["downloads"]) {
				rows[i], rows[j] = rows[j], rows[i]
			}
		}
	}
	if offset > len(rows) {
		offset = len(rows)
	}
	end := offset + limit
	if end > len(rows) {
		end = len(rows)
	}
	writeJSON(w, 200, rows[offset:end])
}

func GetApp(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/apps/")
	idStr = strings.Split(idStr, "/")[0]
	id, _ := strconv.ParseInt(idStr, 10, 64)
	if id <= 0 {
		writeErr(w, 400, "Invalid id.")
		return
	}
	ctx := r.Context()
	db := firebase.Client()
	if db == nil {
		writeErr(w, 503, "Firebase not ready")
		return
	}
	doc, err := db.Collection("apps").Doc(strconv.FormatInt(id, 10)).Get(ctx)
	if err != nil || !doc.Exists() {
		writeErr(w, 404, "App not found.")
		return
	}
	catNames, _ := loadCatNames(ctx)
	writeJSON(w, 200, mapApp(doc.Data(), catNames))
}

func ListCategories(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			writeErr(w, 500, fmt.Sprintf("panic: %v", rec))
		}
	}()
	ctx := r.Context()
	db := firebase.Client()
	if db == nil {
		writeErr(w, 503, "Firebase not ready")
		return
	}
	appDocs, err := db.Collection("apps").Documents(ctx).GetAll()
	if err != nil {
		writeErr(w, 500, "apps count: "+err.Error())
		return
	}
	counts := map[int64]int{}
	for _, doc := range appDocs {
		cid := asInt(doc.Data()["categoryId"])
		counts[cid]++
	}
	catDocs, err := db.Collection("categories").Documents(ctx).GetAll()
	if err != nil {
		writeErr(w, 500, "categories: "+err.Error())
		return
	}
	rows := make([]map[string]any, 0)
	for _, doc := range catDocs {
		d := doc.Data()
		id := asInt(d["id"])
		if id == 0 {
			if n, err := strconv.ParseInt(doc.Ref.ID, 10, 64); err == nil {
				id = n
			}
		}
		rows = append(rows, map[string]any{
			"id": id, "name": d["name"], "icon": strOr(d["icon"], "Package"),
			"createdAt": toISO(d["createdAt"]), "appCount": counts[id],
		})
	}
	writeJSON(w, 200, rows)
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
	ctx := r.Context()
	id, err := firebase.NextID(ctx, "categories")
	if err != nil {
		writeErr(w, 500, "counter: "+err.Error())
		return
	}
	row := map[string]any{
		"id": id, "name": body.Name, "icon": body.Icon,
		"createdAt": time.Now().UTC().Format(time.RFC3339),
	}
	_, err = firebase.Client().Collection("categories").Doc(strconv.FormatInt(id, 10)).Set(ctx, row)
	if err != nil {
		writeErr(w, 500, "write: "+err.Error())
		return
	}
	row["appCount"] = 0
	writeJSON(w, 201, row)
}

func DeleteCategory(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/categories/")
	id, _ := strconv.ParseInt(idStr, 10, 64)
	if id <= 0 {
		writeErr(w, 400, "Invalid id.")
		return
	}
	_, err := firebase.Client().Collection("categories").Doc(strconv.FormatInt(id, 10)).Delete(r.Context())
	if err != nil {
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
	id, _ := strconv.ParseInt(parts[2], 10, 64)
	ctx := r.Context()
	docs, err := firebase.Client().Collection("reviews").Where("appId", "==", id).Documents(ctx).GetAll()
	if err != nil {
		// fallback: scan all
		docs, err = firebase.Client().Collection("reviews").Documents(ctx).GetAll()
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
	}
	rows := make([]map[string]any, 0)
	for _, doc := range docs {
		d := doc.Data()
		if asInt(d["appId"]) != id {
			continue
		}
		rows = append(rows, map[string]any{
			"id": asInt(d["id"]), "appId": asInt(d["appId"]),
			"reviewerName": d["reviewerName"], "rating": asInt(d["rating"]),
			"comment": d["comment"], "createdAt": toISO(d["createdAt"]),
		})
	}
	writeJSON(w, 200, rows)
}

func CreateReview(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 4 {
		writeErr(w, 400, "Invalid path.")
		return
	}
	appID, _ := strconv.ParseInt(parts[2], 10, 64)
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
	ctx := r.Context()
	id, err := firebase.NextID(ctx, "reviews")
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	row := map[string]any{
		"id": id, "appId": appID, "reviewerName": name,
		"rating": body.Rating, "comment": body.Comment,
		"createdAt": time.Now().UTC().Format(time.RFC3339),
	}
	_, err = firebase.Client().Collection("reviews").Doc(strconv.FormatInt(id, 10)).Set(ctx, row)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, row)
}

func DownloadApp(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 4 {
		writeErr(w, 400, "Invalid path.")
		return
	}
	id, _ := strconv.ParseInt(parts[2], 10, 64)
	ctx := r.Context()
	ref := firebase.Client().Collection("apps").Doc(strconv.FormatInt(id, 10))
	doc, err := ref.Get(ctx)
	if err != nil || !doc.Exists() {
		writeErr(w, 404, "App not found.")
		return
	}
	_, _ = ref.Update(ctx, []firestore.Update{{Path: "downloads", Value: firestore.Increment(1)}})
	writeJSON(w, 200, map[string]any{"ok": true, "downloadUrl": doc.Data()["downloadUrl"]})
}

func AdminStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db := firebase.Client()
	if db == nil {
		writeErr(w, 503, "Firebase not ready")
		return
	}
	apps, _ := db.Collection("apps").Documents(ctx).GetAll()
	cats, _ := db.Collection("categories").Documents(ctx).GetAll()
	revs, _ := db.Collection("reviews").Documents(ctx).GetAll()
	subs, _ := db.Collection("submissions").Documents(ctx).GetAll()
	var downloads int64
	var featured int
	for _, d := range apps {
		data := d.Data()
		downloads += asInt(data["downloads"])
		if asBool(data["isFeatured"]) {
			featured++
		}
	}
	pending := 0
	for _, d := range subs {
		if asString(d.Data()["status"]) == "pending" {
			pending++
		}
	}
	writeJSON(w, 200, map[string]any{
		"apps": len(apps), "categories": len(cats), "reviews": len(revs),
		"submissions": len(subs), "pendingSubmissions": pending,
		"totalDownloads": downloads, "featuredApps": featured,
	})
}

func AdminListApps(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			writeErr(w, 500, fmt.Sprintf("panic: %v", rec))
		}
	}()
	ctx := r.Context()
	db := firebase.Client()
	if db == nil {
		writeErr(w, 503, "Firebase not ready")
		return
	}
	catNames, _ := loadCatNames(ctx)
	docs, err := db.Collection("apps").Documents(ctx).GetAll()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	rows := make([]map[string]any, 0, len(docs))
	for _, doc := range docs {
		rows = append(rows, mapApp(doc.Data(), catNames))
	}
	writeJSON(w, 200, rows)
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
	ctx := r.Context()
	id, err := firebase.NextID(ctx, "apps")
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	row := map[string]any{
		"id": id, "name": body["name"], "packageName": body["packageName"],
		"description": body["description"], "categoryId": asInt(body["categoryId"]),
		"version": strOr(body["version"], "1.0.0"), "size": strOr(body["size"], "10 MB"),
		"iconUrl": body["iconUrl"], "screenshotUrls": body["screenshotUrls"],
		"downloadUrl": body["downloadUrl"], "developerSlug": body["developerSlug"],
		"developerLogoUrl": strOr(body["developerLogoUrl"], ""),
		"developerDescription": strOr(body["developerDescription"], ""),
		"scanStatus": strOr(body["scanStatus"], "pending"), "scanReport": body["scanReport"],
		"downloads": int64(0), "rating": float64(0), "reviewCount": int64(0),
		"isFeatured": asBool(body["isFeatured"]), "isActive": true,
		"developer": body["developer"],
		"createdAt": time.Now().UTC().Format(time.RFC3339),
	}
	if row["screenshotUrls"] == nil {
		row["screenshotUrls"] = []string{}
	}
	_, err = firebase.Client().Collection("apps").Doc(strconv.FormatInt(id, 10)).Set(ctx, row)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	catNames, _ := loadCatNames(ctx)
	writeJSON(w, 201, mapApp(row, catNames))
}

func AdminUpdateApp(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/admin/apps/")
	id, _ := strconv.ParseInt(idStr, 10, 64)
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	ctx := r.Context()
	ref := firebase.Client().Collection("apps").Doc(strconv.FormatInt(id, 10))
	doc, err := ref.Get(ctx)
	if err != nil || !doc.Exists() {
		writeErr(w, 404, "App not found.")
		return
	}
	_, err = ref.Set(ctx, body, firestore.MergeAll)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	doc, _ = ref.Get(ctx)
	catNames, _ := loadCatNames(ctx)
	writeJSON(w, 200, mapApp(doc.Data(), catNames))
}

func AdminDeleteApp(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/admin/apps/")
	id, _ := strconv.ParseInt(idStr, 10, 64)
	_, err := firebase.Client().Collection("apps").Doc(strconv.FormatInt(id, 10)).Delete(r.Context())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	w.WriteHeader(204)
}

func ListSubmissions(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			writeErr(w, 500, fmt.Sprintf("panic: %v", rec))
		}
	}()
	status := r.URL.Query().Get("status")
	ctx := r.Context()
	db := firebase.Client()
	if db == nil {
		writeErr(w, 503, "Firebase not ready")
		return
	}
	docs, err := db.Collection("submissions").Documents(ctx).GetAll()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	rows := make([]map[string]any, 0)
	for _, doc := range docs {
		d := doc.Data()
		if status != "" && asString(d["status"]) != status {
			continue
		}
		d["id"] = asInt(d["id"])
		rows = append(rows, d)
	}
	writeJSON(w, 200, rows)
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
	ctx := r.Context()
	id, err := firebase.NextID(ctx, "submissions")
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
		"scanStatus": "pending", "scanReport": nil, "status": "pending",
		"reviewNote": nil, "submittedAt": time.Now().UTC().Format(time.RFC3339), "reviewedAt": nil,
	}
	if row["screenshotUrls"] == nil {
		row["screenshotUrls"] = []string{}
	}
	_, err = firebase.Client().Collection("submissions").Doc(strconv.FormatInt(id, 10)).Set(ctx, row)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, row)
}

func ApproveSubmission(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/admin/submissions/")
	idStr = strings.TrimSuffix(idStr, "/approve")
	id, _ := strconv.ParseInt(idStr, 10, 64)
	ctx := r.Context()
	ref := firebase.Client().Collection("submissions").Doc(strconv.FormatInt(id, 10))
	doc, err := ref.Get(ctx)
	if err != nil || !doc.Exists() {
		writeErr(w, 404, "Not found.")
		return
	}
	d := doc.Data()
	catName := asString(d["categoryName"])
	if catName == "" {
		catName = "Other"
	}
	catID := int64(0)
	catDocs, _ := firebase.Client().Collection("categories").Documents(ctx).GetAll()
	for _, cd := range catDocs {
		if asString(cd.Data()["name"]) == catName {
			catID = asInt(cd.Data()["id"])
			break
		}
	}
	if catID == 0 {
		catID, _ = firebase.NextID(ctx, "categories")
		_, _ = firebase.Client().Collection("categories").Doc(strconv.FormatInt(catID, 10)).Set(ctx, map[string]any{
			"id": catID, "name": catName, "icon": "Package",
			"createdAt": time.Now().UTC().Format(time.RFC3339),
		})
	}
	appID, _ := firebase.NextID(ctx, "apps")
	app := map[string]any{
		"id": appID, "name": d["appName"], "packageName": d["packageName"],
		"description": d["description"], "categoryId": catID,
		"version": strOr(d["version"], "1.0.0"), "size": strOr(d["size"], "Unknown"),
		"iconUrl": d["iconUrl"], "screenshotUrls": d["screenshotUrls"],
		"downloadUrl": d["apkUrl"], "developerSlug": d["developerSlug"],
		"developerLogoUrl": d["developerLogoUrl"], "developerDescription": d["developerDescription"],
		"scanStatus": "safe", "downloads": int64(0), "rating": float64(0), "reviewCount": int64(0),
		"isFeatured": false, "isActive": true, "developer": d["developerName"],
		"createdAt": time.Now().UTC().Format(time.RFC3339),
	}
	_, _ = firebase.Client().Collection("apps").Doc(strconv.FormatInt(appID, 10)).Set(ctx, app)
	_, _ = ref.Set(ctx, map[string]any{
		"status": "approved", "reviewedAt": time.Now().UTC().Format(time.RFC3339),
	}, firestore.MergeAll)
	catNames, _ := loadCatNames(ctx)
	writeJSON(w, 200, map[string]any{"submission": "approved", "app": mapApp(app, catNames)})
}

func RejectSubmission(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/admin/submissions/")
	idStr = strings.TrimSuffix(idStr, "/reject")
	id, _ := strconv.ParseInt(idStr, 10, 64)
	var body struct {
		Note string `json:"note"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	ctx := r.Context()
	ref := firebase.Client().Collection("submissions").Doc(strconv.FormatInt(id, 10))
	_, err := ref.Set(ctx, map[string]any{
		"status": "rejected", "reviewNote": body.Note,
		"reviewedAt": time.Now().UTC().Format(time.RFC3339),
	}, firestore.MergeAll)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "status": "rejected"})
}

func StorageStatus(w http.ResponseWriter, r *http.Request) {
	c := storage.NewFromEnv()
	writeJSON(w, 200, c.Status())
}

func UploadAPK(w http.ResponseWriter, r *http.Request) {
	c := storage.NewFromEnv()
	if c.Token == "" {
		writeErr(w, 503, "GitHub storage not configured (GITHUB_STORAGE_TOKEN)")
		return
	}
	if err := r.ParseMultipartForm(512 << 20); err != nil {
		writeErr(w, 400, "Invalid multipart form (max 512MB)")
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		writeErr(w, 400, "file field required")
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
		writeErr(w, 500, "pick repo: "+err.Error())
		return
	}
	url, size, err := c.UploadAPK(repo.Name, tag, assetName, data)
	if err != nil {
		writeErr(w, 500, "upload: "+err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{
		"ok": true, "repo": repo.FullName, "tag": tag, "asset": assetName,
		"size": size, "downloadUrl": url,
	})
}

func asInt(v any) int64 {
	switch t := v.(type) {
	case int64:
		return t
	case int:
		return int64(t)
	case float64:
		return int64(t)
	case json.Number:
		n, _ := t.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(t, 10, 64)
		return n
	default:
		return 0
	}
}
func asBool(v any) bool {
	b, _ := v.(bool)
	return b
}
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
func mapApp(d map[string]any, catNames map[int64]string) map[string]any {
	id := asInt(d["id"])
	cid := asInt(d["categoryId"])
	return map[string]any{
		"id": id, "name": d["name"], "packageName": d["packageName"],
		"description": d["description"], "categoryId": cid,
		"categoryName": catNames[cid], "version": d["version"], "size": d["size"],
		"iconUrl": d["iconUrl"], "screenshotUrls": d["screenshotUrls"],
		"downloadUrl": d["downloadUrl"], "developerSlug": d["developerSlug"],
		"developerLogoUrl": d["developerLogoUrl"], "developerDescription": d["developerDescription"],
		"scanStatus": d["scanStatus"], "scanReport": d["scanReport"],
		"downloads": asInt(d["downloads"]), "rating": d["rating"], "reviewCount": asInt(d["reviewCount"]),
		"isFeatured": asBool(d["isFeatured"]), "isActive": d["isActive"] != false,
		"developer": d["developer"], "createdAt": toISO(d["createdAt"]),
	}
}
func loadCatNames(ctx context.Context) (map[int64]string, error) {
	m := map[int64]string{}
	db := firebase.Client()
	if db == nil {
		return m, fmt.Errorf("no firestore")
	}
	docs, err := db.Collection("categories").Documents(ctx).GetAll()
	if err != nil {
		return m, err
	}
	for _, doc := range docs {
		d := doc.Data()
		id := asInt(d["id"])
		if id == 0 {
			if n, e := strconv.ParseInt(doc.Ref.ID, 10, 64); e == nil {
				id = n
			}
		}
		m[id] = asString(d["name"])
	}
	return m, nil
}
