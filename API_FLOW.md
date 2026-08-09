# Div Store — publish flow (Flutter)

## 0. Developer Studio (required before APK upload)
`POST /api/developers/register`
- multipart or JSON: `companyName`, `description`, `email`, optional `companyIcon` file or `iconUrl`
- Response: developer profile + slug

`GET /api/developers/by-email?email=`

Hide APK upload UI until this succeeds.

## 1–4 Submit pages
`POST /api/submit` multipart:

| Field | Notes |
|-------|--------|
| `icon` file **or** `iconUrl` | one required |
| `apk` file **.apk only** **or** `apkUrl`/`downloadUrl` | one required |
| `appName`, `packageName`, `description` | required |
| `categories` | JSON array or comma names, **max 4**, must exist in admin list |
| `developerEmail` | must match Studio account |

Server: validates `.apk` → uploads to GitHub Releases → saves release URL on submission.

## Categories
`GET /api/categories` — show names; user picks up to 4.

## Install
`POST /api/apps/:id/download` → `{ downloadUrl }` (GitHub Release)

## Admin
Approve submission → app gets `downloadUrl` + `categoryNames` (max 4)

## IDs (no mix)
| Kind | Prefix | Example | Collection |
|------|--------|---------|------------|
| Published APK/app | `apk_` | `apk_1` | `apps` |
| User submit request | `req_` | `req_1` | `submissions` |
| Category | `cat_` | `cat_8` | `categories` |
| Review | `rev_` | `rev_1` | `reviews` |

Approve: `POST /api/admin/submissions/req_1/approve` → creates `apk_N`, sets `fromRequestId`.
