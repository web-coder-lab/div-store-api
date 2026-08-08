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
	"time"

	"cloud.google.com/go/firestore"
)

// DataSync exports Firestore snapshots to GitHub every few minutes.
type DataSync struct {
	Owner  string
	Repo   string
	Token  string
	Branch string
	HTTP   *http.Client
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
		Owner:  owner,
		Repo:   repo,
		Token:  token,
		Branch: "main",
		HTTP:   &http.Client{Timeout: 90 * time.Second},
	}
}

func (d *DataSync) Enabled() bool {
	return d.Token != "" && d.Owner != "" && d.Repo != ""
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

// RunOnce exports key collections to GitHub as JSON.
func (d *DataSync) RunOnce(ctx context.Context, db *firestore.Client) error {
	if !d.Enabled() || db == nil {
		return fmt.Errorf("sync disabled or no db")
	}
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
			// empty / missing collection — still record empty
			snapshot[col] = []any{}
			continue
		}
		snapshot[col] = rows
		// also write per-collection file
		b, _ := json.MarshalIndent(rows, "", "  ")
		path := fmt.Sprintf("data/%s.json", col)
		msg := fmt.Sprintf("sync %s @ %s", col, time.Now().UTC().Format(time.RFC3339))
		if err := d.putFile(path, b, msg); err != nil {
			log.Printf("[datasync] %v", err)
		}
	}
	full, _ := json.MarshalIndent(snapshot, "", "  ")
	return d.putFile("data/snapshot.json", full, "full snapshot @ "+time.Now().UTC().Format(time.RFC3339))
}

// StartBackground runs sync every interval (e.g. 3 minutes).
func (d *DataSync) StartBackground(db *firestore.Client, interval time.Duration) {
	if !d.Enabled() {
		log.Printf("[datasync] disabled (set GITHUB_STORAGE_TOKEN)")
		return
	}
	log.Printf("[datasync] every %s → %s/%s", interval, d.Owner, d.Repo)
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		// first run after short delay
		time.Sleep(15 * time.Second)
		ctx := context.Background()
		if err := d.RunOnce(ctx, db); err != nil {
			log.Printf("[datasync] first: %v", err)
		} else {
			log.Printf("[datasync] first OK")
		}
		for range t.C {
			if err := d.RunOnce(context.Background(), db); err != nil {
				log.Printf("[datasync] %v", err)
			} else {
				log.Printf("[datasync] OK")
			}
		}
	}()
}
