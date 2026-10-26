// Package lease defines Leasekeeper's core primitive: time-bounded exclusive
// ownership of one named shared resource, and the table that records who owns
// what right now.
//
// A resource name carries its own kind, so the broker can reason about file,
// port, gpu and quota leases without a registry or a schema:
//
//	file:<path>   port:<n>   gpu:<id>   quota:<name>
package lease

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Resource names a shared thing one agent can own. The kind prefix is part of
// the name, so "file:src/a.go" and "port:8080" can never collide even though
// both are just strings.
type Resource string

// Resource kinds Leasekeeper accepts. Anything else is rejected, so a typo
// cannot silently create a second, unrelated namespace.
const (
	KindFile  = "file"
	KindPort  = "port"
	KindGPU   = "gpu"
	KindQuota = "quota"
)

// ParseResource validates a "<kind>:<name>" resource string and normalises it.
func ParseResource(s string) (Resource, error) {
	s = strings.TrimSpace(s)
	kind, name, ok := strings.Cut(s, ":")
	if !ok || name == "" {
		return "", fmt.Errorf("resource %q: want <kind>:<name>, e.g. file:src/a.go or port:8080", s)
	}
	switch kind {
	case KindFile:
		return Resource(KindFile + ":" + name), nil
	case KindPort:
		n, err := strconv.Atoi(name)
		if err != nil || n < 1 || n > 65535 {
			return "", fmt.Errorf("resource %q: port must be a number between 1 and 65535", s)
		}
		return Resource(KindPort + ":" + strconv.Itoa(n)), nil
	case KindGPU:
		if _, err := strconv.Atoi(name); err != nil {
			return "", fmt.Errorf("resource %q: gpu id must be a number", s)
		}
		return Resource(KindGPU + ":" + name), nil
	case KindQuota:
		return Resource(KindQuota + ":" + name), nil
	default:
		return "", fmt.Errorf("resource %q: unknown kind %q (want %s, %s, %s or %s)",
			s, kind, KindFile, KindPort, KindGPU, KindQuota)
	}
}

// Lease is a granted, time-bounded exclusive ownership of one resource. TTL 0
// means "held until released"; the broker's lifetime bounds the lease either
// way, because lease state lives only in the broker's memory.
type Lease struct {
	Resource Resource
	Owner    string
	Acquired time.Time
	TTL      time.Duration
}

// ExpiresAt reports when the lease stops being valid. A zero TTL has no
// expiry.
func (l Lease) ExpiresAt() time.Time { return l.Acquired.Add(l.TTL) }

// Expired reports whether the lease has outlived its TTL at now. A zero TTL
// never expires.
func (l Lease) Expired(now time.Time) bool {
	return l.TTL > 0 && !now.Before(l.ExpiresAt())
}

// Age reports how long the lease has been held at now.
func (l Lease) Age(now time.Time) time.Duration { return now.Sub(l.Acquired) }
