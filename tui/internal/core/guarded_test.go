package core

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

// These tests run the real script, so they can reach its delete and stop paths.
// Only testdata/guarded.sh runs them, behind the guard of test/run.sh. Elsewhere
// they skip themselves before they run anything.

func guardedClient(t *testing.T) *ExecClient {
	t.Helper()
	if os.Getenv("WTREE_TEST_STAGE") != "2" {
		t.Skip("runs only behind the guard: testdata/guarded.sh test")
	}
	scratch := os.Getenv("WTREE_TEST_SCRATCH")
	if !strings.Contains(scratch, "/wtree-test.") {
		t.Fatalf("WTREE_TEST_SCRATCH is %q", scratch)
	}
	for _, name := range []string{"HOME", "XDG_CACHE_HOME", "WTREE_ROOTS", "DOCKER_CONFIG"} {
		if !strings.HasPrefix(os.Getenv(name), scratch+"/") {
			t.Fatalf("%s is not in the scratch directory", name)
		}
	}
	host := strings.TrimPrefix(os.Getenv("DOCKER_HOST"), "unix://")
	if !strings.HasPrefix(host, scratch+"/") {
		t.Fatalf("DOCKER_HOST is %q", host)
	}
	if _, err := os.Stat(host); err == nil {
		t.Fatal("DOCKER_HOST is reachable")
	}
	out, err := exec.Command("docker", "--wtree-shim-id").Output()
	if err != nil || strings.TrimSpace(string(out)) != "wtree-test-docker-shim" {
		t.Fatalf("docker is not the shim: %q, %v", out, err)
	}
	return NewExecClient(Config{Path: os.Getenv("WTREE_TEST_BIN")})
}

func dockerLog(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(os.Getenv("WTREE_SHIM_DIR") + "/docker.log")
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestGuardedListEvents(t *testing.T) {
	c := guardedClient(t)
	ctx := context.Background()
	version, err := c.Version(ctx)
	if err != nil || version.Contract != ContractVersion {
		t.Fatalf("%+v, %v", version, err)
	}
	s, err := c.List(ctx, ListOptions{NoRefresh: true})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for ev := range s.Events() {
		if ev.Gen != 1 {
			t.Fatalf("gen %d", ev.Gen)
		}
		switch ev.Value.(type) {
		case BadLine:
			t.Fatalf("%#v", ev.Value)
		case UnknownEvent:
			t.Fatalf("an event that types.go does not know: %#v", ev.Value)
		case WorktreeEvent:
			seen["worktree"] = true
		case WorktreeGit:
			seen["git"] = true
		case StackEvent:
			seen["stack"] = true
		case CleanupRow:
			seen["cleanup"] = true
		}
	}
	r := s.Wait()
	if r.Err != nil || r.Exit != 0 || r.End == nil {
		t.Fatalf("%+v", r)
	}
	for _, want := range []string{"worktree", "git", "stack", "cleanup"} {
		if !seen[want] {
			t.Errorf("no %s event", want)
		}
	}
}

func TestGuardedListCancelEndsAllChildren(t *testing.T) {
	c := guardedClient(t)
	s, err := c.List(context.Background(), ListOptions{NoRefresh: true})
	if err != nil {
		t.Fatal(err)
	}
	waitEvent(t, s, func(value any) bool { _, ok := value.(WorktreeEvent); return ok })
	// the first read of docker hangs now, in a child of the script
	waitFor(t, "docker is called", func() bool { return strings.Contains(dockerLog(t), "ps -a") })
	started := time.Now()
	s.Stop()
	r := s.Wait()
	if !r.Stopped || !errors.Is(r.Err, context.Canceled) {
		t.Fatalf("%+v", r)
	}
	waitFor(t, "the group is gone", func() bool { return groupGone(s.pid) })
	if took := time.Since(started); took > 4*time.Second {
		t.Fatalf("it took %v", took)
	}
}

// the scan of the disk sizes outlives the list that started it, and the list does not wait for it
func TestGuardedListLeavesTheScanBehind(t *testing.T) {
	c := guardedClient(t)
	s, err := c.List(context.Background(), ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-s.pid, syscall.SIGTERM) })
	started := time.Now()
	collect(t, s)
	if r := s.Wait(); r.Err != nil || r.Stopped {
		t.Fatalf("%+v", r)
	}
	if took := time.Since(started); took > 8*time.Second {
		t.Fatalf("the list waited for the scan: %v", took)
	}
	waitFor(t, "the scan started", func() bool { return strings.Contains(dockerLog(t), "system df") })
	if groupGone(s.pid) {
		t.Fatal("the scan was stopped")
	}
}

func TestGuardedListOutsideARepo(t *testing.T) {
	c := guardedClient(t)
	s, err := c.List(context.Background(), ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	collect(t, s)
	r := s.Wait()
	var coreErr *CoreError
	if !errors.As(r.Err, &coreErr) || coreErr.Code != ErrorNotARepo || r.Exit != ExitEnvironment {
		t.Fatalf("%+v", r)
	}
}

func TestGuardedPlanUsageError(t *testing.T) {
	c := guardedClient(t)
	_, err := c.Plan(context.Background(), PlanRequest{Action: ActionDeleteWorktrees})
	var coreErr *CoreError
	if !errors.As(err, &coreErr) || coreErr.Code != ErrorUsage || coreErr.Exit != ExitUsage {
		t.Fatalf("%v", err)
	}
}

func TestGuardedPlanCancel(t *testing.T) {
	c := guardedClient(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	time.AfterFunc(300*time.Millisecond, cancel)
	started := time.Now()
	_, err := c.Plan(ctx, PlanRequest{Action: ActionDeleteStacks, Targets: []string{"pa", "pb"}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("%v", err)
	}
	if took := time.Since(started); took > 4*time.Second {
		t.Fatalf("it took %v", took)
	}
}

func TestGuardedApplyCancelLetsTheStepFinish(t *testing.T) {
	c := guardedClient(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := PlanRequest{Action: ActionDeleteStacks, Targets: []string{"pa", "pb"}}
	plan, err := c.Plan(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	s, err := c.Apply(ctx, ApplyRequest{PlanRequest: request, PlanID: plan.PlanID, Choice: "stacks"})
	if err != nil {
		t.Fatal(err)
	}
	waitEvent(t, s, isRunning)
	waitFor(t, "the down of pa started", func() bool { return strings.Contains(dockerLog(t), "compose -p pa down") })
	cancel()
	states := map[string]string{}
	because := ""
	for ev := range s.Events() {
		if step, ok := ev.Value.(StepEvent); ok {
			states[step.ID] = step.State
			if step.Because != "" {
				because = step.Because
			}
		}
	}
	r := s.Wait()
	if r.Err != nil || r.Stopped || r.Exit != ExitFailure || r.End == nil || !r.End.Cancelled || r.End.OK != 1 || r.End.Skipped != 1 {
		t.Fatalf("%+v", r)
	}
	if states["s1"] != StepOK || states["s2"] != StepSkipped || because != "cancelled" {
		t.Fatalf("%v, %q", states, because)
	}
}

func TestGuardedApplyStalePlan(t *testing.T) {
	c := guardedClient(t)
	ctx := context.Background()
	worktree := os.Getenv("WP2_WT")
	request := PlanRequest{Action: ActionDeleteWorktrees, Targets: []string{worktree}}
	plan, err := c.Plan(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(worktree+"/new.txt", []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := c.Apply(ctx, ApplyRequest{PlanRequest: request, PlanID: plan.PlanID, Choice: plan.Choices[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	r := s.Wait()
	var refused *RefusedError
	if !errors.As(r.Err, &refused) || refused.Code != RefusedPlanStale || refused.Plan == nil || refused.Plan.PlanID == plan.PlanID || r.Exit != ExitRefused {
		t.Fatalf("%+v", r)
	}
}

func TestGuardedApplyPhrases(t *testing.T) {
	c := guardedClient(t)
	ctx := context.Background()
	request := PlanRequest{Action: ActionDeleteStacks, Volumes: true, Targets: []string{"pa", "pb"}}
	plan, err := c.Plan(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	choice := plan.Choices[0]
	if choice.Confirm.Kind != ConfirmPhrase {
		t.Fatalf("%+v", choice.Confirm)
	}
	// a near miss: exact means exact
	s, err := c.Apply(ctx, ApplyRequest{PlanRequest: request, PlanID: plan.PlanID, Choice: choice.ID, Phrases: []string{strings.ToUpper(choice.Confirm.Phrases[0].Phrase)}})
	if err != nil {
		t.Fatal(err)
	}
	var refused *RefusedError
	if r := s.Wait(); !errors.As(r.Err, &refused) || refused.Code != RefusedPhraseMismatch {
		t.Fatalf("%+v", r)
	}
	if strings.Contains(dockerLog(t), "compose -p") {
		t.Fatal("a refusal ran a step")
	}
	var phrases []string
	for _, phrase := range choice.Confirm.Phrases {
		phrases = append(phrases, phrase.Phrase)
	}
	s, err = c.Apply(ctx, ApplyRequest{PlanRequest: request, PlanID: plan.PlanID, Choice: choice.ID, Phrases: phrases})
	if err != nil {
		t.Fatal(err)
	}
	collect(t, s)
	if r := s.Wait(); r.Err != nil || r.Exit != 0 || r.End == nil || r.End.Failed != 0 {
		t.Fatalf("%+v", r)
	}
}

func TestGuardedNew(t *testing.T) {
	c := guardedClient(t)
	ctx := context.Background()
	s, err := c.New(ctx, NewRequest{DryRun: true, Name: "fresh"})
	if err != nil {
		t.Fatal(err)
	}
	r := s.Wait()
	if r.Err != nil || r.Created == nil || r.Created.Created || r.Created.Name != "fresh" {
		t.Fatalf("%+v", r)
	}
	s, err = c.New(ctx, NewRequest{NoOpen: true, Name: "fresh"})
	if err != nil {
		t.Fatal(err)
	}
	r = s.Wait()
	if r.Err != nil || r.Created == nil || !r.Created.Created {
		t.Fatalf("%+v", r)
	}
	s, err = c.New(ctx, NewRequest{Name: "second"})
	if err != nil {
		t.Fatal(err)
	}
	r = s.Wait()
	if r.Err != nil || r.Created == nil || r.End == nil || r.End.OK != 1 {
		t.Fatalf("%+v", r)
	}
	s, err = c.New(ctx, NewRequest{NoOpen: true, Name: "fresh"})
	if err != nil {
		t.Fatal(err)
	}
	r = s.Wait()
	var refused *RefusedError
	if !errors.As(r.Err, &refused) || refused.Code != RefusedNameTaken {
		t.Fatalf("%+v", r)
	}
}

func TestGuardedOpenFailedStep(t *testing.T) {
	c := guardedClient(t)
	s, err := c.Open(context.Background(), OpenRequest{Dirs: []string{os.Getenv("WP2_WT"), os.Getenv("HOME") + "/nowhere"}})
	if err != nil {
		t.Fatal(err)
	}
	collect(t, s)
	r := s.Wait()
	if r.Err != nil || r.Exit != ExitFailure || r.End == nil || r.End.Failed != 1 || r.End.OK != 1 {
		t.Fatalf("%+v", r)
	}
}
