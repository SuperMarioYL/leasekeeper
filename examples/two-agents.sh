#!/usr/bin/env sh
#
# The whole story in one terminal: two agents, one resource pair, one refused
# acquire.
#
#   go build -o leasekeeper ./cmd/leasekeeper
#   LEASEKEEPER=./leasekeeper ./examples/two-agents.sh
#
# Both agents run against one broker on a temporary socket. Agent a takes
# file:src/a.go and then waits for port:8080; agent b holds the port and is
# refused when it asks for a's file, because that would close b -> a -> b.
#
set -eu

cd "$(dirname "$0")/.."

LEASEKEEPER="${LEASEKEEPER:-leasekeeper}"
if [ ! -x "$LEASEKEEPER" ] && ! command -v "$LEASEKEEPER" >/dev/null 2>&1; then
	echo "two-agents.sh: build it first: go build -o leasekeeper ./cmd/leasekeeper" >&2
	exit 1
fi

socket_dir=$(mktemp -d)
LEASEKEEPER_SOCKET="$socket_dir/lk.sock"
export LEASEKEEPER_SOCKET
trap 'rm -rf "$socket_dir"' EXIT

echo "leasekeeper: broker on $LEASEKEEPER_SOCKET"
echo

# a runs in the background because it is the one that blocks.
"$LEASEKEEPER" run --name a -- ./examples/agent-a.sh &
a_pid=$!

# Let a take the file before b starts, so the transcript reads in order.
tries=0
while ! "$LEASEKEEPER" status 2>/dev/null | grep -q 'file:src/a.go'; do
	tries=$((tries + 1))
	if [ "$tries" -gt 600 ]; then
		echo "two-agents.sh: agent a never took the file" >&2
		kill "$a_pid" 2>/dev/null || true
		exit 1
	fi
	sleep 0.1
done

"$LEASEKEEPER" run --name b -- ./examples/agent-b.sh

wait "$a_pid"

echo
echo "== after both agents exit =="
"$LEASEKEEPER" status
