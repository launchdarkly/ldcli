#!/usr/bin/env bash

DIFF=$(git diff internal/awsdevops/skills internal/awsdevops/agents)
if [ "$DIFF" ]; then
  echo "The AWS DevOps Agent files differ from launchdarkly/ai-tooling. Run 'make aws-devops-agent-sync' and commit the files."
  exit 1
else
  echo "The AWS DevOps Agent files match launchdarkly/ai-tooling."
fi
