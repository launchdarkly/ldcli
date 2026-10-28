#!/usr/bin/env bash
# Downloads the AWS DevOps Agent skill and custom agent prompt from
# launchdarkly/ai-tooling at a fixed commit. ldcli embeds the files, so they
# are committed here. To update them, change AI_TOOLING_COMMIT, run this
# script, and add the new hashes in internal/awsdevops.
set -euo pipefail

AI_TOOLING_COMMIT=bbdf962eb696d649d24ff128d4db0ff894c6943c
BASE_URL="https://raw.githubusercontent.com/launchdarkly/ai-tooling/${AI_TOOLING_COMMIT}/skills/aws-devops-agent"
ROOT=$(git rev-parse --show-toplevel)
DEST="${ROOT}/internal/awsdevops"

curl -fsSL -o "${DEST}/skills/experiment-orchestration/SKILL.md" "${BASE_URL}/experiment-orchestration/SKILL.md"
curl -fsSL -o "${DEST}/agents/experiment-orchestrator.md" "${BASE_URL}/agents/experiment-orchestrator.md"
