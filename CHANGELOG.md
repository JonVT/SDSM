# Changelog

All notable changes to this project will be documented in this file.

The format is based on Keep a Changelog, and this project adheres to Semantic Versioning.

## [Unreleased]
- Added:
  -
- Changed:
  -
- Fixed:
  -
- Removed:
  -

## [v0.4.0] - 2025-11-09
- Added:
  - Per-server BepInEx init timeout (UI + config) with validation and clamping.
- Changed:
  - Detached servers are now configured via `sdsm.config` only (default: false).
  - Cross-platform process detachment via OS-specific helpers; unified start logic.
- Fixed:
  - Windows build failure due to Unix-only `SysProcAttr.Setpgid` usage.
- Removed:
  - Redundant process attribute files superseded by platform abstraction.
- CI:
  - New lint workflow (gofmt check + go vet).

[Unreleased]: https://github.com/JonVT/SDSM/compare/v0.4.0...HEAD
[v0.4.0]: https://github.com/JonVT/SDSM/releases/tag/v0.4.0
