package storage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/husdainshah2-web/div-store/internal/config"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultMaxBytes = int64(3) * 1024 * 1024 * 1024 // 3 GB
	OwnerEnv        = "GITHUB_STORAGE_OWNER"
	TokenEnv        = "GITHUB_STORAGE_TOKEN"
	PrefixEnv       = "GITHUB_STORAGE_PREFIX"
)

type PoolRepo struct {
	Name      string `json:"name"`
	FullName  string `json:"fullName"`
	Index     int    `json:"index"`
	BytesUsed int64  `json:"bytesUsed"`
	Active    bool   `json:"active"`
}

type Client struct {
	Owner   string
	Token   string
	Prefix  string
	MaxBytes int64
	HTTP    *http.Client
}

func NewFromEnv() *Client {
	return &Client{
		Owner:    config.Owner(),
		Token:    config.Token(),
		Prefix:   config.APKPrefix(), // ONLY div-store-apks-* repos
		MaxBytes: DefaultMaxBytes,
		HTTP:     &http.Client{Timeout: 120 * time.Second},
	}
}

func (c *Client) enabled() bool {
	return c.Token != "" && c.Owner != ""
}

func (c *Client) api(method, path string, body io.Reader, contentType string) (*http.Response, error) {
	req, err := http.NewRequest(method, "https://api.github.com"+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return c.HTTP.Do(req)
}

func (c *Client) ListStorageRepos() ([]PoolRepo, error) {
	if !c.enabled() {
		return nil, fmt.Errorf("github storage not configured")
	}
	resp, err := c.api("GET", "/user/repos?per_page=100&sort=full_name", nil, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list repos: %s %s", resp.Status, string(b))
	}
	var repos []struct {
		Name     string `json:"name"`
		FullName string `json:"full_name"`
		Private  bool   `json:"private"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&repos); err != nil {
		return nil, err
	}
	var out []PoolRepo
	for _, r := range repos {
		if !strings.HasPrefix(r.Name, c.Prefix+"-") {
			continue
		}
		suf := strings.TrimPrefix(r.Name, c.Prefix+"-")
		idx, _ := strconv.Atoi(suf)
		used, _ := c.RepoReleaseBytes(r.Name)
		out = append(out, PoolRepo{
			Name: r.Name, FullName: r.FullName, Index: idx, BytesUsed: used,
		})
	}
	// sort by index
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].Index < out[i].Index {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out, nil
}

func (c *Client) RepoReleaseBytes(repo string) (int64, error) {
	resp, err := c.api("GET", fmt.Sprintf("/repos/%s/%s/releases?per_page=100", c.Owner, repo), nil, "")
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return 0, nil
	}
	var releases []struct {
		Assets []struct {
			Size int64 `json:"size"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return 0, err
	}
	var total int64
	for _, rel := range releases {
		for _, a := range rel.Assets {
			total += a.Size
		}
	}
	return total, nil
}

func (c *Client) CreateNextRepo(index int) (PoolRepo, error) {
	name := fmt.Sprintf("%s-%02d", c.Prefix, index)
	if name == config.DataRepo() || c.Prefix == config.DataRepo() {
		return PoolRepo{}, fmt.Errorf("refusing to use data repo for APK storage")
	}
	payload := map[string]any{
		"name":        name,
		"private":     true,
		"description": fmt.Sprintf("Div Store APK storage pool #%02d", index),
		"auto_init":   true,
	}
	b, _ := json.Marshal(payload)
	resp, err := c.api("POST", "/user/repos", bytes.NewReader(b), "application/json")
	if err != nil {
		return PoolRepo{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return PoolRepo{}, fmt.Errorf("create repo: %s %s", resp.Status, string(body))
	}
	var created struct {
		Name     string `json:"name"`
		FullName string `json:"full_name"`
	}
	_ = json.Unmarshal(body, &created)
	return PoolRepo{Name: created.Name, FullName: created.FullName, Index: index, Active: true}, nil
}

func (c *Client) PickRepo(extraBytes int64) (PoolRepo, error) {
	repos, err := c.ListStorageRepos()
	if err != nil {
		return PoolRepo{}, err
	}
	if len(repos) == 0 {
		return c.CreateNextRepo(1)
	}
	// last repo
	last := repos[len(repos)-1]
	if last.BytesUsed+extraBytes <= c.MaxBytes {
		last.Active = true
		return last, nil
	}
	return c.CreateNextRepo(last.Index + 1)
}

// UploadAPK creates/uses a release tag and uploads asset. Returns browser download URL.
func (c *Client) UploadAPK(repo, tag, assetName string, data []byte) (string, int64, error) {
	if !c.enabled() {
		return "", 0, fmt.Errorf("github storage not configured")
	}
	// ensure release exists
	rel, err := c.getOrCreateRelease(repo, tag)
	if err != nil {
		return "", 0, err
	}
	// upload asset
	url := fmt.Sprintf("https://uploads.github.com/repos/%s/%s/releases/%d/assets?name=%s",
		c.Owner, repo, rel.ID, assetName)
	req, err := http.NewRequest("POST", url, bytes.NewReader(data))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", 0, fmt.Errorf("upload asset: %s %s", resp.Status, string(body))
	}
	var asset struct {
		BrowserDownloadURL string `json:"browser_download_url"`
		Size               int64  `json:"size"`
	}
	if err := json.Unmarshal(body, &asset); err != nil {
		return "", 0, err
	}
	return asset.BrowserDownloadURL, asset.Size, nil
}

type release struct {
	ID      int64  `json:"id"`
	TagName string `json:"tag_name"`
}

func (c *Client) getOrCreateRelease(repo, tag string) (*release, error) {
	resp, err := c.api("GET", fmt.Sprintf("/repos/%s/%s/releases/tags/%s", c.Owner, repo, tag), nil, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 200 {
		var rel release
		if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
			return nil, err
		}
		return &rel, nil
	}
	payload, _ := json.Marshal(map[string]any{
		"tag_name": tag,
		"name":     tag,
		"body":     "Div Store APK release",
		"draft":    false,
		"prerelease": false,
	})
	resp2, err := c.api("POST", fmt.Sprintf("/repos/%s/%s/releases", c.Owner, repo), bytes.NewReader(payload), "application/json")
	if err != nil {
		return nil, err
	}
	defer resp2.Body.Close()
	body, _ := io.ReadAll(resp2.Body)
	if resp2.StatusCode >= 300 {
		return nil, fmt.Errorf("create release: %s %s", resp2.Status, string(body))
	}
	var rel release
	if err := json.Unmarshal(body, &rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

func (c *Client) Status() map[string]any {
	out := map[string]any{
		"enabled":  c.enabled(),
		"owner":    c.Owner,
		"prefix":   c.Prefix,
		"maxBytes": c.MaxBytes,
		"maxGB":    float64(c.MaxBytes) / (1024 * 1024 * 1024),
		"purpose":  "apk_binaries_only",
		"repos":    []string{c.Prefix + "-01", c.Prefix + "-02"},
		"note":     "catalog JSON must never be written to APK repos",
	}
	if !c.enabled() {
		out["error"] = "GITHUB_STORAGE_TOKEN not set"
		return out
	}
	repos, err := c.ListStorageRepos()
	if err != nil {
		out["error"] = err.Error()
		return out
	}
	out["repos"] = repos
	return out
}
