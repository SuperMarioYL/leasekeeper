# Changelog

All notable changes to Leasekeeper are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] — 2026-10-27

The first release: a local lease broker with a wait-for graph that refuses a
circular wait before it deadlocks.

### Added

- `leasekeeper run -- <command>` starts a broker on a Unix socket and runs the
  command with `LEASEKEEPER_SOCKET` and `LEASEKEEPER_AGENT` in its environment.
  Every lease the command still holds is handed back when it exits.
- `leasekeeper acquire <resource>` takes a lease and blocks until the agent owns
  it, queueing behind the current holder. `--no-wait` reports the holder and
  returns; `--ttl` bounds the lease.
- `leasekeeper release <resource>` and `release --all` hand leases back. Only the
  holder may release, and the first queued waiter is granted the lease in order.
- `leasekeeper status` prints the lease table and the current wait-for graph.
- `leasekeeper demo` plays the two-agent cycle refusal in one process on a
  temporary socket, with no configuration and no second terminal.
- The wait-for graph refuses an acquire that would close a cycle, printing the
  path (`b -> a -> b`) and exiting non-zero instead of blocking.
- Four resource kinds: `file:<path>`, `port:<n>`, `gpu:<id>` and `quota:<name>`.
  The kind is part of the name, so a path can never collide with a port.
- Terminal reports for the lease table, the wait-for graph and each verdict,
  with colour that turns itself off when output is piped or `NO_COLOR` is set.
- The broker counts live run sessions, so the last run to leave shuts it down and
  every lease drops with it.

### Notes

- The broker is one local process per run on a single host. There is no daemon,
  no database and no distributed coordination.
- An agent, or its wrapper, must call `acquire` and `release`. Kernel-level
  interception is out of scope.
- GPU and quota are named resources an agent leases by agreement; nothing is
  fenced in a driver or a runtime.

[Unreleased]: https://github.com/SuperMarioYL/leasekeeper/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/SuperMarioYL/leasekeeper/releases/tag/v0.1.0
