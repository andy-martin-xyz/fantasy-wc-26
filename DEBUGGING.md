# Local Dev Debugging Notes

> Recorded after Phase 3 bring-up (2026-02-19)

---

## How to Run Locally

**Three terminals required (must start in this order):**

```bash
# Terminal 1 — Firebase emulators (Auth + Firestore)
firebase emulators:start

# Terminal 2 — Go API server
./dev.sh          # sets all env vars and runs on port 8081

# Terminal 3 — Static file server
python3 -m http.server 3000
```

- App: http://localhost:3000
- Emulator UI: http://127.0.0.1:4000

---

## Issues Found and Fixed

### 1. Firestore emulator not included in firebase.json
**Symptom:** All Firestore operations timed out (context deadline exceeded after 30s).
**Root cause:** `firebase init emulators` only enabled Auth + Hosting. Port 8080 was the *Hosting* emulator, not Firestore.
**Fix:** Added `"firestore": { "port": 8080 }` to `firebase.json` emulators block, removed `hosting` emulator entry (it moved to 5002). Java is required for the Firestore emulator — install with `brew install openjdk`.

### 2. Firebase Auth emulator env var set in shell
**Symptom:** `POST /api/user/register` returned 401 "no user exists with the uid: ..." even with a valid real Firebase token.
**Root cause:** `FIREBASE_AUTH_EMULATOR_HOST=localhost:9099` was set in the shell (from a previous `firebase emulators:start` session). The Go Admin SDK was verifying tokens against the Auth emulator, which had no record of the real Google-authenticated user.
**Fix:** `unset FIREBASE_AUTH_EMULATOR_HOST` before starting the Go server. The `dev.sh` script explicitly unsets this.

### 3. Firestore client connected via Firebase Admin SDK ignored emulator
**Symptom:** Even with `FIRESTORE_EMULATOR_HOST=127.0.0.1:8080` set, Firestore operations timed out.
**Root cause:** The Firebase Admin SDK (`app.Firestore(ctx)`) passed credential options that conflicted with the emulator's insecure gRPC connection.
**Fix:** When `FIRESTORE_EMULATOR_HOST` is set, create the Firestore client directly via `firestore.NewClient(ctx, projectID)` (no credentials), bypassing the Firebase Admin SDK path.

### 4. FIRESTORE_EMULATOR_HOST used `localhost` instead of `127.0.0.1`
**Symptom:** Firestore still timed out even after fix #3.
**Root cause:** `localhost` resolved to `::1` (IPv6) but the Firestore emulator node process only bound to IPv4 (`127.0.0.1`).
**Fix:** Use `FIRESTORE_EMULATOR_HOST=127.0.0.1:8080` explicitly.

### 5. Go server port conflicted with Firestore emulator
**Symptom:** CORS errors with 404s — browser requests hit the emulator instead of Go API.
**Root cause:** Both Go server and Firestore emulator defaulted to port 8080.
**Fix:** Run Go server on port 8081 (`PORT=8081`). The `dev.sh` script handles this.

### 6. RegisterUser called GetUser (live Firebase Auth API)
**Symptom:** Register endpoint timed out — no response, then CORS null-status error in browser.
**Root cause:** `handlers/user.go` called `h.Auth.GetUser(ctx, uid)` to fetch display name/email/photo. This is a blocking HTTP call to Firebase Auth API that wasn't needed.
**Fix:** Auth middleware now stores `name`, `email`, `picture` claims from the already-verified JWT into the request context. `RegisterUser` reads them from context — no extra API call needed.

### 7. CORS middleware rejected requests despite localhost origin
**Symptom:** "CORS header 'Access-Control-Allow-Origin' missing" in browser.
**Root cause:** The original CORS middleware only set headers when `origin` exactly matched `allowedOrigin` (env var) or started with `http://localhost`. Debugging showed the logic was correct, but the deeper issue was the emulator/port issues above masking CORS responses.
**Fix:** Simplified CORS middleware to allow any request that sends an Origin header (`origin != ""`). Fine for local dev; production is locked to `ALLOWED_ORIGIN` env var.

---

## Code Changes Made (beyond Phase 3 plan)

| File | Change |
|------|--------|
| `cmd/server/main.go` | CORS middleware: allow any origin with a header; accept logger param; debug-level logging |
| `internal/db/firestore.go` | When `FIRESTORE_EMULATOR_HOST` set, use `firestore.NewClient` directly (no Admin SDK credentials) |
| `internal/middleware/auth.go` | Store `name`, `email`, `picture` JWT claims in context; expose `EmailFromContext`, `DisplayNameFromContext`, `PhotoURLFromContext` |
| `internal/handlers/user.go` | Remove `h.Auth.GetUser` call; pull user info from context claims instead |
| `firebase.json` | Replace `hosting` emulator with `firestore` emulator on port 8080 |
| `dev.sh` | New script — sets all env vars and starts Go server cleanly |
| `js/config.js` | `API_BASE` set to `http://localhost:8081` for local dev |

---

## Environment Variables (working local setup)

```bash
unset FIREBASE_AUTH_EMULATOR_HOST         # must NOT be set
export FIREBASE_PROJECT_ID=andy-personal-1bb38
export GOOGLE_APPLICATION_CREDENTIALS=/Users/andrewmartin/keys/fantasy-wc-sa.json
export FIRESTORE_EMULATOR_HOST=127.0.0.1:8080   # 127.0.0.1 not localhost!
export PORT=8081
```

The `dev.sh` script sets all of these automatically.

---

## Setting Yourself as Admin

After first sign-in:
1. Open http://127.0.0.1:4000/firestore
2. `users` collection → your UID doc
3. Add field: `isAdmin` = `true` (boolean)
