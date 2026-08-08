# Div Store — Phase 1 (Foundation)

## Hard verify: pnpm Crock ↔ Go

| Area | pnpm | Go | Status |
|------|------|-----|--------|
| categories | ✅ | ✅ | match |
| apps + reviews + download | ✅ | ✅ | match |
| developers | ✅ | ✅ | match |
| submit (company) | ✅ | ✅ | match |
| admin stats/apps/submissions/settings | ✅ | ✅ | match |
| user auth register/login | ❌ | ❌ | correctly absent |
| GitHub APK releases | (new) | ✅ | Phase 1 storage |
| GitHub data backup ≥1MB | (new) | ✅ | Phase 1 backup |

## What is the “database”?

| Layer | Role |
|-------|------|
| **Firebase Firestore** | **Primary live DB** — reads/writes for apps, categories, submissions, etc. |
| **GitHub `div-store-data`** | **Durable backup / scan source** — JSON snapshot when size ≥ 1MB (or force sync) |
| **GitHub Releases `apks-01/02`** | **APK blob store** — binaries + public download URLs |

GitHub is **not** a transactional database. It is backup + artifact storage.

## Phase 1 security
- Admin key: constant-time compare
- Rate limit: 120 req/min/IP general, 40/min admin writes
- Body limit: 32MB (APK upload separate multipart 512MB)
- Headers: nosniff, frame deny, HSTS when HTTPS
- CORS: configured

## Phase 2 (later)
- Firestore security rules / tighter admin rotation
- Signed download tokens
- Index/query optimization
- Optional restore-from-GitHub scan pipeline UI
