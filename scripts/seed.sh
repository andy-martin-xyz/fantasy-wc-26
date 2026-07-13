#!/bin/sh
# Seed the Firestore emulator with test players.
# Requires: firebase emulators:start already running, and a filled-in .env.

set -a
[ -f .env ] && . ./.env
set +a

export FIRESTORE_EMULATOR_HOST="${FIRESTORE_EMULATOR_HOST:-127.0.0.1:8080}"

exec go run ./cmd/seed
