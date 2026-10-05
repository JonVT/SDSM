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

# Usage: tools/build.sh [--bump major|minor|patch]
#   --bump PART   Increment that part of the version in ./VERSION before building
#                 (major resets minor/patch; minor resets patch).
BUMP=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    -b|--bump)
      BUMP="${2:-}"
      if [[ -z "$BUMP" ]]; then echo "error: --bump requires major, minor, or patch" >&2; exit 2; fi
      shift 2 ;;
    --bump=*) BUMP="${1#--bump=}"; shift ;;
    -h|--help) sed -n '2,4p;/^# Usage/,/^$/p' "${BASH_SOURCE[0]}"; exit 0 ;;
    *) echo "error: unknown argument: $1" >&2; exit 2 ;;
  esac
done

VERSION_FILE="${REPO_ROOT}/VERSION"
if [[ -n "$BUMP" ]]; then
  CURRENT="$(tr -d '[:space:]' < "$VERSION_FILE" 2>/dev/null || true)"
  CURRENT="${CURRENT#v}"
  if [[ ! "$CURRENT" =~ ^([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
    echo "error: ${VERSION_FILE} must contain MAJOR.MINOR.PATCH (found '${CURRENT}')" >&2
    exit 2
  fi
  MAJ="${BASH_REMATCH[1]}"; MIN="${BASH_REMATCH[2]}"; PAT="${BASH_REMATCH[3]}"
  case "$BUMP" in
    major) MAJ=$((MAJ + 1)); MIN=0; PAT=0 ;;
    minor) MIN=$((MIN + 1)); PAT=0 ;;
    patch) PAT=$((PAT + 1)) ;;
    *) echo "error: --bump must be major, minor, or patch (got '${BUMP}')" >&2; exit 2 ;;
  esac
  printf '%s.%s.%s\n' "$MAJ" "$MIN" "$PAT" > "$VERSION_FILE"
  printf "Version bumped: %s -> %s.%s.%s\n" "$CURRENT" "$MAJ" "$MIN" "$PAT"
fi

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
  # Version: exact tag on HEAD, else keep the default
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

# The VERSION file is the source of truth when present.
if [[ -s "$VERSION_FILE" ]]; then
  VERSION="v$(tr -d '[:space:]' < "$VERSION_FILE" | sed 's/^v//')"
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