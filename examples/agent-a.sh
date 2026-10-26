#!/usr/bin/env sh
#
# Agent A — the half of the pair that ends up waiting.
#
# It takes the shared source file, waits until agent B holds the port, and only
# then asks for the port. That acquire blocks: the broker queues a behind b and
# records the edge a -> b in the wait-for graph.
#
# Run it under `leasekeeper run`, which puts LEASEKEEPER_SOCKET and
# LEASEKEEPER_AGENT in the environment:
#
#   leasekeeper run --name a -- ./examples/agent-a.sh
#
set -eu

leasekeeper acquire file:src/a.go
echo "a: holding file:src/a.go"

# Wait until b holds the port, so a's request for it is a real wait rather than
# a silent grab of a free resource.
tries=0
while ! leasekeeper status | grep -q 'port:8080'; do
	tries=$((tries + 1))
	if [ "$tries" -gt 600 ]; then
		echo "a: b never took the port; giving up" >&2
		exit 1
	fi
	sleep 0.1
done

leasekeeper acquire port:8080
echo "a: holding port:8080"

leasekeeper release port:8080
leasekeeper release file:src/a.go
