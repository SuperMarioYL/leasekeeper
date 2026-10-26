package lease

import (
	"sort"
	"time"
)

// Table records the lease currently held for each resource. It is owned by one
// broker and holds no lock of its own: the broker serialises access.
type Table struct {
	held map[Resource]Lease
}

// NewTable returns an empty lease table.
func NewTable() *Table { return &Table{held: make(map[Resource]Lease)} }

// Get returns the lease held for r, if any.
func (t *Table) Get(r Resource) (Lease, bool) {
	l, ok := t.held[r]
	return l, ok
}

// Put records l as the lease for its resource, replacing any previous holder.
func (t *Table) Put(l Lease) { t.held[l.Resource] = l }

// Delete drops the lease for r.
func (t *Table) Delete(r Resource) { delete(t.held, r) }

// Owner reports which agent holds r.
func (t *Table) Owner(r Resource) (string, bool) {
	l, ok := t.held[r]
	if !ok {
		return "", false
	}
	return l.Owner, true
}

// Len reports how many leases are held.
func (t *Table) Len() int { return len(t.held) }

// Expire drops every lease whose TTL has passed and returns the freed
// resources, ordered by name.
func (t *Table) Expire(now time.Time) []Resource {
	var freed []Resource
	for r, l := range t.held {
		if l.Expired(now) {
			delete(t.held, r)
			freed = append(freed, r)
		}
	}
	sort.Slice(freed, func(i, j int) bool { return freed[i] < freed[j] })
	return freed
}

// Snapshot returns the held leases ordered by resource name.
func (t *Table) Snapshot() []Lease {
	out := make([]Lease, 0, len(t.held))
	for _, l := range t.held {
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Resource < out[j].Resource })
	return out
}

// HeldBy returns the resources owned by agent, ordered by name.
func (t *Table) HeldBy(agent string) []Resource {
	var out []Resource
	for r, l := range t.held {
		if l.Owner == agent {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
