# aether

Personal daemons with memory, schedules, and shared threads across channels.

Status: decided in spec, not built.

- `aether`: always-on control plane (REST, channels, scheduler, registry).
- hosts: run daemon processes on real machines.
- daemons: per-directory workers that execute turns and report outcomes.
- state: Postgres is truth; Inngest carries events and realtime hints.

First build slice:

1. Freeze event and payload contracts.
2. Boot Postgres + Inngest + a minimal `aether` service.
3. Register one host and run one daemon end-to-end.
