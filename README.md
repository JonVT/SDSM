# SDSM – Stationeers Dedicated Server Manager

SDSM is a Go-powered control plane for Stationeers dedicated servers. It exposes a modern web UI (served by Gin) that guides first-time setup, keeps core server components up to date, and provides day-to-day operations such as configuration management, restarts, and log review.

## Highlights

- **Guided setup overlay** surfaces missing components (game builds, SteamCMD, BepInEx, LaunchPad) and tracks installation progress.
- **One-click deployments** let you pull Release, Beta, SteamCMD, BepInEx, LaunchPad, or all assets at once. Progress is streamed live via server-sent events.
- **Server fleet management** supports creating, editing, starting, stopping, and redeploying individual Stationeers server instances.
- **Log visibility** includes a rolling updates feed inside the UI plus quick links to view `updates.log` and `sdsm.log` in new tabs.
- **Authentication & rate limiting** are handled by project defaults, with environment overrides for credentials and access tokens.
- **Optional HTTPS** can be enabled with your own certificate/key pair for encrypted access to the manager.

## Requirements

- Go 1.24 or newer (matches the version declared in `go.mod`)
- Git (for fetching module dependencies)
- Stationeers dedicated server files available to the user running SDSM

The default installation path for managed assets is `/tmp/sdsm`, but this can be overridden in the configuration file or during the guided setup.

## Building

```bash
git clone <repo>
cd SDSM/go
go build .
```

The build step produces an executable named `sdsm` in the project root.

## Configuration

SDSM reads its configuration from a JSON file. Pass the path on startup via the `SDSM_CONFIG` environment variable or as the first CLI argument (e.g. `./sdsm my-config.json`). If no config is supplied, the manager starts with sensible defaults and prompts for any required values through the setup overlay.

Key environment variables:

- `SDSM_CONFIG` – Absolute or relative path to the configuration file.
- `GIN_MODE` – Set to `release` to disable Gin’s debug logging (default is release if unset).
- `SDSM_USE_TLS` – When `true`, the manager serves HTTPS using the certificate and key paths below.
- `SDSM_TLS_CERT` – Filesystem path to the TLS certificate (PEM) used when TLS is enabled.
- `SDSM_TLS_KEY` – Filesystem path to the TLS private key (PEM) used when TLS is enabled.
- `SDSM_ADMIN_PASSWORD`, `SDSM_USER1_PASSWORD`, `SDSM_USER2_PASSWORD` – Override default UI credentials.

The configuration JSON persisted by the manager tracks:

- Steam game ID and download targets (Release/Beta/SteamCMD)
- Deployment paths and log directories
- HTTP port to bind (`manager.Port`)
- Language selection and server inventory
- Auto-update timetable and startup behaviour

Sample configuration files (e.g. `test_config.json`) live in the repo for reference.

## Running

```bash
# with an explicit config file
SDSM_CONFIG=/path/to/sdsm.config ./sdsm

# serve over HTTPS using supplied cert/key
SDSM_USE_TLS=true \
SDSM_TLS_CERT=/path/to/fullchain.pem \
SDSM_TLS_KEY=/path/to/privkey.pem \
SDSM_CONFIG=/path/to/sdsm.config \
./sdsm
```

The server listens on the port defined in the configuration (default 5000). Visit `http://<host>:<port>/manager` (or `https://` when TLS is enabled) and sign in using the configured credentials; defaults are `admin/admin123`, `user1/password1`, and `user2/password2` unless overridden via environment variables.

Log files are written to the `logs` directory under the configured root (default `/tmp/sdsm/logs`).

## Project Layout

```
.
├── handlers/          # HTTP handlers, HTML rendering, auth flows, log streaming
├── manager/           # Core orchestration logic, deployment state, config persistence
├── middleware/        # Auth, rate limiting, security headers, WebSocket hub
├── models/            # Server model definitions and helpers
├── steam/             # SteamCMD/BepInEx/LaunchPad download + extraction routines
├── templates/         # Go HTML templates (manager UI, dashboard, login, etc.)
├── static/            # CSS assets for the modern UI
├── utils/             # Shared helpers (logging, filesystem paths)
├── main.go            # Application entry point and router setup
├── go.mod / go.sum    # Module metadata
└── README.md
```

## API Surface

The web UI is backed by JSON endpoints under `/api`, including:

- `GET /api/stats` – Summary statistics for servers.
- `GET /api/servers` – List configured servers.
- `GET /api/manager/status` – Manager heartbeat and update state.
- `GET /api/servers/:id/status` – Status for a single server.
- `POST /api/servers/:id/start` – Start the specified server.
- `POST /api/servers/:id/stop` – Stop the specified server.

All API routes require an authenticated token generated via the login flow.

## Development Tips

- UI templates live in `templates/manager.html` and related files; styles are in `static/`.
- Deployment logic is centralised in `manager/manager.go`; changes here typically require reviewing setup overlay behaviour as well.
- Long-running operations write to `updates.log`, streamed to the UI via `/updating` (server-sent events).
- When adding new features, run `go fmt ./...` and `go build ./...` to verify formatting and compilation.

---

SDSM is maintained under the same licence as the original project. Contributions are welcome—open an issue or PR with your proposed improvements.