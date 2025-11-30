# Card Architecture Guide

This document explains how the new card-based server dashboard works, how the registry loads cards, and what is expected when building new cards.

## Overview

- Cards live under `app/backend/internal/cards` (Go code) and `app/frontend/templates/cards` (HTML templates).
- Each card implements the `cards.Card` interface and registers itself via `cards.Register` in an `init()` func.
- Handlers render cards by asking the registry for `cards.Renderable` values, then grouping them by layout slots before passing them to templates.
- Shared datasets reduce redundant `manager.Manager` lookups and provide consistent metadata to every card in the request.

## Key Types

### `cards.Card`

```go
 type Card interface {
     ID() string
     Template() string
     Screens() []Screen
     Slot() Slot
     FetchData(*Request) (gin.H, error)
 }
```

- **Screens** specify where the card can render. Currently `cards.ScreenServerStatus` powers the server view, `cards.ScreenManager` drives the manager settings screen, and `cards.ScreenDashboard` covers the main dashboard.
- **Slot** describes layout buckets (e.g., `cards.SlotPrimary`). Templates decide how to place slots.
- **FetchData** receives a `*cards.Request` and returns the template payload.

### `cards.Request`

The request bundles everything a card needs:

- `Context`: the current `*gin.Context` (for localization, flashes, etc.).
- `Server`: the active `*models.Server`.
- `Payload`: legacy handler data while we migrate fully to cards.
- `Manager`: the active `*manager.Manager`.
- `Datasets`: cached shared lookups (see below).

Always prefer `req.Datasets` before reaching into the handler payload or calling the manager directly. This keeps work deduplicated when multiple cards need the same inputs.

### Datasets

`cards.NewDatasets(manager, server)` builds a cache object with three lazily populated helpers:

- `ServerSummary()` → `WorldInfo`, language options, resolved world IDs, start location/condition descriptions.
- `PlayerRoster()` → sorted live/history clients plus ban data.
- `ServerConfig()` → localized world lists, start option maps, per-channel difficulties and language choices.

If a dataset is unavailable (e.g., nil manager), cards should fall back to the legacy payload or direct manager calls. This guarantees that cards continue working during incremental migrations.

## Adding a Card

1. **Create Go file:** add `app/backend/internal/cards/<screen>/<name>_card.go` implementing the `cards.Card` interface and registering in `init()`.
2. **Template:** add `app/frontend/templates/cards/<name>.html` with a `{{define "cards/<name>.html"}}` wrapper. Avoid referencing global template data.
3. **Fetch data:** inside `FetchData`, pull everything you need from `req.Server`, `req.Datasets`, or lightweight manager calls. Return `gin.H`.
4. **Slots / screens:** choose the layout slot and screen(s) for your card. Keep them small and composable.
5. **Testing:** run `go test ./...` at minimum. Consider table tests for data helpers if the card adds logic.
6. **HTMX / JS:** declare optional CSS/JS dependencies on the card root via `data-card-assets`. The `SDSM.cards` loader lazy-loads any listed files (recognizing `.css` vs `.js`). Example: `<div data-card-id="dashboard-stats" data-card-assets='["/static/js/cards/dashboard_stats.js"]'>`.

## Example: Server Status Info Card

- Go: `app/backend/internal/cards/serverstatus/server_status_info_card.go`
- Template: `app/frontend/templates/cards/server_status_info.html`
- Uses `req.Datasets.ServerSummary()` for localized world metadata, with payload fallbacks for safety.

Use it as the reference implementation when migrating additional cards.

## Manager Screen Cards

The manager settings page is migrating card-by-card just like the server status screen. Current cards include:

- `manager-control` → Go: `app/backend/internal/cards/manager/manager_control_card.go`, Template: `app/frontend/templates/cards/manager_control.html` (slot: `primary`).
- `manager-configuration` → Go: `app/backend/internal/cards/manager/manager_configuration_card.go`, Template: `app/frontend/templates/cards/manager_configuration.html` (slot: `grid`).
- `manager-version-status` → Go: `app/backend/internal/cards/manager/manager_version_status_card.go`, Template: `app/frontend/templates/cards/manager_version_status.html` (slot: `grid`).
- `manager-logs` → Go: `app/backend/internal/cards/manager/manager_logs_card.go`, Template: `app/frontend/templates/cards/manager_logs.html` (slot: `footer`).
- `manager-discord` → Go: `app/backend/internal/cards/manager/manager_discord_card.go`, Template: `app/frontend/templates/cards/manager_discord.html` (slot: `footer`).

Each card renders with the same HTMX refresh contract as server cards, so dispatch `sdsm:card-refresh` with the matching `cardId` whenever manager settings change server-side.

## Dashboard Screen Cards

The dashboard is progressing through its migration with the following cards:

- `dashboard-server-deck` → Go: `app/backend/internal/cards/dashboard/dashboard_server_deck_card.go`, Template: `app/frontend/templates/cards/dashboard_server_deck.html` (slot: `primary`). Ships with `/static/js/cards/dashboard_server_deck.js` to keep server-card navigation bound and expose a manual refresh shortcut. The dashboard now treats this card as a legacy fallback whenever the tiles card is disabled.
- `dashboard-server-tiles` → Go: `app/backend/internal/cards/dashboard/dashboard_server_tiles_card.go`, Template: `app/frontend/templates/cards/dashboard_server_tiles.html` (slot: `primary`). It renders the individual server tiles grid (the only server list shown on the dashboard by default), wraps the legacy `server_cards` partial for backwards compatibility, exposes the same HTMX refresh endpoint used by the deck actions, and declares `/static/js/cards/dashboard_server_tiles.js` to rebind navigation after swaps. Any page missing this card will fall back to the deck card automatically.
- `dashboard-system-health` → Go: `app/backend/internal/cards/dashboard/dashboard_system_health_card.go`, Template: `app/frontend/templates/cards/dashboard_system_health.html` (slot: `primary`). This card hydrates the telemetry payload supplied by `buildDashboardPayload`, exposes HTMX refresh hooks, and mirrors the legacy `system_health` partial for graceful fallback.
- `dashboard-stats` → Go: `app/backend/internal/cards/dashboard/dashboard_stats_card.go`, Template: `app/frontend/templates/cards/dashboard_stats.html` (slot: `primary`). It renders the global stats row (total/active servers, players, startable count) with HTMX refresh semantics and falls back to the legacy `stats.html` partial when the card is disabled.
- `dashboard-users` → Go: `app/backend/internal/cards/dashboard/dashboard_users_card.go`, Template: `app/frontend/templates/cards/dashboard_users.html` (slot: `primary`). Admin-only widget that surfaces total/admin/operator counts, reuses the `/api/users` poller for live updates, and replaces the old `users_card.html` partial when registered.

## Users & Access Screen Cards

- `users-overview` → Go: `app/backend/internal/cards/users/users_overview_card.go`, Template: `app/frontend/templates/cards/users_overview.html` (slot: `primary`). Summarizes admin/operator totals with a lightweight status grid and gracefully falls back to the legacy stats block.
- `users-management` → Go: `app/backend/internal/cards/users/users_management_card.go`, Template: `app/frontend/templates/cards/users_management.html` (slot: `primary`). Replaces the previous `users_management_card` partial, wrapping the full table + actions in an HTMX-aware card that re-renders via `/users/cards/users-management`.
- `users-assignment` → Go: `app/backend/internal/cards/users/users_assignment_card.go`, Template: `app/frontend/templates/cards/users_assignment.html` (slot: `primary`). Surfaces role/server assignment tiles with quick access buttons that reuse the modal logic. Falls back to the same card template via `users_assignment_card` when the registry is disabled.

## Handler Integration

`app/backend/internal/handlers/manager_core.go` now:

1. Builds `datasets := cards.NewDatasets(h.manager, s)`.
2. Populates the legacy payload (for compatibility).
3. Builds `cardReq := &cards.Request{..., Datasets: datasets}`.
4. Calls `cards.BuildRenderables(cards.ScreenServerStatus, cardReq)` and `cards.GroupRenderablesBySlot`.

Templates can render default content when no cards exist by checking `.cardSlots`.

The same pattern is reused for the Manager screen: `buildManagerCardRequest` constructs a payload (without server datasets) and the template falls back to legacy partials when no manager cards are registered.

### Single-Card HTMX Endpoints

Need to refresh a card without reloading the whole page? Use these protected routes:

- **Server status cards** – `GET /server/:server_id/cards/:card_id?screen=server_status`
- **Manager cards** – `GET /manager/cards/:card_id`
- **Dashboard cards** – `GET /dashboard/cards/:card_id`
- **Users cards** – `GET /users/cards/:card_id`

Parameters:

- `server_id` – numeric ID of the server whose context should be used (server-status route only).
- `card_id` – matches the string returned by `Card.ID()`.
- `screen` (optional) – registry screen name; defaults to `server_status` (server-status route only).

The handler hydrates the requested card (using the same dataset cache as the full page) and responds with the card’s template fragment, making it perfect for HTMX swaps:

```html
<div
    hx-get="/server/{{.server.ID}}/cards/server-status-info"
    hx-trigger="sdsm:card-refresh[event.detail.cardId == 'server-status-info'] from:body"
    hx-target="this"
    hx-swap="outerHTML">
    {{template "cards/server_status_info.html" .}}
</div>
```


Dispatch a refresh from JavaScript whenever related data changes:

```js
document.body.dispatchEvent(new CustomEvent('sdsm:card-refresh', {
    detail: { cardId: 'server-status-info' },
}));
```
Because the endpoints reuse the same request builders used for full-page renders, cards receive the exact payload/Datasets combo they expect.

## Future Work

- HTMX single-card endpoint for targeted refreshes.
- Card-level feature flags / permissions.
- Frontend loader for per-card JS/CSS bundles.
- Docs for dataset testing strategy.

Keep this file updated as the architecture evolves so new contributors can follow the playbook without spelunking through historical PRs.
