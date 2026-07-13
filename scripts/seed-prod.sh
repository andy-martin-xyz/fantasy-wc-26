#!/bin/sh
# Seed PRODUCTION Firestore with test players.
# Only run this once before draft night. Requires a filled-in .env.

set -a
[ -f .env ] && . ./.env
set +a

export SEED_PRODUCTION=true

exec go run ./cmd/seed
