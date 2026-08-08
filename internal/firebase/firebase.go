package firebase

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"cloud.google.com/go/firestore"
	firebase "firebase.google.com/go/v4"
	"google.golang.org/api/option"
)

var (
	once   sync.Once
	client *firestore.Client
	proj   string
	errInit error
)

func loadCredsJSON() ([]byte, error) {
	if raw := os.Getenv("FIREBASE_SERVICE_ACCOUNT_JSON"); raw != "" {
		if json.Valid([]byte(raw)) {
			return []byte(raw), nil
		}
		if b, err := base64.StdEncoding.DecodeString(raw); err == nil && json.Valid(b) {
			return b, nil
		}
	}
	for _, p := range []string{"Tiktok.txt", "./Tiktok.txt"} {
		if b, err := os.ReadFile(p); err == nil {
			s := string(b)
			// trim whitespace
			for len(s) > 0 && (s[0] == ' ' || s[0] == '\n' || s[0] == '\r' || s[0] == '\t') {
				s = s[1:]
			}
			for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\n' || s[len(s)-1] == '\r' || s[len(s)-1] == '\t') {
				s = s[:len(s)-1]
			}
			if dec, err := base64.StdEncoding.DecodeString(s); err == nil && json.Valid(dec) {
				return dec, nil
			}
		}
	}
	for _, p := range []string{"firebase-service-account.json", "./firebase-service-account.json"} {
		if b, err := os.ReadFile(p); err == nil && json.Valid(b) {
			return b, nil
		}
	}
	return nil, fmt.Errorf("firebase credentials missing")
}

func Init() error {
	once.Do(func() {
		creds, err := loadCredsJSON()
		if err != nil {
			errInit = err
			return
		}
		var meta map[string]any
		_ = json.Unmarshal(creds, &meta)
		if pid, ok := meta["project_id"].(string); ok {
			proj = pid
		}
		ctx := context.Background()
		app, err := firebase.NewApp(ctx, &firebase.Config{ProjectID: proj}, option.WithCredentialsJSON(creds))
		if err != nil {
			errInit = err
			return
		}
		client, err = app.Firestore(ctx)
		if err != nil {
			errInit = err
			return
		}
	})
	return errInit
}

func Client() *firestore.Client {
	return client
}

func ProjectID() string {
	return proj
}

func Status() map[string]any {
	ok := client != nil && errInit == nil
	out := map[string]any{"ok": ok, "projectId": proj}
	if errInit != nil {
		out["error"] = errInit.Error()
	}
	return out
}

func NextID(ctx context.Context, counter string) (int64, error) {
	ref := client.Collection("_counters").Doc(counter)
	var next int64
	err := client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(ref)
		cur := int64(0)
		if err == nil {
			if v, err2 := doc.DataAt("value"); err2 == nil {
				switch t := v.(type) {
				case int64:
					cur = t
				case int:
					cur = int64(t)
				case float64:
					cur = int64(t)
				}
			}
		}
		next = cur + 1
		return tx.Set(ref, map[string]any{"value": next}, firestore.MergeAll)
	})
	return next, err
}
