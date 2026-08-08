# Div Store — Go API + Mobile Admin

## Architecture
- **Firebase Firestore** → users, apps metadata, categories, reviews, submissions
- **GitHub Releases** → APK binary storage (`web-coder-lab/div-store-apks-XX`)
  - Max **~3 GB** per repo (release assets)
  - Auto-create next repo when full

## Run
```bash
export PORT=8080
export ADMIN_API_KEY=your-secret
export GITHUB_STORAGE_TOKEN=ghp_xxx
export GITHUB_STORAGE_OWNER=web-coder-lab
export GITHUB_STORAGE_PREFIX=div-store-apks
go run .
```

- Admin UI: `/admin`
- Health: `/api/health`
- Upload APK: `POST /api/admin/upload-apk` (Bearer admin key, multipart `file`)

## Render env
- `ADMIN_API_KEY`
- `GITHUB_STORAGE_TOKEN`
- `GITHUB_STORAGE_OWNER=web-coder-lab`
- `PORT`
