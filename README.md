# aether

Personal daemons with memory, schedules, and shared threads across channels.

Status: spec decided. Control plane and host supervisor built; first daemon next.

- `aether`: always-on control plane (REST, channels, scheduler, registry).
- hosts: run daemon processes on real machines.
- daemons: per-directory workers that execute turns and report outcomes.
- state: SQLite owns application state; Inngest Cloud owns durable execution.

MVP: Go control plane and CLI, one droplet host, one TypeScript daemon, and Telegram. Messages, memory, schedules, and durable tool approvals are in scope. Additional hosts, deploy automation, self-change, TUI/web/Slack, and other daemon languages follow after MVP. Design is in [the spec](ai_spec.md); layout and rules are in `CLAUDE.md`.

## Run

Set `AETHER_TOKEN`, `INNGEST_EVENT_KEY`, and `INNGEST_SIGNING_KEY`. Aether refuses to start without them.

```
make build
bin/aether serve   --db aether.db --listen :8080
bin/aether connect --aether http://127.0.0.1:8080 --root ./daemons
```

`serve` opens the store, serves REST under `/v1`, connects to Inngest Cloud, registers `scheduler.tick`, and drains the outbox. `connect` registers this machine as a host, then spawns the `run` command from each daemon's `aether.json` under `--root`.

## Progress

Following the order in the spec:

1. Security gate: env scrub, resolved-path containment, fail-closed auth. Done.
2. Control plane: SQLite with outbox, REST, schedule clock, Inngest publish and Connect, host supervisor. Done.
3. `@aether/daemon` runtime, `conjure` and `tell`, one daemon, the morning review. Next.
4. Telegram: approvals out, then messages in.
5. MVP completion checks: retry dedupe, approval recovery after restart, failure and stale-work markers.

Today the CLI has `serve`, `connect`, and `version`. REST covers thread replies, approval requests, soul and memory, schedules, and hosts. The SDK has the REST client and manifest parser but no turn loop yet.
