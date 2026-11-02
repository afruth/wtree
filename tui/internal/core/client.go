package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// Client runs the wtree core. All of its calls take a context, and none of them
// reads the terminal. See Stream for what a cancelled context does.
type Client interface {
	// Version runs `core version`. It returns a *ContractError, with the Version,
	// when the script speaks another contract. Run it first.
	Version(ctx context.Context) (Version, error)

	// List starts `core list --stream`.
	//
	// A list with no Only starts a new generation and replaces all the lists that
	// run. A list with Only keeps the generation, and replaces a running list with
	// the same Only, so a stats tick never stops a load. A replaced list ends with
	// ErrSuperseded. An event in flight can still reach a model, so a model keeps
	// the Gen of its last full list and drops an event with an older Gen.
	List(ctx context.Context, options ListOptions) (*Stream, error)

	// Plan runs `core plan`. A plan only reads, and it is advice: Apply computes it again.
	Plan(ctx context.Context, request PlanRequest) (Plan, error)

	// Apply starts `core apply`. A refusal comes as a RefusedEvent, and as a
	// *RefusedError in the Result. It changes state, so see Stream for how it stops.
	Apply(ctx context.Context, request ApplyRequest) (*Stream, error)

	// Open starts `core open`, with one step for each directory.
	Open(ctx context.Context, request OpenRequest) (*Stream, error)

	// New starts `core new`. With DryRun it is a read.
	New(ctx context.Context, request NewRequest) (*Stream, error)
}

// ListOptions are the flags of `core list`.
type ListOptions struct {
	Global    bool
	Cwd       string
	Only      []string // the stages, such as StageStats. Empty means all of them
	NoRefresh bool
}

// PlanRequest is an action, its flags and its targets. Send the same one to Apply.
type PlanRequest struct {
	Action  string   // ActionDeleteWorktrees and the others
	Force   bool     // delete-worktrees
	Volumes bool     // delete-stacks
	Repos   []string // delete-worktrees: --repo, once for each target, or empty
	Dirs    []string // delete-stacks: --dir, once for each target, or empty
	Cwd     string
	Targets []string
}

// ApplyRequest sends a plan back. The UI asks the human for Phrases. It shows the
// phrases of the Choice, and it never derives one.
type ApplyRequest struct {
	PlanRequest
	PlanID  string
	Choice  string   // empty only for a plan with Confirm.Kind none
	Phrases []string // exact, and in the order of the phrases of the Choice
}

// OpenRequest is the directories to open.
type OpenRequest struct {
	Cwd  string
	Dirs []string
}

// NewRequest is the flags of `core new`.
type NewRequest struct {
	Cwd    string
	Repo   string
	Name   string
	NoOpen bool
	DryRun bool
}

// ---- the command lines

func listArgs(o ListOptions) []string {
	args := []string{"core", "list", "--stream"}
	if o.Global {
		args = append(args, "--global")
	}
	if o.Cwd != "" {
		args = append(args, "--cwd", o.Cwd)
	}
	if len(o.Only) > 0 {
		args = append(args, "--only", strings.Join(o.Only, ","))
	}
	if o.NoRefresh {
		args = append(args, "--no-refresh")
	}
	return args
}

func (r PlanRequest) flagArgs() []string {
	var args []string
	if r.Force {
		args = append(args, "--force")
	}
	if r.Volumes {
		args = append(args, "--volumes")
	}
	for _, repo := range r.Repos {
		args = append(args, "--repo", repo)
	}
	for _, dir := range r.Dirs {
		args = append(args, "--dir", dir)
	}
	if r.Cwd != "" {
		args = append(args, "--cwd", r.Cwd)
	}
	return args
}

func planArgs(r PlanRequest) []string {
	args := append([]string{"core", "plan", r.Action}, r.flagArgs()...)
	args = append(args, "--")
	return append(args, r.Targets...)
}

func applyArgs(r ApplyRequest) []string {
	args := []string{"core", "apply", r.Action, "--plan", r.PlanID}
	if r.Choice != "" {
		args = append(args, "--choice", r.Choice)
	}
	for _, phrase := range r.Phrases {
		args = append(args, "--phrase", phrase)
	}
	args = append(args, r.flagArgs()...)
	args = append(args, "--")
	return append(args, r.Targets...)
}

func openArgs(r OpenRequest) []string {
	args := []string{"core", "open"}
	if r.Cwd != "" {
		args = append(args, "--cwd", r.Cwd)
	}
	args = append(args, "--")
	return append(args, r.Dirs...)
}

func newArgs(r NewRequest) []string {
	args := []string{"core", "new"}
	if r.Cwd != "" {
		args = append(args, "--cwd", r.Cwd)
	}
	if r.Repo != "" {
		args = append(args, "--repo", r.Repo)
	}
	if r.Name != "" {
		args = append(args, "--name", r.Name)
	}
	if r.NoOpen {
		args = append(args, "--no-open")
	}
	if r.DryRun {
		args = append(args, "--dry-run")
	}
	return args
}

// ---- generations of a list

// generations holds the lists that run, for a real and for a fake client.
type generations struct {
	mu      sync.Mutex
	gen     uint64
	running map[*listEntry]struct{}
}

type listEntry struct {
	only   string
	cancel context.CancelCauseFunc
}

// begin replaces what this list replaces, and registers the new one. Call done when it ends.
func (g *generations) begin(only []string, cancel context.CancelCauseFunc) (gen uint64, done func()) {
	key := strings.Join(only, ",")
	entry := &listEntry{only: key, cancel: cancel}
	g.mu.Lock()
	defer g.mu.Unlock()
	if key == "" {
		g.gen++
	}
	for other := range g.running {
		if key == "" || other.only == key {
			other.cancel(ErrSuperseded)
			delete(g.running, other)
		}
	}
	if g.running == nil {
		g.running = make(map[*listEntry]struct{})
	}
	g.running[entry] = struct{}{}
	return g.gen, func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		delete(g.running, entry)
	}
}

// ---- the client that runs the script

// Config says how to run the script.
type Config struct {
	// Path is the script bin/wtree.
	Path string
	// Env is the environment of the commands. Nil means that of this process.
	Env []string
	// StopGrace is the time from the TERM to the KILL of a read. Default 1.5 s.
	StopGrace time.Duration
	// PipeGrace is how long to keep reading a pipe after the command has exited, for
	// a child that still holds it. Default 2 s.
	PipeGrace time.Duration
	// VersionTimeout limits `core version`, which runs before any UI. Default 10 s.
	VersionTimeout time.Duration
}

// ExecClient is the Client that runs the script.
type ExecClient struct {
	config Config
	lists  generations
}

var _ Client = (*ExecClient)(nil)

// NewExecClient returns a Client that runs the script at config.Path.
func NewExecClient(config Config) *ExecClient {
	if config.StopGrace <= 0 {
		config.StopGrace = 1500 * time.Millisecond
	}
	if config.PipeGrace <= 0 {
		config.PipeGrace = 2 * time.Second
	}
	if config.VersionTimeout <= 0 {
		config.VersionTimeout = 10 * time.Second
	}
	return &ExecClient{config: config}
}

// Version implements Client.
func (c *ExecClient) Version(ctx context.Context) (Version, error) {
	ctx, cancel := context.WithTimeout(ctx, c.config.VersionTimeout)
	defer cancel()
	out, state, err := c.runDocument(ctx, []string{"core", "version"})
	if err != nil {
		return Version{}, err
	}
	return parseVersion(out, state, ctx.Err())
}

// Plan implements Client.
func (c *ExecClient) Plan(ctx context.Context, request PlanRequest) (Plan, error) {
	out, state, err := c.runDocument(ctx, planArgs(request))
	if err != nil {
		return Plan{}, err
	}
	return parsePlan(out, state, ctx.Err())
}

// List implements Client.
func (c *ExecClient) List(ctx context.Context, options ListOptions) (*Stream, error) {
	ctx, cancel := context.WithCancelCause(ctx)
	gen, done := c.lists.begin(options.Only, cancel)
	return c.stream(ctx, cancel, gen, done, listArgs(options), true)
}

// Apply implements Client.
func (c *ExecClient) Apply(ctx context.Context, request ApplyRequest) (*Stream, error) {
	ctx, cancel := context.WithCancelCause(ctx)
	return c.stream(ctx, cancel, 0, nil, applyArgs(request), false)
}

// Open implements Client.
func (c *ExecClient) Open(ctx context.Context, request OpenRequest) (*Stream, error) {
	ctx, cancel := context.WithCancelCause(ctx)
	return c.stream(ctx, cancel, 0, nil, openArgs(request), false)
}

// New implements Client.
func (c *ExecClient) New(ctx context.Context, request NewRequest) (*Stream, error) {
	ctx, cancel := context.WithCancelCause(ctx)
	return c.stream(ctx, cancel, 0, nil, newArgs(request), request.DryRun)
}

func (c *ExecClient) stream(ctx context.Context, cancel context.CancelCauseFunc, gen uint64, done func(), args []string, readOnly bool) (*Stream, error) {
	p, err := c.start(ctx, args, readOnly)
	if err != nil {
		cancel(nil)
		if done != nil {
			done()
		}
		return nil, err
	}
	s := newStream(ctx, cancel, gen, readOnly)
	s.pid = p.cmd.Process.Pid
	go func() {
		var t tracker
		p.drain(func(text []byte, terminated, tooLong bool) {
			if value := parseLine(text, terminated, tooLong); value != nil {
				t.observe(value)
				s.push(value)
			}
		})
		state := p.state()
		if done != nil {
			done()
		}
		var cause error
		if ctx.Err() != nil {
			cause = context.Cause(ctx)
		}
		s.finish(t.result(state, cause))
	}()
	return s, nil
}

// runDocument runs a read that writes one object. A cancelled context gives its error.
func (c *ExecClient) runDocument(ctx context.Context, args []string) ([]byte, exitState, error) {
	p, err := c.start(ctx, args, true)
	if err != nil {
		return nil, exitState{}, err
	}
	var out bytes.Buffer
	p.drain(func(text []byte, _, tooLong bool) {
		if !tooLong {
			out.Write(text)
		}
	})
	return out.Bytes(), p.state(), nil
}

// ---- documents

// lastLineOf is the last line that is not empty.
func lastLineOf(out []byte) []byte {
	out = bytes.TrimRight(out, " \r\n\t")
	if i := bytes.LastIndexByte(out, '\n'); i >= 0 {
		return out[i+1:]
	}
	return out
}

// documentFailure explains a document command that did not give its document. It
// returns nil when the exit code is 0 and the output can be parsed.
func documentFailure(out []byte, state exitState, stopped error) error {
	if state.exit == ExitOK {
		return nil
	}
	if stopped != nil {
		return stopped
	}
	last := lastLineOf(out)
	if len(last) > 0 {
		value, err := DecodeEvent(last)
		if err == nil {
			switch ev := value.(type) {
			case ErrorEvent:
				return &CoreError{Code: ev.Code, Message: ev.Message, Exit: state.exit}
			case RefusedEvent:
				return &RefusedError{Code: ev.Code, Message: ev.Message, Plan: ev.Plan}
			}
		}
	}
	if state.signal != "" || len(last) > 0 {
		return &ChildDiedError{Exit: state.exit, Signal: state.signal, Stderr: state.stderr, Partial: string(last)}
	}
	return &ExitError{Exit: state.exit, Stderr: state.stderr}
}

func parseVersion(out []byte, state exitState, stopped error) (Version, error) {
	if err := documentFailure(out, state, stopped); err != nil {
		return Version{}, err
	}
	var version Version
	if err := json.Unmarshal(out, &version); err != nil {
		return Version{}, &ProtocolError{Text: string(out), Err: err}
	}
	if version.V != ContractVersion || version.Contract != ContractVersion {
		got := version.Contract
		if version.V != ContractVersion {
			got = version.V
		}
		return version, &ContractError{Script: version.Script, Got: got, Want: ContractVersion}
	}
	return version, nil
}

func parsePlan(out []byte, state exitState, stopped error) (Plan, error) {
	if err := documentFailure(out, state, stopped); err != nil {
		return Plan{}, err
	}
	var plan Plan
	if err := json.Unmarshal(out, &plan); err != nil {
		return Plan{}, &ProtocolError{Text: string(out), Err: err}
	}
	if plan.V != ContractVersion {
		return Plan{}, &ContractError{Got: plan.V, Want: ContractVersion}
	}
	if plan.PlanID == "" {
		return Plan{}, &ProtocolError{Text: string(out), Err: errors.New("core: a plan with no plan_id")}
	}
	return plan, nil
}

// ---- a process

// process is a running command in its own process group, with no terminal input.
//
// The output goes through pipes that this file owns, and not through what exec
// offers, for two reasons. A child that outlives the command can hold a pipe open,
// and the reader must still end. And WaitDelay would kill a command that changes
// state some time after its context ends.
type process struct {
	cmd      *exec.Cmd
	stdout   *os.File
	stderr   *os.File
	tail     *tailBuffer
	grace    time.Duration
	readOnly bool

	stderrDone chan struct{}
	drained    chan struct{}
	exited     chan struct{}
	stopping   atomic.Bool
}

func (c *ExecClient) start(ctx context.Context, args []string, readOnly bool) (*process, error) {
	if err := ctx.Err(); err != nil {
		return nil, context.Cause(ctx)
	}
	outRead, outWrite, err := os.Pipe()
	if err != nil {
		return nil, &StartError{Path: c.config.Path, Err: err}
	}
	errRead, errWrite, err := os.Pipe()
	if err != nil {
		outRead.Close()
		outWrite.Close()
		return nil, &StartError{Path: c.config.Path, Err: err}
	}
	cmd := exec.CommandContext(ctx, c.config.Path, args...)
	cmd.Env = c.config.Env
	cmd.Stdout = outWrite
	cmd.Stderr = errWrite
	// Stdin stays nil, which is /dev/null. The own group keeps a key press on the
	// terminal away from the command, and lets a read end with all its children.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	p := &process{
		cmd:        cmd,
		stdout:     outRead,
		stderr:     errRead,
		tail:       &tailBuffer{limit: 64 << 10},
		grace:      c.config.PipeGrace,
		readOnly:   readOnly,
		stderrDone: make(chan struct{}),
		drained:    make(chan struct{}),
		exited:     make(chan struct{}),
	}
	stopGrace := c.config.StopGrace
	cmd.Cancel = func() error {
		p.stopping.Store(true)
		pid := cmd.Process.Pid
		if !readOnly {
			// To the process and not to its group, so that the step in flight can
			// finish. There is no KILL after it, because a step has no time limit.
			return ignoreGone(syscall.Kill(pid, syscall.SIGTERM))
		}
		// A group stays valid until its last member ends, and the kernel does not
		// reuse its number until then.
		err := syscall.Kill(-pid, syscall.SIGTERM)
		if !errors.Is(err, syscall.ESRCH) {
			time.AfterFunc(stopGrace, func() { _ = syscall.Kill(-pid, syscall.SIGKILL) })
		}
		return ignoreGone(err)
	}
	startErr := cmd.Start()
	outWrite.Close()
	errWrite.Close()
	if startErr != nil {
		outRead.Close()
		errRead.Close()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, context.Cause(ctx)
		}
		return nil, &StartError{Path: c.config.Path, Err: startErr}
	}
	go func() {
		defer close(p.stderrDone)
		_, _ = p.tail.ReadFrom(errRead)
	}()
	go p.reap(stopGrace)
	return p, nil
}

func ignoreGone(err error) error {
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}

// reap waits for the command. Then it gives a child that holds a pipe some time,
// and it stops reading after that.
func (p *process) reap(stopGrace time.Duration) {
	_ = p.cmd.Wait() // the exit is read from ProcessState
	close(p.exited)
	// a read that a signal ended cannot finish its work, so it leaves no children
	if p.readOnly && !p.stopping.Load() && signalName(p.cmd.ProcessState) != "" {
		pid := p.cmd.Process.Pid
		if !errors.Is(syscall.Kill(-pid, syscall.SIGTERM), syscall.ESRCH) {
			time.AfterFunc(stopGrace, func() { _ = syscall.Kill(-pid, syscall.SIGKILL) })
		}
	}
	timer := time.NewTimer(p.grace)
	defer timer.Stop()
	select {
	case <-p.drained:
	case <-timer.C:
		past := time.Now()
		_ = p.stdout.SetReadDeadline(past)
		_ = p.stderr.SetReadDeadline(past)
	}
}

// drain calls line for each line of the output. It returns when the command is
// over and its output is read.
func (p *process) drain(line func(text []byte, terminated, tooLong bool)) {
	readLines(p.stdout, line)
	<-p.stderrDone
	close(p.drained)
	<-p.exited
	p.stdout.Close()
	p.stderr.Close()
}

// state comes after drain.
func (p *process) state() exitState {
	return exitState{
		exit:   p.cmd.ProcessState.ExitCode(),
		signal: signalName(p.cmd.ProcessState),
		stderr: p.tail.String(),
	}
}

func signalName(state *os.ProcessState) string {
	if state == nil {
		return ""
	}
	if status, ok := state.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return status.Signal().String()
	}
	return ""
}

// tailBuffer keeps the end of what it reads.
type tailBuffer struct {
	mu    sync.Mutex
	limit int
	data  []byte
}

// ReadFrom reads until an error or the end. A deadline on the pipe ends it.
func (b *tailBuffer) ReadFrom(r io.Reader) (int64, error) {
	var total int64
	chunk := make([]byte, 8<<10)
	for {
		n, err := r.Read(chunk)
		total += int64(n)
		b.mu.Lock()
		b.data = append(b.data, chunk[:n]...)
		if len(b.data) > b.limit {
			b.data = bytes.Clone(b.data[len(b.data)-b.limit:])
		}
		b.mu.Unlock()
		if err != nil {
			return total, nil
		}
	}
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.ToValidUTF8(string(b.data), "�")
}
