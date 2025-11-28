# Stationeers Dedicated Server Manager

[![CI](https://github.com/JonVT/SDSM/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/JonVT/SDSM/actions/workflows/ci.yml)

<img src="docs/media/sdsm.png" alt="SDSM logo" width="120" />

Modern control plane for running, updating, and monitoring Stationeers dedicated servers. Self-hosted. No telemetry.

—

## Pre-1.0 Backward Compatibility Policy

Until the first stable `v1.0.0` release, SDSM intentionally does **not** preserve backward compatibility for configuration fields or internal APIs. Obsolete or experimental settings may be removed outright to simplify the codebase and reduce maintenance overhead. If you upgrade a pre-1.0 build and find a deprecated field in your existing `sdsm.config`, it will simply be ignored by the JSON parser.

Rationale:
- Iterate quickly without carrying legacy baggage.
- Keep configuration lean and only expose durable concepts.
- Lower risk of retaining partially hardened or confusing transitional features.

Implication for operators: Before `v1.0.0`, review release notes when upgrading; remove obviously unused/deprecated keys if you want a clean config. Nothing will break solely because a removed key remains present.

Once we tag `v1.0.0`, stability guarantees begin and future removals will follow documented deprecation paths.

## Get Started (60 seconds)

You don’t need to install anything else.

1) Download the latest release binary
- Linux: https://github.com/JonVT/SDSM/releases/latest

2) Run it
```bash
chmod +x ./sdsm
./sdsm
```

3) Open the app
- Visit `http://localhost:5000/login`
- First run guides you through setup at `/setup` (or `/admin/setup` to create the first admin).

What happens automatically
- Creates all needed directories under a safe root path (logs, bin, servers, config).
- Downloads and keeps components current: SteamCMD, Stationeers (Release/Beta), BepInEx, LaunchPad, and SCON.
- Syncs per-server files on demand when you create or start a server.

## System Requirements

- OS: Linux x86_64
- CPU: 64‑bit; 2+ cores recommended if running game servers on the same host
- Memory: Sufficient RAM for Stationeers servers you plan to run concurrently
- Disk: 10+ GB free for SteamCMD, Stationeers (Release/Beta), BepInEx, LaunchPad, SCON, and logs
- Network:
	- Outbound internet access for downloads (Steam, GitHub/CDN)
	- Inbound HTTP to the SDSM port (default `5000`) if accessing from another machine
	- Stationeers game port you configure (default `26017`) and SCON port (`GamePort + 1`) reachable as needed
- Permissions: Ability to execute the binary and write to the chosen root path (directories are created on first run)
- TLS (optional): Terminate HTTPS at a reverse proxy (e.g., Nginx, Caddy, Traefik) if you need TLS.
- Browser: Modern Chromium/Firefox-based browser

---

## Overview

SDSM is a Go (Gin) web application that wraps everything you need to operate Stationeers servers on Linux. It supervises deployments, keeps SteamCMD/BepInEx/LaunchPad files current, exposes a clean dashboard for day-to-day operations, and parses log output to surface real-time player, chat, and save activity.

<!-- What's New section intentionally removed until v1.0.0 release -->

## Feature Highlights

- **Guided setup + progress** – On initial execution SDSM will walk you step-by-step through the setup process to ensure you:
	- Establish an admin account: You will select an initial administrator password.
	- Download needed software: SDSM will download 'steamcmd' and then use it to download the release and beta versions of Stationeers Dedicated Server. StationeersLaunchPad and SCON mods are also downloaded preparing SDSM to deploy and manage multiple Stationeers dedicated servers.  The status and prgress of these downloads are displayed durring setup to provide you constant feedback durring the setup process.
- **Control frame** - All content is presented as cards within a consistent control frame featuring:
	- An informative header letting you know where you are, current date and time, a conveient icon to change UI themes, and, to remind you who is logged in, a user avatar that will allow you to change your profile or log you out of the app.
	- A common footer with copyright information and convienient links to the GitHub project, License file, Privacy statement, and the Stationeers Dedicated Server Guide.
	- The application navigation menu allowing for easy access to all SDSM's features and content.

### SDSM Menu
- **Dashboard** – A unified dashboard displaying card to provide hoolistic system information and status.  Dashboard cards include:
	- Total server count
	- Active server count
	- Connected player count across all servers
	- System health - Information about the host environment including:
		- System cpu usage
		- System memory usage
		- Network busy
		- other system stats and info
	- Manager - Manager level information and control including:
		- Software update status
		- Configured web UI port
		- Configured root path
		- Buttons to Stop SDSM or Restart SDSM
	- Users - Summary inofrmation regarding users allowed to log in to and operate SDSM and associated servers including:
		- Authorized user counts
		- Logged in users
		- A button to manage users
	- Servers - Buttons to start all servers, stop all servers, add a new server, as well as a card for each configured server providing:
		- Server name
		- Server status (Stopped/Starting/Started/Paused/Error)
		- Status duration indicating how long the server has been in it's current state
		- Last started date/time
		- World name
		- Player count/Manx players
		- Game port
		- Server Password
		- CPU used by the server
		- Memory consumed by the server
		- Buttons to Start/Stop, Rename, Delete, and Manage the server
- **Manager** - Comprehensive control of the manager app including cards for:
	- Manager Control: Information regarding the current up-time of the app as well as buttons to Shutdown and Restart SDSM
	- Configuration: All of the configuration items and parameters for the operation of the manager functions of SDSM
	- Software Versions: Deployed vs. latest versions of the software managed my SDSM along with buttons to update individual componenets are all with real-time progress display of any updating component to keep you informed of progress and status.
	- Discord Integration: Define if and how you want to send manager and server events to Discord including which events to send, what mmessage to send for each event, and the color of the event message. Each message can be enhanced using substitution tokens.  The server events configured here can be overriden by individual servers.
	- Logs: Display manager level logs with a tab for each log. Manager logs are in the <root>/logs directory.
- **Users** - Management of users allowed to log in to SDSM.  SDSM has two level of users:
	- Administrator: Allowed to add/change/remove users and perform all functions within SDSM
	- Operator: Allowed to manage Stationeers servers.  Administrators can limit which servers operators are allowed ro manage.
- **Per-server control** – The frame menu presents navigation to each configured server.  Server pages include cards for:
	- Server Control - Information and functions to manipulate running servers including:
		- Buttons to Start/Stop, Restart, Save/Save As the server
		- Started date/time and duration and Last saved date/time
		- a display of the last line from the server output log with a button to navigate to the logs card to see the full log
		- Udate server files including the rocketstation_DedicatedServer, StationeersLaunchPad, and SCON
		- Delete the server (admin only function)
		- Rename the server (admin only function)
		- Send various commands to the server including start/stop storms, cleanup players, and a game console to send other arbitrary commands via the SCON mod
	- Players - Various views of player on this server including:
		- Live - Players currently connected including the date/time and durration of connection.  Each player can be kicked or banned.
		- History - List of all players who have ever connected to this server with sub-list of connect/disconnect date/time and duration. Each player can be banned or unbanned if already banned.
		- Banned - List of all players banned from this server.  Each player can be unbanned.
	- Chat - A live stream of all chats in the current session as well as controlls to send a chat message via the SCON mod.  Chat messages support substitution tokens.
	- Configuration - All of the configuration items and parameters for the definition and operation of this particular server.  Certain changes to configuration may require a restart of the server to take affect.
	- Discord Integration: Define if and how you want override configuration of defaults configured on the Manager screen.
	- Saves - Comprehensive view and control of saves organized by the type of save including:
		- Auto - All auto-saves from the game as stored in the <root>/<Server#>/saves/<servername>/autosave directory
		- Quick - All quick-saves from the game as stored in the <root>/<Server#>/saves/<servername>/quicksave directory
		- Named - All named-saves from the game as stored in the <root>/<Server#>/saves/<servername>/manualsave directory
		- Player - All player-saves from the game as stored in the <root>/<Server#>/saves/<servername>/playersave directory (player saves are initiated by SDSM based on configuration)
		- All - All of the above saves as a summary
	- Logs -  Display server specific logs with a tab for each log. Server logs are in the <root>/<Server#>/logs directory.

### Additional features

- **Centralized command logging** – All SCON command sends (HTTP failures, non-200s) are logged to `sdsm.log` and per-server admin logs.
- **SCON health** – Verify connectivity via `GET /api/servers/:id/scon/health` for quick diagnostics.
- **One-click deployments** – SteamCMD, Release/Beta servers, BepInEx, LaunchPad, SCON, and per-server file sync.
- **Secure access** – Auth, security headers, per-IP rate limiting, optional HTTPS.
- **No telemetry** – SDSM runs locally and does not collect or transmit your data.
- **Fast server-to-server navigation** – Header Prev/Next buttons with wraparound and ArrowLeft/ArrowRight shortcuts accelerate multi-server management; tooltips surface target names and shortcuts.

## UI Preview

| Manager | Dashboard | Server Control |
| --- | --- | --- |
| <img src="docs/media/SDSM%20Manager.png" alt="Manager" width="320" /> | <img src="docs/media/SDSM%20Dashboard.png" alt="Dashboard" width="320" /> | <img src="docs/media/SDSM%20Server%20Control.png" alt="Server Control" width="320" /> |

## Architecture At A Glance

![Architecture diagram](docs/media/architecture.svg)

- **Client:** HTML templates plus lightweight JavaScript polling keep views up to date without heavy frameworks.
- **Gin layer:** Authentication middleware, template rendering, JSON endpoints, and deployment progress streaming.
- **Manager/models:** Long-running orchestration, filesystem interactions, log parsing, and SteamCMD/BepInEx/LaunchPad deployments.

## Using SDSM

SDSM listens on port `5000` by default. Visit `http://localhost:5000/login`.

- Dashboard: overview of servers, players, and deployments.
- Setup: shows live download/install progress parsed from `logs/updates.log`.
- Server pages: start/stop/restart/pause/save, live chat and player lists, historical sessions.
- Health: check SCON connectivity via `GET /api/servers/:id/scon/health` if chat/commands fail.

## Configuration

SDSM persists state to a JSON configuration file. Use `--config` (or `-c`) to point to this file. If omitted, SDSM uses `./sdsm.config` in the current working directory and will bootstrap it on first run.

Example launch:

```bash
./sdsm --config /srv/sdsm/sdsm.config
```

Key settings include the root path (where `bin/*`, `logs/`, and per-server directories live), HTTP port (`manager.Port`, default `5000`), Steam app ID (`manager.SteamID`, default `600760`), server inventory, and the update schedule (`manager.UpdateTime`).

Selected config fields:

- `paths.root_path`: Filesystem root for SDSM directories.
- `port`: HTTP port for the UI/API. Default 5000.
- `language`: Default language for world/difficulty extraction. Default `english`.
- `startup_update`: Run selective component updates at startup. Default `true`.
- `detached_servers`: Keep game servers running if SDSM exits. Default `false`.
- `tray_enabled`: Windows tray integration toggle. Default `true` on Windows.
- `tls_enabled`, `tls_cert`, `tls_key`: Optional HTTPS served directly by SDSM (paths may be relative to `root_path`).
- `auto_port_forward_manager`: Attempt UPnP/NAT-PMP port mapping for the manager HTTP(S) port. Default `false`.
- `verbose_http`: More verbose HTTP request logging. Default `false`.
- `verbose_update`: Verbose update-decision logging for components. Default `false`.
- `jwt_secret`: HMAC secret for UI/API sessions. Set this to a strong random string in production.
- `cookie_force_secure`: Force auth cookies to be Secure. Default `false` (automatically Secure under HTTPS).
- `cookie_samesite`: One of `none`, `lax`, `strict`, or `default`. Default `none`.
- `allow_iframe`: Allow embedding in any parent (`frame-ancestors *`). Default `false` (same-origin only).
- `windows_discovery_wmi_enabled`: Windows-only process discovery via WMI. Default `true`.
- `scon_repo_override`: Alternative `owner/repo` for SCON releases.
- `scon_url_linux_override`, `scon_url_windows_override`: Explicit SCON asset URLs per OS.

See also: `docs/sdsm.config.example` for a ready-to-copy minimal config.

### Minimal sdsm.config example

Save this as `sdsm.config` and point SDSM to it with `--config /path/to/sdsm.config`.

```json
{
	"steam_id": "600760",
	"paths": { "root_path": "/srv/sdsm" },
	"port": 5000,
	"language": "english",
	"startup_update": true,
	"detached_servers": false,
	"tray_enabled": false,
	"tls_enabled": false,
	"tls_cert": "",
	"tls_key": "",
	"auto_port_forward_manager": false,
	"verbose_http": false,
	"verbose_update": false,
	"jwt_secret": "change-me-32+chars",
	"cookie_force_secure": false,
	"cookie_samesite": "none",
	"allow_iframe": false,
	"windows_discovery_wmi_enabled": true,
	"scon_repo_override": "",
	"scon_url_linux_override": "",
	"scon_url_windows_override": "",
	"discord_default_webhook": "",
	"discord_bug_report_webhook": "",
	"servers": []
}
```

Tip: On first run, SDSM will create directories under `paths.root_path` and download/update components as needed. Set a strong `jwt_secret` for production.

## Operating The Manager

1. Sign in via `/login` and open the Dashboard for servers, players, and deployments.
2. Use Deploy controls (global or per-server) for SteamCMD, Release/Beta, BepInEx, LaunchPad, SCON.
3. Use Server Status to start/stop/restart/pause/save; monitor live players/chat/log; review history.
4. Use Setup to watch live deployment progress parsed from `updates.log`.
5. Probe SCON via `GET /api/servers/:id/scon/health` if chat/commands fail.

---

## Troubleshooting
See the [Troubleshooting and Support Guide](SUPPORT.md).

---

SDSM © JonVT and contributors. Stationeers is © RocketWerkz. This project is not affiliated with or endorsed by RocketWerkz.

## License

This project is licensed under the MIT License. See `LICENSE` for details.

