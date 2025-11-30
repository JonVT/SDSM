## Troubleshooting

- UI not reachable:
	- Ensure the binary is running and printing the startup URL (http://localhost:5000).
	- Check firewall rules; allow inbound to port 5000 (or your configured port).
	- Port in use? Change the HTTP port via Settings in the app or edit `port` in `sdsm.config` and restart.

- Setup shows no progress:
	- Open the Setup screen; it reads `logs/updates.log` for real-time progress.
	- Check `logs/updates.log` for the latest line; re-run Deploy from Setup or Manager pages if stuck.

- Chat/commands fail (SCON):
	- Open the server page and try sending a chat message; errors now log to `logs/sdsm.log` and `ServerN/logs/ServerN_admin.log`.
	- Use the SCON health probe (requires login) at `GET /api/servers/:id/scon/health`.
	- Verify SCON files exist under your server’s `BepInEx/plugins` directory; SCON port defaults to `GamePort + 1`.

- Welcome message not sent:
	- Set a non-empty Welcome Message in the server settings.
	- Ensure SCON is reachable (use the health probe); welcome uses `SAY` with a short delay after connect.

 

- SteamCMD or downloads failing:
	- Check network connectivity and try again from the Setup or Manager Deploy controls.
	- Review `logs/updates.log` for component-specific errors (SteamCMD, BepInEx, LaunchPad, SCON).

- Where are logs?
	- Manager: `logs/sdsm.log` and `logs/updates.log`
	- Server: `ServerN/logs/ServerN_admin.log`, `ServerN/logs/ServerN_output.log`, `ServerN/logs/players.log`

- Quick health checks (no auth needed):
	- `GET /healthz` – liveness (process responding)
	- `GET /readyz` – readiness (200 only when manager active and all required components detected; 503 with JSON list of missing components otherwise)
	- `GET /version` – build metadata

Tip: If the binary isn’t executable, run `chmod +x ./sdsm` before starting it.

### Tray Behavior (Windows vs. Linux)

SDSM includes an optional Windows system tray integration (`TrayEnabled` in config). Behavior:

- Windows: When tray enabled, the app may spawn a detached background instance so the launching console returns immediately, then show a tray icon. Quitting the tray or sending SIGINT/SIGTERM triggers graceful shutdown.
- Non-Windows (Linux/macOS): Tray is automatically disabled; no systray dependencies are required and the process runs normally in foreground/background. Absence of the tray never causes early exit.

The readiness endpoint `/readyz` is unaffected by tray state—it reports readiness based on manager activation and missing component detection.

### Firewall Ports

| Port | Direction | Protocol | Purpose |
| --- | --- | --- | --- |
| `5000` | Inbound | TCP | SDSM UI/API (changeable via config) |
| `GamePort` (e.g., `26017`) | Inbound | Typically UDP (open UDP; TCP if needed) | Stationeers gameplay traffic |
| `GamePort + 1` (e.g., `26018`) | Inbound | TCP | SCON HTTP API used by SDSM |

Examples (UFW)
```bash
sudo ufw allow 5000/tcp
sudo ufw allow 26017/udp
sudo ufw allow 26018/tcp
```

Examples (iptables)
```bash
sudo iptables -A INPUT -p tcp --dport 5000 -j ACCEPT
sudo iptables -A INPUT -p udp --dport 26017 -j ACCEPT
sudo iptables -A INPUT -p tcp --dport 26018 -j ACCEPT
```

---

## TLS via reverse proxy (optional)

SDSM serves plain HTTP on the configured port (default `5000`). If you need HTTPS, terminate TLS at a reverse proxy and forward to SDSM over localhost. Below are minimal examples.

### Nginx

```nginx
server {
		listen 80;
		server_name example.com;
		return 301 https://$host$request_uri;
}

server {
		listen 443 ssl http2;
		server_name example.com;

		ssl_certificate     /etc/letsencrypt/live/example.com/fullchain.pem;
		ssl_certificate_key /etc/letsencrypt/live/example.com/privkey.pem;

		# Proxy to SDSM
		location / {
				proxy_pass http://127.0.0.1:5000;
				proxy_set_header Host $host;
				proxy_set_header X-Real-IP $remote_addr;
				proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
				proxy_set_header X-Forwarded-Proto $scheme;

				# WebSockets (if used)
				proxy_http_version 1.1;
				proxy_set_header Upgrade $http_upgrade;
				proxy_set_header Connection $connection_upgrade;
				map $http_upgrade $connection_upgrade { default upgrade; '' close; }
		}
}
```

### Caddy

```caddyfile
example.com {
		encode zstd gzip
		reverse_proxy 127.0.0.1:5000
}
```

### Traefik (static file + dynamic router)

```yaml
# traefik.yml (static)
entryPoints:
	web:
		address: ":80"
	websecure:
		address: ":443"
providers:
	file:
		filename: /etc/traefik/dynamic.yml
certificatesResolvers:
	letsencrypt:
		acme:
			email: you@example.com
			storage: /letsencrypt/acme.json
			httpChallenge:
				entryPoint: web
```

```yaml
# dynamic.yml
http:
	routers:
		sdsm:
			rule: Host(`example.com`)
			entryPoints: [websecure]
			service: sdsm
			tls:
				certResolver: letsencrypt
		sdsm-redirect:
			rule: Host(`example.com`)
			entryPoints: [web]
			middlewares: [redirect]
			service: noop@internal
	middlewares:
		redirect:
			redirectScheme:
				scheme: https
				permanent: true
	services:
		sdsm:
			loadBalancer:
				servers:
					- url: http://127.0.0.1:5000
```

Notes
- Open port 443/tcp on your firewall when exposing HTTPS.
- Keep SDSM bound to localhost or restrict access at the proxy if exposing to the internet.
- Enable HSTS only after validating HTTPS works across your domain/subdomains.

## Run as a systemd Service (optional)

Create a unit file at `/etc/systemd/system/sdsm.service`:

```ini
[Unit]
Description=Stationeers Dedicated Server Manager (SDSM)
After=network.target

[Service]
Type=simple
User=sdsm
Group=sdsm
WorkingDirectory=/srv/sdsm
ExecStart=/srv/sdsm/sdsm --config /srv/sdsm/sdsm.config
Restart=on-failure
RestartSec=5s
NoNewPrivileges=true
ProtectSystem=full
ProtectHome=true
PrivateTmp=true
AmbientCapabilities=

[Install]
WantedBy=multi-user.target
```

Then enable and start it:

```bash
sudo useradd --system --home /srv/sdsm --shell /usr/sbin/nologin sdsm || true
sudo mkdir -p /srv/sdsm
sudo cp ./sdsm /srv/sdsm/sdsm
sudo chown -R sdsm:sdsm /srv/sdsm
sudo chmod +x /srv/sdsm/sdsm
sudo systemctl daemon-reload
sudo systemctl enable --now sdsm.service
sudo systemctl status sdsm.service --no-pager
```

Note: Adjust paths and user/group for your environment. Logs live under the configured root path; ensure the `sdsm` user can write to it.

## Developers

Build from source
```bash
git clone https://github.com/JonVT/SDSM.git
cd SDSM
go build -o dist/sdsm ./app/backend/cmd/sdsm
./dist/sdsm --config /path/to/sdsm.config
```

### Notification Templates & Token Escaping

SDSM supports customizable Discord notification message templates for server lifecycle, update, and deploy events. Templates can include dynamic tokens replaced at send time:

- `{{server_name}}` – The server's display name
- `{{event}}` – Lifecycle event keyword (e.g., started, stopping, stopped, restarting)
- `{{detail}}` – Additional contextual detail (may be empty)
- `{{timestamp}}` – Event timestamp (UTC or local depending on implementation)

When you want to SHOW these tokens as examples inside HTML templates (e.g., placeholders or help text) you must escape them so Go's `html/template` engine does not attempt to evaluate them as pipelines (which would panic if no matching variable/function exists). Use the literal embedding pattern:

```html
Tokens available: {{`{{server_name}}`}}, {{`{{event}}`}}, {{`{{detail}}`}}, {{`{{timestamp}}`}}
```

This works because ``{{`…`}}`` tells the template engine to render the back‑quoted string verbatim, including the inner braces. Do NOT write raw `{{server_name}}` in a template unless you intend actual evaluation. Attribute placeholders follow the same rule:

```html
<input placeholder="Server {{`{{server_name}}`}} started." />
```

If you forget this escaping, the template will compile but panic at render time with an error like: `function "server_name" not defined`. Always audit new help/placeholder text for raw token patterns before committing.

Token substitution itself (for real notifications) occurs in Go code before sending the webhook; escaped examples never interfere with runtime replacement.

Deploy event templates add the following tokens:

- `{{component}}` – The component being deployed (SteamCMD, Release, Beta, BepInEx, LaunchPad, SCON, Sync)
- `{{status}}` – Status keyword (started, completed, error, skipped) if applicable
- `{{duration}}` – Human-friendly elapsed time for the deploy action
- `{{errors}}` – Condensed error summary (only present for error cases)
- `{{timestamp}}` – Event timestamp

Escaping works identically—for example in help text:

```html
Deploy tokens: {{`{{component}}`}}, {{`{{status}}`}}, {{`{{duration}}`}}, {{`{{errors}}`}}, {{`{{timestamp}}`}}
```

If you see a panic like `function "component" not defined`, you likely forgot to escape one of these deploy token examples.

### Architecture Decision Records (ADRs)

We track notable design and operations decisions in ADRs under `docs/adr/`.

- ADR Index: `docs/adr/README.md`
- ADR 0001 — Session Decisions (2025-11-14): `docs/adr/0001-session-decisions-2025-11-14.md`
	- New ADRs: start from `docs/adr/_template.md` and add to the index.

These documents capture conventions like Discord integration, lifecycle notifications, detached server attach-on-restart, Windows/Linux PID liveness checks, SCON-only command delivery, adaptive port forwarding, in-process log parsers, player-save automation, and attach/rehydration knobs.

### Developer Notes: Helper APIs (new)

Cross-cutting concerns are now centralized. Prefer these helpers in handlers:

- Toasts (headers for UI notifications)
	- `ToastSuccess(c, title, msg)`, `ToastInfo`, `ToastWarn`, `ToastError`
	- Under the hood sets `X-Toast-Type`, `X-Toast-Title`, `X-Toast-Message` for HTML/JSON consumers.
- Validation and creation
	- `ValidateServerNameAvailable(mgr, name, excludeID)`
	- `ValidatePortAvailable(mgr, portRaw, excludeID) (port, suggested, error)`
	- `SanitizeWelcome(str, maxLen) string`
	- `ValidateNewServerConfig(mgr, NewServerInput) (*ValidatedServerCreation, error)` to normalize and validate inputs across web and API flows.
	- `DefaultDifficulty(mgr, beta) string` for sensible defaults when unset.
- Realtime updates
	- `BroadcastStatusAndStats(s *models.Server)` replaces ad-hoc paired broadcasts.
- Core start parameter changes and redeploys
	- `ApplyCoreChangeEffects(s, origWorld, origStartLoc, origStartCond, originalBeta)` encapsulates pending save purge flagging and beta redeploy behavior.

UI notes

- Utility-first CSS lives in `app/frontend/static/css/ui-theme.css` (plus `modern.css`).
- Templates in `app/frontend/templates/` avoid inline styles; HTMX-triggered updates consume toast headers for user feedback.
- Modals: include `{{ template "modal_templates" . }}` and `{{ template "modal_scripts" . }}` on pages that need dialogs.
	- Confirm: `openConfirm({ title:'Delete Server', body:'<p>…</p>', confirmText:'Delete', danger:true })` → Promise<boolean>.
	- Prompt: `openPrompt({ title:'Save As…', label:'Name', validate:(v)=>v?true:'Required' })` → Promise<string|null>.
	- Info: `openInfo({ title:'Help', body: someNodeOrHTML })` → Promise<void>.
	- Helpers auto-handle focus trapping, Escape/backdrop close, and return Promises for clean async flows.

Formatting, tests, lint
- `gofmt -w ./internal ./cmd`
- `go test ./...`
- `make lint` or `make lint-css`

## Project Layout

```text
cmd/                   # Entrypoint (sdsm)
internal/
	handlers/           # HTTP handlers, HTML rendering, async workflows
		toast.go           # Toast header helpers (new)
		validation.go      # Validation + server creation pipeline (new)
		broadcast.go       # Combined status+stats broadcast (new)
		update_helpers.go  # Core change effects + beta redeploy (new)
	manager/            # Orchestrator, deploy pipeline, config, paths, logging
	middleware/         # Auth, security headers, CORS, rate limiting, websockets
	models/             # Server lifecycle, SCON commands, logs, players/chat
	utils/              # Logger, filesystem paths, process helpers
steam/                # SteamCMD + component updaters (BepInEx, LaunchPad, SCON)
ui/
	static/             # CSS and assets (embedded)
	templates/          # HTML templates (embedded)
webassets/            # Go embed glue for assets
docs/media/           # Diagrams and screenshots
LICENSE               # MIT License
```

## Development

- **Formatting:** `gofmt -w ./internal ./cmd`
- **Build:** `go build -o dist/sdsm ./app/backend/cmd/sdsm`
- **Tests:** `go test ./...`
- **Logs:** `logs/sdsm.log` and `logs/updates.log` are under the configured root path and are truncated on startup.
- **Player history:** `ServerN/logs/players.log` is deduplicated and rewritten automatically on stop/restart.

#### Formatting

This repository enforces `gofmt -s`. Use these helpers:

- Auto-format everything:

```bash
make fmt
```

- Check for unformatted files (fails if any):

```bash
make fmt-check
```

Optional: install local Git hooks to auto-format and stage changes on commit:

```bash
bash tools/install-git-hooks.sh
```

### Linting

- Run all linters: `make lint`
- CSS heuristics only: `make lint-css`

The CSS lint checks for obviously unused selectors by scanning HTML templates and JavaScript for class usage (including dynamic `classList.*` and `className` patterns). It’s heuristic by design; review findings before removal.

For UI work, edit the HTML in `app/frontend/templates/` and the styles in `app/frontend/static/`. Utility CSS lives in `app/frontend/static/css/ui-theme.css`; avoid inline styles in templates. The JavaScript inside server status templates powers live player/chat/log updates. A shared footer and `/terms` page are included; the footer links to Terms and the GitHub repo.

### SCON Integration

- Commands are sent via the Stationeers SCON HTTP API at `http://localhost:<SCONPort>/command`.
- Default `SCONPort` is the server game port + 1 (e.g., `26017` -> `26018`).
- All send attempts and failures are centrally logged by the model layer.
- Probe reachability via `GET /api/servers/:id/scon/health`.

## Contributing

Issues and pull requests are welcome. Please run `go test ./...` and `go build ./app/backend/cmd/sdsm` before submitting changes.

### PR Conventions

- Title style: follow Conventional Commits (e.g., `feat: add setup progress timeline`, `fix: handle empty updates.log gracefully`, `chore(ci): run lint on PRs`).
- Labels: use `type/*` (e.g., `type/bug`, `type/feature`, `type/docs`, `type/ci`) and `area/*` (e.g., `area/ui`, `area/templates`, `area/backend`, `area/steam`). A labeler workflow will auto-apply many of these based on changed paths.
- Checklist: ensure build/tests/lint pass; update docs/screenshots if UI changes; prefer small focused PRs.

---
