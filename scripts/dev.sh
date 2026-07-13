#!/bin/sh
# Run the Go API server for local development.
# Requires: firebase emulators:start already running in another terminal,
# and a filled-in .env (cp .env.example .env — see that file for what's needed,
# including a Firebase service account key).

set -a
[ -f .env ] && . ./.env
set +a

# Not emulated — Auth always verifies against your real Firebase project (see
# .env.example), so make sure this isn't left pointing at a local emulator.
unset FIREBASE_AUTH_EMULATOR_HOST

export FIRESTORE_EMULATOR_HOST="${FIRESTORE_EMULATOR_HOST:-127.0.0.1:8080}"
export PORT="${PORT:-8081}"

exec go run ./cmd/server
