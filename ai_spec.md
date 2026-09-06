# Aether

Status: decided, not built.

## Goals

- Daemons: personal agents, each with its own memory and channels.
- Two daemon classes: **anchored** familiars (always-on, recurring work) and **opportunistic** daemons
  (can be offline and report not summoned).
- Reach them from a phone (Telegram, then web), a terminal, and Slack. One conversation, any device.
- Scheduled work while every laptop is closed.
- Work on real machines, including the laptop's files.
- Inngest is the backbone. One operator, but anyone can run their own.

## Shape

```
droplet                                  laptop
  inngest      self-hosted                 host-laptop   Inngest app, supervisor
  postgres     the one truth                 daemon-foo  Inngest app, child process
  aether       Go: REST, channels, clock
  host-droplet Inngest app, supervisor
    daemon-bar Inngest app, child process
```

- **Aether** is the always-on service the outside world talks to. It runs no turns.
- **A host** is a machine that ran `aether connect`. It supervises daemon processes by running the command in
  each one's manifest.
- **A daemon** is a directory on one host, run as its own Inngest app `daemon-<name>`. Wherever it connects
  from is where it lives.
- **Postgres** holds daemons, souls, memory, threads, messages, approvals, schedules, and hosts. Soul and
  memory are whole documents in rows, and every write is versioned.
- **Inngest** carries events and realtime hints. State stays in Postgres.
- **Connect** auto-syncs function and trigger config when a worker connects or reconnects.

## Contract

Canonical event names use `scope/name.action`. Commands end in `.requested`; reported outcomes use past tense.

| message | direction | payload | usage |
|---|---|---|---|
| `aether/message.sent` | aether -> daemon | daemon, thread, text | Deliver user text into the daemon turn loop. |
| `aether/schedule.fired` | aether -> daemon | daemon, thread, schedule | Run one due anchored schedule. |
| `aether/approval.answered` | aether -> daemon | daemon, call, decision | Resume a paused turn after approval. |
| `daemon/message.replied` | daemon -> aether | daemon, thread, text | Persist assistant output and fan out to channels. |
| `daemon/approval.requested` | daemon -> aether | daemon, thread, calls | Create approval rows for pending calls. |
| `daemon/conjure.requested` | aether -> host | daemon, host, language | Request scaffold + start on a host. |
| `daemon/deploy.requested` | aether -> host | daemon | Request pull, install, and restart. |
| `daemon/dismiss.requested` | aether -> host | daemon | Request stop + archive. |
| `daemon/change.requested` | daemon -> aether | daemon, patch, tests | Request approval for a self-change patch. |

A daemon's functions filter on `event.data.daemon`. Aether's adapters take everything. An offline daemon's runs
wait in Inngest and execute on reconnect.

Daemons talk to aether over REST and never open Postgres. That boundary lets a daemon be written in any language.

## Scheduling

- Recurring schedules apply to anchored daemons.
- Schedules are rows in aether (`cron`, `tz`, `next_run_at`, policy).
- Inngest functions stay static: `scheduler.tick` (cron) finds due rows and emits `aether/schedule.fired`.
- Agent-created schedules update rows, not function definitions.
- Offline policy is explicit per daemon: queue, skip with marker, or TTL then skipped marker.

## Self-change

- A daemon can propose a patch (`daemon/change.requested`).
- Aether stores it as an approval row. On approval, host applies patch, runs tests, restarts daemon.
- Function or trigger changes appear after reconnect via Connect auto-sync.

## Lifecycle

- `aether connect` registers this machine, stores a host token, installs a service, starts `host-<machine>`.
- `aether conjure <name> [--host h] [-l ts|python]` inserts a row and sends `daemon/conjure.requested`. The host
  scaffolds a project with the language's own tool, installs, runs `git init`, and spawns the manifest's `run`.
- `aether daemon <name> deploy` sends `daemon/deploy.requested`. The host pulls, installs, restarts.
- `aether dismiss <name>` archives the row and sends `daemon/dismiss.requested`. The host stops the child.
- `aether tell <name> <text>` sends `aether/message.sent`. `aether summon <name>` opens the TUI on one daemon.
- On boot a host reads its daemons from aether and spawns any not running. Spawning is idempotent.

A daemon directory is a git repo. The host reads one file in it:

```
aether.json   { "name": "foo", "run": "bun run start" }
```

The default scaffold is that file, a `package.json` depending on `@aether/daemon`, and a three-line `index.ts`.
A daemon with no custom code is the default scaffold left alone.

## Channels

Channels live in aether, so a message lands and queues even when the daemon's host is asleep. Each channel binds an
external conversation to a thread and renders `daemon/message.replied` back. Telegram first, web second, Slack third.

## Laws

- Aether owns state. Clients render it and call actions.
- Rows are truth. Realtime is a hint, and every client is correct having received none.
- Inside a function, only non-deterministic edges are steps.
- A new capability is an event consumed by a function.
- Nothing is silently dropped. A failed or cancelled turn leaves a marker.
- Missed scheduled work leaves a visible marker.
- Approval is out of band: a row, answerable from any surface, no timeout.
- A paused turn serializes to a row and the run ends. `invoke` and `sleep` are fine where Inngest persists.
- A thread belongs to a directory on a machine.
- Recurring work belongs to anchored daemons.
- Core never knows the surface.
- One connected worker per daemon app.
- The soul is written by the operator and read-only to the model. The daemon writes its memory.
- Bound to the tailnet, auth fails closed.

## Packages

- `@aether/daemon` (npm): the SDK. Turn loop, tools, approval pause, aether client. Today's `packages/daemon`.
- `@aether/client`: unchanged. Every surface imports it.
- `aether` (Go): REST, Postgres, channels, schedule clock, host and daemon registry, the CLI.
- `aether-daemon` (PyPI, future): Python daemon SDK on the same REST/event contract.

## Verify first

- What happens to queued runs when a daemon has zero connected workers, and for how long they persist.
- Cron behavior while workers are disconnected.
- Connect limits and accounting (connections, apps per connection, worker identity by `instanceId`).
- Reconnect sync behavior for function and trigger changes.

## Order

1. Security: scrub `shell`'s env, `realpath` in path checks, auth that fails closed.
2. Droplet: Inngest, Postgres, aether with REST and the schedule clock, `host-droplet`.
3. `@aether/daemon` against REST, one conjured daemon, the morning review on a schedule.
4. `daemon/approval.requested` delivered to Telegram, then Telegram inbound.
5. `aether connect` on the laptop, a second daemon, `deploy`.
6. TUI over `@aether/client` pointed at aether. Slack.
