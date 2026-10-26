package waitgraph

import (
	"reflect"
	"strings"
	"testing"
)

// TestTwoEdgeCycleRefused is the load-bearing test for the whole product: the
// second edge of a two-agent cycle must be refused, and the refusal must name
// the cycle.
func TestTwoEdgeCycleRefused(t *testing.T) {
	g := New()

	// a waits on b. Safe: nothing waits on a yet.
	if cycle, err := g.Add("a", "b"); err != nil {
		t.Fatalf("first edge a -> b must be accepted, got cycle %v err %v", cycle, err)
	}

	// b waits on a. This closes b -> a -> b.
	cycle, err := g.Add("b", "a")
	if err == nil {
		t.Fatal("second edge b -> a must be refused, but it was accepted")
	}
	if want := []string{"b", "a", "b"}; !reflect.DeepEqual(cycle, want) {
		t.Fatalf("cycle = %v, want %v", cycle, want)
	}
	if got := strings.Join(cycle, " -> "); got != "b -> a -> b" {
		t.Fatalf("cycle reads %q, want %q", got, "b -> a -> b")
	}

	// The refused edge must not have been recorded.
	if g.WaitingOn("b", "a") {
		t.Fatal("refused edge b -> a was recorded anyway")
	}
	if edges := g.Edges(); len(edges) != 1 || edges[0] != (Edge{Waiter: "a", Holder: "b"}) {
		t.Fatalf("graph changed by a refused edge: %v", edges)
	}
}

// TestNonCyclicWaitGranted proves a chain that cannot loop is accepted, so the
// guard refuses cycles rather than all waits.
func TestNonCyclicWaitGranted(t *testing.T) {
	g := New()
	for _, e := range [][2]string{{"a", "b"}, {"b", "c"}, {"c", "d"}} {
		if cycle, err := g.Add(e[0], e[1]); err != nil {
			t.Fatalf("non-cyclic wait %s -> %s refused with cycle %v: %v", e[0], e[1], cycle, err)
		}
	}
	if got := len(g.Edges()); got != 3 {
		t.Fatalf("edges = %d, want 3", got)
	}
	// A separate pair that shares no node with the chain is also fine.
	if _, err := g.Add("x", "y"); err != nil {
		t.Fatalf("independent wait x -> y refused: %v", err)
	}
	// Closing the chain is not: d -> a would loop back through the whole path.
	cycle, err := g.Add("d", "a")
	if err == nil {
		t.Fatal("closing edge d -> a must be refused")
	}
	if want := []string{"d", "a", "b", "c", "d"}; !reflect.DeepEqual(cycle, want) {
		t.Fatalf("cycle = %v, want %v", cycle, want)
	}
}

// TestSelfWaitRefused covers an agent waiting on a resource it already holds.
func TestSelfWaitRefused(t *testing.T) {
	g := New()
	cycle, err := g.Add("a", "a")
	if err == nil {
		t.Fatal("self wait a -> a must be refused")
	}
	if want := []string{"a", "a"}; !reflect.DeepEqual(cycle, want) {
		t.Fatalf("cycle = %v, want %v", cycle, want)
	}
}

// TestThreeAgentCycleRefused covers the longer loop, where the refusal must
// report the whole path and not just the closing edge.
func TestThreeAgentCycleRefused(t *testing.T) {
	g := New()
	for _, e := range [][2]string{{"a", "b"}, {"b", "c"}} {
		if _, err := g.Add(e[0], e[1]); err != nil {
			t.Fatalf("setup edge %s -> %s refused: %v", e[0], e[1], err)
		}
	}
	cycle, err := g.Add("c", "a")
	if err == nil {
		t.Fatal("closing edge c -> a must be refused")
	}
	if want := []string{"c", "a", "b", "c"}; !reflect.DeepEqual(cycle, want) {
		t.Fatalf("cycle = %v, want %v", cycle, want)
	}
}

// TestRemoveEdgeLetsTheCycleCloseOnceTheHolderLetsGo proves the refusal is
// stateful: once a's edge is gone, b -> a is safe again.
func TestRemoveEdgeLetsTheCycleCloseOnceTheHolderLetsGo(t *testing.T) {
	g := New()
	if _, err := g.Add("a", "b"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if _, err := g.Add("b", "a"); err == nil {
		t.Fatal("expected refusal while a -> b exists")
	}

	g.Remove("a", "b")

	if _, err := g.Add("b", "a"); err != nil {
		t.Fatalf("b -> a must be accepted after a stopped waiting: %v", err)
	}
}

// TestWaitersAndEdgesAreOrdered keeps report output deterministic.
func TestWaitersAndEdgesAreOrdered(t *testing.T) {
	g := New()
	for _, e := range [][2]string{{"c", "z"}, {"a", "z"}, {"b", "z"}, {"a", "y"}} {
		if _, err := g.Add(e[0], e[1]); err != nil {
			t.Fatalf("setup %s -> %s: %v", e[0], e[1], err)
		}
	}
	if got, want := g.Waiters("z"), []string{"a", "b", "c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("waiters(z) = %v, want %v", got, want)
	}
	want := []Edge{{"a", "y"}, {"a", "z"}, {"b", "z"}, {"c", "z"}}
	if got := g.Edges(); !reflect.DeepEqual(got, want) {
		t.Fatalf("edges = %v, want %v", got, want)
	}
	if g.Empty() {
		t.Fatal("graph with edges reports Empty")
	}
	g.RemoveWaiter("a")
	if g.Empty() {
		t.Fatal("graph still has b -> z and c -> z")
	}
	g.RemoveWaiter("b")
	g.RemoveWaiter("c")
	if !g.Empty() {
		t.Fatalf("graph should be empty, edges: %v", g.Edges())
	}
}
