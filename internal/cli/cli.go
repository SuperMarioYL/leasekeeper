// Package cli is Leasekeeper's command surface: run, acquire, release, status
// and demo. It stays about plumbing — starting a broker, spawning a child with
// the socket in its environment, and printing what the report package renders.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/SuperMarioYL/leasekeeper/internal/broker"
	"github.com/SuperMarioYL/leasekeeper/internal/client"
	"github.com/SuperMarioYL/leasekeeper/internal/lease"
	"github.com/SuperMarioYL/leasekeeper/internal/report"
)

// Version is stamped by the build. It is the single place the CLI reports a
// version, and it matches the repo's VERSION file.
var Version = "dev"

// ExitError carries a child's exit status up to main, so `leasekeeper run`
// returns the status of the command it wrapped instead of its own.
type ExitError struct {
	Code int
	Msg  string
}

func (e *ExitError) Error() string {
	if e.Msg != "" {
		return e.Msg
	}
	return fmt.Sprintf("exit status %d", e.Code)
}

type app struct {
	socket  string
	noColor bool
	out     io.Writer
	errOut  io.Writer
}

// New builds the leasekeeper command tree.
func New() *cobra.Command {
	a := &app{out: os.Stdout, errOut: os.Stderr}

	root := &cobra.Command{
		Use:   "leasekeeper",
		Short: "Leases and deadlock detection for parallel coding agents",
		Long: "Leasekeeper is a local lease broker. Agents acquire and release\n" +
			"per-resource leases (file, port, gpu, quota) and a wait-for graph\n" +
			"refuses a circular wait before it deadlocks the fleet.",
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(a.out)
	root.SetErr(a.errOut)
	root.PersistentFlags().StringVar(&a.socket, "socket", "", "broker socket path (default $LEASEKEEPER_SOCKET or a per-user path in the temp dir)")
	root.PersistentFlags().BoolVar(&a.noColor, "no-color", false, "disable colour in reports")

	root.AddCommand(
		a.runCommand(),
		a.acquireCommand(),
		a.releaseCommand(),
		a.statusCommand(),
		a.demoCommand(),
	)
	return root
}

func (a *app) theme() report.Theme {
	return report.New(!a.noColor && report.IsTerminal(os.Stdout))
}

// socketPath resolves the broker socket: flag, then environment, then the
// per-user default.
func (a *app) socketPath() string {
	if a.socket != "" {
		return a.socket
	}
	return broker.DefaultSocketPath()
}

// resolveAgent resolves the agent identity: flag, then the environment a
// `run` session exports, then an error. Leases are only useful when they are
// attributable, so an unnamed agent is refused rather than guessed at.
func (a *app) resolveAgent(flag string) (string, error) {
	if flag != "" {
		return flag, nil
	}
	if env := os.Getenv(broker.EnvAgent); env != "" {
		return env, nil
	}
	return "", fmt.Errorf("no agent identity: pass --agent NAME or run the command under `leasekeeper run`")
}

func (a *app) runCommand() *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "run [flags] -- COMMAND [ARG...]",
		Short: "Run one agent against a shared broker",
		Long: "Run starts a broker if none is listening, then runs COMMAND with\n" +
			"LEASEKEEPER_SOCKET and LEASEKEEPER_AGENT in its environment. Every lease\n" +
			"the command still holds is released when it exits, so a finished or\n" +
			"crashed agent cannot leave a stale lease behind.\n\n" +
			"Run the agent that will wait first: the process that hosts the broker\n" +
			"stays alive until the last attached run leaves.",
		Example: "  leasekeeper run --name a -- ./examples/agent-a.sh\n" +
			"  leasekeeper run --name b -- ./examples/agent-b.sh",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runAgent(cmd, args, name)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "agent identity the child acquires leases as (default: the command's base name)")
	return cmd
}

func (a *app) runAgent(cmd *cobra.Command, args []string, name string) error {
	ctx := cmd.Context()
	path := a.socketPath()
	cl := client.Dial(path)

	var hosted *broker.Broker
	if !cl.Ping(ctx) {
		brk, err := broker.Listen(path)
		if err != nil {
			// Another run won the race to bind; attach to it instead.
			if !cl.Ping(ctx) {
				return err
			}
		} else {
			brk.Serve()
			hosted = brk
		}
	}

	session, err := cl.OpenSession(ctx)
	if err != nil {
		if hosted != nil {
			hosted.Close()
		}
		return err
	}
	defer func() {
		session.Close()
		if hosted != nil {
			// Hold the broker open only while another run is still attached.
			// With no other session this returns at once.
			<-hosted.Done()
		}
	}()

	agent := name
	if agent == "" {
		agent = agentName(args[0])
	}

	child := exec.CommandContext(ctx, args[0], args[1:]...)
	child.Stdin, child.Stdout, child.Stderr = os.Stdin, os.Stdout, os.Stderr
	child.Env = append(os.Environ(),
		broker.EnvSocket+"="+path,
		broker.EnvAgent+"="+agent,
	)
	if err := child.Start(); err != nil {
		return fmt.Errorf("start %s: %w", args[0], err)
	}
	waitErr := child.Wait()

	if result, rerr := cl.ReleaseAll(ctx, agent); rerr == nil && len(result.Released) > 0 {
		fmt.Fprintln(a.errOut, a.theme().ResourceList(toResources(result.Released)))
	}

	if waitErr != nil {
		var exit *exec.ExitError
		if errors.As(waitErr, &exit) {
			return &ExitError{
				Code: exit.ExitCode(),
				Msg:  fmt.Sprintf("%s exited with status %d", args[0], exit.ExitCode()),
			}
		}
		return waitErr
	}
	return nil
}

// agentName derives an agent identity from the wrapped command, so the common
// case needs no flag: ./examples/agent-a.sh becomes "agent-a".
func agentName(command string) string {
	base := filepath.Base(command)
	if ext := filepath.Ext(base); ext != "" && ext != base {
		base = strings.TrimSuffix(base, ext)
	}
	if base == "" || base == "." || base == "/" {
		return "agent"
	}
	return base
}

func (a *app) acquireCommand() *cobra.Command {
	var (
		agentFlag string
		ttl       time.Duration
		noWait    bool
		timeout   time.Duration
	)
	cmd := &cobra.Command{
		Use:   "acquire RESOURCE",
		Short: "Take a lease on a resource",
		Long: "Acquire blocks until the agent owns RESOURCE. When another agent holds\n" +
			"it, the caller is queued behind the holder and served in order. If waiting\n" +
			"would close a cycle in the wait-for graph, the acquire is refused and the\n" +
			"cycle is printed instead of hanging.\n\n" +
			"RESOURCE is one of file:<path>, port:<n>, gpu:<id> or quota:<name>.\n" +
			"A refused acquire exits non-zero so a script can branch on it.",
		Example: "  leasekeeper acquire file:src/a.go\n" +
			"  leasekeeper acquire --ttl 5m port:8080\n" +
			"  leasekeeper acquire --no-wait port:8080",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := lease.ParseResource(args[0])
			if err != nil {
				return err
			}
			agent, err := a.resolveAgent(agentFlag)
			if err != nil {
				return err
			}
			theme := a.theme()
			cl := client.Dial(a.socketPath())
			result, err := cl.Acquire(cmd.Context(), agent, res, ttl, !noWait, timeout, func(queued broker.Result) {
				fmt.Fprintln(a.out, theme.Verdict(queued))
			})
			if err != nil {
				return err
			}
			fmt.Fprintln(a.out, theme.Verdict(result))
			if result.Verdict != broker.Granted {
				return &ExitError{Code: 1}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&agentFlag, "agent", "", "agent identity (default $LEASEKEEPER_AGENT)")
	cmd.Flags().DurationVar(&ttl, "ttl", 0, "how long the lease stays valid, e.g. 5m (0: held until released)")
	cmd.Flags().BoolVar(&noWait, "no-wait", false, "report who holds the resource and exit instead of queueing")
	cmd.Flags().DurationVar(&timeout, "timeout", broker.DefaultWaitTimeout, "how long to wait before giving up")
	return cmd
}

func (a *app) releaseCommand() *cobra.Command {
	var (
		agentFlag string
		all       bool
	)
	cmd := &cobra.Command{
		Use:   "release RESOURCE",
		Short: "Hand a lease back",
		Long: "Release hands RESOURCE back to the broker. Only the agent that holds it\n" +
			"may release it. The first queued waiter is granted the lease in order.",
		Example: "  leasekeeper release file:src/a.go\n" +
			"  leasekeeper release --all",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !all && len(args) == 0 {
				return fmt.Errorf("release needs a resource, or --all to hand back everything")
			}
			if all && len(args) == 1 {
				return fmt.Errorf("release takes either a resource or --all, not both")
			}
			agent, err := a.resolveAgent(agentFlag)
			if err != nil {
				return err
			}
			theme := a.theme()
			cl := client.Dial(a.socketPath())

			if all {
				result, err := cl.ReleaseAll(cmd.Context(), agent)
				if err != nil {
					return err
				}
				if len(result.Released) == 0 {
					fmt.Fprintln(a.out, theme.Verdict(broker.Result{Verdict: broker.OK, Message: fmt.Sprintf("%s holds nothing", agent)}))
					return nil
				}
				fmt.Fprintln(a.out, theme.ResourceList(toResources(result.Released)))
				return nil
			}

			res, err := lease.ParseResource(args[0])
			if err != nil {
				return err
			}
			result, err := cl.Release(cmd.Context(), agent, res)
			if err != nil {
				return err
			}
			fmt.Fprintln(a.out, theme.Verdict(result))
			if result.Verdict == broker.Error {
				return &ExitError{Code: 1}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&agentFlag, "agent", "", "agent identity (default $LEASEKEEPER_AGENT)")
	cmd.Flags().BoolVar(&all, "all", false, "hand back every lease this agent holds")
	return cmd
}

func (a *app) statusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Print the lease table and the wait-for graph",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cl := client.Dial(a.socketPath())
			status, err := cl.Status(cmd.Context())
			if err != nil {
				if errors.Is(err, client.ErrNoBroker) {
					fmt.Fprintln(a.out, a.theme().Dim(fmt.Sprintf(
						"no broker is running on %s — no leases are held", a.socketPath())))
					return nil
				}
				return err
			}
			fmt.Fprintln(a.out, a.theme().Status(status, time.Now()))
			return nil
		},
	}
}

func toResources(names []string) []lease.Resource {
	out := make([]lease.Resource, 0, len(names))
	for _, n := range names {
		out = append(out, lease.Resource(n))
	}
	return out
}

func (a *app) demoCommand() *cobra.Command {
	var pause time.Duration
	cmd := &cobra.Command{
		Use:   "demo",
		Short: "Run the two-agent cycle-refusal demo in one process",
		Long: "Demo starts a broker on a temporary socket and plays the whole story in\n" +
			"one process: agent a takes file:src/a.go, agent b takes port:8080, a then\n" +
			"waits for the port, and b's request for a's file is refused because it\n" +
			"would close b -> a -> b. Then b releases the port and the queued waiter is\n" +
			"served in order. No configuration and no second terminal.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.runDemo(cmd.Context(), pause)
		},
	}
	cmd.Flags().DurationVar(&pause, "pause", 0, "wait this long after each step, so a recording reads at human speed")
	return cmd
}

// runDemo drives a real broker over a real socket with a fixed script, so the
// recording and the test suite exercise the same cycle check the product ships.
func (a *app) runDemo(ctx context.Context, pause time.Duration) error {
	dir, err := os.MkdirTemp("", "lk-demo-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "b.sock")

	brk, err := broker.Listen(path)
	if err != nil {
		return err
	}
	brk.Serve()
	defer brk.Close()

	cl := client.Dial(path)
	theme := a.theme()
	out := a.out

	const agentA, agentB = "a", "b"
	fileA, err := lease.ParseResource("file:src/a.go")
	if err != nil {
		return err
	}
	port, err := lease.ParseResource("port:8080")
	if err != nil {
		return err
	}

	step := func(agent, command string) {
		fmt.Fprintf(out, "\n  %s %s\n", theme.Agent(agent), theme.Dim(command))
	}
	show := func(r broker.Result) {
		fmt.Fprintf(out, "     %s\n", theme.Verdict(r))
	}
	// beat paces the transcript for a recording without changing the words.
	beat := func() {
		if pause > 0 {
			time.Sleep(pause)
		}
	}

	fmt.Fprintln(out, theme.Section("Leasekeeper demo — two agents, one resource pair, one refused cycle"))
	fmt.Fprintf(out, "%s\n", theme.Dim("broker started on a temporary socket; no configuration needed"))

	step(agentA, "acquire file:src/a.go")
	r, err := cl.Acquire(ctx, agentA, fileA, 0, true, 5*time.Second, nil)
	if err != nil {
		return err
	}
	show(r)
	beat()

	step(agentB, "acquire port:8080")
	r, err = cl.Acquire(ctx, agentB, port, 0, true, 5*time.Second, nil)
	if err != nil {
		return err
	}
	show(r)
	beat()

	// a asks for the port b holds. The broker records the edge a -> b and
	// queues a, which now blocks until the lease is handed over.
	step(agentA, "acquire port:8080   (blocks)")
	queued := make(chan broker.Result, 1)
	granted := make(chan broker.Result, 1)
	go func() {
		res, err := cl.Acquire(ctx, agentA, port, 0, true, 30*time.Second, func(q broker.Result) {
			select {
			case queued <- q:
			default:
			}
		})
		if err == nil {
			granted <- res
		}
	}()
	select {
	case q := <-queued:
		show(q)
	case <-time.After(3 * time.Second):
		return fmt.Errorf("demo: a never reported a wait; the broker did not record the edge")
	}
	if err := waitForEdge(ctx, cl, agentA, agentB, 3*time.Second); err != nil {
		return err
	}

	// b asks for the file a holds. That would close b -> a -> b, so the acquire
	// is refused instead of hanging both agents.
	step(agentB, "acquire file:src/a.go")
	r, err = cl.Acquire(ctx, agentB, fileA, 0, true, 5*time.Second, nil)
	if err != nil {
		return err
	}
	show(r)
	beat()

	status, err := cl.Status(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "\n%s\n", theme.Status(status, time.Now()))
	beat()

	// b gives the port back; the queued waiter is served in order.
	step(agentB, "release port:8080")
	r, err = cl.Release(ctx, agentB, port)
	if err != nil {
		return err
	}
	show(r)
	beat()

	select {
	case g := <-granted:
		fmt.Fprintf(out, "     %s\n", theme.Verdict(g))
		beat()
	case <-time.After(3 * time.Second):
		return fmt.Errorf("demo: a was never granted the port after b released it")
	}

	if _, err := cl.ReleaseAll(ctx, agentA); err != nil {
		return err
	}
	status, err = cl.Status(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "\n%s\n", theme.Status(status, time.Now()))
	beat()
	fmt.Fprintf(out, "\n%s\n", theme.Dim(
		"a held half the pair and b held the other half, so the second acquire was refused\n"+
			"instead of hanging. Nothing was preempted: the refusal failed fast, and once b\n"+
			"released the port the queued waiter was served in order."))
	// Hold the closing note, so a recording ends on it rather than mid-frame.
	beat()
	return nil
}

// waitForEdge waits until the broker has recorded waiter -> holder, so the
// demo's transcript reads in the same order on every machine.
func waitForEdge(ctx context.Context, cl *client.Client, waiter, holder string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		status, err := cl.Status(ctx)
		if err != nil {
			return err
		}
		for _, e := range status.Edges {
			if e.Waiter == waiter && e.Holder == holder {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("demo: broker never recorded the wait %s -> %s", waiter, holder)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Execute runs the CLI and turns a child's exit status into this process's
// exit status.
func Execute(ctx context.Context) int {
	root := New()
	root.SetContext(ctx)
	err := root.Execute()
	if err == nil {
		return 0
	}
	var exit *ExitError
	if errors.As(err, &exit) {
		if exit.Msg != "" {
			fmt.Fprintln(os.Stderr, "leasekeeper: "+exit.Msg)
		}
		if exit.Code == 0 {
			return 1
		}
		return exit.Code
	}
	fmt.Fprintln(os.Stderr, "leasekeeper: "+err.Error())
	return 1
}
