# Div Store — Go API + Mobile Admin

Server **Go** + Admin panel **HTML/CSS/JS** (bottom nav, APK-style).

## Run
```bash
export PORT=8080
export ADMIN_API_KEY=your-secret
go run .
```

- API: `/api/*`
- Admin: `/admin`

## Firebase
`Tiktok.txt` (base64) or `firebase-service-account.json` or env `FIREBASE_SERVICE_ACCOUNT_JSON`.

## Render
Build: `go build -o div-store .`
Start: `./div-store`
