#!/bin/sh
# Seed the Firestore emulator with test players.
# Requires: firebase emulators:start already running.

export FIREBASE_PROJECT_ID=andy-personal-1bb38
export FIRESTORE_EMULATOR_HOST=127.0.0.1:8080

exec go run ./cmd/seed
