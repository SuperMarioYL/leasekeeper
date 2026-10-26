// Package report renders the lease table, the wait-for graph and acquire
// verdicts for a terminal. It is the only place Leasekeeper decides how state
// looks, so the CLI stays about plumbing and the demo reads like the tool.
package report

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/SuperMarioYL/leasekeeper/internal/broker"
	"github.com/SuperMarioYL/leasekeeper/internal/lease"
)

// Theme renders reports with or without colour. Colour is decided once, from
// the output file and the environment, so piped output stays plain text.
type Theme struct {
	color bool
}

// New returns a theme. Colour is enabled when stdout is a terminal and neither
// NO_COLOR nor a --no-color flag has turned it off.
func New(color bool) Theme { return Theme{color: color} }

// ColorEnabled reports whether this theme paints.
func (t Theme) ColorEnabled() bool { return t.color }

// IsTerminal reports whether f is an interactive terminal.
func IsTerminal(f *os.File) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("CLICOLOR_FORCE") != "" && os.Getenv("CLICOLOR_FORCE") != "0" {
		return true
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func (t Theme) paint(color, s string) string {
	if !t.color || color == "" {
		return s
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(s)
}

func (t Theme) dim(s string) string   { return t.paint("#8a93a5", s) }
func (t Theme) bold(s string) string  { return lipgloss.NewStyle().Bold(true).Render(s) }
func (t Theme) green(s string) string { return t.paint("#3fb950", s) }
func (t Theme) red(s string) string   { return t.paint("#f85149", s) }
func (t Theme) amber(s string) string { return t.paint("#d29922", s) }

// Section is a titled block of report output.
func (t Theme) Section(title string) string {
	return t.bold(title)
}

// Dim renders secondary text: labels, hints and closing notes.
func (t Theme) Dim(s string) string { return t.dim(s) }

// Agent renders an agent label such as [a].
func (t Theme) Agent(name string) string { return t.bold("[" + name + "]") }

// Verdict renders one acquire or release outcome as a single line.
func (t Theme) Verdict(r broker.Result) string {
	switch r.Verdict {
	case broker.Granted:
		return fmt.Sprintf("%s  %s", t.green("granted"), r.Message)
	case broker.DeniedCycle:
		line := fmt.Sprintf("%s  %s", t.red("acquire denied"), r.Message)
		return line
	case broker.DeniedHeld:
		if r.Queued {
			return fmt.Sprintf("%s  %s", t.amber("waiting"), r.Message)
		}
		return fmt.Sprintf("%s  %s", t.amber("denied"), r.Message)
	case broker.Released:
		return fmt.Sprintf("%s  %s", t.green("released"), r.Message)
	case broker.OK:
		return fmt.Sprintf("%s  %s", t.dim("ok"), r.Message)
	default:
		return fmt.Sprintf("%s  %s", t.red("error"), r.Message)
	}
}

// GraphEdge renders one waiter -> holder edge.
func (t Theme) GraphEdge(e broker.EdgeView) string {
	return fmt.Sprintf("%s %s %s", t.bold(e.Waiter), t.dim("->"), t.bold(e.Holder))
}

// Table renders the lease table: who owns what, right now.
func (t Theme) Table(leases []broker.LeaseView, now time.Time) string {
	var b strings.Builder
	b.WriteString(t.Section("Leases") + "\n")
	if len(leases) == 0 {
		b.WriteString("  " + t.dim("none held") + "\n")
		return strings.TrimRight(b.String(), "\n")
	}
	rows := [][]string{{"RESOURCE", "OWNER", "HELD", "TTL"}}
	for _, l := range leases {
		rows = append(rows, []string{l.Resource, l.Owner, humanSeconds(l.AgeSec), ttl(l)})
	}
	widths := make([]int, len(rows[0]))
	for _, row := range rows {
		for i, cell := range row {
			if w := lipgloss.Width(cell); w > widths[i] {
				widths[i] = w
			}
		}
	}
	for i, row := range rows {
		cells := make([]string, len(row))
		for j, cell := range row {
			cells[j] = cell + strings.Repeat(" ", widths[j]-lipgloss.Width(cell))
		}
		line := "  " + strings.TrimRight(strings.Join(cells, "  "), " ")
		if i == 0 {
			line = t.dim(line)
		}
		b.WriteString(line + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// Graph renders the wait-for graph, or says plainly that nobody is waiting.
func (t Theme) Graph(edges []broker.EdgeView) string {
	var b strings.Builder
	b.WriteString(t.Section("Wait-for graph") + "\n")
	if len(edges) == 0 {
		b.WriteString("  " + t.dim("no agent is waiting on another") + "\n")
		return strings.TrimRight(b.String(), "\n")
	}
	sorted := append([]broker.EdgeView(nil), edges...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Waiter != sorted[j].Waiter {
			return sorted[i].Waiter < sorted[j].Waiter
		}
		return sorted[i].Holder < sorted[j].Holder
	})
	for _, e := range sorted {
		b.WriteString("  " + t.GraphEdge(e) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// Status renders the whole picture: leases, then the wait-for graph.
func (t Theme) Status(s broker.Status, now time.Time) string {
	return t.Table(s.Leases, now) + "\n\n" + t.Graph(s.Edges)
}

func ttl(l broker.LeaseView) string {
	if l.TTLSec <= 0 {
		return "-"
	}
	if l.ExpiresInSec <= 0 {
		return "expired"
	}
	return humanSeconds(l.ExpiresInSec)
}

func humanSeconds(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	d := time.Duration(sec * float64(time.Second)).Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	default:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
}

// ResourceList renders a list of resources one per line, for release output.
func (t Theme) ResourceList(resources []lease.Resource) string {
	var b strings.Builder
	for _, r := range resources {
		b.WriteString("  " + t.dim("released") + " " + string(r) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
