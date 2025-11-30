# Card Loader Smoke Tests

These checks cover the interactive pieces added during Milestone 4. They intentionally focus on the dashboard cards that now rely on `SDSM.cards` and HTMX refreshes. Run through the list whenever the loader or any dashboard card module changes.

## Prerequisites

1. Build the UI assets and server:
   - `./build.sh`
2. Start SDSM locally (or run the compiled binary) and log in as an admin.
3. Use a Chromium-based browser with the DevTools console available.
4. Ensure the dashboard has at least one active server so stats and health values can change.

## Smoke Checklist

| # | Scenario | Steps | Expected Result |
|---|----------|-------|-----------------|
| 1 | Loader bootstraps cards on initial page load | Load `/dashboard`, open DevTools ➝ Console, run `SDSM.cards` to ensure it is defined. Inspect the DOM for `.card[data-card-id]` nodes and verify each has an entry in `SDSM.cards.instances`. | Every dashboard card initializes without console errors; cards show `aria-busy` only during HTMX swaps. |
| 2 | Manual refresh buttons trigger HTMX swaps | For each dashboard card with a refresh icon (Stats, System Health, Manager, Users), click the button. | Card re-renders in place, `aria-busy` toggles on/off, and HTMX requests `/dashboard/cards/<card-id>` without page navigation. |
| 3 | Stats card hydrates with cached telemetry | In DevTools, run `document.dispatchEvent(new CustomEvent('sdsm:stats-update',{detail:{totalServers:3}}))`. | The stats card updates immediately with the injected totals. The numbers persist until the next poll. |
| 4 | System health card listens for `sdsm:stats-update` | Dispatch a synthetic `sdsm:stats-update` event that contains `health: { score: '95%', pill: 'Warning' }`. | Pills, gauges, and the updated timestamp reflect the injected data without a network request. |
| 5 | Manager/users cards redraw after actions | Trigger any manager action (e.g., click Restart ➝ cancel) or users card action (click Refresh). | `sdsm:card-refresh` fires, the cards HTMX-refresh, and accessory buttons remain bound (no duplicate listeners). |
| 6 | Asset loader honors `data-card-assets` | In DevTools ➝ Network, filter for `cards/`. Reload the dashboard; ensure each dashboard card script loads exactly once (cache-busted URL). Toggle to another page and back to confirm scripts are not re-requested. | Each module script is fetched once per build timestamp. No duplicate `<script>` tags appear in `<head>`. |
| 7 | CSS dependencies are injected once (future cards) | For cards declaring CSS in `data-card-assets`, reload the page and check `<head>` for one `<link data-card-style>` per unique path. | Stylesheets are injected before card mount and never duplicated during HTMX swaps. |
| 8 | Error handling degrades gracefully | Temporarily block `/static/js/cards/dashboard_system_health.js` via DevTools ➝ Network conditions, reload `/dashboard`. | Console logs a warning but the card still renders server output; manual refresh button shows toast error but page remains usable. |
| 9 | Server tiles refresh + navigation | Click the refresh icon on the server tiles card while DevTools ➝ Elements highlights `#server-grid`. After the HTMX swap completes, click any server tile. | Card refresh indicators toggle on/off, HTMX requests `/dashboard/cards/dashboard-server-tiles` and `/api/servers`, and server tiles remain navigable (no duplicate listeners). |

## Optional Automation Notes

- **Playwright**: add a spec that boots SDSM with seeded data, navigates to `/dashboard`, and clicks each refresh button while waiting for HTMX swaps.
- **Cypress**: mock `/dashboard/cards/<card>` responses and assert DOM updates using `cy.intercept` + `cy.wait`.

## Reporting

When running the suite, capture:
- Browser + OS version
- Git commit
- Any console/network errors (screenshot or log)
- Deviations from the expectations above

Store results in the sprint QA notes or attach to the PR if failures block merging.
