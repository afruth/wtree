package core

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sync"
	"time"
)

// The recorded output of the script: see testdata/README.md. The linker keeps these
// only in a program that uses the fake, so the real binary does not carry them.
//
//go:embed testdata/*.json testdata/*.ndjson
var fixtures embed.FS

// Fixtures gives the recorded files, by their names in testdata.
func Fixtures() fs.FS {
	sub, err := fs.Sub(fixtures, "testdata")
	if err != nil {
		panic(err) // the name is a constant
	}
	return sub
}

// FakeScript says what the fake answers to one kind of call.
type FakeScript struct {
	// Fixture is the name of a file of Fixtures. Data takes its place when it is set.
	Fixture string
	Data    []byte
	// Delay comes before each line of a stream.
	Delay time.Duration
	// PauseAfter stops a stream after this many lines, until Release or a cancel: a
	// scene that holds a stage or a step. Zero means no pause. To pause before the
	// first line, use a negative number.
	PauseAfter int
	// OnCancel is for a stream that changes state: after a cancel, the replay goes on
	// at the same line of this fixture, as a TERM between two steps does. Empty means
	// that the same file goes on.
	OnCancel string
	// DieAfter ends a stream after this many lines, as a child that dies. With
	// Truncate, the next line comes cut in half first. Zero means that it does not die.
	DieAfter int
	Truncate bool
	// Exit takes the place of the exit code that the last event implies.
	Exit *int
	// Stderr is what the command wrote there.
	Stderr string
}

// FakeCall is one call to a FakeClient. Args is the command line that the real
// client runs for it.
type FakeCall struct {
	Op   string
	Args []string
}

// FakeClient is a Client that replays recorded output, and starts no process. It
// keeps the rules of the real one: generations, how a read and a stream that changes
// state stop, and how a stream ends. It decides nothing else: it checks no plan id
// and no phrase, so a test looks at Calls for what a UI sent.
type FakeClient struct {
	files fs.FS
	lists generations

	mu      sync.Mutex
	scripts map[string]FakeScript
	calls   []FakeCall
	release chan struct{}
}

var _ Client = (*FakeClient)(nil)

// NewFakeClient answers each kind of call with a recorded fixture, which On can change.
func NewFakeClient() *FakeClient {
	return &FakeClient{
		files:   Fixtures(),
		release: make(chan struct{}),
		scripts: map[string]FakeScript{
			"version":                       {Fixture: "version.json"},
			"list":                          {Fixture: "list-stream.ndjson"},
			"list:global":                   {Fixture: "list-global.ndjson"},
			"list:only":                     {Fixture: "list-only-stats.ndjson"},
			"plan:" + ActionDeleteWorktrees: {Fixture: "plan-delete-worktrees.json"},
			"plan:" + ActionDeleteStacks:    {Fixture: "plan-delete-stacks.json"},
			"plan:" + ActionDeleteStacks + ":volumes":  {Fixture: "plan-delete-stacks-volumes.json"},
			"plan:" + ActionDeleteWorktrees + ":force": {Fixture: "plan-delete-worktrees-force.json"},
			"plan:" + ActionStopStacks:                 {Fixture: "plan-stop-stacks.json"},
			"plan:" + ActionOpen:                       {Fixture: "plan-open.json"},
			"apply":                                    {Fixture: "apply-ok.ndjson", OnCancel: "apply-cancelled.ndjson"},
			"open":                                     {Fixture: "open.ndjson"},
			"new":                                      {Fixture: "new.ndjson"},
			"new:dry-run":                              {Fixture: "new-dry-run.ndjson"},
		},
	}
}

// On sets the answer to a kind of call: version, list, list:global, list:only (a list
// with Only), plan:<action>, plan:<action>:force, plan:<action>:volumes, apply,
// apply:<action>, open, new or new:dry-run. A call with no script of its own kind uses
// the next one that fits: plan:<action>:force, then plan:<action>, then plan.
func (f *FakeClient) On(op string, script FakeScript) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scripts[op] = script
}

// Release lets all paused streams go on.
func (f *FakeClient) Release() {
	f.mu.Lock()
	defer f.mu.Unlock()
	close(f.release)
	f.release = make(chan struct{})
}

// Calls gives the calls so far.
func (f *FakeClient) Calls() []FakeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]FakeCall(nil), f.calls...)
}

// lookup records the call, and finds its script: the first op that has one.
func (f *FakeClient) lookup(args []string, ops ...string) (FakeScript, []byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, FakeCall{Op: ops[0], Args: args})
	for _, op := range ops {
		script, ok := f.scripts[op]
		if !ok {
			continue
		}
		if script.Data != nil {
			return script, script.Data, nil
		}
		data, err := fs.ReadFile(f.files, script.Fixture)
		if err != nil {
			return script, nil, fmt.Errorf("core: fake: %w", err)
		}
		return script, data, nil
	}
	return FakeScript{}, nil, fmt.Errorf("core: fake: no script for %s", ops[0])
}

// Version implements Client.
func (f *FakeClient) Version(ctx context.Context) (Version, error) {
	if err := ctx.Err(); err != nil {
		return Version{}, err
	}
	script, data, err := f.lookup([]string{"core", "version"}, "version")
	if err != nil {
		return Version{}, err
	}
	return parseVersion(data, fakeDocumentState(data, script), nil)
}

// Plan implements Client.
func (f *FakeClient) Plan(ctx context.Context, request PlanRequest) (Plan, error) {
	if err := ctx.Err(); err != nil {
		return Plan{}, err
	}
	ops := []string{"plan:" + request.Action, "plan"}
	if request.Force {
		ops = append([]string{ops[0] + ":force"}, ops...)
	}
	if request.Volumes {
		ops = append([]string{ops[0] + ":volumes"}, ops...)
	}
	script, data, err := f.lookup(planArgs(request), ops...)
	if err != nil {
		return Plan{}, err
	}
	return parsePlan(data, fakeDocumentState(data, script), nil)
}

// List implements Client.
func (f *FakeClient) List(ctx context.Context, options ListOptions) (*Stream, error) {
	op := "list"
	switch {
	case len(options.Only) > 0:
		op = "list:only"
	case options.Global:
		op = "list:global"
	}
	ctx, cancel := context.WithCancelCause(ctx)
	gen, done := f.lists.begin(options.Only, cancel)
	return f.stream(ctx, cancel, gen, done, true, listArgs(options), op, "list")
}

// Apply implements Client.
func (f *FakeClient) Apply(ctx context.Context, request ApplyRequest) (*Stream, error) {
	ctx, cancel := context.WithCancelCause(ctx)
	return f.stream(ctx, cancel, 0, nil, false, applyArgs(request), "apply:"+request.Action, "apply")
}

// Open implements Client.
func (f *FakeClient) Open(ctx context.Context, request OpenRequest) (*Stream, error) {
	ctx, cancel := context.WithCancelCause(ctx)
	return f.stream(ctx, cancel, 0, nil, false, openArgs(request), "open")
}

// New implements Client.
func (f *FakeClient) New(ctx context.Context, request NewRequest) (*Stream, error) {
	op := "new"
	if request.DryRun {
		op = "new:dry-run"
	}
	ctx, cancel := context.WithCancelCause(ctx)
	return f.stream(ctx, cancel, 0, nil, request.DryRun, newArgs(request), op)
}

func (f *FakeClient) stream(ctx context.Context, cancel context.CancelCauseFunc, gen uint64, done func(), readOnly bool, args []string, ops ...string) (*Stream, error) {
	fail := func(err error) (*Stream, error) {
		cancel(nil)
		if done != nil {
			done()
		}
		return nil, err
	}
	if ctx.Err() != nil {
		return fail(context.Cause(ctx))
	}
	script, data, err := f.lookup(args, ops...)
	if err != nil {
		return fail(err)
	}
	var onCancel [][]byte
	if script.OnCancel != "" {
		extra, err := fs.ReadFile(f.files, script.OnCancel)
		if err != nil {
			return fail(fmt.Errorf("core: fake: %w", err))
		}
		onCancel = splitLines(extra)
	}
	f.mu.Lock()
	release := f.release
	f.mu.Unlock()
	s := newStream(ctx, cancel, gen, readOnly)
	go f.replay(ctx, s, script, release, splitLines(data), onCancel, readOnly, done)
	return s, nil
}

// replay plays the lines of a script, and it ends the stream the way the real client does.
func (f *FakeClient) replay(ctx context.Context, s *Stream, script FakeScript, release <-chan struct{}, lines, onCancel [][]byte, readOnly bool, done func()) {
	var t tracker
	emit := func(text []byte, terminated bool) {
		if value := parseLine(text, terminated, false); value != nil {
			t.observe(value)
			s.push(value)
		}
	}
	end := func(state exitState) {
		if done != nil {
			done()
		}
		var cause error
		if ctx.Err() != nil {
			cause = context.Cause(ctx)
		}
		s.finish(t.result(state, cause))
	}
	stopped := func() { end(exitState{exit: -1, signal: "terminated", stderr: script.Stderr}) }

	switched := false
	for i := 0; i < len(lines); i++ {
		if script.DieAfter > 0 && i == script.DieAfter {
			if script.Truncate {
				emit(lines[i][:len(lines[i])/2], false)
			}
			end(exitState{exit: -1, signal: "killed", stderr: script.Stderr})
			return
		}
		if (script.PauseAfter > 0 && i == script.PauseAfter) || (script.PauseAfter < 0 && i == 0) {
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
		if script.Delay > 0 {
			select {
			case <-time.After(script.Delay):
			case <-ctx.Done():
			}
		}
		if ctx.Err() != nil {
			if readOnly {
				stopped()
				return
			}
			if !switched && onCancel != nil {
				// the same line of the other recording
				switched = true
				lines = onCancel
				if i >= len(lines) {
					break
				}
			}
		}
		emit(lines[i], true)
	}
	if t.end == nil && t.refused == nil && t.coreErr == nil && t.created == nil && ctx.Err() != nil {
		stopped()
		return
	}
	end(exitState{exit: impliedExit(&t, script), stderr: script.Stderr})
}

func splitLines(data []byte) [][]byte {
	var lines [][]byte
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(line)) > 0 {
			lines = append(lines, line)
		}
	}
	return lines
}

// impliedExit is the exit code of the contract for what the stream said.
func impliedExit(t *tracker, script FakeScript) int {
	switch {
	case script.Exit != nil:
		return *script.Exit
	case t.refused != nil:
		return ExitRefused
	case t.coreErr != nil:
		return errorExit(t.coreErr.Code)
	case t.end != nil && (t.end.Failed > 0 || t.end.Cancelled):
		return ExitFailure
	}
	return ExitOK
}

func errorExit(code string) int {
	switch code {
	case ErrorUsage:
		return ExitUsage
	case ErrorNotARepo:
		return ExitEnvironment
	}
	return ExitFailure
}

// fakeDocumentState gives a document the exit code that its content implies.
func fakeDocumentState(data []byte, script FakeScript) exitState {
	state := exitState{exit: ExitOK, stderr: script.Stderr}
	if script.Exit != nil {
		state.exit = *script.Exit
		return state
	}
	// a plan or a version decodes as an unknown event, and it stays at exit 0
	if value, err := DecodeEvent(lastLineOf(data)); err == nil {
		var t tracker
		t.observe(value)
		state.exit = impliedExit(&t, script)
	}
	return state
}
