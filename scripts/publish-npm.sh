#!/usr/bin/env bash
yarn workspace $WORKSPACE pack
if $LD_RELEASE_IS_DRYRUN ; then
  # Dry run just pack the workspace.
  echo "Doing a dry run of publishing."
else
  if $LD_RELEASE_IS_PRERELEASE ; then
    echo "Publishing with prerelease tag."
    npm publish --tag prerelease --provenance --access public "./$WORKSPACE_PATH/package.tgz" || { echo "npm publish failed" >&2; exit 1; }
  else
    npm publish --provenance --access public "./$WORKSPACE_PATH/package.tgz" || { echo "npm publish failed" >&2; exit 1; }
  fi
fi
