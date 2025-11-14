# Architecture Decision Records (ADRs)

This folder tracks architectural and operational decisions made over time. Each ADR captures the context, decision, and consequences so contributors and tools can follow established conventions.

## Index

- 0001 — Session Decisions (2025-11-14)
  - Discord integrations (bug reports + lifecycle notifications)
  - In-process log parsers and SCON port detection
  - Detached servers: PID file tracking, attach-on-restart, Windows PID liveness
  - SCON-only commands; adaptive port forwarding
  - Player save automation; welcome messages
  - Attach/rehydration knobs (log replay window, CLIENTS retries)
  - Link: [0001-session-decisions-2025-11-14.md](./0001-session-decisions-2025-11-14.md)
- 0002 — Discord Bug Report Payload
  - Payload shape for bug reports posted via Discord webhook; field names and limits.
  - Link: [0002-discord-bug-report-payload.md](./0002-discord-bug-report-payload.md)

  ## How to add a new ADR

  1) Copy the template:

    - Template: [_template.md](./_template.md)

  2) Name the file incrementally (e.g., `0002-meaningful-title.md`) and fill in:

    - Context — background, constraints, drivers
    - Decision — the explicit choice and scope
    - Consequences — trade-offs, operational impact
    - Alternatives — options considered with pros/cons
    - References — related issues/PRs/diagrams

  3) Add the ADR to the Index above.
