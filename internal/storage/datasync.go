package storage

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"cloud.google.com/go/firestore"
)

// Push when exported JSON payload exceeds this size (default 1 MB).
const DefaultPushThreshold = int64(1 * 1024 * 1024)

type DataSync struct {
	Owner     string
	Repo      string
	Token     string
	Branch    string
	Threshold int64
	HTTP      *http.Client

	mu       sync.Mutex
	lastPush time.Time
	lastSize int64
}

func NewDataSyncFromEnv() *DataSync {
	token := os.Getenv("GITHUB_STORAGE_TOKEN")
	owner := os.Getenv("GITHUB_STORAGE_OWNER")
	if owner == "" {
		owner = "web-coder-lab"
	}
	repo := os.Getenv("GITHUB_DATA_REPO")
	if repo == "" {
		repo = "div-store-data"
	}
	return &DataSync{
		Owner:     owner,
		Repo:      repo,
		Token:     token,
		Branch:    "main",
		Threshold: DefaultPushThreshold,
		HTTP:      &http.Client{Timeout: 90 * time.Second},
	}
}

func (d *DataSync) Enabled() bool {
	return d.Token != "" && d.Owner != "" && d.Repo != ""
}

func (d *DataSync) Status() map[string]any {
	d.mu.Lock()
	defer d.mu.Unlock()
	return map[string]any{
		"enabled":   d.Enabled(),
		"owner":     d.Owner,
		"repo":      d.Repo,
		"threshold": d.Threshold,
		"lastSize":  d.lastSize,
		"lastPush":  d.lastPush.Format(time.RFC3339),
		"mode":      "size>=1MB then push (no timer)",
	}
}

func (d *DataSync) api(method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, "https://api.github.com"+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+d.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return d.HTTP.Do(req)
}

func (d *DataSync) putFile(path string, content []byte, message string) error {
	var sha string
	resp, err := d.api("GET", fmt.Sprintf("/repos/%s/%s/contents/%s?ref=%s", d.Owner, d.Repo, path, d.Branch), nil)
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == 200 {
			var existing struct {
				SHA string `json:"sha"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&existing)
			sha = existing.SHA
		}
	}
	payload := map[string]any{
		"message": message,
		"content": base64.StdEncoding.EncodeToString(content),
		"branch":  d.Branch,
	}
	if sha != "" {
		payload["sha"] = sha
	}
	b, _ := json.Marshal(payload)
	resp2, err := d.api("PUT", fmt.Sprintf("/repos/%s/%s/contents/%s", d.Owner, d.Repo, path), bytes.NewReader(b))
	if err != nil {
		return err
	}
	defer resp2.Body.Close()
	body, _ := io.ReadAll(resp2.Body)
	if resp2.StatusCode >= 300 {
		return fmt.Errorf("put %s: %s %s", path, resp2.Status, string(body))
	}
	return nil
}

func dumpCollection(ctx context.Context, db *firestore.Client, name string) ([]map[string]any, error) {
	docs, err := db.Collection(name).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(docs))
	for _, doc := range docs {
		row := doc.Data()
		row["_docId"] = doc.Ref.ID
		out = append(out, row)
	}
	return out, nil
}

func (d *DataSync) buildSnapshot(ctx context.Context, db *firestore.Client) ([]byte, map[string]any, error) {
	collections := []string{
		"apps", "categories", "reviews", "submissions",
		"developer_profiles", "settings", "users",
	}
	snapshot := map[string]any{
		"exportedAt": time.Now().UTC().Format(time.RFC3339),
		"project":    "div-store",
	}
	for _, col := range collections {
		rows, err := dumpCollection(ctx, db, col)
		if err != nil {
			snapshot[col] = []any{}
			continue
		}
		snapshot[col] = rows
	}
	b, err := json.MarshalIndent(snapshot, "", "  ")
	return b, snapshot, err
}

// MaybePush exports data; only writes to GitHub if payload size >= 1MB
// OR force=true (manual admin trigger).
func (d *DataSync) MaybePush(ctx context.Context, db *firestore.Client, force bool) (map[string]any, error) {
	if !d.Enabled() {
		return nil, fmt.Errorf("github data sync not configured")
	}
	if db == nil {
		return nil, fmt.Errorf("firestore not ready")
	}
	raw, snapshot, err := d.buildSnapshot(ctx, db)
	if err != nil {
		return nil, err
	}
	size := int64(len(raw))
	d.mu.Lock()
	d.lastSize = size
	d.mu.Unlock()

	result := map[string]any{
		"size":      size,
		"threshold": d.Threshold,
		"pushed":    false,
		"reason":    "below_1MB",
	}
	if !force && size < d.Threshold {
		log.Printf("[datasync] size=%d < threshold=%d — skip push", size, d.Threshold)
		return result, nil
	}

	// write snapshot + per-collection files
	msg := fmt.Sprintf("data sync size=%d @ %s", size, time.Now().UTC().Format(time.RFC3339))
	if err := d.putFile("data/snapshot.json", raw, msg); err != nil {
		return result, err
	}
	for _, col := range []string{"apps", "categories", "reviews", "submissions", "developer_profiles", "settings", "users"} {
		part, _ := json.MarshalIndent(snapshot[col], "", "  ")
		_ = d.putFile("data/"+col+".json", part, msg+" · "+col)
	}
	d.mu.Lock()
	d.lastPush = time.Now().UTC()
	d.mu.Unlock()
	result["pushed"] = true
	if force {
		result["reason"] = "forced"
	} else {
		result["reason"] = "size>=1MB"
	}
	result["repo"] = d.Owner + "/" + d.Repo
	log.Printf("[datasync] PUSHED size=%d → %s/%s", size, d.Owner, d.Repo)
	return result, nil
}

// AfterWrite should be called after any mutation that grows user data.
// Runs size check in background; pushes only if >= 1MB.
func (d *DataSync) AfterWrite(db *firestore.Client) {
	if !d.Enabled() || db == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		if _, err := d.MaybePush(ctx, db, false); err != nil {
			log.Printf("[datasync] after-write: %v", err)
		}
	}()
}
