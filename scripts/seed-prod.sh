#!/bin/sh
# Seed PRODUCTION Firestore with test players.
# Only run this once before draft night.

export FIREBASE_PROJECT_ID=andy-personal-1bb38
export GOOGLE_APPLICATION_CREDENTIALS=/Users/andrewmartin/keys/fantasy-wc-sa.json
export SEED_PRODUCTION=true

exec go run ./cmd/seed
