#!/usr/bin/env bash
set -euo pipefail
# The deployment build itself is the gate. A missing fixture database is a failure.
: "${TEST_DATABASE_URL:?A separate PostgreSQL test database is required for the deployment gate}"
export REQUIRE_HOSTED_TESTS=1
node --check static/app.js
env -u GOOS -u GOARCH go vet ./...
env -u GOOS -u GOARCH go test -race ./...
go build -trimpath -o "${VERCEL_OUTPUT_FILE:-server}" .
