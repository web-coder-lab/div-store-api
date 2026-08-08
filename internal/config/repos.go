package config

import (
	"os"
	"strings"
)

func Token() string {
	if t := strings.TrimSpace(os.Getenv("GITHUB_STORAGE_TOKEN")); t != "" {
		return t
	}
	// Private-repo fallback (Render can ship this file with the deploy)
	for _, p := range []string{
		"secrets/github.token",
		"/opt/render/project/src/secrets/github.token",
		"github.token",
	} {
		b, err := os.ReadFile(p)
		if err == nil {
			t := strings.TrimSpace(string(b))
			if t != "" {
				return t
			}
		}
	}
	return ""
}

func Owner() string {
	o := os.Getenv("GITHUB_STORAGE_OWNER")
	if o == "" {
		return "web-coder-lab"
	}
	return o
}

func DataRepo() string {
	r := os.Getenv("GITHUB_DATA_REPO")
	if r == "" {
		return "div-store-data"
	}
	return r
}

func APKPrefix() string {
	p := os.Getenv("GITHUB_STORAGE_PREFIX")
	if p == "" {
		return "div-store-apks"
	}
	return p
}

func Layout() map[string]any {
	tok := Token()
	return map[string]any{
		"owner": Owner(),
		"data": map[string]any{
			"repo":        DataRepo(),
			"purpose":     "user_catalog_database",
			"paths":       []string{"db/apps.json", "db/categories.json", "db/reviews.json", "db/submissions.json", "db/developer_profiles.json", "db/settings.json", "db/_counters.json"},
			"fullName":    Owner() + "/" + DataRepo(),
			"neverStores": "apk_binaries",
		},
		"apk": map[string]any{
			"prefix":      APKPrefix(),
			"purpose":     "apk_release_binaries_only",
			"repos":       []string{APKPrefix() + "-01", APKPrefix() + "-02"},
			"fullNames":   []string{Owner() + "/" + APKPrefix() + "-01", Owner() + "/" + APKPrefix() + "-02"},
			"neverStores": "catalog_json",
		},
		"tokenSet": tok != "",
		"tokenSource": func() string {
			if strings.TrimSpace(os.Getenv("GITHUB_STORAGE_TOKEN")) != "" {
				return "env"
			}
			if tok != "" {
				return "secrets/github.token"
			}
			return "none"
		}(),
	}
}
