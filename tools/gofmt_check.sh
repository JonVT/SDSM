#!/usr/bin/env bash
# Fails if any Go files are not formatted with gofmt -s
set -euo pipefail
cd "$(dirname "$0")/.."
FILES=$(gofmt -l .)
if [[ -n "$FILES" ]]; then
  echo "The following files are not gofmt'd:" >&2
  echo "$FILES" >&2
  exit 1
fi
