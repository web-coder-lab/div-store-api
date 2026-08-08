package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/husdainshah2-web/div-store/internal/firebase"
	"google.golang.org/api/iterator"
)

func hashPass(p string) string {
	sum := sha256.Sum256([]byte("div-store|" + p))
	return hex.EncodeToString(sum[:])
}

func newToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func publicUser(d map[string]any) map[string]any {
	return map[string]any{
		"id":          asInt(d["id"]),
		"email":       d["email"],
		"name":        d["name"],
		"role":        d["role"],
		"company":     d["company"],
		"createdAt":   d["createdAt"],
		"lastLoginAt": d["lastLoginAt"],
	}
}

// POST /api/auth/register
func Register(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Name     string `json:"name"`
		Role     string `json:"role"`
		Company  string `json:"company"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "Invalid body.")
		return
	}
	email := strings.ToLower(strings.TrimSpace(body.Email))
	name := strings.TrimSpace(body.Name)
	if email == "" || body.Password == "" || name == "" {
		writeErr(w, 400, "email, password, name required.")
		return
	}
	if len(body.Password) < 6 {
		writeErr(w, 400, "password min 6 chars.")
		return
	}
	role := body.Role
	if role != "company" {
		role = "user"
	}
	ctx := r.Context()
	db := firebase.Client()
	if db == nil {
		writeErr(w, 503, "Firebase not ready. Create Firestore database first.")
		return
	}
	it := db.Collection("users").Where("email", "==", email).Limit(1).Documents(ctx)
	if _, err := it.Next(); err != iterator.Done {
		writeErr(w, 409, "Email already registered.")
		return
	}
	id, err := firebase.NextID(ctx, "users")
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	token := newToken()
	now := time.Now().UTC().Format(time.RFC3339)
	row := map[string]any{
		"id": id, "email": email, "passwordHash": hashPass(body.Password),
		"name": name, "role": role, "company": strings.TrimSpace(body.Company),
		"token": token, "createdAt": now, "updatedAt": now, "lastLoginAt": now,
	}
	_, err = db.Collection("users").Doc(strconv.FormatInt(id, 10)).Set(ctx, row)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	triggerDataSync()
	writeJSON(w, 201, map[string]any{"ok": true, "token": token, "user": publicUser(row)})
}

// POST /api/auth/login — same account after APK reinstall
func Login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "Invalid body.")
		return
	}
	email := strings.ToLower(strings.TrimSpace(body.Email))
	if email == "" || body.Password == "" {
		writeErr(w, 400, "email and password required.")
		return
	}
	ctx := r.Context()
	db := firebase.Client()
	if db == nil {
		writeErr(w, 503, "Firebase not ready")
		return
	}
	docs, err := db.Collection("users").Where("email", "==", email).Limit(1).Documents(ctx).GetAll()
	if err != nil || len(docs) == 0 {
		writeErr(w, 401, "Invalid email or password.")
		return
	}
	d := docs[0].Data()
	if asString(d["passwordHash"]) != hashPass(body.Password) {
		writeErr(w, 401, "Invalid email or password.")
		return
	}
	token := newToken()
	now := time.Now().UTC().Format(time.RFC3339)
	_, _ = docs[0].Ref.Set(ctx, map[string]any{
		"token": token, "lastLoginAt": now, "updatedAt": now,
	}, firestore.MergeAll)
	d["token"] = token
	d["lastLoginAt"] = now
	triggerDataSync()
	writeJSON(w, 200, map[string]any{"ok": true, "token": token, "user": publicUser(d)})
}

// GET /api/auth/me
func Me(w http.ResponseWriter, r *http.Request) {
	u, err := userFromAuth(r)
	if err != nil {
		writeErr(w, 401, "Unauthorized")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "user": publicUser(u)})
}

func userFromAuth(r *http.Request) (map[string]any, error) {
	auth := r.Header.Get("Authorization")
	token := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
	if token == "" {
		return nil, errors.New("no token")
	}
	db := firebase.Client()
	if db == nil {
		return nil, errors.New("no db")
	}
	docs, err := db.Collection("users").Where("token", "==", token).Limit(1).Documents(r.Context()).GetAll()
	if err != nil || len(docs) == 0 {
		return nil, errors.New("invalid token")
	}
	return docs[0].Data(), nil
}
