# ADR 0001 — Session Decisions (2025-11-14)

Status: Accepted

This document records key decisions and conventions established during the 2025-11-14 development session for SDSM. It exists to make our choices visible and persistent for future contributors and tools.

## Discord integration

- Webhooks
  - Bug reports: endpoint posts to a dedicated webhook with environment and log-tail info.
  - Lifecycle notifications: deploy start/end/failure; server start/stop/restart; server file updates.
- Implementation
  - Central helpers in manager: `Manager.DiscordNotify`, `notifyDeployStart/Complete`, `NotifyServerEvent`.
  - Best-effort delivery, no retry logic (keep the server responsive).
  - Manager config fields used: `DiscordDefaultWebhook`, `DiscordBugReportWebhook`.

## Logs and parsers

- In-process log parsers (non-exhaustive):
  - CLIENTS block parsing to refresh live players.
  - Player connect/disconnect, admin detection, chat messages (with live-client validation),
    world save events, world loaded, difficulty, pause/resume, weather events, server started,
    fatal startup errors (invalid world).
- SCON port detection
  - Parse BepInEx `LogOutput.log` for HTTP listener details; fallback to `Port+1`.

## Detached servers and restart behavior

- PID file per server when launched in detached mode (`Paths.ServerPIDFile(id)`).
- On manager startup, if `DetachedServers` is enabled and the PID is alive, we attach:
  - `Server.AttachToRunning(pid)`:
    - Marks Running, starts tailing the output log from the end (rehydrate recent state quickly).
    - Monitors PID liveness and cleans up when it exits (disconnects clients, rewrites players log, clears chat, removes PID file).
    - Best-effort SCON `CLIENTS` query with limited retries to repopulate live clients.
- Cross-platform liveness
  - Linux: `kill(pid, 0)` check.
  - Windows: `OpenProcess + GetExitCodeProcess == STILL_ACTIVE (259)`.

### Attach/rehydration knobs (per server)

- `log_attach_rehydrate_kb` (default 256): Replay this much of the output log when attaching.
- `clients_query_retry_count` (default 3): Number of `CLIENTS` attempts after attach.
- `clients_query_retry_delay_seconds` (default 2): Wait between attempts and after each send.

## Server command delivery

- SCON HTTP API only (stdin fallback removed due to unreliability).
- `SendRaw` allows commands when `Server.Running` is true even if `Proc` is nil (attach case).

## Port forwarding

- `UseGameUPnP` and `AutoPortForward` supported together with an adaptive strategy:
  - Let the game attempt UPnP first; probe after a delay.
  - If probe indicates a mapping, assume game mapping and skip SDSM-managed loop.
  - Otherwise, fall back to SDSM’s manage/refresh loop (UPnP + NAT-PMP).

## Player save automation

- On player connect (when enabled and not excluded):
  - Issue `FILE saveas <ddmmyy_hhmmss_steamid>`.
  - When a subsequent `Saved` line appears, move the resulting `.save` into `playersave`.

## Welcome messages

- Optional per-server `WelcomeMessage` and `WelcomeBackMessage` sent via chat after a short configurable delay.

## Notes on scope

- Webhook delivery is best-effort (no retries) to avoid blocking.
- UI log viewer initial-tail optimization is noted as a future enhancement: start tail immediately while log list loads.

---

Rationale: Persisting these decisions in-repo allows developers and tools (including assistants) to adhere to agreed behavior across sessions and contributors.
