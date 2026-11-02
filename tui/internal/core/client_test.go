package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

// These tests run small shell scripts in the place of bin/wtree. The real script
// runs only in guarded_test.go.

const (
	endLine  = `{"v":1,"event":"end","ms":1}`
	lineWait = 10 * time.Second
)

// child writes a script that acts as the core, and a client that runs it.
func child(t *testing.T, body string) *ExecClient {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "wtree")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ncd "+dir+"\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return NewExecClient(Config{Path: script, StopGrace: 150 * time.Millisecond, PipeGrace: 200 * time.Millisecond})
}

func dirOf(c *ExecClient) string { return filepath.Dir(c.config.Path) }

func collect(t *testing.T, s *Stream) []any {
	t.Helper()
	var values []any
	timeout := time.After(30 * time.Second)
	for {
		select {
		case ev, ok := <-s.Events():
			if !ok {
				return values
			}
			values = append(values, ev.Value)
		case <-timeout:
			t.Fatal("the stream did not end")
		}
	}
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(lineWait)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("never: %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func groupGone(pid int) bool { return errors.Is(syscall.Kill(-pid, 0), syscall.ESRCH) }

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func waitEvent(t *testing.T, s *Stream, match func(any) bool) {
	t.Helper()
	timeout := time.After(lineWait)
	for {
		select {
		case ev, ok := <-s.Events():
			if !ok {
				t.Fatal("the stream ended before the event")
			}
			if match(ev.Value) {
				return
			}
		case <-timeout:
			t.Fatal("the event did not come")
		}
	}
}

func isStage(value any) bool {
	_, ok := value.(StageEvent)
	return ok
}

// ---- command lines

func TestCommandLines(t *testing.T) {
	cases := []struct {
		got  []string
		want string
	}{
		{listArgs(ListOptions{}), "core list --stream"},
		{listArgs(ListOptions{Global: true, Cwd: "/d", Only: []string{StageStats, StageDisk}, NoRefresh: true}),
			"core list --stream --global --cwd /d --only stats,disk --no-refresh"},
		{planArgs(PlanRequest{Action: ActionDeleteWorktrees, Force: true, Repos: []string{"/r/.git"}, Cwd: "/d", Targets: []string{"-odd"}}),
			"core plan delete-worktrees --force --repo /r/.git --cwd /d -- -odd"},
		{planArgs(PlanRequest{Action: ActionDeleteStacks, Volumes: true, Dirs: []string{"/a", "/b"}, Targets: []string{"p", "q"}}),
			"core plan delete-stacks --volumes --dir /a --dir /b -- p q"},
		{applyArgs(ApplyRequest{PlanRequest: PlanRequest{Action: ActionStopStacks, Targets: []string{"p"}}, PlanID: "id"}),
			"core apply stop-stacks --plan id -- p"},
		{applyArgs(ApplyRequest{PlanRequest: PlanRequest{Action: ActionDeleteStacks, Volumes: true, Targets: []string{"p", "q"}},
			PlanID: "id", Choice: "stacks", Phrases: []string{"2 stacks", "2 stacks w"}}),
			"core apply delete-stacks --plan id --choice stacks --phrase 2 stacks --phrase 2 stacks w --volumes -- p q"},
		{openArgs(OpenRequest{Cwd: "/d", Dirs: []string{"/a", "/b"}}), "core open --cwd /d -- /a /b"},
		{newArgs(NewRequest{}), "core new"},
		{newArgs(NewRequest{Cwd: "/d", Repo: "/r", Name: "n", NoOpen: true, DryRun: true}), "core new --cwd /d --repo /r --name n --no-open --dry-run"},
	}
	for _, c := range cases {
		if got := strings.Join(c.got, " "); got != c.want {
			t.Errorf("got  %s\nwant %s", got, c.want)
		}
	}
}

func TestArgumentsReachTheChildUnchanged(t *testing.T) {
	c := child(t, `for a in "$@"; do printf '%s\n' "$a"; done >args`+"\necho '"+endLine+"'")
	phrase := "  2 Stacks\twith\ttabs  "
	s, err := c.Apply(context.Background(), ApplyRequest{PlanRequest: PlanRequest{Action: ActionDeleteStacks, Targets: []string{"a b"}}, PlanID: "id", Choice: "c", Phrases: []string{phrase}})
	if err != nil {
		t.Fatal(err)
	}
	s.Wait()
	data, err := os.ReadFile(filepath.Join(dirOf(c), "args"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "\n"+phrase+"\n") || !strings.Contains(string(data), "\na b\n") {
		t.Fatalf("args: %q", data)
	}
}

func TestTheChildHasNoTerminalInputAndItsOwnGroup(t *testing.T) {
	c := child(t, `ps -o pgid= -p $$ | tr -d ' ' >pgid; cat >stdin; echo '`+endLine+`'`)
	s, err := c.List(context.Background(), ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if r := s.Wait(); r.Err != nil {
		t.Fatal(r.Err)
	}
	if data, _ := os.ReadFile(filepath.Join(dirOf(c), "stdin")); len(data) != 0 {
		t.Fatalf("stdin gave %q", data)
	}
	data, _ := os.ReadFile(filepath.Join(dirOf(c), "pgid"))
	if strings.TrimSpace(string(data)) != fmt.Sprint(s.pid) {
		t.Fatalf("pgid %q, pid %d", data, s.pid)
	}
}

// ---- a stream that ends well

func TestListStreamsAFixtureInOrder(t *testing.T) {
	file := filepath.Join("testdata", "list-stream.ndjson")
	abs, err := filepath.Abs(file)
	if err != nil {
		t.Fatal(err)
	}
	c := child(t, "cat "+abs)
	s, err := c.List(context.Background(), ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if s.Gen() != 1 {
		t.Fatalf("gen %d", s.Gen())
	}
	values := collect(t, s)
	r := s.Wait()
	if r.Err != nil || r.Exit != 0 || r.Stopped {
		t.Fatalf("%+v", r)
	}
	if r.End == nil {
		t.Fatal("no end")
	}
	if len(values) != len(splitLines(fixtureData(t, "list-stream.ndjson"))) {
		t.Fatalf("%d events", len(values))
	}
	if first, ok := values[0].(StageEvent); !ok || first.Stage != StageStructure {
		t.Fatalf("first: %#v", values[0])
	}
	if _, ok := values[len(values)-1].(EndEvent); !ok {
		t.Fatalf("last: %#v", values[len(values)-1])
	}
}

func TestEveryEventCarriesTheGeneration(t *testing.T) {
	c := child(t, "echo '{\"v\":1,\"event\":\"stage\",\"stage\":\"git\"}'; echo oops; echo '"+endLine+"'")
	_, _ = c.List(context.Background(), ListOptions{})
	s, err := c.List(context.Background(), ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for ev := range s.Events() {
		count++
		if ev.Gen != 2 {
			t.Fatalf("gen %d", ev.Gen)
		}
	}
	if count != 3 {
		t.Fatalf("%d events", count)
	}
}

func TestAFailedStepIsNotAnError(t *testing.T) {
	c := child(t, `echo '{"v":1,"event":"start","plan_id":"p","steps":1}'
echo '{"v":1,"event":"step","id":"s1","state":"failed","ms":1,"status":1,"tail":["x"]}'
echo '{"v":1,"event":"end","ok":0,"failed":1,"skipped":0,"disk_refresh":false,"cancelled":false}'
exit 1`)
	s, err := c.Apply(context.Background(), ApplyRequest{PlanRequest: PlanRequest{Action: ActionStopStacks, Targets: []string{"p"}}, PlanID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	collect(t, s)
	r := s.Wait()
	if r.Err != nil || r.Exit != 1 || r.End == nil || r.End.Failed != 1 {
		t.Fatalf("%+v", r)
	}
}

func TestAnExitWithAnEventExplainsItself(t *testing.T) {
	cases := []struct {
		name string
		line string
		exit int
		code string
	}{
		{"usage", `{"v":1,"event":"error","code":"usage","message":"m"}`, ExitUsage, ErrorUsage},
		{"not a repo", `{"v":1,"event":"error","code":"not_a_repo","message":"m"}`, ExitEnvironment, ErrorNotARepo},
		{"internal", `{"v":1,"event":"error","code":"internal","message":"m"}`, ExitFailure, ErrorInternal},
	}
	for _, kase := range cases {
		t.Run(kase.name, func(t *testing.T) {
			c := child(t, fmt.Sprintf("echo '%s'\nexit %d", kase.line, kase.exit))
			s, err := c.List(context.Background(), ListOptions{})
			if err != nil {
				t.Fatal(err)
			}
			r := s.Wait()
			var coreErr *CoreError
			if !errors.As(r.Err, &coreErr) || coreErr.Code != kase.code || coreErr.Exit != kase.exit || r.Exit != kase.exit {
				t.Fatalf("%+v", r)
			}
		})
	}
}

func TestARefusalHasItsPlan(t *testing.T) {
	abs, _ := filepath.Abs("testdata/apply-refused-stale.ndjson")
	c := child(t, "cat "+abs+"\nexit 4")
	s, err := c.Apply(context.Background(), ApplyRequest{PlanRequest: PlanRequest{Action: ActionDeleteWorktrees, Targets: []string{"/w"}}, PlanID: "old"})
	if err != nil {
		t.Fatal(err)
	}
	values := collect(t, s)
	if len(values) != 1 {
		t.Fatalf("%d events", len(values))
	}
	r := s.Wait()
	var refused *RefusedError
	if !errors.As(r.Err, &refused) || refused.Code != RefusedPlanStale || refused.Plan == nil || refused.Plan.PlanID == "" || r.Exit != ExitRefused {
		t.Fatalf("%+v", r)
	}
}

func TestNewEndsWithOneLine(t *testing.T) {
	c := child(t, `echo '{"v":1,"event":"created","path":"/w","name":"n","head":"h"}'`)
	s, err := c.New(context.Background(), NewRequest{NoOpen: true})
	if err != nil {
		t.Fatal(err)
	}
	r := s.Wait()
	if r.Err != nil || r.Created == nil || !r.Created.Created || r.Created.Path != "/w" {
		t.Fatalf("%+v", r)
	}
}

// ---- bad output

func TestAGarbageLineDoesNotStopTheStream(t *testing.T) {
	c := child(t, "printf 'bash: \\033[31moops\\n'; echo '"+endLine+"'")
	s, err := c.List(context.Background(), ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	values := collect(t, s)
	if len(values) != 2 {
		t.Fatalf("%#v", values)
	}
	bad, ok := values[0].(BadLine)
	if !ok || bad.Truncated || !strings.Contains(bad.Text, "\x1b") {
		t.Fatalf("%#v", values[0])
	}
	if strings.Contains(bad.Err.Error(), "\x1b") {
		t.Fatalf("the error text is not safe: %q", bad.Err.Error())
	}
	if r := s.Wait(); r.Err != nil {
		t.Fatalf("%+v", r)
	}
}

func TestATruncatedLastLineAtExitZero(t *testing.T) {
	c := child(t, `echo '{"v":1,"event":"stage","stage":"git"}'; printf '{"v":1,"event":"en'`)
	s, err := c.List(context.Background(), ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	values := collect(t, s)
	if len(values) != 2 {
		t.Fatalf("%#v", values)
	}
	if bad, ok := values[1].(BadLine); !ok || !bad.Truncated {
		t.Fatalf("%#v", values[1])
	}
	r := s.Wait()
	var died *ChildDiedError
	if !errors.As(r.Err, &died) || died.Partial != `{"v":1,"event":"en` {
		t.Fatalf("%+v", r)
	}
}

func TestALineAboveTheLimitDoesNotStopTheStream(t *testing.T) {
	c := child(t, fmt.Sprintf("head -c %d /dev/zero | tr '\\0' x; echo; echo '%s'", maxLineBytes+1024, endLine))
	s, err := c.List(context.Background(), ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	values := collect(t, s)
	if len(values) != 2 {
		t.Fatalf("%d events", len(values))
	}
	bad, ok := values[0].(BadLine)
	if !ok || !errors.Is(bad.Err, errLineTooLong) || len(bad.Text) > maxLineText {
		t.Fatalf("%T", values[0])
	}
}

// ---- a child that dies

func TestAChildThatDies(t *testing.T) {
	stage := `echo '{"v":1,"event":"stage","stage":"git"}'`
	cases := []struct {
		name    string
		body    string
		exit    int
		signal  string
		partial string
		stderr  string
	}{
		{"exit 0 with no end", stage, 0, "", "", ""},
		{"exit 1 with no end", stage + "\nexit 1", 1, "", "", ""},
		{"exit 137 with no end", stage + "\nexit 137", 137, "", "", ""},
		{"no output", "exit 0", 0, "", "", ""},
		{"kill -9", stage + "\nkill -9 $$", -1, "killed", "", ""},
		{"kill -TERM", stage + "\nkill -TERM $$", -1, "terminated", "", ""},
		{"kill -9 in a line", stage + "\nprintf '{\"v\":1,\"ev'\nkill -9 $$", -1, "killed", `{"v":1,"ev`, ""},
		{"stderr on the way out", stage + "\necho boom >&2\nexit 3", 3, "", "", "boom\n"},
	}
	for _, kase := range cases {
		t.Run(kase.name, func(t *testing.T) {
			c := child(t, kase.body)
			s, err := c.Apply(context.Background(), ApplyRequest{PlanRequest: PlanRequest{Action: ActionStopStacks, Targets: []string{"p"}}, PlanID: "p"})
			if err != nil {
				t.Fatal(err)
			}
			collect(t, s)
			r := s.Wait()
			var died *ChildDiedError
			if !errors.As(r.Err, &died) {
				t.Fatalf("%+v", r)
			}
			if r.Stopped || r.Exit != kase.exit || r.Signal != kase.signal || died.Partial != kase.partial || r.Stderr != kase.stderr {
				t.Fatalf("%+v, %+v", r, died)
			}
			if kase.stderr != "" && !strings.Contains(died.Error(), "boom") {
				t.Fatalf("the error does not say why: %s", died)
			}
		})
	}
}

func TestAReadWhoseLeaderDiesLeavesNoChildren(t *testing.T) {
	c := child(t, `sleep 30 >/dev/null 2>&1 </dev/null &
kill -9 $$`)
	s, err := c.List(context.Background(), ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	r := s.Wait()
	var died *ChildDiedError
	if !errors.As(r.Err, &died) {
		t.Fatalf("%+v", r)
	}
	waitFor(t, "the group is gone", func() bool { return groupGone(s.pid) })
}

// A child that outlives the command must not hang the reader.
func TestAChildThatHoldsThePipeDoesNotHangTheStream(t *testing.T) {
	c := child(t, "sleep 30 &\necho '"+endLine+"'")
	started := time.Now()
	s, err := c.List(context.Background(), ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-s.pid, syscall.SIGKILL) })
	values := collect(t, s)
	if len(values) != 1 {
		t.Fatalf("%#v", values)
	}
	if r := s.Wait(); r.Err != nil {
		t.Fatalf("%+v", r)
	}
	if took := time.Since(started); took > 5*time.Second {
		t.Fatalf("it took %v", took)
	}
}

// the disk scan runs behind the list, redirected, in the group of the command
func TestANormalExitLeavesAScanBehindTheList(t *testing.T) {
	c := child(t, "(sleep 2; : >scan-done) >/dev/null 2>&1 </dev/null &\necho '"+endLine+"'")
	s, err := c.List(context.Background(), ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-s.pid, syscall.SIGKILL) })
	collect(t, s)
	if r := s.Wait(); r.Err != nil {
		t.Fatalf("%+v", r)
	}
	if groupGone(s.pid) {
		t.Fatal("the scan was stopped")
	}
}

// ---- cancel of a read

func TestCancelOfAListEndsTheCommandAndItsChildren(t *testing.T) {
	c := child(t, `echo '{"v":1,"event":"stage","stage":"git"}'
sleep 30 &
sleep 30`)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, err := c.List(ctx, ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	waitEvent(t, s, isStage)
	cancel()
	r := s.Wait()
	if !r.Stopped || !errors.Is(r.Err, context.Canceled) {
		t.Fatalf("%+v", r)
	}
	if _, open := <-s.Events(); open {
		t.Fatal("an event after the stop")
	}
	waitFor(t, "the group is gone", func() bool { return groupGone(s.pid) })
}

func TestCancelOfAListKillsChildrenThatIgnoreTerm(t *testing.T) {
	c := child(t, `trap '' TERM
echo '{"v":1,"event":"stage","stage":"git"}'
sleep 30 &
sleep 30`)
	s, err := c.List(context.Background(), ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	waitEvent(t, s, isStage)
	started := time.Now()
	s.Stop()
	r := s.Wait()
	if !r.Stopped || r.Signal != "killed" {
		t.Fatalf("%+v", r)
	}
	waitFor(t, "the group is gone", func() bool { return groupGone(s.pid) })
	if took := time.Since(started); took > 5*time.Second {
		t.Fatalf("it took %v", took)
	}
}

// the leader exits on TERM at once, and a child that ignores TERM must still go
func TestCancelKillsAChildThatOutlivesTheLeader(t *testing.T) {
	c := child(t, `(trap '' TERM; exec sleep 30) >/dev/null 2>&1 </dev/null &
echo '{"v":1,"event":"stage","stage":"git"}'
sleep 30`)
	s, err := c.List(context.Background(), ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	waitEvent(t, s, isStage)
	s.Stop()
	s.Wait()
	waitFor(t, "the group is gone", func() bool { return groupGone(s.pid) })
}

func TestCancelDropsTheEventsThatWereNotRead(t *testing.T) {
	c := child(t, `i=0; while [ $i -lt 2000 ]; do echo '{"v":1,"event":"stage","stage":"git"}'; i=$((i+1)); done
sleep 30`)
	s, err := c.List(context.Background(), ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	s.Stop()
	s.Wait()
	count := 0
	for range s.Events() {
		count++
	}
	if count > 1 {
		t.Fatalf("%d events came after the stop", count)
	}
}

func TestCloseOfAReadStopsIt(t *testing.T) {
	c := child(t, "sleep 30")
	s, err := c.List(context.Background(), ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	if r := s.Wait(); !r.Stopped {
		t.Fatalf("%+v", r)
	}
	waitFor(t, "the group is gone", func() bool { return groupGone(s.pid) })
}

func TestATimeoutIsACancel(t *testing.T) {
	c := child(t, "sleep 30")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	s, err := c.List(ctx, ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	r := s.Wait()
	if !r.Stopped || !errors.Is(r.Err, context.DeadlineExceeded) {
		t.Fatalf("%+v", r)
	}
}

func TestACancelledContextStartsNothing(t *testing.T) {
	c := child(t, ": >started\necho '"+endLine+"'")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.List(ctx, ListOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := c.Apply(ctx, ApplyRequest{PlanID: "p"}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := c.Plan(ctx, PlanRequest{Action: ActionOpen}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := c.Version(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if fileExists(filepath.Join(dirOf(c), "started")) {
		t.Fatal("a command started")
	}
}

func TestADryRunIsARead(t *testing.T) {
	c := child(t, "sleep 30")
	s, err := c.New(context.Background(), NewRequest{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	s.Stop()
	if r := s.Wait(); !r.Stopped {
		t.Fatalf("%+v", r)
	}
	waitFor(t, "the group is gone", func() bool { return groupGone(s.pid) })
}

// ---- cancel of what changes state

// a stand-in for run_steps_ndjson: a step in the group of the command, and a trap for TERM
const stepsScript = `cancelled=''
trap 'cancelled=1' TERM
echo '{"v":1,"event":"start","plan_id":"p","steps":2}'
echo '{"v":1,"event":"step","id":"s1","state":"running","label":"one"}'
( trap ': >step-saw-term; exit 9' TERM; : >step-started; sleep 0.6 & wait; : >step-done ) &
step=$!
while ! wait $step; do kill -0 $step 2>/dev/null || break; done
echo '{"v":1,"event":"step","id":"s1","state":"ok","ms":600}'
if [ -n "$cancelled" ]; then
  echo '{"v":1,"event":"step","id":"s2","state":"skipped","because":"cancelled"}'
  echo '{"v":1,"event":"end","ok":1,"failed":0,"skipped":1,"disk_refresh":false,"cancelled":true}'
  exit 1
fi
: >step-two
echo '{"v":1,"event":"step","id":"s2","state":"running","label":"two"}'
echo '{"v":1,"event":"step","id":"s2","state":"ok","ms":1}'
echo '{"v":1,"event":"end","ok":2,"failed":0,"skipped":0,"disk_refresh":false,"cancelled":false}'`

func assertStepFinishedAndNextSkipped(t *testing.T, c *ExecClient, s *Stream) {
	t.Helper()
	dir := dirOf(c)
	var last []any
	for ev := range s.Events() {
		last = append(last, ev.Value)
	}
	r := s.Wait()
	if r.Err != nil || r.Stopped || r.Exit != ExitFailure || r.End == nil || !r.End.Cancelled {
		t.Fatalf("%+v", r)
	}
	if fileExists(filepath.Join(dir, "step-saw-term")) {
		t.Fatal("the TERM reached the step")
	}
	if !fileExists(filepath.Join(dir, "step-done")) {
		t.Fatal("the step did not finish")
	}
	if fileExists(filepath.Join(dir, "step-two")) {
		t.Fatal("the next step ran")
	}
	want := StepEvent{ID: "s2", State: StepSkipped, Because: "cancelled"}
	found := false
	for _, value := range last {
		if reflect.DeepEqual(value, want) {
			found = true
		}
	}
	if !found {
		t.Fatalf("events after the cancel: %#v", last)
	}
}

func isRunning(value any) bool {
	step, ok := value.(StepEvent)
	return ok && step.State == StepRunning
}

func TestCancelOfAnApplySendsOneTermToTheProcessAlone(t *testing.T) {
	c := child(t, stepsScript)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, err := c.Apply(ctx, ApplyRequest{PlanRequest: PlanRequest{Action: ActionDeleteStacks, Targets: []string{"p"}}, PlanID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	waitEvent(t, s, isRunning)
	waitFor(t, "the step started", func() bool { return fileExists(filepath.Join(dirOf(c), "step-started")) })
	cancel()
	assertStepFinishedAndNextSkipped(t, c, s)
}

func TestStopOfAnApplyIsTheSame(t *testing.T) {
	c := child(t, stepsScript)
	s, err := c.Apply(context.Background(), ApplyRequest{PlanRequest: PlanRequest{Action: ActionDeleteStacks, Targets: []string{"p"}}, PlanID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	waitEvent(t, s, isRunning)
	waitFor(t, "the step started", func() bool { return fileExists(filepath.Join(dirOf(c), "step-started")) })
	s.Stop()
	s.Stop()
	assertStepFinishedAndNextSkipped(t, c, s)
}

func TestApplyWithNoCancelRunsAllItsSteps(t *testing.T) {
	c := child(t, stepsScript)
	s, err := c.Apply(context.Background(), ApplyRequest{PlanRequest: PlanRequest{Action: ActionDeleteStacks, Targets: []string{"p"}}, PlanID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	collect(t, s)
	if r := s.Wait(); r.Err != nil || r.End == nil || r.End.OK != 2 {
		t.Fatalf("%+v", r)
	}
}

// no grace ends a step: it runs far longer than the grace, with a TERM long before
func TestAStepThatChangesStateIsNeverKilled(t *testing.T) {
	c := child(t, `trap '' TERM
echo '{"v":1,"event":"start","plan_id":"p","steps":1}'
sleep 1
: >finished
echo '{"v":1,"event":"end","ok":1,"failed":0,"skipped":0,"disk_refresh":false,"cancelled":false}'`)
	c.config.StopGrace = time.Millisecond
	c.config.PipeGrace = time.Millisecond
	for name, start := range map[string]func(context.Context) (*Stream, error){
		"apply": func(ctx context.Context) (*Stream, error) { return c.Apply(ctx, ApplyRequest{PlanID: "p"}) },
		"open":  func(ctx context.Context) (*Stream, error) { return c.Open(ctx, OpenRequest{Dirs: []string{"/d"}}) },
		"new":   func(ctx context.Context) (*Stream, error) { return c.New(ctx, NewRequest{}) },
	} {
		t.Run(name, func(t *testing.T) {
			finished := filepath.Join(dirOf(c), "finished")
			_ = os.Remove(finished)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s, err := start(ctx)
			if err != nil {
				t.Fatal(err)
			}
			waitEvent(t, s, func(value any) bool { _, ok := value.(StartEvent); return ok })
			cancel()
			collect(t, s)
			r := s.Wait()
			if r.Signal != "" || r.End == nil || !fileExists(finished) {
				t.Fatalf("%+v", r)
			}
		})
	}
}

func TestATermBeforeTheStepsGivesAStoppedResult(t *testing.T) {
	c := child(t, `echo '{"v":1,"event":"stage","stage":"git"}'
sleep 30 &
wait`)
	s, err := c.Apply(context.Background(), ApplyRequest{PlanID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-s.pid, syscall.SIGKILL) })
	waitEvent(t, s, isStage)
	s.Stop()
	r := s.Wait()
	if !r.Stopped || !errors.Is(r.Err, context.Canceled) || r.Signal != "terminated" {
		t.Fatalf("%+v", r)
	}
}

func TestCloseOfAnApplySendsNoSignalAndDropsTheEvents(t *testing.T) {
	c := child(t, stepsScript)
	s, err := c.Apply(context.Background(), ApplyRequest{PlanID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	waitEvent(t, s, isRunning)
	s.Close()
	r := s.Wait()
	if r.Err != nil || r.End == nil || r.End.Cancelled || r.End.OK != 2 {
		t.Fatalf("%+v", r)
	}
	if !fileExists(filepath.Join(dirOf(c), "step-two")) {
		t.Fatal("the steps did not all run")
	}
	waitFor(t, "the channel closes", func() bool {
		select {
		case _, open := <-s.Events():
			return !open
		default:
			return false
		}
	})
}

func TestASlowReaderNeverHoldsUpTheChild(t *testing.T) {
	const count = 30000 // more than a pipe holds
	c := child(t, fmt.Sprintf(`i=0; while [ $i -lt %d ]; do echo '{"v":1,"event":"output","id":"s1","line":"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"}'; i=$((i+1)); done
echo '{"v":1,"event":"end","ok":1,"failed":0,"skipped":0,"disk_refresh":false,"cancelled":false}'`, count))
	s, err := c.Apply(context.Background(), ApplyRequest{PlanID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.Done():
	case <-time.After(60 * time.Second):
		t.Fatal("the child waited for the reader")
	}
	values := collect(t, s)
	if len(values) != count+1 {
		t.Fatalf("%d events", len(values))
	}
	if _, ok := values[count].(EndEvent); !ok {
		t.Fatalf("last: %#v", values[count])
	}
}

// ---- generations

func TestGenerations(t *testing.T) {
	c := child(t, "sleep 30")
	background := context.Background()
	full1, err := c.List(background, ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	stats1, err := c.List(background, ListOptions{Only: []string{StageStats}})
	if err != nil {
		t.Fatal(err)
	}
	if full1.Gen() != 1 || stats1.Gen() != 1 {
		t.Fatalf("gens %d, %d", full1.Gen(), stats1.Gen())
	}
	select {
	case <-full1.Done():
		t.Fatal("a stats list stopped the full list")
	case <-time.After(50 * time.Millisecond):
	}
	// the same Only replaces it, and the generation stays
	stats2, err := c.List(background, ListOptions{Only: []string{StageStats}})
	if err != nil {
		t.Fatal(err)
	}
	if r := stats1.Wait(); !errors.Is(r.Err, ErrSuperseded) || !r.Stopped {
		t.Fatalf("%+v", r)
	}
	if stats2.Gen() != 1 {
		t.Fatalf("gen %d", stats2.Gen())
	}
	// another Only does not
	disk, err := c.List(background, ListOptions{Only: []string{StageDisk}})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-stats2.Done():
		t.Fatal("a disk list stopped the stats list")
	case <-time.After(50 * time.Millisecond):
	}
	// a full list replaces all
	full2, err := c.List(background, ListOptions{Global: true})
	if err != nil {
		t.Fatal(err)
	}
	if full2.Gen() != 2 {
		t.Fatalf("gen %d", full2.Gen())
	}
	for _, old := range []*Stream{full1, stats2, disk} {
		if r := old.Wait(); !errors.Is(r.Err, ErrSuperseded) {
			t.Fatalf("%+v", r)
		}
		waitFor(t, "the old group is gone", func() bool { return groupGone(old.pid) })
	}
	full2.Stop()
	if r := full2.Wait(); !errors.Is(r.Err, context.Canceled) {
		t.Fatalf("%+v", r)
	}
	c.lists.mu.Lock()
	defer c.lists.mu.Unlock()
	if len(c.lists.running) != 0 {
		t.Fatalf("%d lists are still registered", len(c.lists.running))
	}
}

func TestAListThatDoesNotStartLeavesNothingRegistered(t *testing.T) {
	c := NewExecClient(Config{Path: filepath.Join(t.TempDir(), "nosuch")})
	_, err := c.List(context.Background(), ListOptions{})
	var startErr *StartError
	if !errors.As(err, &startErr) {
		t.Fatalf("%v", err)
	}
	if len(c.lists.running) != 0 {
		t.Fatal("a list is still registered")
	}
	if _, err := c.Version(context.Background()); !errors.As(err, &startErr) {
		t.Fatalf("%v", err)
	}
}

// ---- documents

func TestVersion(t *testing.T) {
	cases := []struct {
		name  string
		body  string
		check func(error) bool
	}{
		{"this contract", `echo '{"v":1,"script":"0.4.0","contract":1}'`, isNil},
		{"a newer contract", `echo '{"v":1,"script":"9.0.0","contract":2}'`, is[*ContractError]},
		{"a newer document", `echo '{"v":2,"script":"9.0.0","contract":2}'`, is[*ContractError]},
		{"an old script", `echo 'wtree: unknown option core'; exit 2`, is[*ChildDiedError]},
		{"not json", `echo 'hello'`, is[*ProtocolError]},
		{"no output", `exit 0`, is[*ProtocolError]},
		{"no output, exit 1", `exit 1`, is[*ExitError]},
		{"killed", `kill -9 $$`, is[*ChildDiedError]},
		{"an error", `echo '{"v":1,"event":"error","code":"internal","message":"m"}'; exit 1`, is[*CoreError]},
	}
	for _, kase := range cases {
		t.Run(kase.name, func(t *testing.T) {
			v, err := child(t, kase.body).Version(context.Background())
			if !kase.check(err) {
				t.Fatalf("%#v", err)
			}
			if err == nil && (v.Script != "0.4.0" || v.Contract != ContractVersion) {
				t.Fatalf("%+v", v)
			}
			var contract *ContractError
			if errors.As(err, &contract) && v.Script != "9.0.0" {
				t.Fatalf("the version must come with the error: %+v", v)
			}
		})
	}
}

func TestVersionHasATimeLimit(t *testing.T) {
	c := child(t, "sleep 30")
	c.config.VersionTimeout = 100 * time.Millisecond
	started := time.Now()
	_, err := c.Version(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("%v", err)
	}
	if took := time.Since(started); took > 5*time.Second {
		t.Fatalf("it took %v", took)
	}
}

func TestPlan(t *testing.T) {
	abs, _ := filepath.Abs("testdata/plan-delete-worktrees.json")
	plan, err := child(t, "cat "+abs).Plan(context.Background(), PlanRequest{Action: ActionDeleteWorktrees, Targets: []string{"/w"}})
	if err != nil {
		t.Fatal(err)
	}
	if plan.PlanID == "" || len(plan.Choices) != 3 || plan.Confirm.Kind != ConfirmKey {
		t.Fatalf("%+v", plan)
	}
	if plan.Choices[0].ID != "worktrees" || !plan.Choices[0].Default {
		t.Fatalf("%+v", plan.Choices[0])
	}
}

func TestPlanFailures(t *testing.T) {
	cases := []struct {
		name  string
		body  string
		check func(error) bool
	}{
		{"usage", `echo '{"v":1,"event":"error","code":"usage","message":"m"}'; exit 2`, is[*CoreError]},
		{"not json at exit 0", `echo oops`, is[*ProtocolError]},
		{"no plan id", `echo '{"v":1}'`, is[*ProtocolError]},
		{"another contract", `echo '{"v":2,"plan_id":"x"}'`, is[*ContractError]},
		{"cut and killed", `printf '{"v":1,"plan_id":"x","ite'; kill -9 $$`, is[*ChildDiedError]},
		{"cut at exit 0", `printf '{"v":1,"plan_id":"x","ite'`, is[*ProtocolError]},
	}
	for _, kase := range cases {
		t.Run(kase.name, func(t *testing.T) {
			_, err := child(t, kase.body).Plan(context.Background(), PlanRequest{Action: ActionOpen, Targets: []string{"/d"}})
			if !kase.check(err) {
				t.Fatalf("%#v", err)
			}
		})
	}
}

func TestPlanCancelEndsTheCommandAndItsChildren(t *testing.T) {
	c := child(t, `echo $$ >pid
sleep 30 &
sleep 30`)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := c.Plan(ctx, PlanRequest{Action: ActionOpen, Targets: []string{"/d"}})
		result <- err
	}()
	pidFile := filepath.Join(dirOf(c), "pid")
	waitFor(t, "the command started", func() bool { return fileExists(pidFile) })
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("%v", err)
		}
	case <-time.After(lineWait):
		t.Fatal("the plan did not stop")
	}
	data, _ := os.ReadFile(pidFile)
	var pid int
	if _, err := fmt.Sscan(string(data), &pid); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the group is gone", func() bool { return groupGone(pid) })
}
