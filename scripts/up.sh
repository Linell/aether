#!/usr/bin/env sh
set -eu

cd "$(dirname "$0")/.."

if [ -f .env ]; then
	set -a
	. ./.env
	set +a
fi

: "${AETHER_TOKEN:?set AETHER_TOKEN (see .env.example)}"
: "${INNGEST_EVENT_KEY:?set INNGEST_EVENT_KEY}"
: "${INNGEST_SIGNING_KEY:?set INNGEST_SIGNING_KEY}"

DB="${DB:-aether.db}"
LISTEN="${LISTEN:-:8080}"
ROOT="${ROOT:-./daemons}"
INNGEST_CLI_VERSION="${INNGEST_CLI_VERSION:-1.44.0}"
AETHER_URL="http://127.0.0.1:${LISTEN##*:}"

mkdir -p "$ROOT"

pids=""

killtree() {
	for p in "$@"; do
		kids=$(pgrep -P "$p" || true)
		kill "$p" 2>/dev/null || true
		killtree $kids
	done
}

alive() {
	for p in $pids; do kill -0 "$p" 2>/dev/null || return 1; done
}

wait_for() {
	tries=0
	until curl -sf -o /dev/null "$1"; do
		alive || { echo "up: a process exited while waiting for $1" >&2; exit 1; }
		tries=$((tries + 1))
		[ "$tries" -lt 60 ] || { echo "up: $1 did not answer within 60s" >&2; exit 1; }
		sleep 1
	done
}

trap 'killtree $pids; wait' EXIT
trap 'exit 130' INT TERM

if [ -n "${INNGEST_DEV:-}" ] && ! curl -sf -o /dev/null "$INNGEST_DEV/"; then
	npx --yes "inngest-cli@$INNGEST_CLI_VERSION" dev --no-discovery &
	pids="$pids $!"
	wait_for "$INNGEST_DEV/"
fi

set -- --db "$DB" --listen "$LISTEN"
[ -n "${ALLOWLIST:-}" ] && set -- "$@" --allowlist "$ALLOWLIST"
bin/aether serve "$@" &
pids="$pids $!"
wait_for "$AETHER_URL/healthz"

bin/aether connect --aether "$AETHER_URL" --root "$ROOT" &
pids="$pids $!"

while alive; do sleep 1; done
echo "up: a process exited, stopping the rest" >&2
exit 1
