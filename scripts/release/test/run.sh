#!/usr/bin/env bash
# Unit tests for the release verifier (inventory, plan, verdict, CLI surface).
# No network, Go, or GitHub needed. Run: scripts/release/test/run.sh
set -euo pipefail
cd "$(dirname "$0")"
python3 -m unittest -v test_release
