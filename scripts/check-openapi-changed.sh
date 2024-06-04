#!/usr/bin/env bash

DIFF=$(git diff ld-openapi.json)
if [ "$DIFF" ]; then
  echo "The OpenAPI spec has been changed. Run 'make openapi-spec-update'."
  exit 1
else
  echo "The OpenAPI has not changed."
fi
