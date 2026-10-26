// Package broker owns the lease table and the wait-for graph for one run and
// serves them over a Unix socket.
//
// One broker process exists per `leasekeeper run` session: the first run starts
// it, later runs attach to the same socket, and it shuts down when the last
// attached run exits. Lease state lives only in this process's memory, so when
// the broker dies every lease drops and no agent can hold a stale lease
// forever.
package broker

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/SuperMarioYL/leasekeeper/internal/lease"
	"github.com/SuperMarioYL/leasekeeper/internal/waitgraph"
)

// Environment variables the CLI and the broker agree on.
const (
	// EnvSocket names the broker socket a child agent should talk to.
	EnvSocket = "LEASEKEEPER_SOCKET"
	// EnvAgent names the agent identity a child agent acts as.
	EnvAgent = "LEASEKEEPER_AGENT"
)

// DefaultWaitTimeout bounds a blocking acquire that does not set its own.
const DefaultWaitTimeout = 30 * time.Second

// unixPathLimit is the conservative end of the sun_path limit (104 bytes on
// macOS, 108 on Linux). A path longer than this fails to bind.
const unixPathLimit = 100

// Verdict is the broker's answer to one request.
type Verdict string

// The verdicts a request can come back with.
const (
	// Granted means the caller now owns the resource.
	Granted Verdict = "granted"
	// DeniedHeld means someone else owns it and the caller is queued (or,
	// without --wait, told to come back later).
	DeniedHeld Verdict = "denied_held"
	// DeniedCycle means waiting would close a loop, so the acquire is refused.
	DeniedCycle Verdict = "denied_cycle"
	// Released means the caller handed a lease back.
	Released Verdict = "released"
	// OK is a successful bookkeeping answer with nothing to own.
	OK Verdict = "ok"
	// Error is a refused or malformed request.
	Error Verdict = "error"
)

// Result is the outcome of one acquire or release.
type Result struct {
	Verdict  Verdict
	Resource lease.Resource
	Holder   string
	Cycle    []string
	Message  string
	Queued   bool
	// Released lists the resources handed back, for release-all.
	Released []string
}

// LeaseView is a lease as it appears on the wire and in reports.
type LeaseView struct {
	Resource     string  `json:"resource"`
	Owner        string  `json:"owner"`
	AgeSec       float64 `json:"age_sec"`
	TTLSec       float64 `json:"ttl_sec"`
	ExpiresInSec float64 `json:"expires_in_sec"`
}

// EdgeView is one waiter -> holder edge.
type EdgeView struct {
	Waiter string `json:"waiter"`
	Holder string `json:"holder"`
}

// Status is the broker's view of the world at one instant.
type Status struct {
	Leases []LeaseView `json:"leases"`
	Edges  []EdgeView  `json:"edges"`
}

// Request is one client operation.
type Request struct {
	Op         string  `json:"op"`
	Agent      string  `json:"agent,omitempty"`
	Resource   string  `json:"resource,omitempty"`
	TTLSec     float64 `json:"ttl_sec,omitempty"`
	Wait       bool    `json:"wait,omitempty"`
	TimeoutSec float64 `json:"timeout_sec,omitempty"`
}

// Response is one line of a reply. A waiting acquire produces two: a queued
// notice and then the final verdict.
type Response struct {
	Verdict  Verdict     `json:"verdict"`
	Message  string      `json:"message,omitempty"`
	Resource string      `json:"resource,omitempty"`
	Holder   string      `json:"holder,omitempty"`
	Cycle    []string    `json:"cycle,omitempty"`
	Leases   []LeaseView `json:"leases,omitempty"`
	Edges    []EdgeView  `json:"edges,omitempty"`
	Released []string    `json:"released,omitempty"`
	Queued   bool        `json:"queued,omitempty"`
}

// AcquireOptions describes one acquire.
type AcquireOptions struct {
	Agent    string
	Resource lease.Resource
	TTL      time.Duration
	// Wait blocks until the resource is handed over. Without it the caller is
	// told who holds the resource and returns immediately.
	Wait bool
	// Timeout bounds a blocking acquire. Zero uses DefaultWaitTimeout.
	Timeout time.Duration
	// Notify, when set, is called once with the queued verdict before the
	// caller starts blocking. It lets the CLI print the wait as it happens.
	Notify func(Result)
}

// waiter is one queued acquire.
type waiter struct {
	agent    string
	holder   string
	resource lease.Resource
	ttl      time.Duration
	ch       chan Result
	granted  bool
}

// Broker is the in-memory lease service for one run.
type Broker struct {
	path string
	ln   net.Listener

	mu      sync.Mutex
	table   *lease.Table
	graph   *waitgraph.WaitGraph
	queues  map[lease.Resource][]*waiter
	pending map[string]map[lease.Resource]*waiter
	refs    int

	stop   chan struct{}
	done   chan struct{}
	closed sync.Once
	wg     sync.WaitGroup
}

// DefaultSocketPath returns the socket to use when the caller does not set
// LEASEKEEPER_SOCKET. It is per-user so two users on one host never share a
// broker.
func DefaultSocketPath() string {
	if p := os.Getenv(EnvSocket); p != "" {
		return p
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("leasekeeper-%d.sock", os.Getuid()))
}

// Listen binds the broker to path. A stale socket file left by a dead broker is
// removed first; a live one is reported as an error so the caller attaches
// instead of starting a second broker.
func Listen(path string) (*Broker, error) {
	// A Unix socket address is a fixed-size buffer, so an over-long path fails
	// with an opaque bind error. Say what is wrong instead.
	if len(path) > unixPathLimit {
		return nil, fmt.Errorf("socket path %q is %d characters; Unix sockets allow about %d, so use a shorter path or set %s",
			path, len(path), unixPathLimit, EnvSocket)
	}
	if conn, err := net.DialTimeout("unix", path, 200*time.Millisecond); err == nil {
		conn.Close()
		return nil, fmt.Errorf("a broker is already listening on %s", path)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("clear stale socket %s: %w", path, err)
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return nil, fmt.Errorf("secure socket %s: %w", path, err)
	}
	return &Broker{
		path:    path,
		ln:      ln,
		table:   lease.NewTable(),
		graph:   waitgraph.New(),
		queues:  make(map[lease.Resource][]*waiter),
		pending: make(map[string]map[lease.Resource]*waiter),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}, nil
}

// Path reports the socket the broker is listening on.
func (b *Broker) Path() string { return b.path }

// Serve accepts connections until the broker is closed.
func (b *Broker) Serve() {
	b.wg.Add(2)
	go b.sweep()
	go func() {
		defer b.wg.Done()
		for {
			conn, err := b.ln.Accept()
			if err != nil {
				return
			}
			go b.handle(conn)
		}
	}()
}

// sweep expires leases whose TTL has passed and hands the freed resources to
// whoever is queued for them.
func (b *Broker) sweep() {
	defer b.wg.Done()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-b.stop:
			return
		case <-ticker.C:
			b.mu.Lock()
			b.expireLocked(time.Now())
			b.mu.Unlock()
		}
	}
}

// Close stops the broker, drops every lease and removes the socket. Blocked
// acquires are woken and fail closed.
func (b *Broker) Close() {
	b.closed.Do(func() {
		close(b.stop)
		b.ln.Close()
		os.Remove(b.path)
		b.mu.Lock()
		b.table = lease.NewTable()
		b.graph = waitgraph.New()
		b.queues = make(map[lease.Resource][]*waiter)
		b.pending = make(map[string]map[lease.Resource]*waiter)
		b.mu.Unlock()
		close(b.done)
	})
	b.wg.Wait()
}

// Done is closed once the broker has shut down.
func (b *Broker) Done() <-chan struct{} { return b.done }

// Acquire grants Resource to Agent, or queues Agent behind the current holder.
// A wait that would close a cycle is refused immediately and never blocks.
func (b *Broker) Acquire(ctx context.Context, opt AcquireOptions) Result {
	b.mu.Lock()
	b.expireLocked(time.Now())

	if l, held := b.table.Get(opt.Resource); held {
		if l.Owner == opt.Agent {
			b.mu.Unlock()
			return Result{
				Verdict:  Granted,
				Resource: opt.Resource,
				Message:  fmt.Sprintf("%s already holds %s", opt.Agent, opt.Resource),
			}
		}
		if cycle := b.graph.CyclePath(opt.Agent, l.Owner); cycle != nil {
			b.mu.Unlock()
			return Result{
				Verdict:  DeniedCycle,
				Resource: opt.Resource,
				Holder:   l.Owner,
				Cycle:    cycle,
				Message:  fmt.Sprintf("acquire denied — would deadlock: %s", strings.Join(cycle, " -> ")),
			}
		}
		if !opt.Wait {
			b.mu.Unlock()
			return Result{
				Verdict:  DeniedHeld,
				Resource: opt.Resource,
				Holder:   l.Owner,
				Message:  fmt.Sprintf("%s is held by %s; not waiting", opt.Resource, l.Owner),
			}
		}
		if _, err := b.graph.Add(opt.Agent, l.Owner); err != nil {
			// Unreachable: the cycle precheck above runs under the same lock.
			b.mu.Unlock()
			return Result{Verdict: Error, Resource: opt.Resource, Message: err.Error()}
		}
		w := b.enqueueLocked(opt.Agent, l.Owner, opt.Resource, opt.TTL)
		holder := l.Owner
		b.mu.Unlock()

		queued := Result{
			Verdict:  DeniedHeld,
			Resource: opt.Resource,
			Holder:   holder,
			Queued:   true,
			Message:  fmt.Sprintf("waiting for %s held by %s", opt.Resource, holder),
		}
		if opt.Notify != nil {
			opt.Notify(queued)
		}

		timeout := opt.Timeout
		if timeout <= 0 {
			timeout = DefaultWaitTimeout
		}
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case got := <-w.ch:
			return got
		case <-timer.C:
			if got, ok := b.cancelWaiter(w); ok {
				return got
			}
			return Result{
				Verdict:  DeniedHeld,
				Resource: opt.Resource,
				Holder:   holder,
				Message:  fmt.Sprintf("gave up after %s waiting for %s held by %s", timeout, opt.Resource, holder),
			}
		case <-ctx.Done():
			if got, ok := b.cancelWaiter(w); ok {
				return got
			}
			return Result{
				Verdict:  DeniedHeld,
				Resource: opt.Resource,
				Holder:   holder,
				Message:  fmt.Sprintf("cancelled while waiting for %s held by %s", opt.Resource, holder),
			}
		case <-b.stop:
			b.cancelWaiter(w)
			return Result{Verdict: Error, Resource: opt.Resource, Message: "broker stopped"}
		}
	}

	b.grantLocked(opt.Agent, opt.Resource, opt.TTL)
	b.mu.Unlock()
	return Result{
		Verdict:  Granted,
		Resource: opt.Resource,
		Message:  fmt.Sprintf("granted %s to %s", opt.Resource, opt.Agent),
	}
}

// Release hands Resource back. Only the current owner may release it.
func (b *Broker) Release(agent string, res lease.Resource) Result {
	b.mu.Lock()
	defer b.mu.Unlock()

	l, held := b.table.Get(res)
	if !held {
		return Result{Verdict: OK, Resource: res, Message: fmt.Sprintf("%s is not held", res)}
	}
	if l.Owner != agent {
		return Result{
			Verdict:  Error,
			Resource: res,
			Holder:   l.Owner,
			Message:  fmt.Sprintf("%s is held by %s, not %s", res, l.Owner, agent),
		}
	}
	b.table.Delete(res)
	b.handOffLocked(res)
	return Result{Verdict: Released, Resource: res, Message: fmt.Sprintf("released %s", res)}
}

// ReleaseAll hands back every resource agent owns and stops it waiting on
// anything. It is how a finished `run` cleans up after itself.
func (b *Broker) ReleaseAll(agent string) []lease.Resource {
	b.mu.Lock()
	defer b.mu.Unlock()

	queued := make([]*waiter, 0, len(b.pending[agent]))
	for _, w := range b.pending[agent] {
		queued = append(queued, w)
	}
	for _, w := range queued {
		b.dropWaitLocked(w)
	}
	freed := b.table.HeldBy(agent)
	for _, res := range freed {
		b.table.Delete(res)
		b.handOffLocked(res)
	}
	sort.Slice(freed, func(i, j int) bool { return freed[i] < freed[j] })
	return freed
}

// Status reports the lease table and the current wait-for graph.
func (b *Broker) Status() Status {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.expireLocked(time.Now())

	now := time.Now()
	leases := make([]LeaseView, 0, b.table.Len())
	for _, l := range b.table.Snapshot() {
		view := LeaseView{
			Resource: string(l.Resource),
			Owner:    l.Owner,
			AgeSec:   l.Age(now).Seconds(),
			TTLSec:   l.TTL.Seconds(),
		}
		if l.TTL > 0 {
			view.ExpiresInSec = l.ExpiresAt().Sub(now).Seconds()
		}
		leases = append(leases, view)
	}
	edges := make([]EdgeView, 0, len(b.graph.Edges()))
	for _, e := range b.graph.Edges() {
		edges = append(edges, EdgeView{Waiter: e.Waiter, Holder: e.Holder})
	}
	return Status{Leases: leases, Edges: edges}
}

// RefCount reports how many runs are currently attached.
func (b *Broker) RefCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.refs
}

// grantLocked records a lease and stops the owner waiting on it.
func (b *Broker) grantLocked(agent string, res lease.Resource, ttl time.Duration) {
	b.table.Put(lease.Lease{Resource: res, Owner: agent, Acquired: time.Now(), TTL: ttl})
	if w := b.pending[agent][res]; w != nil {
		b.dropWaitLocked(w)
	}
}

// enqueueLocked adds w to the FIFO for its resource. The caller has already
// recorded the wait edge.
func (b *Broker) enqueueLocked(agent, holder string, res lease.Resource, ttl time.Duration) *waiter {
	w := &waiter{agent: agent, holder: holder, resource: res, ttl: ttl, ch: make(chan Result, 1)}
	b.queues[res] = append(b.queues[res], w)
	if b.pending[agent] == nil {
		b.pending[agent] = make(map[lease.Resource]*waiter)
	}
	b.pending[agent][res] = w
	return w
}

// dropWaitLocked removes a queued acquire and the wait edge it stands for. The
// edge goes only when the agent is no longer waiting on anything that holder
// owns, because one agent may wait on two resources held by the same peer.
func (b *Broker) dropWaitLocked(w *waiter) {
	if q := b.pending[w.agent]; q != nil {
		if q[w.resource] != w {
			return
		}
		delete(q, w.resource)
		if len(q) == 0 {
			delete(b.pending, w.agent)
		}
	}
	queue := b.queues[w.resource]
	for i, other := range queue {
		if other == w {
			b.queues[w.resource] = append(queue[:i], queue[i+1:]...)
			break
		}
	}
	if len(b.queues[w.resource]) == 0 {
		delete(b.queues, w.resource)
	}
	for _, other := range b.pending[w.agent] {
		if other.holder == w.holder {
			return
		}
	}
	b.graph.Remove(w.agent, w.holder)
}

// cancelWaiter drops a queued acquire, or reports the grant if the hand-off won
// the race with the timeout.
func (b *Broker) cancelWaiter(w *waiter) (Result, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if w.granted {
		return Result{
			Verdict:  Granted,
			Resource: w.resource,
			Message:  fmt.Sprintf("granted %s to %s", w.resource, w.agent),
		}, true
	}
	b.dropWaitLocked(w)
	return Result{}, false
}

// handOffLocked gives a just-freed resource to the first agent queued for it.
// Waiting is FIFO: a queue is a line, not a lottery.
func (b *Broker) handOffLocked(res lease.Resource) {
	queue := b.queues[res]
	if len(queue) == 0 {
		return
	}
	w := queue[0]
	w.granted = true
	b.table.Put(lease.Lease{Resource: res, Owner: w.agent, Acquired: time.Now(), TTL: w.ttl})
	b.dropWaitLocked(w)
	w.ch <- Result{
		Verdict:  Granted,
		Resource: res,
		Message:  fmt.Sprintf("granted %s to %s after waiting", res, w.agent),
	}
}

// expireLocked drops leases past their TTL and hands the resources on.
func (b *Broker) expireLocked(now time.Time) {
	for _, res := range b.table.Expire(now) {
		b.handOffLocked(res)
	}
}

// handle serves one connection. A waiting acquire writes a queued line and then
// the final verdict; everything else writes exactly one line.
func (b *Broker) handle(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	defer writer.Flush()

	line, err := reader.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return
	}
	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		writeResponse(writer, Response{Verdict: Error, Message: "malformed request: " + err.Error()})
		return
	}

	switch req.Op {
	case "acquire":
		res, err := lease.ParseResource(req.Resource)
		if err != nil {
			writeResponse(writer, Response{Verdict: Error, Message: err.Error()})
			return
		}
		if req.Agent == "" {
			writeResponse(writer, Response{Verdict: Error, Message: "acquire needs an agent identity"})
			return
		}
		result := b.Acquire(context.Background(), AcquireOptions{
			Agent:    req.Agent,
			Resource: res,
			TTL:      seconds(req.TTLSec),
			Wait:     req.Wait,
			Timeout:  seconds(req.TimeoutSec),
			Notify: func(queued Result) {
				writeResponse(writer, responseOf(queued))
			},
		})
		writeResponse(writer, responseOf(result))

	case "release":
		res, err := lease.ParseResource(req.Resource)
		if err != nil {
			writeResponse(writer, Response{Verdict: Error, Message: err.Error()})
			return
		}
		writeResponse(writer, responseOf(b.Release(req.Agent, res)))

	case "release-all":
		freed := b.ReleaseAll(req.Agent)
		names := make([]string, 0, len(freed))
		for _, r := range freed {
			names = append(names, string(r))
		}
		writeResponse(writer, Response{
			Verdict:  OK,
			Released: names,
			Message:  fmt.Sprintf("released %d lease(s) held by %s", len(names), req.Agent),
		})

	case "status":
		st := b.Status()
		writeResponse(writer, Response{Verdict: OK, Leases: st.Leases, Edges: st.Edges})

	case "session":
		// A run holds one session connection open for its whole lifetime. The
		// broker counts live sessions, so a crashed run is noticed as an EOF
		// rather than a missing goodbye, and the last one out closes the
		// broker.
		b.mu.Lock()
		b.refs++
		b.mu.Unlock()
		writeResponse(writer, Response{Verdict: OK, Message: "session open"})
		for {
			if _, err := reader.ReadBytes('\n'); err != nil {
				break
			}
		}
		b.mu.Lock()
		b.refs--
		last := b.refs <= 0
		b.mu.Unlock()
		if last {
			b.Close()
		}

	default:
		writeResponse(writer, Response{Verdict: Error, Message: fmt.Sprintf("unknown op %q", req.Op)})
	}
}

func writeResponse(w *bufio.Writer, r Response) {
	payload, err := json.Marshal(r)
	if err != nil {
		return
	}
	w.Write(payload)
	w.WriteByte('\n')
	w.Flush()
}

func responseOf(r Result) Response {
	return Response{
		Verdict:  r.Verdict,
		Message:  r.Message,
		Resource: string(r.Resource),
		Holder:   r.Holder,
		Cycle:    r.Cycle,
		Queued:   r.Queued,
	}
}

func seconds(v float64) time.Duration {
	if v <= 0 {
		return 0
	}
	return time.Duration(v * float64(time.Second))
}
