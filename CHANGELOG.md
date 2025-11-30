# Changelog

All notable changes to this project will be documented in this file.

The format is based on Keep a Changelog, and this project adheres to Semantic Versioning.

## [Unreleased]

### Added

- Card refactor Milestone 5: all server status, manager, dashboard, and users screens now render exclusively through the card registry with HTMX single-card refresh endpoints and per-card JS modules.
- Create Server presets can now be edited in `sdsm.config` via a new `server_presets` array. The UI consumes these dynamically so operators can tweak defaults without rebuilding.

### Changed

- Port forwarding is now adaptive: SDSM first prefers a mapping created by the game via UPnP (when available), and falls back to creating a NAT-PMP/UPnP mapping itself.
- Default server port suggestions now walk 27016, 27019, 27022, ... ensuring each new server form picks a port spaced by three unless that slot is already in use.

### Fixed

- _None yet_

### Removed

- Steam P2P networking option. It is now always disabled at startup and no longer configurable in the UI or API. Legacy `net_mode` parsing has been removed.

## [v0.6.0] - 2025-11-09

- Added:
  - Readiness probe at `/readyz` for orchestration and startup gating.
  - Root path redirect logic: `/` now routes users to Login, Dashboard, or Manager based on auth and role.
  - Centralized role attachment and safety net via `EnsureRoleContext` middleware (auto-promotes user "admin" if no admins exist to prevent lockout), used for both API and UI routes.
  - CI/Security hardening:
    - Build/Test/Vet/Govulncheck workflow
    - CodeQL analysis
    - Scheduled weekly vulnerability scan
- Changed:
  - Enforced API-only mutations: introduced POST guard to block non-API POSTs except an allowlist (`/login`, `/admin/setup`, `/setup/skip`, `/setup/install`, `/setup/update`, `/shutdown`, `/update`, `/api/*`).
  - Converted legacy HTML form submissions to JS `fetch` calls against `/api` endpoints throughout the UI.
    - Neutralized forms in Users, New Server, Player actions, Chat, Startup Parameters, and Profile pages (action removed or set to `#`, `data-api-only` flags added).
  - Consolidated Gin access/error logs to dedicated GIN.log file alongside existing operational logs.
  - Cleaned up duplicated CORS header assignment; now a single `Access-Control-Allow-Methods` includes PATCH.
- Removed:
  - Deprecated HTML POST handlers and routes; UI now exclusively hits `/api` for state changes.
  - Stale "Legacy" references and comments in client-side scripts to reduce confusion.
- Fixed:
  - Root 404 by adding the explicit redirect logic mentioned above.
  - Minor stability and clarity improvements across middleware and templates.

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

[Unreleased]: https://github.com/JonVT/SDSM/compare/v0.6.0...HEAD
[v0.6.0]: https://github.com/JonVT/SDSM/releases/tag/v0.6.0
[v0.5.0]: https://github.com/JonVT/SDSM/releases/tag/v0.5.0
[v0.4.0]: https://github.com/JonVT/SDSM/releases/tag/v0.4.0
