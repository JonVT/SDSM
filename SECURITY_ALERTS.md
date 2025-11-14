# Security Alert Remediation Guide

This document tracks the strategy for driving GitHub Code Scanning alerts (CodeQL + SARIF uploads) to zero.

## Objectives
1. Fix all High severity alerts.
2. Fix or justify (dismiss) Medium severity alerts with clear rationale.
3. Surface third-party static analysis (staticcheck, govulncheck) results in the Security tab via SARIF.
4. Prevent regressions with PR gating and baseline comparison.

## Workflow Enhancements
- `lint.yml` now uploads `staticcheck` SARIF (`category: staticcheck`).
- Placeholder SARIF for `govulncheck` added; convert JSON -> SARIF for richer data (tool implementation pending).
- Consider adding a baseline job to fail PRs that increase High severity count.

## Secure Coding Helpers
- `utils.SecureJoin(root, userPath)` ensures joined paths remain inside `root`.
- Launch argument sanitization in `Server.Start()` restricts characters for command-safe inputs.

## Typical Alert Families & Responses
| Category | Mitigation Implemented | Next Actions |
|----------|------------------------|--------------|
| Command injection | Sanitized launch args; exec.Command with arg slice | Extend sanitizer to new user-supplied fields |
| Path traversal | SecureJoin + existing Rel checks | Migrate all ad-hoc joins to SecureJoin |
| Insecure temp files | (Pending audit) | Replace manual temp naming with `os.CreateTemp` |
| Weak randomness | None found yet | Use `crypto/rand` for future token generation |
| Resource leaks | Ensure `defer Close()` usage | Audit remaining file openings |
| Hard-coded secrets | None detected | Keep secrets external (env/Actions) |
| Unvalidated input | Sanitize + allowlist | Add explicit bounds/allowlists per new feature |

## Triage Process
1. Export alerts (UI or API: `/code-scanning/alerts`).
2. Group by rule ID & severity.
3. For each rule instance: assess exploitability → Fix or Dismiss.
4. Record decision & PR/commit in this file or project board.

## Dismissal Guidelines
Use GitHub UI reasons: False positive, Won't fix, Used in tests. Always add a short justification:
```
False positive: Path validated via SecureJoin (rel containment) prevents traversal.
Won't fix: Legacy pattern; risk accepted pending refactor (target date Q1).
```

## Pending Improvements
- Implement govulncheck SARIF converter script to replace placeholder empty SARIF.
- Add `SECURITY_BASELINE.json` generation to track counts per severity.
- Integrate PR status check failing on increased High severity.

## Verification Steps
Run locally before PR:
```
go build ./...
go vet ./...
staticcheck ./...
govulncheck ./...
```
Check GitHub Security tab after merge for alert reductions.

## Change Log
- 2025-11-14: Added sanitizer & SecureJoin; SARIF upload for staticcheck; placeholder govulncheck SARIF.
