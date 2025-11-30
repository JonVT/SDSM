# Contributing to SDSM

Thanks for helping improve the Stationeers Dedicated Server Manager! This guide outlines the workflows, coding standards, and testing practices to keep contributions smooth and maintainable.

- [Development Workflow](#development-workflow)
- [Coding Standards](#coding-standards)
- [Testing Requirements](#testing-requirements)
- [Documentation Expectations](#documentation-expectations)
- [Theming & UI Guidelines](#theming--ui-guidelines)
- [API & Backend Conventions](#api--backend-conventions)
- [Card System](#card-system)

## Development Workflow

1. **Fork and Branch**
   - Fork the repository and clone it locally.
   - Create a feature branch (`feature/<short-description>`) off `main`.

2. **Environment**
   - Install Go 1.22+ and Node 18+ (for tooling like `npm`/`pnpm` if needed).
   - Copy `sdsm.config.example` to `sdsm.config` and customize paths/ports for a local test install.
   - Run `go test ./...` before each commit.

3. **Commits / PRs**
   - Keep commits focused and descriptive.
   - Reference issues or milestones in the PR description when applicable.
   - Ensure CI passes before requesting review.

## Coding Standards

- **Go**
  - Follow `gofmt` and `goimports` formatting.
  - Keep functions small, single-purpose, and covered by tests.
  - Prefer explicit error handling; avoid panics outside of `main`.
  - Use context-aware logging via the provided logger utilities.

- **JavaScript / CSS**
  - Stick with modern ES modules and avoid global pollution.
  - Prefer composable functions over large monoliths.
  - Keep CSS scoped via BEM-style class names when practical.

- **Templates (Go HTML / HTMX)**
  - Keep logic in Go handlers/cards; templates should only render.
  - Avoid inline JS; use modules loaded via `data-card-assets`.

## Testing Requirements

- `go test ./...` must pass; add regression tests around new logic.
- For UI changes, update smoke/manual checklists in `docs/cards_smoke_tests.md` or add new automated coverage.
- When touching card registry logic, add/update tests in `app/backend/internal/cards/registry_test.go`.

## Documentation Expectations

- Update `CHANGELOG.md` under the *Unreleased* section.
- Reflect structural work or future tasks in `refactor_plan.txt`.
- When adding config fields, update `docs/sdsm.config.example` and mention them in relevant docs.
- For new developer workflows, update this CONTRIBUTING guide.

## Theming & UI Guidelines

- Maintain theme parity (light/dark) with variables defined in `app/frontend/static/css/ui-theme.css`.
- Use existing utility classes (flex/grid helpers, chips, badges) instead of introducing duplicates.
- UI interactions should be accessible (ARIA labels, keyboard focus states, etc.).

## API & Backend Conventions

- REST-ish JSON endpoints under `/api/*` should return `{ "status": "ok" }` or `{ "error": "message" }`.
- All mutations must be authenticated and role-checked in middleware or handlers.
- Keep handler files focused: `server_handlers.go` for server UI, `server_api.go` for JSON APIs, etc.

## Card System

The new card architecture powers Dashboard, Manager, Server Status, and Users screens via reusable Go + HTML + JS modules. Cards are registered in `app/backend/internal/cards` and rendered by the registry.

```
type Card interface {
    ID() string
    Template() string
    Screens() []Screen
    Slot() Slot
    FetchData(*Request) (gin.H, error)
}
```

* `Screen` identifies the page (dashboard, manager, server_status, users).
* `Slot` groups cards into layout buckets (`primary`, `grid`, `footer`).
* `Request` provides `Context`, `Server`, `Manager`, `Payload`, and shared `Datasets`.

### Capability & Toggle Flags

Each card can express optional capability requirements via the `Capabilities()` method (and `CardCapabilities` struct).

- `RequireServerRunning` – hide the card unless the active server is running.
- `RequirePlayerSaves` – hide the card unless Player Saves are enabled for the server.
- `AllowedRoles` – restrict visibility to specific roles (`admin`, `operator`, `viewer`, etc.). Role checks combine with the middleware-provided context so cards remain RBAC-aware on HTMX swaps.

Handlers automatically respect per-server toggles stored on `models.Server.CardToggles`. Cards listed in `app/backend/internal/cards/toggles.go` surface in the Server Status settings UI so operators can enable/disable them per server without code changes.

### Adding a Card

1. Create a Go struct in `app/backend/internal/cards/<feature>_card.go` that implements `Card`.
2. Add an HTML template in `app/frontend/templates/cards/<card>.html`.
3. (Optional) Add JS/CSS modules referenced via `data-card-assets` on the root element.
4. Register the card via `func init()` in the Go file.
5. Update documentation/tests:
   - Add smoke test steps in `docs/cards_smoke_tests.md`.
   - Mention the new card in `docs/cards.md`.
   - Note the change in `CHANGELOG.md`.

### Lazy Loading

Below-the-fold or heavy cards can set `data-card-lazy="true"` on their root element. The `SDSM.cards` loader waits until the card scrolls near the viewport before hydrating it (IntersectionObserver-based). Any refresh requests dispatched before the card mounts are queued and replayed automatically once it becomes visible. Use this for log viewers, large tables, or cards that require extra JS bootstrapping.

### Removing or Disabling a Card

- Remove the card registration + template + assets.
- Verify no screens still reference the card ID.
- Update documentation and tests to reflect the removal.
- For temporary disablement, prefer capability flags or server toggles rather than deleting files.

---

Happy hacking! Reach out via Discussions/GitHub issues for design clarifications.
