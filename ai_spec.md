# Aether

Status: decided. Build progress is tracked in `README.md`.

## Goals

- Daemons: personal agents, each with its own memory and channels.
- Two daemon classes: **anchored** familiars (always-on, recurring work) and **opportunistic** daemons
  (can be offline and report not summoned).
- Reach them from a phone (Telegram, then web), a terminal, and Slack. One conversation, any device.
- Scheduled work while every laptop is closed.
- Work on real machines, including the laptop's files.
- Inngest Cloud is the backbone. One operator, but anyone can run their own Aether.

## MVP scope

The architecture below is decided. MVP limits the supported capabilities, not the control plane / host / daemon split.

- Go control plane and CLI, one host on the droplet, and one TypeScript daemon using `@aether/daemon`.
- SQLite application state and Inngest Cloud execution, with messages, memory, scheduled morning review, and durable tool approvals.
- Telegram for messages and approvals; CLI for lifecycle commands and sending text.
- Complete when a message, scheduled task, and approved tool call work end to end, including retry deduplication, approval recovery after restart, and visible failure or stale-work markers. Order step 1 is a gate, not a preference.

After MVP: laptop/second-host rollout and deploy automation, self-change, TUI, web, Slack, `@aether/client`, and Python or other daemon SDKs. Their descriptions below are the target design, not MVP requirements.

## Shape

```
Inngest Cloud: events, durable execution, cron, realtime
  ^ outbound Connect from aether, hosts, and daemons

droplet                                  laptop
  sqlite       application state           host-laptop   Inngest app, supervisor
                                            daemon-foo  Inngest app, child process
  aether       Go: REST, channels, clock
  host-droplet Inngest app, supervisor
    daemon-bar Inngest app, child process
```

- **Aether** is the always-on service the outside world talks to. It runs no turns.
- **A host** is a machine that ran `aether connect`. It supervises daemon processes by running the command in
  each one's manifest.
- **A daemon** is a directory on one host, run as its own Inngest app `daemon-<name>`. Wherever it connects
  from is where it lives. The agent is the daemon: `@aether/daemon` builds one OpenAI Agents JS agent per turn.
  Aether holds soul, memory, threads, approvals, the allowlist, and the daemon's model, and enforces policy. Providers sit behind one
  model adapter. The model is operator configuration on the daemon row (`daemons.model`), not a deployment setting; `AETHER_MODEL` is only the fallback default. Spec format `openai:<model>`, `anthropic:<model>`, or `scripted` for the offline stub. One app per daemon holds into the tens; past that, consolidate to one app per host,
  still filtering on `event.data.daemon`.
- **SQLite** holds daemons, souls, memory, threads, messages, approvals, schedules, and hosts. Soul and
  memory are whole documents in rows, and every write is versioned.
- One aether instance owns the database on persistent local disk, with WAL mode and backups; hosts and daemons access state through REST.
- **Inngest Cloud** owns queues, execution state, step results, and retries. Application state stays in SQLite.
- **Connect** auto-syncs function and trigger config when a worker connects or reconnects.

## Contract

Canonical event names use `scope/name.action`. Commands end in `.requested`; reported outcomes use past tense.

| message | direction | payload | usage |
|---|---|---|---|
| `aether/message.sent` | aether -> daemon | daemon, thread, text | Deliver user text into the daemon turn loop. |
| `aether/schedule.fired` | aether -> daemon | daemon, thread, schedule, prompt | Run one due anchored schedule. |
| `aether/approval.answered` | aether -> daemon | daemon, call, decision | Resume a paused turn after approval. |
| `daemon/message.replied` | daemon -> aether | daemon, thread, text | Persist assistant output and fan out to channels. |
| `daemon/approval.requested` | daemon -> aether | daemon, thread, calls | Create approval rows for pending calls. |
| `daemon/conjure.requested` | aether -> host | daemon, host, language | Request scaffold + start on a host. |
| `daemon/deploy.requested` | aether -> host | daemon | Request pull, install, and restart. |
| `daemon/dismiss.requested` | aether -> host | daemon | Request stop + archive. |
| `daemon/change.requested` | daemon -> aether | daemon, patch, tests | Request approval for a self-change patch. |

A daemon's functions filter on `event.data.daemon`. The turn function sets concurrency key `event.data.thread`, limit 1. Aether's adapters take everything. Register functions before dispatching work.
Connect's app semaphore keeps offline work queued without consuming retries. Pending runs execute on reconnect,
subject to cancellation, timeouts, and retention; this is not an indefinite-delivery guarantee.

Daemons talk to aether over REST and never open the database. That boundary lets a daemon be written in any language.

- Approval pause is REST, not an event. The daemon posts the pending calls with the paused run's state; `contract/events.json` is unchanged. A later pause for the same call supersedes the earlier state.
- Turn function retries at least 5. Routing-level `connect_no_healthy_connection` consumes one per attempt, so the budget must outlast a reconnect.
- `GET /threads/{id}/messages?before=<id>&limit=<n>` returns a thread's user and assistant rows newest first, `before` exclusive. Each turn loads recent history in its `load` step, budgeted by bytes and excluding the triggering message, and feeds it to the model ahead of the request. The memory pass sees only the current turn.
- Model is a daemon field over REST. `POST /daemons` accepts `model`, `GET /daemons/{name}` returns it, `PATCH /daemons/{name}` with `{model}` sets it. The SDK reads the daemon doc in the turn's `load` step, so a change applies on the next turn.

## Scheduling

- Recurring schedules apply to anchored daemons.
- Schedules are rows in aether (`cron`, `tz`, `next_run_at`, policy, `prompt`). A fired turn delivers the prompt as its request.
- Inngest functions stay static: `scheduler.tick` (cron) finds due rows and emits `aether/schedule.fired`.
- Daemons may create, edit, and delete their own schedules through REST without approval; aether enforces ownership and the anchored-only rule. These changes update rows, not function definitions.
- The SDK exposes `schedule_list`, `schedule_put`, and `schedule_delete`, bound to the turn's daemon and thread. A scheduled turn runs under the same allowlist as any other.
- Turn instructions state the current date, time, and timezone and the thread's directory and host.
- Offline policy is explicit per daemon: queue, skip with marker, or TTL then skipped marker.
- Cloud cron continues while daemons are offline. Check schedule deadlines before execution; do not blindly replay stale work.
- Ticks may burst after reconnect. Each fire checks its own deadline; stale ones leave a marker.

## Action policy

- MVP uses an operator-managed allowlist for actions that may run without approval; daemons cannot change it. Own-schedule management is allowed by default.
- Rules match the tool and constrained arguments, paths, and working directory, not just a shell-command prefix. Matching runs on resolved paths and a scrubbed environment, never raw strings. Unmatched actions require operator approval; actions outside the daemon's permissions are denied.
- A rule names a tool and a tool-specific constraint: `argv` prefix (`*` one arg, `...` the rest) for `shell`, whose `cwd` and path-like args must resolve inside the thread directory; a `path` glob (`**` spans directories) for `read_file`, `edit_file`, and `write_file`, matched against the path resolved inside the thread directory; an `args` object for any other tool, MCP tools included, whose entries must equal the call's arguments exactly (`{}` matches any arguments). A rule without its constraint matches nothing.
- Channel text, memory, and tool output are untrusted. They can request an action; they cannot widen a rule.
- Approval covers one operation's exact tool, arguments, and execution context; retries retain that operation's identity.
- The approval row carries the paused run's state as an opaque blob. Aether stores and returns it and never reads inside. Answers for calls not in the state are ignored.
- Claim the operation ID before every effect. A lapsed Connect lease can run a step twice; a failed claim returns a note, not a rerun.
- After MVP: explore a separate model judging actions not covered by the allowlist. Uncertain or unavailable judgments fall back to operator approval; model approval cannot override permission boundaries. This is risk assessment, not a sandbox.

## Self-change

After MVP.

- A daemon can propose a patch (`daemon/change.requested`).
- Aether stores it as an approval row. On approval, host applies patch, runs tests, restarts daemon.
- Function or trigger changes appear after reconnect via Connect auto-sync.

## Lifecycle

- `aether connect` registers this machine, stores a host token, installs a service, starts `host-<machine>`.
- `aether conjure <name> [--host h] [--model spec]` inserts a row and sends `daemon/conjure.requested` with language `ts`. The host
  scaffolds a TypeScript project, installs, runs `git init`, and spawns the manifest's `run`. Language selection comes with future SDKs.
- `aether model <name> [<spec>]` shows or sets the daemon's model. Sets `daemons.model` through `PATCH /daemons/{name}`; takes effect on the next turn, no restart.
- `aether daemon <name> deploy` sends `daemon/deploy.requested`. The host pulls, installs, restarts. Aether then compares the synced function set to the pre-deploy set and marks the deploy failed if it regressed.
- `aether dismiss <name>` archives the row and sends `daemon/dismiss.requested`. The host stops the child.
- `aether tell <name> <text>` sends `aether/message.sent`. `aether summon <name>` opens the TUI on one daemon.
- On boot a host reads its daemons from aether and spawns any not running. Spawning is idempotent.
- One connected worker per daemon app is a host-supervisor rule: one process per daemon, and the daemon connects with worker concurrency 1.
- Provider keys (`OPENAI_API_KEY`, `ANTHROPIC_API_KEY`) pass through the host's scrubbed env to daemon processes. The model itself is not env; it lives on the daemon row.

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
- State changes and dispatch intent commit together in a SQLite transaction with an outbox; publish with stable event IDs.
- Effects must tolerate retries. Persist operation deduplication and per-thread turn ordering; Cloud does not make external effects exactly-once.
- Rows are truth. Realtime is a hint, and every client is correct having received none.
- Inside a function, only non-deterministic edges are steps.
- A new capability is an event consumed by a function.
- Nothing is silently dropped. A failed or cancelled turn leaves a marker.
- Missed scheduled work leaves a visible marker.
- Approval is out of band: a row, answerable from any surface, no timeout.
- A paused turn serializes to a row and the run ends. Approval dispatches a new run through the outbox. `invoke` and `sleep` are fine where Inngest persists.
- A thread belongs to a directory on a machine.
- Recurring work belongs to anchored daemons.
- Core never knows the surface.
- One connected worker per daemon app.
- The soul is written by the operator and read-only to the model. The daemon writes its memory.
- Bound to the tailnet, auth fails closed.

## Packages

- `@aether/daemon` (npm): the SDK on OpenAI Agents JS. Turn loop, tools, approval pause, aether client. OpenAI direct; Anthropic through the AI SDK adapter. Model precedence per turn: explicit `defineDaemon({ model })`, then the daemon row, then `AETHER_MODEL`, then `openai:gpt-5.4-mini`. Function tools only; hosted tools are excluded so tools stay portable. The SDK ships `shell`, `read_file`, `edit_file` (unique exact replacement), `write_file` (temp file and rename, symlinks never followed), and the schedule tools. `mcp({ name, command, args })` or `mcp({ name, url })` attaches an MCP server over stdio (scrubbed env) or streamable HTTP; its tools are listed once per process and each is wrapped in the same match, claim, and step plumbing as the built-ins, so the Agent never receives `mcpServers` directly. One step per model call and per tool call. Memory is a post-turn extraction step: a tool-less agent rewrites the memory document, put with the version loaded at start, conflict leaves a marker.
- `@aether/client` (after MVP): shared client for TypeScript surfaces.
- `aether` (Go): REST, SQLite, channels, schedule clock, host and daemon registry, the CLI.
- `aether-daemon` (PyPI, future): Python daemon SDK on the same REST/event contract.

## Verification

- Verified by the operator: Inngest Cloud offline delivery and reconnection work as expected. This is not an unresolved architecture dependency; the cancellation, timeout, and retention limits above still apply. Routing-level `connect_no_healthy_connection` consumes retries if gating is bypassed.
- Application verification still required: crash after an effect but before acknowledgement; a step run twice after a lapsed Connect lease claims once; approval resume after restart, including a second pause from the restored state; stale scheduled work and tick bursts after reconnect; memory conflict marker; visible failure and cancellation markers.
- Configuration verification still required: first registration and reconnect after function changes. Reconnecting old code can roll back app configuration.

## Rejected

- `step.ai.wrap`: memoizes each call's input, and a model request is the whole item list, so run state doubles.
- Waiting on approval inside the turn (`waitForEvent`, `sleep`): approval has no timeout, so the paused run ends.
- `step.ai.infer`: no tool loop; the Agents runner owns the model call.
- Hosted tools (shell, apply patch, web search): tie the daemon to one provider.

## Order

1. Security: scrub `shell`'s env, `realpath` in path checks, auth that fails closed.
2. Droplet: aether with SQLite, REST, and the schedule clock, `host-droplet`; connect to Inngest Cloud.
3. `@aether/daemon` against REST, one conjured daemon, the morning review on a schedule.
4. `daemon/approval.requested` delivered to Telegram, then Telegram inbound.
5. Pass the MVP completion checks above before expanding scope.
6. After MVP: `aether connect` on the laptop, a second daemon, `deploy`.
7. After MVP: TUI over `@aether/client` pointed at aether. Web and Slack.
