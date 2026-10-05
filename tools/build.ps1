#!/usr/bin/env pwsh
# Simple build script for SDSM
# - Injects version metadata via -ldflags
# - Supports cross-compiling with GOOS/GOARCH
# - Outputs binaries to ./dist

# Usage: tools/build.ps1 [-Bump major|minor|patch]
#   -Bump PART   Increment that part of the version in ./VERSION before building
#                (major resets minor/patch; minor resets patch).
param(
    [ValidateSet("major", "minor", "patch")]
    [string]$Bump = ""
)

$ErrorActionPreference = "Stop"

# Resolve repo root (script lives in ./tools)
$SCRIPT_DIR = Split-Path -Parent $MyInvocation.MyCommand.Path
$REPO_ROOT = (Resolve-Path (Join-Path $SCRIPT_DIR "..")).Path
Set-Location $REPO_ROOT

$VERSION_FILE = Join-Path $REPO_ROOT "VERSION"
if ($Bump) {
    $current = if (Test-Path $VERSION_FILE) { (Get-Content $VERSION_FILE -Raw).Trim().TrimStart("v") } else { "" }
    if ($current -notmatch '^(\d+)\.(\d+)\.(\d+)$') {
        Write-Error "$VERSION_FILE must contain MAJOR.MINOR.PATCH (found '$current')"
        exit 2
    }
    $maj = [int]$Matches[1]; $min = [int]$Matches[2]; $pat = [int]$Matches[3]
    switch ($Bump) {
        "major" { $maj++; $min = 0; $pat = 0 }
        "minor" { $min++; $pat = 0 }
        "patch" { $pat++ }
    }
    $next = "$maj.$min.$pat"
    Set-Content -Path $VERSION_FILE -Value $next -NoNewline:$false
    Write-Host "Version bumped: $current -> $next"
}

# Go target platform (override by setting environment variables before running)
$GOOS = if ($env:GOOS) { $env:GOOS } else { go env GOOS }
$GOARCH = if ($env:GOARCH) { $env:GOARCH } else { go env GOARCH }

$EXT = ""
if ($GOOS -eq "windows") {
    $EXT = ".exe"
}
$ARTIFACT = "sdsm$EXT"
New-Item -ItemType Directory -Force -Path (Join-Path $REPO_ROOT "dist") | Out-Null
$OUT_PATH = Join-Path (Join-Path $REPO_ROOT "dist") $ARTIFACT

# Git-derived metadata (best effort; falls back to sensible dev defaults)
$VERSION = "0.0.1"
$COMMIT = ""
$DATE = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
$DIRTY = "clean"

# Check if git is available and we're in a git repository
$gitAvailable = $false
try {
    git rev-parse --git-dir 2>&1 | Out-Null
    $gitAvailable = $LASTEXITCODE -eq 0
} catch {
    $gitAvailable = $false
}

if ($gitAvailable) {
    # Version: exact tag on HEAD, else empty for dev builds
    try {
        git describe --tags --exact-match 2>&1 | Out-Null
        if ($LASTEXITCODE -eq 0) {
            $VERSION = git describe --tags --exact-match
        }
    } catch {
        # No exact tag, keep default version
    }

    # Commit: short SHA
    try {
        $COMMIT = git rev-parse --short HEAD 2>&1
        if ($LASTEXITCODE -ne 0) {
            $COMMIT = ""
        }
    } catch {
        $COMMIT = ""
    }

    # Dirty: mark if there are uncommitted changes
    try {
        git diff-index --quiet HEAD -- 2>&1 | Out-Null
        if ($LASTEXITCODE -ne 0) {
            $DIRTY = "dirty"
        }
    } catch {
        # Assume clean if check fails
    }
}

# The VERSION file is the source of truth when present.
if ((Test-Path $VERSION_FILE) -and (Get-Content $VERSION_FILE -Raw).Trim()) {
    $VERSION = "v" + (Get-Content $VERSION_FILE -Raw).Trim().TrimStart("v")
}

# ldflags wiring to sdsm/app/backend/internal/version
$LDFLAGS = @(
    "-s", "-w",
    "-X", "sdsm/app/backend/internal/version.Version=$VERSION",
    "-X", "sdsm/app/backend/internal/version.Commit=$COMMIT",
    "-X", "sdsm/app/backend/internal/version.Date=$DATE",
    "-X", "sdsm/app/backend/internal/version.Dirty=$DIRTY"
) -join " "

# Display a brief build header
$versionDisplay = if ($VERSION) { $VERSION } else { "dev" }
$commitDisplay = if ($COMMIT) { $COMMIT } else { "local" }
Write-Host "`nBuilding SDSM $versionDisplay ($commitDisplay) for $GOOS/$GOARCH [$DIRTY]"

# Ensure modules are available
go mod download

# Build
$env:GOOS = $GOOS
$env:GOARCH = $GOARCH
go build -trimpath -ldflags $LDFLAGS -o $OUT_PATH ./app/backend/cmd/sdsm

if ($LASTEXITCODE -ne 0) {
    Write-Error "Build failed"
    exit $LASTEXITCODE
}

# Done
Write-Host "`n✅ Built $OUT_PATH"

exit 0