package ghdb

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/husdainshah2-web/div-store/internal/config"
	"sync"
	"time"
)

// Store: private GitHub repo as sole JSON data store. No server-side durable DB.
type Store struct {
	Owner  string
	Repo   string
	Token  string
	Branch string
	HTTP   *http.Client
	mu     sync.Mutex
}

func NewFromEnv() *Store {
	return &Store{
		Owner:  config.Owner(),
		Repo:   config.DataRepo(), // ONLY div-store-data — never APK repos
		Token:  config.Token(),
		Branch: "main",
		HTTP:   &http.Client{Timeout: 60 * time.Second},
	}
}

func (s *Store) Enabled() bool {
	return s.Token != "" && s.Owner != "" && s.Repo != ""
}

func (s *Store) Status() map[string]any {
	return map[string]any{
		"enabled": s.Enabled(), "owner": s.Owner, "repo": s.Repo,
		"branch": s.Branch, "mode": "github-json-db", "path": "db/",
		"purpose": "catalog_and_user_data_only",
		"note":    "APK binaries must never be written to this repo",
	}
}

func (s *Store) api(method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, "https://api.github.com"+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return s.HTTP.Do(req)
}

func stripWS(s string) string {
	var out []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != ' ' && c != '\n' && c != '\r' && c != '\t' {
			out = append(out, c)
		}
	}
	return string(out)
}

type fileMeta struct {
	SHA     string
	Content []byte
}

func (s *Store) getFile(path string) (*fileMeta, error) {
	resp, err := s.api("GET", fmt.Sprintf("/repos/%s/%s/contents/%s?ref=%s", s.Owner, s.Repo, path, s.Branch), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == 404 {
		return &fileMeta{Content: []byte("[]")}, nil
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("get %s: %s %s", path, resp.Status, string(b))
	}
	var obj struct {
		SHA      string `json:"sha"`
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := json.Unmarshal(b, &obj); err != nil {
		return nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(stripWS(obj.Content))
	if err != nil {
		return nil, err
	}
	return &fileMeta{SHA: obj.SHA, Content: raw}, nil
}

func (s *Store) putFile(path string, content []byte, sha, message string) error {
	payload := map[string]any{
		"message": message,
		"content": base64.StdEncoding.EncodeToString(content),
		"branch":  s.Branch,
	}
	if sha != "" {
		payload["sha"] = sha
	}
	b, _ := json.Marshal(payload)
	resp, err := s.api("PUT", fmt.Sprintf("/repos/%s/%s/contents/%s", s.Owner, s.Repo, path), bytes.NewReader(b))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("put %s: %s %s", path, resp.Status, string(body))
	}
	return nil
}

func (s *Store) collPath(c string) string { return "db/" + c + ".json" }

func (s *Store) ReadAll(_ context.Context, collection string) ([]map[string]any, error) {
	if !s.Enabled() {
		return nil, fmt.Errorf("GITHUB_STORAGE_TOKEN not set — GitHub is the database")
	}
	meta, err := s.getFile(s.collPath(collection))
	if err != nil {
		return nil, err
	}
	if len(meta.Content) == 0 {
		return []map[string]any{}, nil
	}
	var rows []map[string]any
	if err := json.Unmarshal(meta.Content, &rows); err != nil {
		return []map[string]any{}, nil
	}
	if rows == nil {
		rows = []map[string]any{}
	}
	return rows, nil
}

func (s *Store) WriteAll(_ context.Context, collection string, rows []map[string]any, message string) error {
	if !s.Enabled() {
		return fmt.Errorf("GITHUB_STORAGE_TOKEN not set")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	meta, err := s.getFile(s.collPath(collection))
	if err != nil {
		return err
	}
	if rows == nil {
		rows = []map[string]any{}
	}
	raw, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return err
	}
	if message == "" {
		message = fmt.Sprintf("update %s @ %s", collection, time.Now().UTC().Format(time.RFC3339))
	}
	return s.putFile(s.collPath(collection), raw, meta.SHA, message)
}

func (s *Store) NextID(_ context.Context, name string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := "db/_counters.json"
	meta, err := s.getFile(path)
	if err != nil {
		return 0, err
	}
	counters := map[string]int64{}
	if len(meta.Content) > 2 {
		_ = json.Unmarshal(meta.Content, &counters)
	}
	counters[name] = counters[name] + 1
	raw, _ := json.MarshalIndent(counters, "", "  ")
	if err := s.putFile(path, raw, meta.SHA, "counter "+name); err != nil {
		return 0, err
	}
	return counters[name], nil
}

func (s *Store) UpsertByID(ctx context.Context, collection string, id int64, row map[string]any) error {
	rows, err := s.ReadAll(ctx, collection)
	if err != nil {
		return err
	}
	found := false
	for i := range rows {
		if ToInt(rows[i]["id"]) == id {
			rows[i] = row
			found = true
			break
		}
	}
	if !found {
		rows = append(rows, row)
	}
	return s.WriteAll(ctx, collection, rows, fmt.Sprintf("upsert %s id=%d", collection, id))
}

func (s *Store) DeleteByID(ctx context.Context, collection string, id int64) error {
	rows, err := s.ReadAll(ctx, collection)
	if err != nil {
		return err
	}
	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		if ToInt(r["id"]) != id {
			out = append(out, r)
		}
	}
	return s.WriteAll(ctx, collection, out, fmt.Sprintf("delete %s id=%d", collection, id))
}

func (s *Store) GetByID(ctx context.Context, collection string, id int64) (map[string]any, error) {
	rows, err := s.ReadAll(ctx, collection)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if ToInt(r["id"]) == id {
			return r, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func ToInt(v any) int64 {
	switch t := v.(type) {
	case float64:
		return int64(t)
	case int64:
		return t
	case int:
		return int64(t)
	case json.Number:
		n, _ := t.Int64()
		return n
	case string:
		var n int64
		fmt.Sscanf(t, "%d", &n)
		return n
	default:
		return 0
	}
}

var (
	gMu sync.RWMutex
	gSt *Store
)

func SetGlobal(s *Store) { gMu.Lock(); gSt = s; gMu.Unlock() }
func Global() *Store     { gMu.RLock(); defer gMu.RUnlock(); return gSt }
