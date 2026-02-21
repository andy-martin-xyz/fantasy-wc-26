#!/bin/sh
# Generates public/js/config.js from environment variables.
# Runs automatically as an nginx startup hook in Docker (docker-entrypoint.d/).
# Also called manually for production deploys.
set -e

OUTPUT="${CONFIG_OUTPUT:-/usr/share/nginx/html/js/config.js}"

cat > "$OUTPUT" <<EOF
const FIREBASE_CONFIG = {
  apiKey:            "${FIREBASE_API_KEY}",
  authDomain:        "${FIREBASE_AUTH_DOMAIN}",
  projectId:         "${FIREBASE_PROJECT_ID}",
  storageBucket:     "${FIREBASE_STORAGE_BUCKET}",
  messagingSenderId: "${FIREBASE_MESSAGING_SENDER_ID}",
  appId:             "${FIREBASE_APP_ID}",
};

const API_BASE = (location.hostname === 'localhost' || location.hostname === '127.0.0.1')
  ? 'http://localhost:8081'
  : '';
EOF

echo "[config] Generated $OUTPUT"
