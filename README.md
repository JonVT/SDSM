# Stationeers Dedicated Server Manager

![SDSM logo](sdsm.png)

Modern control plane for running, updating, and monitoring Stationeers dedicated servers.

---

## Overview

SDSM is a Go (Gin) web application that wraps everything you need to operate Stationeers servers on Linux. It supervises deployments, keeps SteamCMD/BepInEx/LaunchPad files current, exposes a clean dashboard for day-to-day operations, and parses log output to surface real-time player, chat, and save activity.

## Feature Highlights

- **Unified dashboard** – Track multiple servers, player counts, deployment progress, and recent log activity at a glance.
- **Guided setup & health checks** – Detect missing dependencies, validate filesystem paths, and highlight anything blocking a launch.
- **Per-server control center** – Start, stop, restart, pause, and save servers with one click while watching live chat, players, and session history.
- **Smart log parsing** – Deduplicate player sessions, map admins, detect saves, and keep chat scoped to active runs without polluting history.
- **One-click deployments** – Queue Release, Beta, SteamCMD, BepInEx, LaunchPad, or full refreshes with progress bars and status reporting.
- **Secure access** – Built-in authentication, optional HTTPS, and rate-limited API endpoints for dashboard and automation use.

## UI Preview

| Dashboard Overview | Server Status Deep Dive |
| --- | --- |
| ![Dashboard preview](docs/media/dashboard.svg) | ![Server status preview](docs/media/server-status.svg) |

## Architecture At A Glance

![Architecture diagram](docs/media/architecture.svg)

- **Client:** HTML templates plus lightweight JavaScript polling keep views up to date without heavy frameworks.
- **Gin layer:** Authentication middleware, template rendering, JSON endpoints, and deployment progress streaming.
- **Manager/models:** Long-running orchestration, filesystem interactions, log parsing, and SteamCMD/BepInEx/LaunchPad deployments.

## Quick Start

```bash
# Clone the repository
git clone https://github.com/JonVT/SDSM.git
cd SDSM/go

# Build the manager binary
go build ./cmd/sdsm

# Launch with an explicit configuration file
SDSM_CONFIG=/path/to/sdsm.config ./sdsm
```

SDSM listens on port `5000` by default. Browse to `http://localhost:5000/login` (or `https://` when TLS is enabled) and sign in using the configured credentials.

## Configuration

SDSM persists state to a JSON configuration file. Point the manager at this file using the `SDSM_CONFIG` environment variable or by passing the path as the first CLI argument.

Key settings include the Steam account (`manager.SteamID`), root install path (default `/tmp/sdsm`), server inventory (world, ports, auto-start/update flags), and the update schedule (`manager.UpdateTime`).

### Environment Variables

| Variable | Purpose |
| --- | --- |
| `SDSM_CONFIG` | Absolute or relative path to the configuration JSON. |
| `GIN_MODE` | Set to `release` to suppress Gin debug logging. |
| `SDSM_USE_TLS` | Enable HTTPS delivery when set to `true`. |
| `SDSM_TLS_CERT` / `SDSM_TLS_KEY` | PEM files used when TLS is enabled. |
| `SDSM_ADMIN_PASSWORD`, `SDSM_USER1_PASSWORD`, `SDSM_USER2_PASSWORD` | Override the default UI credentials. |

### Sample Launch Commands

```bash
# Plain HTTP
SDSM_CONFIG=/srv/sdsm/sdsm.config ./sdsm

# HTTPS (env-style arguments)
SDSM_USE_TLS=true SDSM_TLS_CERT=/etc/ssl/mycert.pem SDSM_TLS_KEY=/etc/ssl/mykey.pem SDSM_CONFIG=/srv/sdsm/sdsm.config ./sdsm
```

## Operating The Manager

1. **Sign in** via `/login` and open the **Dashboard** to see active servers, players, and deployment status.
2. Use the **Deploy** controls (global or per-server) to refresh Release/Beta builds, SteamCMD, BepInEx, and LaunchPad assets.
3. Dive into a **Server Status** page to start/stop/restart/pause/save, watch player rosters with admin badges, review historical sessions, and monitor live chat.
4. Tail log output from the control panel, open the full log viewer modal, or download logs directly for deeper analysis.
5. Visit the **Manager** page to inspect missing components, configure root paths, review deployment history, and adjust global settings.

## Project Layout

```text
go/
├── cmd/                # Entrypoints (sdsm CLI)
├── internal/
│   ├── handlers/       # HTTP handlers, HTML rendering, async workflows
│   ├── manager/        # Core orchestrator, deploy pipeline, config persistence
│   ├── middleware/     # Auth, rate limiting, security headers
│   ├── models/         # Server lifecycle, log parsing, players/chat buffers
│   └── utils/          # Logging, filesystem paths, process helpers
├── static/             # CSS and front-end assets
├── templates/          # HTML templates for dashboard, manager, auth, server detail
├── docs/media/         # SVG illustrations used in this README
├── test_config.json    # Example configuration for local testing
└── README.md
```

## Development

- **Formatting:** `gofmt -w ./internal ./cmd`
- **Build:** `go build ./cmd/sdsm`
- **Tests:** `go test ./...`
- **Logs:** `sdsm.log` and `updates.log` are truncated on startup for clean sessions.
- **Player history:** `players.log` is deduplicated and rewritten automatically when servers stop or restart.

For UI work, edit the HTML in `templates/` and the styles in `static/`; the JavaScript inside `templates/server_status.html` powers live player/chat/log updates.

## Contributing

Issues and pull requests are welcome. Please run `go test ./...` and `go build ./cmd/sdsm` before submitting changes.

---

SDSM © JonVT and contributors. Stationeers is © RocketWerkz. This project is not affiliated with or endorsed by RocketWerkz.

