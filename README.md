# Div Store API (Go)

## Auth (mobile)
- `POST /api/auth/register` `{email,password,name,role?,company?}`
- `POST /api/auth/login` `{email,password}` → same account after reinstall
- `GET /api/auth/me` `Authorization: Bearer <token>`

## Apps / download
- List/detail/reviews same as before
- `POST /api/apps/:id/download` → `{downloadUrl}` = **GitHub Release** URL
- `POST /api/admin/upload-apk` multipart `file` + optional `appId` → Release + link on app

## Data backup
- Firebase live
- Push to `web-coder-lab/div-store-data` only when snapshot **≥ 1MB** (or `POST /api/admin/sync-data`)

## APK storage
- `div-store-apks-01` / `02` GitHub Releases (~3GB each)

## Env
ADMIN_API_KEY, GITHUB_STORAGE_TOKEN, GITHUB_STORAGE_OWNER, GITHUB_DATA_REPO
