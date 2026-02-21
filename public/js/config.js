// =========================================================
// config.js — Project configuration.
// Fill in FIREBASE_CONFIG before deploying.
// Get values from: Firebase Console → Project Settings → Your Apps
// =========================================================

const FIREBASE_CONFIG = {
  apiKey:            "AIzaSyCatUYtIxlkhR8YVvsSOdLvncbD4yr9JvY",
  authDomain:        "andy-personal-1bb38.firebaseapp.com",
  projectId:         "andy-personal-1bb38",
  storageBucket:     "andy-personal-1bb38.firebasestorage.app",
  messagingSenderId: "439489396970",
  appId:             "1:439489396970:web:7e4ff585342fc9b5481793",
};

// API base URL — auto-detected from hostname.
// Local dev: Go server on port 8081.
// Production: empty string so /api/* paths go through Firebase Hosting → Cloud Run rewrite.
const API_BASE = (location.hostname === 'localhost' || location.hostname === '127.0.0.1')
  ? 'http://localhost:8081'
  : '';
