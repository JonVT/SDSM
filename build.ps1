#!/usr/bin/env pwsh
# Simple build script for SDSM
# - Injects version metadata via -ldflags
# - Supports cross-compiling with GOOS/GOARCH
# - Outputs binaries to ./dist/

$ErrorActionPreference = "Stop"

# Resolve repo root (location of this script)
$SCRIPT_DIR = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $SCRIPT_DIR

# Go target platform (override by setting environment variables before running)
$GOOS = if ($env:GOOS) { $env:GOOS } else { go env GOOS }
$GOARCH = if ($env:GOARCH) { $env:GOARCH } else { go env GOARCH }

# Output directory and artifact name
$OUT_DIR = "dist"
New-Item -ItemType Directory -Force -Path $OUT_DIR | Out-Null

$EXT = ""
if ($GOOS -eq "windows") {
    $EXT = ".exe"
}
$ARTIFACT = "sdsm$EXT"
$OUT_PATH = Join-Path $OUT_DIR $ARTIFACT

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

# ldflags wiring to sdsm/internal/version
$LDFLAGS = @(
    "-s", "-w",
    "-X", "sdsm/internal/version.Version=$VERSION",
    "-X", "sdsm/internal/version.Commit=$COMMIT",
    "-X", "sdsm/internal/version.Date=$DATE",
    "-X", "sdsm/internal/version.Dirty=$DIRTY"
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
go build -trimpath -ldflags $LDFLAGS -o $OUT_PATH ./cmd/sdsm

if ($LASTEXITCODE -ne 0) {
    Write-Error "Build failed"
    exit $LASTEXITCODE
}

# Done
Write-Host "`n✅ Built $OUT_PATH"

exit 0
