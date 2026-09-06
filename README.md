# aether

Personal daemons with memory, schedules, and shared threads across channels.

Status: spec decided. Control plane, host, and the first conjured daemon run end to end; Telegram next.

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
bin/aether conjure foo --host $(hostname)
bin/aether tell foo remember to water the plants
curl -X PUT localhost:8080/v1/daemons/foo/schedules/morning-review \
  -H "Authorization: Bearer $AETHER_TOKEN" \
  -d '{"cron":"0 7 * * *","tz":"America/New_York","policy":"queue"}'
```

`serve` opens the store, serves REST under `/v1`, connects to Inngest Cloud as app `aether`, registers `scheduler.tick`, and drains the outbox. `connect` registers this machine as a host, connects as `host-<name>`, scaffolds daemons on `daemon/conjure.requested`, and spawns the `run` command from each daemon's `aether.json` under `--root` with a scrubbed env plus `AETHER_URL`, `AETHER_TOKEN`, and the Inngest keys. `--sdk` is the `@aether/daemon` dependency written into scaffolds; the default `file:../../packages/daemon` fits `--root ./daemons` in this repo. A schedule PUT with no `thread` uses the daemon's default thread, the one `tell` writes to. Set `INNGEST_DEV=http://127.0.0.1:8288` on every process to run against a local dev server instead of Cloud.

## Progress

Following the order in the spec:

1. Security gate: env scrub, resolved-path containment, fail-closed auth. Done.
2. Control plane: SQLite with outbox, REST, schedule clock, Inngest publish and Connect, host supervisor. Done.
3. `@aether/daemon` runtime, `conjure` and `tell`, one daemon, the morning review. Done.
4. Telegram: approvals out, then messages in. Next.
5. MVP completion checks: retry dedupe, approval recovery after restart, failure and stale-work markers.

Manual run, 2026-09-06, against a local Inngest dev server (Cloud keys were not on the build machine; the Cloud pass is still owed): `serve` and `connect --name devhost` connected as `aether` and `host-devhost`; `conjure foo --host devhost` scaffolded `daemons/foo`, ran `bun install` and `git init`, and `daemon-foo` connected with `turn`; `tell foo remember to water the plants` produced the assistant reply and memory v1; a `morning-review` schedule due the next minute fired through `scheduler.tick`, and the daemon replied with the review and wrote memory v2. Outbox rows all published, no markers written.

Today the CLI has `serve`, `connect`, `conjure`, `tell`, and `version`. REST covers daemons, messages, thread replies, approval requests, markers, soul and memory, schedules, and hosts. The SDK connects over Inngest Connect, runs the turn behind steps (load, model, reply, memory), skips stale schedules with a marker, and ships a template model that is swappable through `defineDaemon({ model })`.
