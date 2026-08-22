#!/usr/bin/env bash
# Simple build script for SDSM
# - Injects version metadata via -ldflags
# - Supports cross-compiling with GOOS/GOARCH
# - Outputs binaries to ./dist/

set -euo pipefail

# Resolve repo root (location of this script)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# Go target platform (override by exporting GOOS/GOARCH before running)
GOOS="${GOOS:-$(go env GOOS)}"
GOARCH="${GOARCH:-$(go env GOARCH)}"

# Output directory and artifact name
OUT_DIR="dist"
mkdir -p "$OUT_DIR"

EXT=""
if [[ "$GOOS" == "windows" ]]; then
  EXT=".exe"
fi
ARTIFACT="sdsm" #-${GOOS}-${GOARCH}${EXT}"
OUT_PATH="${OUT_DIR}/${ARTIFACT}"

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
