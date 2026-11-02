package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// One line for each event of the contract, with all its fields, as the contract shows them.
func TestDecodeEveryEventShape(t *testing.T) {
	hello := "hello"
	cases := []struct {
		name string
		line string
		want any
	}{
		{EventStage, `{"v":1,"event":"stage","stage":"structure"}`, StageEvent{Stage: StageStructure}},
		{EventStage + " done", `{"v":1,"event":"stage","stage":"git","done":true,"ms":118}`, StageEvent{Stage: StageGit, Done: true, MS: 118}},
		{EventRepo, `{"v":1,"event":"repo","repo":{"common":"/p/.git","name":"p","main":"/p","default_branch":"hello"}}`,
			RepoEvent{Repo{Common: "/p/.git", Name: "p", Main: "/p", DefaultBranch: &hello}}},
		{EventRepo + " no default", `{"v":1,"event":"repo","repo":{"common":"/p/.git","name":"p","main":"/p","default_branch":null}}`,
			RepoEvent{Repo{Common: "/p/.git", Name: "p", Main: "/p"}}},
		{EventWorktree, `{"v":1,"event":"worktree","worktree":{"path":"/w","common":"/p/.git","repo":"p","main":false,"branch":"hello","detached":false,"head":"hello","missing":false,"bare":false,"locked":"hello","current":true,"odd":true}}`,
			WorktreeEvent{Worktree{Path: "/w", Common: "/p/.git", Repo: "p", Branch: &hello, Head: &hello, Locked: &hello, Current: true, Odd: true}}},
		{EventStack, `{"v":1,"event":"stack","stack":{"project":"s","dir":null,"dirs":[],"kind":"volumes","worktree":null,"common":null,"worktrees":[],"states":{"running":1},"containers":[{"id":"c","name":"n","service":"api","state":"running","health":"hello","status":"Up","ports":"80","published":["80"]}],"volumes":null}}`,
			StackEvent{Stack{Project: "s", Dirs: []string{}, Kind: StackVolumes, Worktrees: []string{}, States: map[string]int{"running": 1},
				Containers: []Container{{ID: "c", Name: "n", Service: "api", State: "running", Health: &hello, Status: "Up", Ports: "80", Published: []string{"80"}}}}}},
		{EventWorktreeGit, `{"v":1,"event":"worktree_git","path":"/w","missing":true}`, WorktreeGit{Path: "/w", Missing: true}},
		{EventStackStats, `{"v":1,"event":"stack_stats","project":"s","cpu_pct":2.5,"mem_bytes":9,"containers":[{"id":"c","cpu_pct":2.5,"mem_bytes":9}]}`,
			StackStats{Project: "s", CPUPct: 2.5, MemBytes: 9, Containers: []ContainerStats{{ID: "c", CPUPct: 2.5, MemBytes: 9}}}},
		{EventStackAge, `{"v":1,"event":"stack_age","project":"s","last_started_at":null,"last_finished_at":null,"running":true,"stale":true}`,
			StackAge{Project: "s", Running: true, Stale: true}},
		{EventEngine, `{"v":1,"event":"engine","ncpu":null,"mem_total":null,"cpu_pct":null,"mem_bytes":null}`, Engine{}},
		{EventStackDisk, `{"v":1,"event":"stack_disk","project":"s","bytes":3,"age_s":null,"refreshing":true}`, StackDisk{Project: "s", Bytes: 3, Refreshing: true}},
		{EventDiskTotals, `{"v":1,"event":"disk_totals","images":null,"containers":null,"volumes":null,"build":null,"age_s":null,"refreshing":false}`, DiskTotals{}},
		{EventCleanup, `{"v":1,"event":"cleanup","id":"stack:s","kind":"stack","label":"s","tier":"safe","reasons":[{"code":"c","text":"t"}],"bytes":null,"stacks":[]}`,
			CleanupRow{ID: "stack:s", Kind: "stack", Label: "s", Tier: TierSafe, Reasons: []Reason{{"c", "t"}}, Stacks: []string{}}},
		{EventCleanupTotals, `{"v":1,"event":"cleanup_totals","safe":1,"needs_look":2,"never":3,"unknown":true}`, CleanupTotals{Safe: 1, NeedsLook: 2, Never: 3, Unknown: true}},
		{EventWarning, `{"v":1,"event":"warning","code":"docker_timeout","message":"m"}`, Warning{Code: WarnDockerTimeout, Message: "m"}},
		{EventStart, `{"v":1,"event":"start","plan_id":"p","steps":3}`, StartEvent{PlanID: "p", Steps: 3}},
		{EventStep + " running", `{"v":1,"event":"step","id":"s1","state":"running","label":"l"}`, StepEvent{ID: "s1", State: StepRunning, Label: "l"}},
		{EventStep + " ok", `{"v":1,"event":"step","id":"s1","state":"ok","ms":5}`, StepEvent{ID: "s1", State: StepOK, MS: 5}},
		{EventStep + " failed", `{"v":1,"event":"step","id":"s1","state":"failed","ms":5,"status":1,"tail":["a"]}`, StepEvent{ID: "s1", State: StepFailed, MS: 5, Status: 1, Tail: []string{"a"}}},
		{EventStep + " skipped", `{"v":1,"event":"step","id":"s2","state":"skipped","because":"cancelled"}`, StepEvent{ID: "s2", State: StepSkipped, Because: "cancelled"}},
		{EventOutput, `{"v":1,"event":"output","id":"s1","line":"a"}`, OutputEvent{ID: "s1", Line: "a"}},
		{EventEnd + " list", `{"v":1,"event":"end","ms":260}`, EndEvent{MS: 260}},
		{EventEnd + " apply", `{"v":1,"event":"end","ok":1,"failed":2,"skipped":3,"disk_refresh":true,"cancelled":true}`, EndEvent{OK: 1, Failed: 2, Skipped: 3, DiskRefresh: true, Cancelled: true}},
		{EventRefused, `{"v":1,"event":"refused","code":"phrase_mismatch","message":"m"}`, RefusedEvent{Code: RefusedPhraseMismatch, Message: "m"}},
		{EventError, `{"v":1,"event":"error","code":"usage","message":"m"}`, ErrorEvent{Code: ErrorUsage, Message: "m"}},
		{EventCreated, `{"v":1,"event":"created","path":"/w","name":"n","head":"h"}`, NewWorktreeEvent{Created: true, Path: "/w", Name: "n", Head: "h"}},
		{EventPlanNew, `{"v":1,"event":"plan_new","path":"/w","name":"n","head":"h"}`, NewWorktreeEvent{Path: "/w", Name: "n", Head: "h"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseLine([]byte(c.line+"\n"), true, false)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got  %#v\nwant %#v", got, c.want)
			}
		})
	}
}

func TestParseLineKeepsAnEventOfANewerScript(t *testing.T) {
	got := parseLine([]byte(`{"v":1,"event":"future","x":1}`), true, false)
	unknown, ok := got.(UnknownEvent)
	if !ok || unknown.Name != "future" {
		t.Fatalf("got %#v", got)
	}
	// a new field on a known event is ignored
	got = parseLine([]byte(`{"v":1,"event":"output","id":"s1","line":"a","future":[1,2]}`), true, false)
	if !reflect.DeepEqual(got, OutputEvent{ID: "s1", Line: "a"}) {
		t.Fatalf("got %#v", got)
	}
}

func TestParseLineBadLines(t *testing.T) {
	cases := []struct {
		name       string
		line       string
		terminated bool
		truncated  bool
	}{
		{"not json", "bash: line 3: oops", true, false},
		{"cut json with a newline", `{"v":1,"event":"st`, true, false},
		{"cut json at the end", `{"v":1,"event":"st`, false, true},
		{"another contract", `{"v":2,"event":"stage","stage":"git"}`, true, false},
		{"no version", `{"event":"stage","stage":"git"}`, true, false},
		{"a field of the wrong type", `{"v":1,"event":"stage","stage":7}`, true, false},
		{"a scalar", `7`, true, false},
		{"an array", `[1]`, true, false},
		{"null", `null`, true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := parseLine([]byte(c.line), c.terminated, false).(BadLine)
			if !ok {
				t.Fatalf("not a BadLine")
			}
			if got.Text != c.line || got.Truncated != c.truncated || got.Err == nil {
				t.Fatalf("got %#v", got)
			}
		})
	}
	if got := parseLine([]byte("  \r\n"), true, false); got != nil {
		t.Fatalf("an empty line gave %#v", got)
	}
	// a whole object with no newline is whole
	if _, ok := parseLine([]byte(`{"v":1,"event":"end","ms":1}`), false, false).(EndEvent); !ok {
		t.Fatal("a whole last line with no newline must decode")
	}
}

func TestReadLinesSplitsCutsAndBounds(t *testing.T) {
	type got struct {
		text       string
		terminated bool
		tooLong    bool
	}
	read := func(input string) []got {
		var lines []got
		readLines(strings.NewReader(input), func(text []byte, terminated, tooLong bool) {
			lines = append(lines, got{string(text), terminated, tooLong})
		})
		return lines
	}
	if lines := read(""); len(lines) != 0 {
		t.Fatalf("empty: %v", lines)
	}
	lines := read("a\nb\n\nc")
	want := []got{{"a\n", true, false}, {"b\n", true, false}, {"\n", true, false}, {"c", false, false}}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("got %v", lines)
	}
	// longer than the buffer of the reader, and still whole
	long := strings.Repeat("x", 200<<10)
	lines = read(long + "\nnext\n")
	if len(lines) != 2 || lines[0].text != long+"\n" || lines[0].tooLong || lines[1].text != "next\n" {
		t.Fatalf("a long line: %d lines", len(lines))
	}
	// longer than the limit: it comes cut, and the next line is fine
	huge := strings.Repeat("y", maxLineBytes+(200<<10))
	lines = read(huge + "\nnext\n" + huge)
	if len(lines) != 3 || !lines[0].tooLong || !lines[0].terminated || len(lines[0].text) != maxLineText {
		t.Fatalf("a huge line: %d lines", len(lines))
	}
	if lines[1].text != "next\n" || !lines[2].tooLong || lines[2].terminated {
		t.Fatalf("after a huge line: %#v", lines[1])
	}
	if bad, ok := parseLine([]byte(lines[0].text), true, true).(BadLine); !ok || !errors.Is(bad.Err, errLineTooLong) {
		t.Fatalf("a huge line must be a BadLine: %#v", bad)
	}
}

func TestTrackerResult(t *testing.T) {
	stop := errors.New("stop")
	killed := exitState{exit: -1, signal: "killed", stderr: "boom\n"}
	plan := &Plan{PlanID: "new"}
	cases := []struct {
		name    string
		events  []any
		state   exitState
		cause   error
		check   func(error) bool
		stopped bool
	}{
		{"a list that ended", []any{EndEvent{MS: 1}}, exitState{}, nil, isNil, false},
		{"an apply with a failed step", []any{EndEvent{Failed: 1}}, exitState{exit: 1}, nil, isNil, false},
		{"a cancelled apply", []any{EndEvent{Cancelled: true}}, exitState{exit: 1}, nil, isNil, false},
		{"an end, and a stop at the same moment", []any{EndEvent{}}, exitState{}, stop, isNil, false},
		{"an end, and an exit it does not explain", []any{EndEvent{}}, exitState{exit: 1}, nil, is[*ExitError], false},
		{"an end with failures, and exit 2", []any{EndEvent{Failed: 1}}, exitState{exit: 2}, nil, is[*ExitError], false},
		{"refused", []any{RefusedEvent{Code: RefusedPlanStale, Plan: plan}}, exitState{exit: 4}, nil, is[*RefusedError], false},
		{"an error", []any{ErrorEvent{Code: ErrorUsage}}, exitState{exit: 2}, nil, is[*CoreError], false},
		{"no events and exit 0", nil, exitState{}, nil, is[*ChildDiedError], false},
		{"no end and exit 0", []any{StageEvent{}}, exitState{}, nil, is[*ChildDiedError], false},
		{"killed", []any{StageEvent{}}, killed, nil, is[*ChildDiedError], false},
		{"stopped", []any{StageEvent{}}, killed, stop, func(err error) bool { return err == stop }, true},
		{"new with one line", []any{NewWorktreeEvent{Created: true}}, exitState{}, nil, isNil, false},
		{"new that died before it opened", []any{NewWorktreeEvent{Created: true}}, killed, nil, is[*ChildDiedError], false},
		{"new that died while it opened", []any{NewWorktreeEvent{Created: true}, StartEvent{}}, exitState{}, nil, is[*ChildDiedError], false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var tr tracker
			for _, ev := range c.events {
				tr.observe(ev)
			}
			r := tr.result(c.state, c.cause)
			if !c.check(r.Err) {
				t.Fatalf("err = %#v", r.Err)
			}
			if r.Stopped != c.stopped {
				t.Fatalf("stopped = %v", r.Stopped)
			}
		})
	}
}

func isNil(err error) bool { return err == nil }

func is[T error](err error) bool {
	var target T
	return errors.As(err, &target)
}

func TestErrorTextIsSafeToDraw(t *testing.T) {
	dirty := "a\x1b[31mb\x07c\xff" + strings.Repeat("z", 2*maxErrorText)
	errs := []error{
		&ContractError{Script: dirty},
		&StartError{Path: dirty, Err: errors.New("x")},
		&CoreError{Message: dirty},
		&RefusedError{Message: dirty},
		&ProtocolError{Text: dirty},
		&ProtocolError{Text: dirty, Truncated: true},
		&ChildDiedError{Stderr: dirty},
		&ExitError{Stderr: dirty},
	}
	for _, err := range errs {
		text := err.Error()
		if strings.ContainsAny(text, "\x1b\x07") || !strings.Contains(text, "�") {
			t.Errorf("%T: %q", err, text)
		}
		if len(text) > maxErrorText+200 {
			t.Errorf("%T: %d bytes", err, len(text))
		}
	}
}

// ---- the recordings

func fixtureNames(t *testing.T) []string {
	t.Helper()
	entries, err := fs.ReadDir(Fixtures(), ".")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if len(names) < 30 {
		t.Fatalf("only %d fixtures", len(names))
	}
	return names
}

func fixtureData(t *testing.T, name string) []byte {
	t.Helper()
	data, err := fs.ReadFile(Fixtures(), name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// strictly decodes a recording: a field that types.go does not know fails, which a
// consumer may ignore, but the mirror of the contract must not.
func strictly(line []byte, into any) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(line, &fields); err != nil {
		return err
	}
	delete(fields, "v")
	delete(fields, "event")
	clean, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(clean))
	decoder.DisallowUnknownFields()
	return decoder.Decode(into)
}

func TestFixturesMatchTheTypesExactly(t *testing.T) {
	for _, name := range fixtureNames(t) {
		data := fixtureData(t, name)
		t.Run(name, func(t *testing.T) {
			switch {
			case name == "version.json":
				if err := strictly(data, &Version{V: 1}); err != nil {
					t.Fatal(err)
				}
			case name == "list-document.json":
				if err := strictly(data, &ListDoc{}); err != nil {
					t.Fatal(err)
				}
			case strings.HasPrefix(name, "plan-"):
				if err := strictly(data, &Plan{}); err != nil {
					t.Fatal(err)
				}
			default:
				for _, line := range splitLines(data) {
					value := parseLine(line, true, false)
					switch value.(type) {
					case BadLine, UnknownEvent, nil:
						t.Fatalf("not an event of the types: %s", line)
					}
					into := reflect.New(reflect.TypeOf(value)).Interface()
					if err := strictly(line, into); err != nil {
						t.Fatalf("%v\nline: %s", err, line)
					}
				}
			}
		})
	}
}

func TestFixturesCoverEveryEvent(t *testing.T) {
	seen := map[string]bool{}
	for _, name := range fixtureNames(t) {
		if path.Ext(name) != ".ndjson" {
			continue
		}
		for _, line := range splitLines(fixtureData(t, name)) {
			var head struct {
				Event string `json:"event"`
			}
			if err := json.Unmarshal(line, &head); err != nil {
				t.Fatal(err)
			}
			seen[head.Event] = true
		}
	}
	for _, event := range []string{EventStage, EventRepo, EventWorktree, EventStack, EventWorktreeGit, EventStackStats, EventStackAge,
		EventEngine, EventStackDisk, EventDiskTotals, EventCleanup, EventCleanupTotals, EventWarning, EventStart, EventStep, EventOutput,
		EventEnd, EventRefused, EventError, EventCreated, EventPlanNew} {
		if !seen[event] {
			t.Errorf("no recording has the event %s", event)
		}
	}
}

// the fake derives an exit code from the last event, and the recordings hold what the script said
func TestFixturesExitCodesAreTheOnesTheFakeImplies(t *testing.T) {
	data, err := os.ReadFile("testdata/exit-codes.tsv")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, row := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		name, code, ok := strings.Cut(row, "\t")
		want, convErr := strconv.Atoi(code)
		if !ok || convErr != nil {
			t.Fatalf("bad row %q", row)
		}
		count++
		var tr tracker
		for _, line := range splitLines(fixtureData(t, name)) {
			tr.observe(parseLine(line, true, false))
		}
		if got := impliedExit(&tr, FakeScript{}); got != want {
			t.Errorf("%s: recorded exit %d, the fake implies %d", name, want, got)
		}
	}
	if count != len(fixtureNames(t)) {
		t.Errorf("%d exit codes for %d fixtures", count, len(fixtureNames(t)))
	}
}

func TestFixturesHoldNoPathOfThisHost(t *testing.T) {
	for _, name := range fixtureNames(t) {
		data := fixtureData(t, name)
		for _, leak := range []string{"wtree-test.", "/root/", "/tmp/"} {
			if bytes.Contains(data, []byte(leak)) {
				t.Errorf("%s has %q", name, leak)
			}
		}
	}
}

// A scene of the recordings has what a UI needs to show.
func TestFixtureListStreamHasTheFactsOfTheScene(t *testing.T) {
	var gits = map[string]WorktreeGit{}
	var locked *string
	tiers := map[string]string{}
	for _, line := range splitLines(fixtureData(t, "list-stream.ndjson")) {
		switch ev := parseLine(line, true, false).(type) {
		case WorktreeEvent:
			if strings.HasSuffix(ev.Worktree.Path, "/locked") {
				locked = ev.Worktree.Locked
			}
		case WorktreeGit:
			gits[path.Base(ev.Path)] = ev
		case CleanupRow:
			tiers[ev.Label] = ev.Tier
		}
	}
	if locked == nil || !strings.Contains(*locked, "\x1b[31m") {
		t.Errorf("the lock reason must keep its escape, for a test of a sanitizer: %v", locked)
	}
	if g := gits["beta"]; g.Changed == nil || *g.Changed != 2 || g.Untracked == nil || *g.Untracked != 1 {
		t.Errorf("beta: %+v", g)
	}
	if g := gits["gamma"]; g.HeadHeld == nil || *g.HeadHeld {
		t.Errorf("gamma must have a commit that nothing holds: %+v", g)
	}
	if !gits["vanished"].Missing || gits["vanished"].Changed != nil {
		t.Errorf("vanished: %+v", gits["vanished"])
	}
	want := map[string]string{"proj": TierNever, "locked": TierNever, "merged": TierSafe, "beta": TierNeedsLook, "gamma": TierNeedsLook, "proj-vols": TierNeedsLook}
	for label, tier := range want {
		if tiers[label] != tier {
			t.Errorf("%s: tier %q, want %q", label, tiers[label], tier)
		}
	}
}
