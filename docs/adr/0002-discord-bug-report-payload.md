# ADR 0002 — Discord Bug Report Payload

Status: Accepted
Date: 2025-11-14

## Context

SDSM implements a minimal bug reporting flow that posts reports to Discord via a webhook. We want a stable, documented payload shape to keep downstream automations predictable and allow future evolution without breaking consumers.

## Decision

Adopt a compact JSON payload with a single embed and fixed fields; avoid attachments for simplicity. Include a bounded log tail and environment context.

- HTTP: POST to Discord webhook URL (server-configured)
- Content-Type: application/json
- Body fields:
  - `content`: optional mention (usually empty)
  - `embeds`: array with one embed containing the details
- Embed fields:
  - `title`: "SDSM Bug Report"
  - `description`: freeform user text (trimmed, max length guarded in handler)
  - `color`: informational (blue) or warning (yellow) per severity, but we currently default to blue
  - `fields`: name/value pairs
    - `Environment`: OS, arch, SDSM version, build commit (when available)
    - `Manager State`: Active/Updating, detached mode flag
    - `Server Context`: If a server is selected, include ID, name, beta flag, running/starting state
    - `Tail (updates.log)`: Last N lines (bounded to <= 50 lines or ~4KB)
    - `Tail (sdsm.log)`: Last N lines (bounded to <= 50 lines or ~4KB)
  - `timestamp`: ISO8601 time when the report was created

The handler truncates each field to stay under Discord embed limits (6000 characters overall, 1024 per field value recommended). If a field would overflow, it is shortened and suffixed with "…".

## Consequences

- Bug reports remain predictable for Discord bots and humans.
- Downstream automation can rely on field names and add routing rules.
- No binary attachments (e.g., entire logs) are sent; reporters can follow up with files if needed.

## Alternatives Considered

- Multiple embeds: More structure but risks hitting limits faster; chosen to keep a single embed.
- File attachments: Better for long logs but complicates webhook use and retention; deferred for now.
- Freeform payload: Flexible but brittle for downstream automation; rejected.

## References

- Discord webhook embeds: https://discord.com/developers/docs/resources/webhook#execute-webhook
- ADR-0001 — Session Decisions (Discord integration overview)
