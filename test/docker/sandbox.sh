#!/usr/bin/env bash
# Drop into a throwaway Node project for manually walking `ldcli setup`.
set -euo pipefail

if [ -z "${LD_ACCESS_TOKEN:-}" ]; then
  echo "warning: LD_ACCESS_TOKEN is not set." >&2
  echo "The setup wizard talks to the LaunchDarkly API (list projects/environments," >&2
  echo "create a flag). Re-run with:  docker run --rm -it -e LD_ACCESS_TOKEN=<token> ldcli-test sandbox" >&2
  echo >&2
fi

cat <<'EOF'
ldcli setup sandbox
-------------------
You are in /work/sample-node, a disposable Node project (express dependency,
index.js entry point) so the wizard detects the Node SDK path.

Try:
  ldcli setup            # the guided TUI wizard
  ldcli setup detect     # non-interactive: just the detection step
  ldcli setup install    # non-interactive: install the detected SDK

Inspect what the wizard changed:
  git diff --no-index /dev/null index.js   # or just cat the files
Nothing here touches your host; exit and the container is gone.
EOF

cd /work/sample-node
exec bash
