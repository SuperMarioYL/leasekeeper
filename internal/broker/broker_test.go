package broker_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SuperMarioYL/leasekeeper/internal/broker"
	"github.com/SuperMarioYL/leasekeeper/internal/client"
	"github.com/SuperMarioYL/leasekeeper/internal/lease"
)

// shortSocketPath keeps the socket inside the Unix address limit, which a
// t.TempDir() path built from a long test name can exceed.
func shortSocketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "lk")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "b.sock")
}

// newTestBroker starts a broker on a socket inside the test's temp dir.
func newTestBroker(t *testing.T) (*broker.Broker, *client.Client) {
	t.Helper()
	path := shortSocketPath(t)
	brk, err := broker.Listen(path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	brk.Serve()
	t.Cleanup(brk.Close)
	return brk, client.Dial(path)
}

func mustResource(t *testing.T, name string) lease.Resource {
	t.Helper()
	res, err := lease.ParseResource(name)
	if err != nil {
		t.Fatalf("parse %q: %v", name, err)
	}
	return res
}

// TestAcquireRefusesCircularWaitOverTheSocket is the product claim, exercised
// the way an agent would hit it: two agents each take half of a resource pair
// and the second acquire is refused with the cycle named.
func TestAcquireRefusesCircularWaitOverTheSocket(t *testing.T) {
	_, cl := newTestBroker(t)
	ctx := context.Background()
	fileA := mustResource(t, "file:src/a.go")
	port := mustResource(t, "port:8080")

	if got, err := cl.Acquire(ctx, "a", fileA, 0, true, time.Second, nil); err != nil || got.Verdict != broker.Granted {
		t.Fatalf("a takes the file: %+v err=%v", got, err)
	}
	if got, err := cl.Acquire(ctx, "b", port, 0, true, time.Second, nil); err != nil || got.Verdict != broker.Granted {
		t.Fatalf("b takes the port: %+v err=%v", got, err)
	}

	// a waits for the port in the background; the broker records a -> b.
	waited := make(chan broker.Result, 1)
	go func() {
		res, err := cl.Acquire(ctx, "a", port, 0, true, 5*time.Second, nil)
		if err == nil {
			waited <- res
		}
	}()
	waitForEdge(t, cl, "a", "b")

	// b asking for a's file would close b -> a -> b.
	got, err := cl.Acquire(ctx, "b", fileA, 0, true, time.Second, nil)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if got.Verdict != broker.DeniedCycle {
		t.Fatalf("verdict = %q, want %q (message %q)", got.Verdict, broker.DeniedCycle, got.Message)
	}
	if want := "acquire denied — would deadlock: b -> a -> b"; got.Message != want {
		t.Fatalf("message = %q, want %q", got.Message, want)
	}

	// Handing the port back serves the queued waiter in order.
	if res, err := cl.Release(ctx, "b", port); err != nil || res.Verdict != broker.Released {
		t.Fatalf("b releases the port: %+v err=%v", res, err)
	}
	select {
	case res := <-waited:
		if res.Verdict != broker.Granted {
			t.Fatalf("queued acquire after hand-off: %+v", res)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("queued acquire was never granted")
	}

	// The cycle is gone: b can now take the file once a lets it go.
	if _, err := cl.Release(ctx, "a", fileA); err != nil {
		t.Fatalf("a releases the file: %v", err)
	}
	if res, err := cl.Acquire(ctx, "b", fileA, 0, true, time.Second, nil); err != nil || res.Verdict != broker.Granted {
		t.Fatalf("b takes the file after the cycle broke: %+v err=%v", res, err)
	}
}

// TestNoWaitReportsTheHolderWithoutQueueing keeps a probe from becoming a
// hidden claim on the resource.
func TestNoWaitReportsTheHolderWithoutQueueing(t *testing.T) {
	_, cl := newTestBroker(t)
	ctx := context.Background()
	port := mustResource(t, "port:8080")

	if _, err := cl.Acquire(ctx, "a", port, 0, true, time.Second, nil); err != nil {
		t.Fatalf("a takes the port: %v", err)
	}
	got, err := cl.Acquire(ctx, "b", port, 0, false, 0, nil)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if got.Verdict != broker.DeniedHeld || got.Holder != "a" {
		t.Fatalf("probe = %+v, want denied_held by a", got)
	}
	status, err := cl.Status(ctx)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if len(status.Edges) != 0 {
		t.Fatalf("a probe must not create a wait edge, got %+v", status.Edges)
	}
}

// TestReleaseByNonOwnerIsRefused keeps one agent from handing back another's
// lease.
func TestReleaseByNonOwnerIsRefused(t *testing.T) {
	_, cl := newTestBroker(t)
	ctx := context.Background()
	port := mustResource(t, "port:8080")

	if _, err := cl.Acquire(ctx, "a", port, 0, true, time.Second, nil); err != nil {
		t.Fatalf("a takes the port: %v", err)
	}
	got, err := cl.Release(ctx, "b", port)
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if got.Verdict != broker.Error {
		t.Fatalf("release by non-owner = %+v, want error", got)
	}
	status, err := cl.Status(ctx)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if len(status.Leases) != 1 || status.Leases[0].Owner != "a" {
		t.Fatalf("lease changed by a refused release: %+v", status.Leases)
	}
}

// TestTTLExpiryHandsTheLeaseOn proves an agent cannot hold a stale lease past
// its TTL, and that the freed resource goes to the queued waiter.
func TestTTLExpiryHandsTheLeaseOn(t *testing.T) {
	_, cl := newTestBroker(t)
	ctx := context.Background()
	gpu := mustResource(t, "gpu:0")

	if _, err := cl.Acquire(ctx, "a", gpu, 40*time.Millisecond, true, time.Second, nil); err != nil {
		t.Fatalf("a takes the gpu: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		status, err := cl.Status(ctx)
		if err != nil {
			t.Fatalf("status: %v", err)
		}
		if len(status.Leases) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("lease outlived its TTL: %+v", status.Leases)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestReleaseAllClearsWaitsAndLeases is what a finished `run` relies on.
func TestReleaseAllClearsWaitsAndLeases(t *testing.T) {
	_, cl := newTestBroker(t)
	ctx := context.Background()
	fileA := mustResource(t, "file:src/a.go")
	port := mustResource(t, "port:8080")

	if _, err := cl.Acquire(ctx, "a", fileA, 0, true, time.Second, nil); err != nil {
		t.Fatalf("a takes the file: %v", err)
	}
	if _, err := cl.Acquire(ctx, "b", port, 0, true, time.Second, nil); err != nil {
		t.Fatalf("b takes the port: %v", err)
	}
	go func() {
		_, _ = cl.Acquire(ctx, "a", port, 0, true, 3*time.Second, nil)
	}()
	waitForEdge(t, cl, "a", "b")

	result, err := cl.ReleaseAll(ctx, "b")
	if err != nil {
		t.Fatalf("release-all: %v", err)
	}
	if len(result.Released) != 1 || result.Released[0] != "port:8080" {
		t.Fatalf("release-all freed %v, want [port:8080]", result.Released)
	}

	status, err := cl.Status(ctx)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if len(status.Edges) != 0 {
		t.Fatalf("b's release-all left a wait edge: %+v", status.Edges)
	}
	// The freed port went to the queued waiter, so a now holds both halves and
	// nothing is waiting on anything.
	if len(status.Leases) != 2 {
		t.Fatalf("after b left, leases = %+v, want both resources held by a", status.Leases)
	}
	for _, l := range status.Leases {
		if l.Owner != "a" {
			t.Fatalf("lease %s is owned by %s, want a", l.Resource, l.Owner)
		}
	}
}

// TestSessionRefCountClosesTheBroker proves the last attached run shuts the
// broker down, so leases never outlive the work that held them.
func TestSessionRefCountClosesTheBroker(t *testing.T) {
	brk, cl := newTestBroker(t)
	ctx := context.Background()

	first, err := cl.OpenSession(ctx)
	if err != nil {
		t.Fatalf("open first session: %v", err)
	}
	second, err := cl.OpenSession(ctx)
	if err != nil {
		t.Fatalf("open second session: %v", err)
	}
	waitFor(t, func() bool { return brk.RefCount() == 2 })

	first.Close()
	waitFor(t, func() bool { return brk.RefCount() == 1 })
	select {
	case <-brk.Done():
		t.Fatal("broker shut down while a session was still attached")
	default:
	}

	second.Close()
	select {
	case <-brk.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("broker stayed up after the last session left")
	}
}

// TestListenRefusesASecondBroker keeps two brokers from racing on one socket.
func TestListenRefusesASecondBroker(t *testing.T) {
	brk, _ := newTestBroker(t)
	if _, err := broker.Listen(brk.Path()); err == nil {
		t.Fatal("a second broker.Listen on a live socket must fail so the caller attaches instead")
	}
}

// TestStatusOnMissingBrokerIsAnError keeps the CLI's "no broker" path honest.
func TestStatusOnMissingBrokerIsAnError(t *testing.T) {
	cl := client.Dial(shortSocketPath(t))
	if _, err := cl.Status(context.Background()); !errors.Is(err, client.ErrNoBroker) {
		t.Fatalf("err = %v, want ErrNoBroker", err)
	}
}

func waitForEdge(t *testing.T, cl *client.Client, waiter, holder string) {
	t.Helper()
	waitFor(t, func() bool {
		status, err := cl.Status(context.Background())
		if err != nil {
			return false
		}
		for _, e := range status.Edges {
			if e.Waiter == waiter && e.Holder == holder {
				return true
			}
		}
		return false
	})
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition never became true")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
