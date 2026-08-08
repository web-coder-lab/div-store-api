package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/husdainshah2-web/div-store/internal/firebase"
	"github.com/husdainshah2-web/div-store/internal/storage"
	"google.golang.org/api/iterator"
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
		return t
	default:
		return time.Now().UTC().Format(time.RFC3339)
	}
}

func Health(w http.ResponseWriter, r *http.Request) {
	st := storage.NewFromEnv()
	writeJSON(w, 200, map[string]any{
		"status":   "ok",
		"name":     "Div Store API",
		"version":  "1.0.1-go",
		"engine":   "go",
		"firebase": firebase.Status(),
		"storage": map[string]any{
			"enabled": st.Token != "",
			"owner":   st.Owner,
			"prefix":  st.Prefix,
			"maxGB":   3,
		},
		"time": time.Now().UTC().Format(time.RFC3339),
	})
}

func ListApps(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db := firebase.Client()
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

	catNames := map[int64]string{}
	cit := db.Collection("categories").Documents(ctx)
	for {
		doc, err := cit.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		d := doc.Data()
		id := asInt(d["id"])
		if n, ok := d["name"].(string); ok {
			catNames[id] = n
		}
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

	var rows []map[string]any
	it := db.Collection("apps").Documents(ctx)
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
		app := mapApp(d, catNames)
		if active, ok := d["isActive"].(bool); ok && !active {
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
	// sort by downloads desc (simple)
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
	doc, err := firebase.Client().Collection("apps").Doc(strconv.FormatInt(id, 10)).Get(ctx)
	if err != nil || !doc.Exists() {
		writeErr(w, 404, "App not found.")
		return
	}
	catNames := loadCatNames(ctx)
	writeJSON(w, 200, mapApp(doc.Data(), catNames))
}

func ListCategories(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db := firebase.Client()
	counts := map[int64]int{}
	ait := db.Collection("apps").Documents(ctx)
	for {
		doc, err := ait.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		cid := asInt(doc.Data()["categoryId"])
		counts[cid]++
	}
	var rows []map[string]any
	it := db.Collection("categories").Documents(ctx)
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
		id := asInt(d["id"])
		rows = append(rows, map[string]any{
			"id":        id,
			"name":      d["name"],
			"icon":      d["icon"],
			"createdAt": toISO(d["createdAt"]),
			"appCount":  counts[id],
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
		writeErr(w, 500, "Internal server error.")
		return
	}
	row := map[string]any{
		"id": id, "name": body.Name, "icon": body.Icon,
		"createdAt": time.Now().UTC().Format(time.RFC3339),
	}
	_, err = firebase.Client().Collection("categories").Doc(strconv.FormatInt(id, 10)).Set(ctx, row)
	if err != nil {
		writeErr(w, 500, "Internal server error.")
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
		writeErr(w, 500, "Internal server error.")
		return
	}
	w.WriteHeader(204)
}

func ListReviews(w http.ResponseWriter, r *http.Request) {
	// /api/apps/:id/reviews
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	// api apps id reviews
	if len(parts) < 4 {
		writeErr(w, 400, "Invalid path.")
		return
	}
	id, _ := strconv.ParseInt(parts[2], 10, 64)
	ctx := r.Context()
	it := firebase.Client().Collection("reviews").Where("appId", "==", id).Documents(ctx)
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
		writeErr(w, 500, "Internal server error.")
		return
	}
	row := map[string]any{
		"id": id, "appId": appID, "reviewerName": name,
		"rating": body.Rating, "comment": body.Comment,
		"createdAt": time.Now().UTC().Format(time.RFC3339),
	}
	_, err = firebase.Client().Collection("reviews").Doc(strconv.FormatInt(id, 10)).Set(ctx, row)
	if err != nil {
		writeErr(w, 500, "Internal server error.")
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
	ctx := r.Context()
	catNames := loadCatNames(ctx)
	it := firebase.Client().Collection("apps").Documents(ctx)
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
		writeErr(w, 500, "Internal server error.")
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
		writeErr(w, 500, "Internal server error.")
		return
	}
	writeJSON(w, 201, mapApp(row, loadCatNames(ctx)))
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
		writeErr(w, 500, "Internal server error.")
		return
	}
	doc, _ = ref.Get(ctx)
	writeJSON(w, 200, mapApp(doc.Data(), loadCatNames(ctx)))
}

func AdminDeleteApp(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/admin/apps/")
	id, _ := strconv.ParseInt(idStr, 10, 64)
	ctx := r.Context()
	_, err := firebase.Client().Collection("apps").Doc(strconv.FormatInt(id, 10)).Delete(ctx)
	if err != nil {
		writeErr(w, 500, "Internal server error.")
		return
	}
	w.WriteHeader(204)
}

func ListSubmissions(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	ctx := r.Context()
	it := firebase.Client().Collection("submissions").Documents(ctx)
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
		writeErr(w, 500, "Internal server error.")
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
		writeErr(w, 500, "Internal server error.")
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
	// ensure category
	catName := asString(d["categoryName"])
	if catName == "" {
		catName = "Other"
	}
	catID := int64(0)
	cit := firebase.Client().Collection("categories").Where("name", "==", catName).Limit(1).Documents(ctx)
	cdoc, err := cit.Next()
	if err == nil {
		catID = asInt(cdoc.Data()["id"])
	} else {
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
	writeJSON(w, 200, map[string]any{"submission": "approved", "app": mapApp(app, loadCatNames(ctx))})
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
		writeErr(w, 500, "Internal server error.")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "status": "rejected"})
}

// helpers
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
func loadCatNames(ctx context.Context) map[int64]string {
	m := map[int64]string{}
	if firebase.Client() == nil {
		return m
	}
	it := firebase.Client().Collection("categories").Documents(ctx)
	for {
		doc, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		d := doc.Data()
		m[asInt(d["id"])] = asString(d["name"])
	}
	return m
}

// --- GitHub APK storage ---

func StorageStatus(w http.ResponseWriter, r *http.Request) {
	c := storage.NewFromEnv()
	writeJSON(w, 200, c.Status())
}

// UploadAPK: multipart form field "file" (apk), optional "name", "tag"
func UploadAPK(w http.ResponseWriter, r *http.Request) {
	c := storage.NewFromEnv()
	if c.Token == "" {
		writeErr(w, 503, "GitHub storage not configured (GITHUB_STORAGE_TOKEN)")
		return
	}
	if err := r.ParseMultipartForm(512 << 20); err != nil { // 512MB
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
		"ok":         true,
		"repo":       repo.FullName,
		"tag":        tag,
		"asset":      assetName,
		"size":       size,
		"downloadUrl": url,
	})
}
