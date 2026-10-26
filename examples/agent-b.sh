#!/usr/bin/env sh
#
# Agent B — the half of the pair whose second acquire is refused.
#
# It takes the port agent A wants, waits until A is actually waiting for it,
# and then asks for the file A holds. Taking both would close b -> a -> b, so
# the broker refuses that acquire instead of letting both agents hang until
# someone notices.
#
# Run it under `leasekeeper run`:
#
#   leasekeeper run --name b -- ./examples/agent-b.sh
#
set -eu

leasekeeper acquire port:8080
echo "b: holding port:8080"

# Wait until a is queued for the port, so the refusal below is a real circular
# wait and not just a race that happened to go the other way.
tries=0
while ! leasekeeper status | grep -q -- '->'; do
	tries=$((tries + 1))
	if [ "$tries" -gt 100 ]; then
		echo "b: a never started waiting for the port; giving up" >&2
		exit 1
	fi
	sleep 0.1
done

if leasekeeper acquire file:src/a.go; then
	echo "b: holding file:src/a.go"
	leasekeeper release file:src/a.go
else
	echo "b: refused file:src/a.go — leaving it to a"
fi

echo
leasekeeper status

leasekeeper release port:8080
