# Firebase Project Setup Guide

Complete these steps once before running Phase 3+.

---

## 1. Create a Firebase Project

1. Go to https://console.firebase.google.com
2. Click **Add project** → name it `fantasy-wc-2026` (or similar)
3. Disable Google Analytics (optional for a small app)
4. Click **Create project**

---

## 2. Enable Firestore

1. In the Firebase console, click **Firestore Database** → **Create database**
2. Choose **Native mode** (not Datastore mode)
3. Pick the closest region (e.g. `us-central`)
4. Start in **production mode** (locked down by default)

### Firestore security rules

Paste these rules in **Firestore → Rules**:

```
rules_version = '2';
service cloud.firestore {
  match /databases/{database}/documents {
    // Public read for leaderboard, teams, players, draft status
    match /leaderboard/{doc} { allow read: if true; }
    match /rosters/{uid}     { allow read: if true; }
    match /players/{pid}     { allow read: if true; }
    match /draft/{doc}       { allow read: if true; }

    // Users can read/write their own user doc
    match /users/{uid} {
      allow read:  if request.auth != null;
      allow write: if request.auth.uid == uid;
    }

    // All writes go through the Go API (server-side auth) — deny direct client writes
    match /{document=**} {
      allow write: if false;
    }
  }
}
```

---

## 3. Enable Google Sign-In

1. In Firebase console → **Authentication** → **Get started**
2. Under **Sign-in method**, enable **Google**
3. Set a support email → **Save**

---

## 4. Enable Firebase Hosting

```bash
npm install -g firebase-tools
firebase login
firebase init hosting
```

When prompted:
- **Public directory:** `.` (the project root)
- **Configure as single-page app:** No
- **Set up automatic builds with GitHub:** No (skip)

This creates `firebase.json` (already in repo) and `.firebaserc`.

---

## 5. Get the Frontend Config

1. Firebase console → **Project settings** (gear icon) → **Your apps**
2. Click **Add app** → Web (`</>`), name it `fantasy-wc-web`
3. Copy the `firebaseConfig` object
4. Paste it into `js/config.js`, replacing each `"REPLACE_ME"` value

```js
// js/config.js
const FIREBASE_CONFIG = {
  apiKey:            "AIzaSy...",
  authDomain:        "fantasy-wc-2026.firebaseapp.com",
  projectId:         "fantasy-wc-2026",
  storageBucket:     "fantasy-wc-2026.firebasestorage.app",
  messagingSenderId: "123456789",
  appId:             "1:123456789:web:abc123",
};
```

---

## 6. Create a Service Account (for Go API)

1. Firebase console → **Project settings** → **Service accounts**
2. Click **Generate new private key** → download the JSON file
3. Store it somewhere safe (e.g. `~/keys/fantasy-wc-sa.json`)
4. Set the environment variable for local dev:

```bash
export FIREBASE_PROJECT_ID=andy-personal-1bb38
export GOOGLE_APPLICATION_CREDENTIALS=~/keys/fantasy-wc-sa.json
```

For Cloud Run, grant the service account the **Cloud Datastore User** role instead of using a key file.

---

## 7. Run the Firestore Emulator (local dev)

```bash
firebase init emulators
# Enable: Firestore, Authentication
# Default ports: Firestore 8080, Auth 9099, UI 4000

firebase emulators:start
```

Then run the Go server in a separate terminal:

```bash
export FIREBASE_PROJECT_ID=andy-personal-1bb38
export FIRESTORE_EMULATOR_HOST=localhost:8080
export FIREBASE_AUTH_EMULATOR_HOST=localhost:9099
go run ./cmd/server
```

Open http://localhost:4000 for the Emulator UI.

---

## 8. Run the Frontend Locally

Open any HTML file directly in a browser, or use a simple server:

```bash
# Python (no install needed)
python3 -m http.server 3000

# Or npx serve
npx serve .
```

Set `API_BASE` in `js/config.js` to point at the local Go server:

```js
const API_BASE = 'http://localhost:8080';
```

---

## 9. Deploy to Firebase Hosting

```bash
firebase deploy --only hosting
```

The `firebase.json` already has a rewrite rule that sends `/api/**` to Cloud Run.
Update the `serviceId` in `firebase.json` to match your Cloud Run service name.

---

## 10. Set Your Account as Admin

After signing in for the first time, set your `isAdmin` flag manually in Firestore:

1. Firebase console → Firestore → `users` collection → your UID doc
2. Add field: `isAdmin` = `true` (boolean)

Or via the Firebase CLI:

```bash
firebase firestore:documents:update users/YOUR_UID --field isAdmin=true
```

---

## Environment Variables Summary

| Variable | Value | Where used |
|---|---|---|
| `FIREBASE_PROJECT_ID` | `fantasy-wc-2026` | Go server (required) |
| `GOOGLE_APPLICATION_CREDENTIALS` | path to service account JSON | Go server (local dev only) |
| `FIRESTORE_EMULATOR_HOST` | `localhost:8080` | Go server (local dev only) |
| `FIREBASE_AUTH_EMULATOR_HOST` | `localhost:9099` | Go server (local dev only) |
| `ANTHROPIC_API_KEY` | `sk-ant-...` | Go server (Phase 4.7 only) |
| `PORT` | `8080` | Go server (Cloud Run sets this automatically) |
| `ALLOWED_ORIGIN` | your Firebase Hosting URL | Go server (CORS) |
