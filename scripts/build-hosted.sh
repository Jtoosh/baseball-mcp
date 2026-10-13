#!/usr/bin/env bash
set -euo pipefail
# The deployment build itself is the gate. A missing fixture database is a failure.
: "${TEST_DATABASE_URL:?A separate PostgreSQL test database is required for the deployment gate}"
export REQUIRE_HOSTED_TESTS=1
node --check static/app.js
env -u GOOS -u GOARCH go vet ./...
# Vercel sets CGO_ENABLED=0 for its production Go build. Linux race tests
# require CGO, so override it only for the host-side verification command.
env -u GOOS -u GOARCH CGO_ENABLED=1 go test -race ./...
go build -trimpath -o "${VERCEL_OUTPUT_FILE:-server}" .
