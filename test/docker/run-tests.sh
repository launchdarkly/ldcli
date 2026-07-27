#!/usr/bin/env bash
# Run the full ldcli automated suite: Go tests plus the dev-server UI tests.
set -euo pipefail

echo "==> Go tests (go test ./...)"
go test ./...

echo
echo "==> dev-server UI tests (vitest)"
cd internal/dev_server/ui && npm test

echo
echo "All tests passed."
