# Repo layout — never mix

| Repo | Purpose | What goes here |
|------|---------|----------------|
| `web-coder-lab/div-store-data` | **User / catalog database** | `db/*.json` only |
| `web-coder-lab/div-store-apks-01` | **APK storage pool 1** | Release `.apk` files only |
| `web-coder-lab/div-store-apks-02` | **APK storage pool 2** | Release `.apk` files only |

One token (`GITHUB_STORAGE_TOKEN`) can access all three.
Server code routes:
- catalog writes → **data repo only**
- APK uploads → **apks-01 / apks-02 only**

## Render environment
```
GITHUB_STORAGE_TOKEN=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
GITHUB_STORAGE_OWNER=web-coder-lab
GITHUB_DATA_REPO=div-store-data
GITHUB_STORAGE_PREFIX=div-store-apks
ADMIN_API_KEY=your-admin-secret
```
