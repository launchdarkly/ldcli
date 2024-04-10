#!/usr/bin/env bash

# Given a path get the name of the package.
# ./scripts/package-name.sh packages/sdk/server-node
# Produces something like:
# @launchdarkly/node-server-sdk

set -e

node -p "require('./$1/package.json').name";
