#!/usr/bin/env bash
# Simple build script for SDSM
# - Injects version metadata via -ldflags
# - Supports cross-compiling with GOOS/GOARCH
# - Outputs binaries to ./dist

set -euo pipefail

# Resolve repo root (script lives in ./tools)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "$REPO_ROOT"

# Keep toolchain behavior consistent across shells/tasks.
# Some environments export GOTOOLCHAIN=local, which can fail when local Go
# is older than go.mod requirements. Force automatic toolchain selection.
export GOTOOLCHAIN=auto

# Go target platform (override by exporting GOOS/GOARCH before running)
GOOS="${GOOS:-$(go env GOOS)}"
GOARCH="${GOARCH:-$(go env GOARCH)}"

EXT=""
if [[ "$GOOS" == "windows" ]]; then
  EXT=".exe"
fi
ARTIFACT="sdsm${EXT}" #-${GOOS}-${GOARCH}${EXT}"
mkdir -p "./dist"
OUT_PATH="./dist/${ARTIFACT}"

# Git-derived metadata (best effort; falls back to sensible dev defaults)
VERSION="0.0.1"
COMMIT=""
DATE="$(date -u +'%Y-%m-%dT%H:%M:%SZ')"
DIRTY="clean"

if command -v git >/dev/null 2>&1 && git rev-parse --git-dir >/dev/null 2>&1; then
  # Version: exact tag on HEAD, else empty for dev builds
  if git describe --tags --exact-match >/dev/null 2>&1; then
    VERSION="$(git describe --tags --exact-match)"
  fi
  # Commit: short SHA
  COMMIT="$(git rev-parse --short HEAD 2>/dev/null || true)"
  # Dirty: mark if there are uncommitted changes
  if ! git diff-index --quiet HEAD -- 2>/dev/null; then
    DIRTY="dirty"
  fi
fi

# ldflags wiring to sdsm/app/backend/internal/version
LDFLAGS=(
  "-s" "-w"
  "-X" "sdsm/app/backend/internal/version.Version=${VERSION}"
  "-X" "sdsm/app/backend/internal/version.Commit=${COMMIT}"
  "-X" "sdsm/app/backend/internal/version.Date=${DATE}"
  "-X" "sdsm/app/backend/internal/version.Dirty=${DIRTY}"
)

# Display a brief build header
printf "\nBuilding SDSM %s (%s) for %s/%s [%s]\n" \
  "${VERSION:-dev}" "${COMMIT:-local}" "${GOOS}" "${GOARCH}" "${DIRTY}"

# Ensure modules are available
go mod download

# Build
GOOS="$GOOS" GOARCH="$GOARCH" \
  go build -trimpath -ldflags "${LDFLAGS[*]}" -o "$OUT_PATH" ./app/backend/cmd/sdsm

# Done
printf "\n✅ Built %s\n" "$OUT_PATH"

# Optional: print embedded version string if the binary supports it later
exit 0