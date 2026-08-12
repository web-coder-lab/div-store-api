package handlers

import (
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/husdainshah2-web/div-store/internal/ghdb"
)

// POST /api/admin/apps/upload — multipart icon + apk only (no URLs)
func AdminUploadApp(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeErr(w, 400, "Invalid form")
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	pkg := strings.TrimSpace(r.FormValue("packageName"))
	desc := strings.TrimSpace(r.FormValue("description"))
	if name == "" || pkg == "" {
		writeErr(w, 400, "name and packageName required")
		return
	}
	var iconURL, apkURL string
	if f, hdr, err := r.FormFile("icon"); err == nil {
		defer f.Close()
		data, _ := io.ReadAll(f)
		if len(data) == 0 {
			writeErr(w, 400, "empty icon")
			return
		}
		url, err := uploadAssetToRelease(data, safeName(hdr.Filename, "icon.png"), "icon")
		if err != nil {
			writeErr(w, 500, "icon: "+err.Error())
			return
		}
		iconURL = url
	} else {
		writeErr(w, 400, "icon file required")
		return
	}
	if f, hdr, err := r.FormFile("apk"); err == nil {
		defer f.Close()
		if !strings.HasSuffix(strings.ToLower(hdr.Filename), ".apk") {
			writeErr(w, 400, "Only .apk files allowed")
			return
		}
		data, err := io.ReadAll(f)
		if err != nil || len(data) == 0 {
			writeErr(w, 400, "empty apk")
			return
		}
		url, err := uploadAssetToRelease(data, filepath.Base(hdr.Filename), "apk")
		if err != nil {
			writeErr(w, 500, "apk: "+err.Error())
			return
		}
		apkURL = url
	} else {
		writeErr(w, 400, "apk file required")
		return
	}
	idStr, idNum, err := db().NextTypedID(r.Context(), "apps", ghdb.PrefixAPK)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	row := map[string]any{
		"id": idStr, "apkId": idStr, "idNum": idNum, "type": "apk",
		"name": name, "packageName": pkg, "description": desc,
		"categoryId": r.FormValue("categoryId"), "iconUrl": iconURL, "downloadUrl": apkURL,
		"version": "1.0.0", "size": "Unknown", "screenshotUrls": []any{},
		"scanStatus": "safe", "downloads": int64(0), "rating": float64(0), "reviewCount": int64(0),
		"isFeatured": false, "isActive": true, "createdAt": time.Now().UTC().Format(time.RFC3339),
	}
	if err := db().UpsertByStringID(r.Context(), "apps", idStr, row); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, row)
}
