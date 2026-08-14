package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/husdainshah2-web/div-store/internal/ghdb"
	"github.com/husdainshah2-web/div-store/internal/storage"
)

// POST /api/developers/register — Developer Studio account
// multipart or JSON: companyName, description, email, companyIcon (file optional) | iconUrl
func RegisterDeveloper(w http.ResponseWriter, r *http.Request) {
	g := db()
	if g == nil || !g.Enabled() {
		writeErr(w, 503, "Database not ready")
		return
	}
	var companyName, description, email, iconURL, bannerURL string
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/") {
		if err := r.ParseMultipartForm(16 << 20); err != nil {
			writeErr(w, 400, "Invalid form")
			return
		}
		companyName = strings.TrimSpace(r.FormValue("companyName"))
		if companyName == "" {
			companyName = strings.TrimSpace(r.FormValue("name"))
		}
		description = strings.TrimSpace(r.FormValue("description"))
		email = strings.ToLower(strings.TrimSpace(r.FormValue("email")))
		iconURL = strings.TrimSpace(r.FormValue("iconUrl"))
		bannerURL = strings.TrimSpace(r.FormValue("bannerUrl"))
		if f, hdr, err := r.FormFile("companyIcon"); err == nil {
			defer f.Close()
			data, _ := io.ReadAll(f)
			if len(data) > 0 {
				url, err := uploadAssetToRelease(data, safeName(hdr.Filename, "icon.png"), "icon")
				if err != nil {
					writeErr(w, 500, "icon upload: "+err.Error())
					return
				}
				iconURL = url
			}
		}
		if f, hdr, err := r.FormFile("banner"); err == nil {
			defer f.Close()
			data, _ := io.ReadAll(f)
			if len(data) > 0 {
				url, err := uploadAssetToRelease(data, safeName(hdr.Filename, "banner.jpg"), "banner")
				if err != nil {
					writeErr(w, 500, "banner upload: "+err.Error())
					return
				}
				bannerURL = url
			}
		}
	} else {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, 400, "Invalid body")
			return
		}
		companyName = strings.TrimSpace(asString(body["companyName"]))
		if companyName == "" {
			companyName = strings.TrimSpace(asString(body["name"]))
		}
		description = strings.TrimSpace(asString(body["description"]))
		email = strings.ToLower(strings.TrimSpace(asString(body["email"])))
		iconURL = strings.TrimSpace(asString(body["iconUrl"]))
		bannerURL = strings.TrimSpace(asString(body["bannerUrl"]))
	}
	if companyName == "" || description == "" || email == "" {
		writeErr(w, 400, "companyName, description, email required")
		return
	}
	slug := slugify(companyName)
	rows, _ := g.ReadAll(r.Context(), "developer_profiles")
	for _, d := range rows {
		if strings.EqualFold(asString(d["email"]), email) {
			writeErr(w, 409, "Developer already registered with this email")
			return
		}
		if asString(d["slug"]) == slug {
			slug = slug + "-" + fmt.Sprintf("%d", time.Now().Unix()%10000)
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	row := map[string]any{
		"slug": slug, "name": companyName, "companyName": companyName,
		"logoUrl": iconURL, "bannerUrl": bannerURL, "description": description, "contactEmail": email,
		"email": email, "verified": false, "createdAt": now, "updatedAt": now,
	}
	rows = append(rows, row)
	if err := g.WriteAll(r.Context(), "developer_profiles", rows, "developer register "+slug); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{
		"ok": true,
		"developer": row,
		"message": "Developer Studio account created. You can now submit APKs.",
	})
}

// GET /api/developers/by-email?email=
func DeveloperByEmail(w http.ResponseWriter, r *http.Request) {
	email := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("email")))
	if email == "" {
		writeErr(w, 400, "email required")
		return
	}
	rows, err := db().ReadAll(r.Context(), "developer_profiles")
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	for _, d := range rows {
		if strings.EqualFold(asString(d["email"]), email) || strings.EqualFold(asString(d["contactEmail"]), email) {
			writeJSON(w, 200, map[string]any{"ok": true, "developer": d})
			return
		}
	}
	writeErr(w, 404, "Developer not found — create Developer Studio account first")
}

// POST /api/submit — full company APK submit
// multipart preferred: icon file + apk file (.apk only)
// optional fallback: iconUrl / apkUrl
// categories max 4 by name; developerEmail required
func SubmitAppFull(w http.ResponseWriter, r *http.Request) {
	g := db()
	if g == nil || !g.Enabled() {
		writeErr(w, 503, "Database not ready")
		return
	}

	var (
		appName, pkg, desc, iconURL, apkURL, devEmail string
		categoryNames                                 []string
	)

	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/") {
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			writeErr(w, 400, "Invalid multipart form")
			return
		}
		appName = strings.TrimSpace(r.FormValue("appName"))
		if appName == "" {
			appName = strings.TrimSpace(r.FormValue("name"))
		}
		pkg = strings.TrimSpace(r.FormValue("packageName"))
		desc = strings.TrimSpace(r.FormValue("description"))
		devEmail = strings.ToLower(strings.TrimSpace(r.FormValue("developerEmail")))
		if devEmail == "" {
			devEmail = strings.ToLower(strings.TrimSpace(r.FormValue("email")))
		}
		iconURL = strings.TrimSpace(r.FormValue("iconUrl"))
		apkURL = strings.TrimSpace(r.FormValue("apkUrl"))
		if apkURL == "" {
			apkURL = strings.TrimSpace(r.FormValue("downloadUrl"))
		}
		catRaw := strings.TrimSpace(r.FormValue("categories"))
		if catRaw == "" {
			catRaw = strings.TrimSpace(r.FormValue("categoryNames"))
		}
		categoryNames = parseCategories(catRaw)

		if f, hdr, err := r.FormFile("icon"); err == nil {
			defer f.Close()
			data, _ := io.ReadAll(f)
			if len(data) > 0 {
				url, err := uploadAssetToRelease(data, safeName(hdr.Filename, "icon.png"), "icon")
				if err != nil {
					writeErr(w, 500, "icon upload failed: "+err.Error())
					return
				}
				iconURL = url
			}
		}
		if f, hdr, err := r.FormFile("apk"); err == nil {
			defer f.Close()
			name := hdr.Filename
			if !strings.HasSuffix(strings.ToLower(name), ".apk") {
				writeErr(w, 400, "Only .apk files are allowed")
				return
			}
			// Stream large APK to temp disk (avoids OOM on 200-300MB)
			tmp, err := os.CreateTemp("", "div-apk-*.apk")
			if err != nil {
				writeErr(w, 500, "Server temp storage error")
				return
			}
			tmpPath := tmp.Name()
			defer os.Remove(tmpPath)
			written, err := io.Copy(tmp, f)
			_ = tmp.Close()
			if err != nil || written == 0 {
				writeErr(w, 400, "Empty APK file")
				return
			}
			if written > 320<<20 {
				writeErr(w, 400, "APK too large (max 320MB)")
				return
			}
			url, err := uploadAssetFileToRelease(tmpPath, filepath.Base(name), "apk")
			if err != nil {
				writeErr(w, 500, "APK upload to GitHub Releases failed: "+err.Error())
				return
			}
			apkURL = url
		}
	} else {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, 400, "Invalid body")
			return
		}
		appName = strings.TrimSpace(asString(body["appName"]))
		if appName == "" {
			appName = strings.TrimSpace(asString(body["name"]))
		}
		pkg = strings.TrimSpace(asString(body["packageName"]))
		desc = strings.TrimSpace(asString(body["description"]))
		devEmail = strings.ToLower(strings.TrimSpace(asString(body["developerEmail"])))
		if devEmail == "" {
			devEmail = strings.ToLower(strings.TrimSpace(asString(body["email"])))
		}
		iconURL = strings.TrimSpace(asString(body["iconUrl"]))
		apkURL = strings.TrimSpace(asString(body["apkUrl"]))
		if apkURL == "" {
			apkURL = strings.TrimSpace(asString(body["downloadUrl"]))
		}
		if arr, ok := body["categories"].([]any); ok {
			for _, v := range arr {
				if s := strings.TrimSpace(asString(v)); s != "" {
					categoryNames = append(categoryNames, s)
				}
			}
		} else {
			categoryNames = parseCategories(asString(body["categories"]))
		}
	}

	if len(categoryNames) > 4 {
		writeErr(w, 400, "Maximum 4 categories allowed")
		return
	}
	if appName == "" || pkg == "" || desc == "" {
		writeErr(w, 400, "appName, packageName, description required")
		return
	}
	if iconURL == "" {
		writeErr(w, 400, "icon file required (gallery)")
		return
	}
	if apkURL == "" {
		writeErr(w, 400, "APK file required (.apk from files)")
		return
	}
	if devEmail == "" {
		writeErr(w, 400, "developerEmail required — create Developer Studio account first")
		return
	}

	// Phase 4: hard gate — only registered Studio emails may submit
	devs, errDevs := g.ReadAll(r.Context(), "developer_profiles")
	if errDevs != nil {
		writeErr(w, 500, "Could not verify developer account")
		return
	}
	var dev map[string]any
	for _, d := range devs {
		em := strings.ToLower(strings.TrimSpace(asString(d["email"])))
		ce := strings.ToLower(strings.TrimSpace(asString(d["contactEmail"])))
		if em == devEmail || ce == devEmail {
			dev = d
			break
		}
	}
	if dev == nil {
		writeErr(w, 403, "developer_not_registered: Create Developer Studio account before uploading APK")
		return
	}

	cats, _ := g.ReadAll(r.Context(), "categories")
	nameToID := map[string]int64{}
	for _, c := range cats {
		nameToID[strings.ToLower(asString(c["name"]))] = ghdb.ToInt(c["id"])
	}
	var resolved []string
	var catIDs []int64
	for _, n := range categoryNames {
		id, ok := nameToID[strings.ToLower(n)]
		if !ok {
			writeErr(w, 400, "Unknown category: "+n+" — choose from admin categories")
			return
		}
		resolved = append(resolved, n)
		catIDs = append(catIDs, id)
	}
	primaryCat := "Other"
	var primaryID int64
	if len(resolved) > 0 {
		primaryCat = resolved[0]
		primaryID = catIDs[0]
	}

	idStr, idNum, err := g.NextTypedID(r.Context(), "submissions", ghdb.PrefixRequest)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	row := map[string]any{
		"id": idStr, "requestId": idStr, "idNum": idNum, "type": "user_request",
		"appName": appName, "packageName": pkg, "description": desc,
		"developerName": asString(dev["name"]), "developerSlug": asString(dev["slug"]),
		"developerLogoUrl": asString(dev["logoUrl"]), "developerDescription": asString(dev["description"]),
		"contactEmail": devEmail, "iconUrl": iconURL, "apkUrl": apkURL, "downloadUrl": apkURL,
		"categoryName": primaryCat, "categoryNames": resolved, "categoryIds": catIDs, "categoryId": primaryID,
		"version": strOr(r.FormValue("version"), "1.0.0"), "size": strOr(r.FormValue("size"), "Unknown"),
		"screenshotUrls": []any{}, "scanStatus": "pending", "status": "pending",
		"submittedAt": now, "reviewedAt": nil,
		"message": "Thanks for submitting your APK. Please wait for approval (up to 24 hours).",
	}
	if err := g.UpsertByStringID(r.Context(), "submissions", idStr, row); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{
		"ok": true, "requestId": idStr, "submission": row,
		"message": "Thanks for submit your apk. Please wait approval (24 hours).",
		"next": "go_back_store",
	})
}


func parseCategories(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if strings.HasPrefix(raw, "[") {
		var arr []string
		if json.Unmarshal([]byte(raw), &arr) == nil {
			out := make([]string, 0, len(arr))
			for _, s := range arr {
				s = strings.TrimSpace(s)
				if s != "" {
					out = append(out, s)
				}
			}
			if len(out) > 4 {
				out = out[:4]
			}
			return out
		}
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, 4)
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
		if len(out) >= 4 {
			break
		}
	}
	return out
}

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
		} else if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "developer"
	}
	return out
}

func safeName(name, fallback string) string {
	name = filepath.Base(name)
	if name == "" || name == "." {
		return fallback
	}
	return name
}

func uploadAssetToRelease(data []byte, filename, kind string) (string, error) {
	c := storage.NewFromEnv()
	if c.Token == "" {
		return "", fmt.Errorf("GITHUB_STORAGE_TOKEN not set")
	}
	tag := kind + "-" + time.Now().UTC().Format("20060102-150405")
	repo, err := c.PickRepo(int64(len(data)))
	if err != nil {
		return "", err
	}
	url, _, err := c.UploadAPK(repo.Name, tag, filename, data)
	return url, err
}

func uploadAssetFileToRelease(path, filename, kind string) (string, error) {
	c := storage.NewFromEnv()
	if c.Token == "" {
		return "", fmt.Errorf("GITHUB_STORAGE_TOKEN not set")
	}
	fi, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	tag := kind + "-" + time.Now().UTC().Format("20060102-150405")
	repo, err := c.PickRepo(fi.Size())
	if err != nil {
		return "", err
	}
	url, _, err := c.UploadAPKReader(repo.Name, tag, filename, f, fi.Size())
	return url, err
}


// PATCH/POST /api/developers/update — update logo + banner (multipart or JSON)
func UpdateDeveloperProfile(w http.ResponseWriter, r *http.Request) {
	g := db()
	if g == nil || !g.Enabled() {
		writeErr(w, 503, "Database not ready")
		return
	}
	var email, iconURL, bannerURL, description, companyName string
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/") {
		_ = r.ParseMultipartForm(32 << 20)
		email = strings.ToLower(strings.TrimSpace(r.FormValue("email")))
		description = strings.TrimSpace(r.FormValue("description"))
		companyName = strings.TrimSpace(r.FormValue("companyName"))
		iconURL = strings.TrimSpace(r.FormValue("iconUrl"))
		bannerURL = strings.TrimSpace(r.FormValue("bannerUrl"))
		if f, hdr, err := r.FormFile("companyIcon"); err == nil {
			defer f.Close()
			data, _ := io.ReadAll(f)
			if len(data) > 0 {
				url, err := uploadAssetToRelease(data, safeName(hdr.Filename, "icon.png"), "icon")
				if err != nil {
					writeErr(w, 500, err.Error())
					return
				}
				iconURL = url
			}
		}
		if f, hdr, err := r.FormFile("banner"); err == nil {
			defer f.Close()
			data, _ := io.ReadAll(f)
			if len(data) > 0 {
				url, err := uploadAssetToRelease(data, safeName(hdr.Filename, "banner.jpg"), "banner")
				if err != nil {
					writeErr(w, 500, err.Error())
					return
				}
				bannerURL = url
			}
		}
	} else {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		email = strings.ToLower(strings.TrimSpace(asString(body["email"])))
		description = strings.TrimSpace(asString(body["description"]))
		companyName = strings.TrimSpace(asString(body["companyName"]))
		iconURL = strings.TrimSpace(asString(body["iconUrl"]))
		bannerURL = strings.TrimSpace(asString(body["bannerUrl"]))
	}
	if email == "" {
		writeErr(w, 400, "email required")
		return
	}
	rows, err := g.ReadAll(r.Context(), "developer_profiles")
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	found := false
	for i := range rows {
		if strings.EqualFold(asString(rows[i]["email"]), email) || strings.EqualFold(asString(rows[i]["contactEmail"]), email) {
			if iconURL != "" {
				rows[i]["logoUrl"] = iconURL
			}
			if bannerURL != "" {
				rows[i]["bannerUrl"] = bannerURL
			}
			if description != "" {
				rows[i]["description"] = description
			}
			if companyName != "" {
				rows[i]["name"] = companyName
				rows[i]["companyName"] = companyName
			}
			rows[i]["updatedAt"] = time.Now().UTC().Format(time.RFC3339)
			found = true
			_ = g.WriteAll(r.Context(), "developer_profiles", rows, "update developer "+email)
			writeJSON(w, 200, map[string]any{"ok": true, "developer": rows[i]})
			return
		}
	}
	if !found {
		writeErr(w, 404, "Developer not found")
	}
}

// GET /api/submissions?email= — developer's own submissions (Phase 5)
func MySubmissions(w http.ResponseWriter, r *http.Request) {
	email := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("email")))
	if email == "" {
		writeErr(w, 400, "email required")
		return
	}
	g := db()
	if g == nil || !g.Enabled() {
		writeErr(w, 503, "Database not ready")
		return
	}
	rows, err := g.ReadAll(r.Context(), "submissions")
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	out := make([]map[string]any, 0)
	for _, s := range rows {
		ce := strings.ToLower(strings.TrimSpace(asString(s["contactEmail"])))
		de := strings.ToLower(strings.TrimSpace(asString(s["developerEmail"])))
		if ce == email || de == email {
			out = append(out, s)
		}
	}
	writeJSON(w, 200, map[string]any{"ok": true, "submissions": out, "count": len(out)})
}

// POST /api/report — user reports an app (Phase 15)
func ReportApp(w http.ResponseWriter, r *http.Request) {
	g := db()
	if g == nil || !g.Enabled() {
		writeErr(w, 503, "Database not ready")
		return
	}
	var appID, appName, reason, email string
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "application/json") {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		appID = strings.TrimSpace(asString(body["appId"]))
		appName = strings.TrimSpace(asString(body["appName"]))
		reason = strings.TrimSpace(asString(body["reason"]))
		email = strings.ToLower(strings.TrimSpace(asString(body["email"])))
	} else {
		_ = r.ParseForm()
		appID = strings.TrimSpace(r.FormValue("appId"))
		appName = strings.TrimSpace(r.FormValue("appName"))
		reason = strings.TrimSpace(r.FormValue("reason"))
		email = strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	}
	if appID == "" && appName == "" {
		writeErr(w, 400, "appId or appName required")
		return
	}
	if reason == "" {
		writeErr(w, 400, "reason required")
		return
	}
	idStr, idNum, err := g.NextTypedID(r.Context(), "reports", "rpt_")
	if err != nil {
		n, e2 := g.NextID(r.Context(), "reports")
		if e2 != nil {
			writeErr(w, 500, err.Error())
			return
		}
		idStr = "rpt_" + fmt.Sprintf("%d", n)
		idNum = n
	}
	now := time.Now().UTC().Format(time.RFC3339)
	row := map[string]any{
		"id": idStr, "idNum": idNum, "type": "app_report",
		"appId": appID, "appName": appName, "reason": reason,
		"email": email, "status": "open", "createdAt": now,
	}
	if err := g.UpsertByStringID(r.Context(), "reports", idStr, row); err != nil {
		rows, _ := g.ReadAll(r.Context(), "reports")
		rows = append(rows, row)
		if err2 := g.WriteAll(r.Context(), "reports", rows, "report "+idStr); err2 != nil {
			writeErr(w, 500, err2.Error())
			return
		}
	}
	// Optional store-wide alert for admin visibility in feed
	pushStoreNotification(r.Context(),
		"App reported",
		"Report on "+appName+": "+reason,
		"app_report",
		"",
	)
	writeJSON(w, 201, map[string]any{"ok": true, "id": idStr, "message": "Thanks — we received your report."})
}
