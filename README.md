# aether

Personal daemons with memory, schedules, and shared threads across channels.

Status: spec decided. Control plane, host, one conjured daemon, and Telegram approvals and messages run end to end against a local dev server; MVP completion checks next.

- `aether`: always-on control plane (REST, channels, scheduler, registry).
- hosts: run daemon processes on real machines.
- daemons: per-directory workers that execute turns and report outcomes.
- state: SQLite owns application state; Inngest Cloud owns durable execution.

MVP: Go control plane and CLI, one droplet host, one TypeScript daemon, and Telegram. Messages, memory, schedules, and durable tool approvals are in scope. Additional hosts, deploy automation, self-change, TUI/web/Slack, and other daemon languages follow after MVP. Design is in [the spec](ai_spec.md); layout and rules are in `CLAUDE.md`. The landing page at [thelinell.com/aether](https://thelinell.com/aether/) lives in `web/`.

MIT licensed.

## Run

Set `AETHER_TOKEN`, `INNGEST_EVENT_KEY`, and `INNGEST_SIGNING_KEY`. Aether refuses to start without them. The Inngest keys come from the Cloud dashboard; the token is any secret you pick, shared by `serve`, the CLI, hosts, and daemons. Set `INNGEST_DEV=http://127.0.0.1:8288` on every process to use a local dev server instead of Cloud.

You need Go, bun, Node (for `npx`), and curl. `serve` listens on 8080 and the dev server on 8288 by default.

Daemons pick a model from `AETHER_MODEL`: `openai:<model>` (default `openai:gpt-5.4-mini`), `anthropic:<model>`, or `scripted` for an offline stub. Set `OPENAI_API_KEY` or `ANTHROPIC_API_KEY` to match; `connect` passes both through to daemon processes. `AETHER_MAX_TURNS` caps model calls per turn (default 50).

Every `aether` command also reads `.env` from the working directory, filling in variables the shell has not already set. Copy `.env.example` to `.env` (git ignores it) and fill in the keys once.

The quick way is `make up`. It builds, starts a local Inngest dev server when `INNGEST_DEV` is set and nothing answers there, then starts `serve` and `connect` on this machine. All three log to the terminal. Ctrl-C stops all of them, and if one exits the rest stop too. `ALLOWLIST`, `DB`, `LISTEN`, and `ROOT` in `.env` pass through to the flags below.

```
cp .env.example .env    # then fill in the keys
make up
```

Or by hand:

```
export AETHER_TOKEN=$(openssl rand -hex 32) INNGEST_EVENT_KEY=... INNGEST_SIGNING_KEY=...
make build
bin/aether serve   --db aether.db --listen :8080
bin/aether connect --aether http://127.0.0.1:8080 --root ./daemons
bin/aether conjure foo                 # --host defaults to this machine
bin/aether tell foo remember to water the plants
bin/aether tell foo run echo hi        # outside the allowlist: pauses for approval
bin/aether approve <approval-id>       # or deny; Telegram buttons hit the same path
curl -X PUT localhost:8080/v1/daemons/foo/schedules/morning-review \
  -H "Authorization: Bearer $AETHER_TOKEN" \
  -d '{"cron":"0 7 * * *","tz":"America/New_York","policy":"queue"}'
```

The schedule PUT has no `thread`, so it lands on the daemon's default thread, the one `tell` writes to.

### serve

Opens the store, serves REST under `/v1`, connects to Inngest Cloud as app `aether`, registers `scheduler.tick` and the Telegram delivery functions, and drains the outbox.

`--allowlist file.json` lists tool calls that run without approval. Anything unmatched becomes an approval row.

```json
{"rules":[{"tool":"shell","argv":["ls","..."]}]}
```

`*` matches one argument and `...` the rest. Path-like arguments must resolve inside the thread directory.

### connect

Registers this machine as a host and connects as `host-<name>`. It scaffolds daemons on `daemon/conjure.requested` and spawns the `run` command from each daemon's `aether.json` under `--root` with a scrubbed env plus `AETHER_URL`, `AETHER_TOKEN`, and the Inngest keys.

`--sdk` is the `@aether/daemon` dependency written into scaffolds. The default, `file:../../packages/daemon`, fits `--root ./daemons` in this repo.

### Telegram

Set `TELEGRAM_BOT_TOKEN` on `serve` to turn it on. Approvals and replies for a bound chat go out through the Bot API. `POST /v1/channels/telegram/webhook` takes text and the approve/deny buttons, authenticated only by `TELEGRAM_WEBHOOK_SECRET`. First contact binds the chat to the daemon's default thread; send `/start <daemon>` when several exist. `TELEGRAM_API_URL` points the client at a fake for local runs.

## Progress

Following the order in the spec:

1. Security gate: env scrub, resolved-path containment, fail-closed auth. Done.
2. Control plane: SQLite with outbox, REST, schedule clock, Inngest publish and Connect, host supervisor. Done.
3. `@aether/daemon` runtime, `conjure` and `tell`, one daemon, the morning review. Done.
4. Telegram: approvals out, then messages in. Done.
5. MVP completion checks: retry dedupe, approval recovery after restart, failure and stale-work markers. Next.
6. Agent runtime: OpenAI Agents JS turn, provider selection via `AETHER_MODEL`, approval state round trip, post-turn memory extraction. In progress.

Manual run, 2026-09-06, against a local Inngest dev server (Cloud keys were not on the build machine; the Cloud pass is still owed): `serve` and `connect --name devhost` connected as `aether` and `host-devhost`; `conjure foo --host devhost` scaffolded `daemons/foo`, ran `bun install` and `git init`, and `daemon-foo` connected with `turn`; `tell foo remember to water the plants` produced the assistant reply and memory v1; a `morning-review` schedule due the next minute fired through `scheduler.tick`, and the daemon replied with the review and wrote memory v2. Outbox rows all published, no markers written.

Manual run, 2026-09-06, local Inngest dev server plus a fake Telegram Bot API (a bun server recording every `sendMessage`): `serve --allowlist` (rule: `shell ls ...`) and `connect --name devhost` came up with `aether` registering `scheduler.tick`, `telegram-approval`, and `telegram-reply`; `conjure foo` scaffolded and `daemon-foo` connected with `turn`. A webhook text `hello` from chat 42 bound the chat to foo's default thread and the reply reached the fake chat; `run echo approved-run` paused the turn (`turn.paused` marker, one `daemon/approval.requested`), and the approval landed in the fake chat with Approve/Deny buttons. Answering through the webhook callback recorded one decision (a replayed `update_id` and a later deny were ignored), the daemon claimed one `tool.call` operation, ran the command once, and the output reached both the thread and the chat. Then `run echo after-restart` was left pending while `serve` and `connect` were stopped and restarted; approving after the restart ran it once with the same result. `tell foo run ls` ran without an approval row. Every outbox row published, no failure markers.

Today the CLI has `serve`, `connect`, `conjure`, `tell`, `approve`, `deny`, and `version`. REST covers daemons, messages, thread replies, approval requests and answers, allowlist matching, operation claims, markers, soul and memory, schedules, hosts, and the Telegram webhook. The SDK connects over Inngest Connect and builds one OpenAI Agents JS agent per turn, with each model call and tool call behind its own step. `AETHER_MODEL` picks the provider: OpenAI directly, Anthropic through the AI SDK adapter, or `scripted` offline. An unmatched tool call pauses the run; the daemon posts the calls plus the serialized run state to the approval row, the run ends, and `aether/approval.answered` starts a new run that restores that state, answers the call, and continues (or pauses again). Every tool execution claims its operation ID first. After a reply, a tool-less agent rewrites memory and puts it against the version loaded at start. The `shell` tool runs with a scrubbed env inside the thread directory, stale schedules leave a marker, and `defineDaemon({ model, tools })` swaps either.

## Going live

Still owed: the Inngest Cloud pass from step 3 (dev server only so far), now covering the Telegram functions too.

Telegram needs to reach `serve` over HTTPS. Locally, `ngrok http 8080` gives you a URL; on a server, point your domain at it.

1. Create a bot with `@BotFather` (`/newbot`). Put the token it returns in `.env` as `TELEGRAM_BOT_TOKEN`, and pick a random `TELEGRAM_WEBHOOK_SECRET`.
2. Tell Telegram where to send updates:

   ```
   curl "https://api.telegram.org/bot$TELEGRAM_BOT_TOKEN/setWebhook" \
     -d url=https://<your-host>/v1/channels/telegram/webhook \
     -d secret_token=$TELEGRAM_WEBHOOK_SECRET
   ```

   Rerun this whenever the URL changes; free ngrok URLs change on every restart.
3. Message the bot once (`/start <daemon>` if more than one) to bind the chat.

For Cloud instead of the dev server: put `INNGEST_EVENT_KEY` and `INNGEST_SIGNING_KEY` from the Inngest dashboard in `.env` and remove `INNGEST_DEV`.
