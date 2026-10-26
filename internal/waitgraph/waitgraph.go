// Package waitgraph implements the waiter -> holder graph whose cycle check is
// the primitive Leasekeeper adds: a circular wait is refused at acquire time,
// before it deadlocks the fleet.
//
// The graph is deliberately tiny. There is no training, no heuristic and no
// timeout guesswork: an edge waiter -> holder means "waiter cannot proceed
// until holder releases something", and an acquire is refused exactly when
// adding that edge would close a loop.
package waitgraph

import (
	"fmt"
	"sort"
	"strings"
)

// WaitGraph records, for each agent, the set of agents it is currently waiting
// on. It is owned by one broker and holds no lock of its own.
type WaitGraph struct {
	wait map[string]map[string]struct{}
}

// New returns an empty wait-for graph.
func New() *WaitGraph { return &WaitGraph{wait: make(map[string]map[string]struct{})} }

// Edge is one waiter -> holder relationship.
type Edge struct {
	Waiter string
	Holder string
}

// CyclePath reports the cycle that would be created by making waiter wait on
// holder, or nil when the edge is safe.
//
// The test is pure reachability: if holder can already reach waiter, then
// waiter -> holder closes a loop. The returned slice is the closed cycle with
// the starting agent repeated at the end, so it reads the way a developer
// would describe it: ["b", "a", "b"] is "b -> a -> b".
func (g *WaitGraph) CyclePath(waiter, holder string) []string {
	if waiter == holder {
		return []string{waiter, waiter}
	}
	path, ok := g.path(holder, waiter)
	if !ok {
		return nil
	}
	return append([]string{waiter}, path...)
}

// path finds a route from start to goal over the wait edges, returning the
// nodes in order. Breadth-first, so the reported cycle is the shortest one.
func (g *WaitGraph) path(start, goal string) ([]string, bool) {
	type frame struct {
		node string
		via  []string
	}
	seen := map[string]bool{start: true}
	queue := []frame{{node: start, via: []string{start}}}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for next := range g.wait[cur.node] {
			if next == goal {
				return append(cur.via, goal), true
			}
			if seen[next] {
				continue
			}
			seen[next] = true
			queue = append(queue, frame{node: next, via: append(append([]string{}, cur.via...), next)})
		}
	}
	return nil, false
}

// Add records waiter -> holder. If that edge would close a cycle it is refused,
// the graph is left unchanged and the cycle is returned as an error.
func (g *WaitGraph) Add(waiter, holder string) ([]string, error) {
	if cycle := g.CyclePath(waiter, holder); cycle != nil {
		return cycle, fmt.Errorf("wait %s -> %s would deadlock: %s", waiter, holder, strings.Join(cycle, " -> "))
	}
	if g.wait[waiter] == nil {
		g.wait[waiter] = make(map[string]struct{})
	}
	g.wait[waiter][holder] = struct{}{}
	return nil, nil
}

// Remove drops the edge waiter -> holder if present.
func (g *WaitGraph) Remove(waiter, holder string) {
	if holders, ok := g.wait[waiter]; ok {
		delete(holders, holder)
		if len(holders) == 0 {
			delete(g.wait, waiter)
		}
	}
}

// RemoveWaiter drops every edge leaving agent and returns the holders it had
// been waiting on.
func (g *WaitGraph) RemoveWaiter(agent string) []string {
	var holders []string
	for holder := range g.wait[agent] {
		holders = append(holders, holder)
	}
	delete(g.wait, agent)
	sort.Strings(holders)
	return holders
}

// WaitingOn reports whether waiter currently has an edge to holder.
func (g *WaitGraph) WaitingOn(waiter, holder string) bool {
	_, ok := g.wait[waiter][holder]
	return ok
}

// Waiters returns the agents waiting on holder, ordered by name.
func (g *WaitGraph) Waiters(holder string) []string {
	var out []string
	for waiter, holders := range g.wait {
		if _, ok := holders[holder]; ok {
			out = append(out, waiter)
		}
	}
	sort.Strings(out)
	return out
}

// Edges returns every waiter -> holder edge, ordered by waiter then holder.
func (g *WaitGraph) Edges() []Edge {
	var out []Edge
	for waiter, holders := range g.wait {
		for holder := range holders {
			out = append(out, Edge{Waiter: waiter, Holder: holder})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Waiter != out[j].Waiter {
			return out[i].Waiter < out[j].Waiter
		}
		return out[i].Holder < out[j].Holder
	})
	return out
}

// Empty reports whether no agent is waiting on any other.
func (g *WaitGraph) Empty() bool { return len(g.wait) == 0 }
