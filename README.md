# aether

Personal daemons with memory, schedules, and shared threads across channels.

Status: decided in spec, not built.

- `aether`: always-on control plane (REST, channels, scheduler, registry).
- hosts: run daemon processes on real machines.
- daemons: per-directory workers that execute turns and report outcomes.
- state: SQLite owns application state; Inngest Cloud owns durable execution.

MVP: Go control plane and CLI, one droplet host, one TypeScript daemon, and Telegram. Messages, memory, schedules, and durable tool approvals are in scope. Additional hosts, deploy automation, self-change, TUI/web/Slack, and other daemon languages follow after MVP.

Inngest Cloud offline delivery and reconnection have been verified by the operator.

Layout: `contract/events.json` (source of truth), `cmd/aether` + `internal/` (Go), `packages/daemon` (TS). See `CLAUDE.md`.

First build slice:

1. Boot a minimal `aether` service with SQLite, connected to Inngest Cloud.
2. Register one host and run one daemon through a message, schedule, and approval.
3. Verify application retry safety, approval recovery after restart, and visible failure/stale-work markers, then freeze contracts. See [the spec](ai_spec.md).
