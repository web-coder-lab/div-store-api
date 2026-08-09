package handlers

import (
	"encoding/json"
	"fmt"
	"io"
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
	var companyName, description, email, iconURL string
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
		if f, hdr, err := r.FormFile("companyIcon"); err == nil {
			defer f.Close()
			data, _ := io.ReadAll(f)
			if len(data) > 0 {
				url, err := uploadAssetToRelease(data, hdr.Filename, "icon")
				if err != nil {
					writeErr(w, 500, "icon upload: "+err.Error())
					return
				}
				iconURL = url
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
		"logoUrl": iconURL, "description": description, "contactEmail": email,
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
// Accepts multipart:
//   icon (file optional), iconUrl (optional)
//   apk (file .apk only) OR apkUrl / downloadUrl (one required)
//   packageName, description, appName
//   categories: JSON array of names OR category names comma-separated (max 4)
//   developerEmail (must have studio account)
func SubmitAppFull(w http.ResponseWriter, r *http.Request) {
	g := db()
	if g == nil || !g.Enabled() {
		writeErr(w, 503, "Database not ready")
		return
	}

	var (
		appName, pkg, desc, iconURL, apkURL, devEmail string
		categoryNames                                  []string
	)

	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/") {
		if err := r.ParseMultipartForm(512 << 20); err != nil {
			writeErr(w, 400, "Invalid multipart (max 512MB)")
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
		// categories: JSON array or comma list
		catRaw := strings.TrimSpace(r.FormValue("categories"))
		if catRaw == "" {
			catRaw = strings.TrimSpace(r.FormValue("categoryNames"))
		}
		categoryNames = parseCategories(catRaw)
		// icon file
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
		// apk file — ONLY .apk
		if f, hdr, err := r.FormFile("apk"); err == nil {
			defer f.Close()
			name := hdr.Filename
			if !strings.HasSuffix(strings.ToLower(name), ".apk") {
				writeErr(w, 400, "Only .apk files are allowed")
				return
			}
			data, err := io.ReadAll(f)
			if err != nil || len(data) == 0 {
				writeErr(w, 400, "Empty APK file")
				return
			}
			url, err := uploadAssetToRelease(data, filepath.Base(name), "apk")
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
		writeErr(w, 400, "icon file or iconUrl required")
		return
	}
	if apkURL == "" {
		writeErr(w, 400, "Provide either .apk file or apkUrl/downloadUrl")
		return
	}
	if apkURL != "" && strings.Contains(strings.ToLower(apkURL), ".") {
		// if client sent a non-url path ending without apk when using url mode — soft check
		if !strings.HasPrefix(apkURL, "http") && !strings.HasSuffix(strings.ToLower(apkURL), ".apk") {
			writeErr(w, 400, "apkUrl must be http(s) link or end with .apk")
			return
		}
	}
	if devEmail == "" {
		writeErr(w, 400, "developerEmail required — create Developer Studio account first")
		return
	}

	// Must have developer studio account
	devs, _ := g.ReadAll(r.Context(), "developer_profiles")
	var dev map[string]any
	for _, d := range devs {
		if strings.EqualFold(asString(d["email"]), devEmail) || strings.EqualFold(asString(d["contactEmail"]), devEmail) {
			dev = d
			break
		}
	}
	if dev == nil {
		writeErr(w, 403, "Create Developer Studio account before uploading APK")
		return
	}

	// Resolve categories from admin list (by name)
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
	primaryCat := ""
	var primaryID int64
	if len(resolved) > 0 {
		primaryCat = resolved[0]
		primaryID = catIDs[0]
	} else {
		primaryCat = "Other"
	}

	idStr, idNum, err := g.NextTypedID(r.Context(), "submissions", ghdb.PrefixRequest)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	row := map[string]any{
		"id": idStr, "requestId": idStr, "idNum": idNum,
		"type": "user_request", "appName": appName, "packageName": pkg, "description": desc,
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
		"ok": true,
		"requestId": idStr,
		"submission": row,
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
