#!/usr/bin/env bash
# Configure repository to use local git hooks from .githooks/
set -euo pipefail
cd "$(dirname "$0")/.."

git config core.hooksPath .githooks
chmod +x .githooks/* || true

echo "Git hooks installed. Pre-commit will auto-format Go files."