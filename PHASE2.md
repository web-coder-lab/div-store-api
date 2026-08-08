# Phase 2 — GitHub as database

## Primary store
- Repo: `web-coder-lab/div-store-data`
- Paths: `db/apps.json`, `db/categories.json`, `db/reviews.json`, `db/submissions.json`, `db/developer_profiles.json`, `db/settings.json`, `db/_counters.json`
- Server holds **no durable catalog data** on disk

## APK blobs
- GitHub Releases on `div-store-apks-01` / `02`
- App records store `downloadUrl` pointing at release assets

## Required env
```
GITHUB_STORAGE_TOKEN=
GITHUB_STORAGE_OWNER=web-coder-lab
GITHUB_DATA_REPO=div-store-data
GITHUB_STORAGE_PREFIX=div-store-apks
ADMIN_API_KEY=
```

## Legal
- `/terms` · `/privacy`
