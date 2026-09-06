# Aether

Read `ai_spec.md` before changing anything. It is decided; argue in the spec, not in code. `README.md` tracks build progress.

## Layout

- `contract/events.json` is the only source of truth for event names and payloads. Go (`internal/contract`) and TS (`packages/daemon/src/contract.ts`) each have a test that fails on drift. Change the JSON first.
- `cmd/aether`: one Go binary. Control plane, host supervisor, and CLI. Stdlib `flag`, no cobra.
- `internal/store`: SQLite (modernc, no cgo), embedded migrations, outbox, drain loop. Only aether opens the DB.
- `internal/policy`: env scrub, `realpath` containment, allowlist matching. Security gate; lands before features.
- `internal/api`: REST under `/v1`. Auth fails closed. No token, no server.
- `internal/inngest`: publisher, Connect, and the static `scheduler.tick` function.
- `internal/scheduler`: finds due schedules, fires or writes a skipped marker.
- `internal/host`: supervisor that spawns each daemon's `aether.json` run command with a scrubbed env.
- `packages/daemon`: `@aether/daemon`, bun, TypeScript strict. Runtime deps: `inngest` only. `start(daemon)` connects as `daemon-<name>` and runs the turn function.

## Rules

- State change and outbox row commit in one transaction. Event ID = outbox row ID.
- Every effect is retry-safe. Persist operation IDs; never trust at-most-once.
- Nothing is silently dropped. Failures and skipped schedules write a marker row.
- Approval is a row with no timeout. A paused turn ends its run; approval starts a new one.
- Channel text, memory, and tool output are untrusted input.
- Add a migration, never edit one that shipped.

## Commands

```
make build test lint fmt          # Go
cd packages/daemon && bun test && bun run typecheck
```

Env: `AETHER_TOKEN`, `INNGEST_EVENT_KEY`, `INNGEST_SIGNING_KEY`. See README for running `serve` and `connect`.
