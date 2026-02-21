#!/bin/sh
# Run the Go API server for local development.
# Requires: firebase emulators:start already running in another terminal.

unset FIREBASE_AUTH_EMULATOR_HOST

export FIREBASE_PROJECT_ID=andy-personal-1bb38
export GOOGLE_APPLICATION_CREDENTIALS=/Users/andrewmartin/keys/fantasy-wc-sa.json
export FIRESTORE_EMULATOR_HOST=127.0.0.1:8080
export PORT=8081
export ADMIN_EMAILS=mandrew307@gmail.com
# export ANTHROPIC_API_KEY=sk-ant-...   # optional: enables AI pick suggestions

exec go run ./cmd/server
