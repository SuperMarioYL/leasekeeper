// Package client is the thin RPC the CLI and the agent shim both use. Every
// call opens one Unix-socket connection, sends one request line and reads the
// reply lines, so there is no session state to keep alive.
package client

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/SuperMarioYL/leasekeeper/internal/broker"
	"github.com/SuperMarioYL/leasekeeper/internal/lease"
)

// Client talks to one broker socket.
type Client struct {
	path string
}

// Dial returns a client for the broker listening on path. It does not connect
// yet: a broker that dies between calls surfaces as an error on the next call.
func Dial(path string) *Client { return &Client{path: path} }

// Path reports the socket the client talks to.
func (c *Client) Path() string { return c.path }

// ErrNoBroker is returned when nothing is listening on the socket.
var ErrNoBroker = fmt.Errorf("no leasekeeper broker is running")

// Acquire asks the broker for res. When wait is set, onQueued (if not nil) is
// called with the queued verdict before the call blocks for the hand-over.
func (c *Client) Acquire(ctx context.Context, agent string, res lease.Resource, ttl time.Duration, wait bool, timeout time.Duration, onQueued func(broker.Result)) (broker.Result, error) {
	req := broker.Request{
		Op:         "acquire",
		Agent:      agent,
		Resource:   string(res),
		TTLSec:     ttl.Seconds(),
		Wait:       wait,
		TimeoutSec: timeout.Seconds(),
	}
	resp, err := c.call(ctx, req, onQueued)
	return resultOf(resp), err
}

// Release hands res back to the broker.
func (c *Client) Release(ctx context.Context, agent string, res lease.Resource) (broker.Result, error) {
	resp, err := c.call(ctx, broker.Request{Op: "release", Agent: agent, Resource: string(res)}, nil)
	return resultOf(resp), err
}

// ReleaseAll hands back everything agent owns. It is what a finished run calls
// so a crashed child cannot leave a stale lease behind.
func (c *Client) ReleaseAll(ctx context.Context, agent string) (broker.Result, error) {
	resp, err := c.call(ctx, broker.Request{Op: "release-all", Agent: agent}, nil)
	return resultOf(resp), err
}

// Status reads the lease table and the wait-for graph.
func (c *Client) Status(ctx context.Context) (broker.Status, error) {
	resp, err := c.call(ctx, broker.Request{Op: "status"}, nil)
	if err != nil {
		return broker.Status{}, err
	}
	return broker.Status{Leases: resp.Leases, Edges: resp.Edges}, nil
}

// Session is a run's persistent claim on the broker. Closing it tells the
// broker the run has gone; the last session to close shuts the broker down.
type Session struct {
	conn net.Conn
}

// OpenSession holds the broker open for the caller's lifetime. The broker
// counts live sessions, so a run that is killed is noticed as a dropped
// connection rather than a missing goodbye.
func (c *Client) OpenSession(ctx context.Context) (*Session, error) {
	conn, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	conn.SetDeadline(time.Time{})
	payload, err := json.Marshal(broker.Request{Op: "session"})
	if err != nil {
		conn.Close()
		return nil, err
	}
	if _, err := conn.Write(append(payload, '\n')); err != nil {
		conn.Close()
		return nil, err
	}
	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		conn.Close()
		return nil, fmt.Errorf("broker refused the session on %s", c.path)
	}
	var resp broker.Response
	if err := json.Unmarshal(line, &resp); err != nil {
		conn.Close()
		return nil, fmt.Errorf("malformed session reply: %w", err)
	}
	if resp.Verdict != broker.OK {
		conn.Close()
		return nil, fmt.Errorf("broker refused the session: %s", resp.Message)
	}
	return &Session{conn: conn}, nil
}

// Close ends the session.
func (s *Session) Close() error { return s.conn.Close() }

// Ping reports whether a broker is listening.
func (c *Client) Ping(ctx context.Context) bool {
	conn, err := c.dial(ctx)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func (c *Client) dial(ctx context.Context) (net.Conn, error) {
	d := net.Dialer{Timeout: 2 * time.Second}
	conn, err := d.DialContext(ctx, "unix", c.path)
	if err != nil {
		return nil, fmt.Errorf("%w on %s: %v", ErrNoBroker, c.path, err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	}
	return conn, nil
}

// call sends one request and collects every reply line. A blocking acquire
// answers twice, so the caller sees the queued notice as it happens. The last
// reply is the one returned.
func (c *Client) call(ctx context.Context, req broker.Request, onQueued func(broker.Result)) (broker.Response, error) {
	conn, err := c.dial(ctx)
	if err != nil {
		return broker.Response{}, err
	}
	defer conn.Close()

	payload, err := json.Marshal(req)
	if err != nil {
		return broker.Response{}, err
	}
	if _, err := conn.Write(append(payload, '\n')); err != nil {
		return broker.Response{}, err
	}

	reader := bufio.NewReader(conn)
	var last broker.Response
	got := false
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) == 0 {
			if readErr != nil {
				break
			}
			continue
		}
		var resp broker.Response
		if err := json.Unmarshal(line, &resp); err != nil {
			return broker.Response{}, fmt.Errorf("malformed reply: %w", err)
		}
		last, got = resp, true
		if resp.Queued && onQueued != nil {
			onQueued(resultOf(resp))
		}
		if readErr != nil {
			break
		}
	}
	if !got {
		return broker.Response{}, fmt.Errorf("broker closed the connection without answering")
	}
	return last, nil
}

func resultOf(resp broker.Response) broker.Result {
	return broker.Result{
		Verdict:  resp.Verdict,
		Resource: lease.Resource(resp.Resource),
		Holder:   resp.Holder,
		Cycle:    resp.Cycle,
		Message:  resp.Message,
		Queued:   resp.Queued,
		Released: resp.Released,
	}
}
