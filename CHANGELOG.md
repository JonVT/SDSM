# Changelog

All notable changes to this project will be documented in this file.

The format is based on Keep a Changelog, and this project adheres to Semantic Versioning.

## [Unreleased]

### Added

- _None yet_

### Changed

- _None yet_

### Fixed

- _None yet_

### Removed

- _None yet_

## [v0.5.0] - 2025-11-09

- Added:
  - HTTPS enablement flow with self-signed certs and seamless restart/polling
    - SANs include localhost, 127.0.0.1, ::1, and hostname
    - Automatic redirect after restart, reduced connection loss
  - Windows tray reliability
    - Ensured tray runs on the main thread
    - Background mode: console hides and parent returns immediately (detached spawn)
  - UI/UX improvements
    - Vertical form groups for key inputs (Max Players, Auto/Quick Saves)
    - Tokens popup: click-to-copy with visual feedback and toast
    - Improved layout and Advanced TLS controls
  - Logging consolidation
    - Centralized operational logs into sdsm.log (incl. http.Server errors/TLS handshake)
    - updates.log captures deployment progress
  - Startup update improvements
    - Startup auto-update now runs every launch (removed previous gating)
    - Cache priming and diagnostics to ensure correct comparison of deployed vs latest
    - Missing Release/Beta now treated as actionable installs

- Fixed:
  - Resolved UI break from non-async await and duplicate bootstrap code
  - Corrected TLS ::1 handshake warning by expanding SANs
  - Fixed Beta auto-update not triggering at startup by removing legacy block and adding diagnostics

- Developer Notes:
  - New verbose evaluation logs gated by env var `SDSM_VERBOSE_UPDATE=1`
  - Build metadata injected via ldflags; tagging `v0.5.0` makes Version reflect in the binary

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

[Unreleased]: https://github.com/JonVT/SDSM/compare/v0.5.0...HEAD
[v0.5.0]: https://github.com/JonVT/SDSM/releases/tag/v0.5.0
[v0.4.0]: https://github.com/JonVT/SDSM/releases/tag/v0.4.0
