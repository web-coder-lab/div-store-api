# Div Store API (Go) — matches Crock/pnpm store API

## Data model (same as original)
collections: `categories`, `developer_profiles`, `apps`, `reviews`, `settings`, `submissions`

## Public API
| Method | Path |
|--------|------|
| GET | `/api/health` |
| GET | `/api/apps` |
| GET | `/api/apps/:id` |
| GET/POST | `/api/apps/:id/reviews` |
| POST | `/api/apps/:id/download` → GitHub Release URL |
| GET/POST | `/api/categories` |
| DELETE | `/api/categories/:id` |
| GET | `/api/developers` |
| GET | `/api/developers/:slug` |
| POST | `/api/developers` (admin) |
| POST | `/api/submit` (company APK metadata submit) |

## Admin (Bearer ADMIN_API_KEY)
| Method | Path |
|--------|------|
| GET | `/api/admin/stats` |
| GET/POST | `/api/admin/apps` |
| PATCH/DELETE | `/api/admin/apps/:id` |
| GET | `/api/admin/submissions` |
| POST | `/api/admin/submissions/:id/approve\|reject` |
| GET | `/api/admin/settings` |
| PUT | `/api/admin/settings/:key` |
| POST | `/api/admin/upload-apk` → GitHub Release |
| GET | `/api/admin/storage` |
| POST | `/api/admin/sync-data` |

## GitHub
- Metadata backup → `web-coder-lab/div-store-data` when size ≥ 1MB
- APK binaries → Releases on `div-store-apks-01` / `02`

No invented user-auth endpoints (not in original store server).
