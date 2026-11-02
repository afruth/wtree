package core

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestFakeDefaultsReplayEachKindOfCall(t *testing.T) {
	ctx := context.Background()
	f := NewFakeClient()
	v, err := f.Version(ctx)
	if err != nil || v.Contract != ContractVersion {
		t.Fatalf("%+v, %v", v, err)
	}
	for _, action := range []string{ActionDeleteWorktrees, ActionDeleteStacks, ActionStopStacks, ActionOpen} {
		plan, err := f.Plan(ctx, PlanRequest{Action: action, Targets: []string{"x"}})
		if err != nil || plan.Action != action {
			t.Fatalf("%s: %+v, %v", action, plan, err)
		}
	}
	if plan, err := f.Plan(ctx, PlanRequest{Action: ActionDeleteStacks, Volumes: true}); err != nil || !plan.Flags.Volumes {
		t.Fatalf("%+v, %v", plan.Flags, err)
	}
	if plan, err := f.Plan(ctx, PlanRequest{Action: ActionDeleteWorktrees, Force: true}); err != nil || !plan.Flags.Force {
		t.Fatalf("%+v, %v", plan.Flags, err)
	}
	starts := map[string]func() (*Stream, error){
		"list":   func() (*Stream, error) { return f.List(ctx, ListOptions{}) },
		"global": func() (*Stream, error) { return f.List(ctx, ListOptions{Global: true}) },
		"only":   func() (*Stream, error) { return f.List(ctx, ListOptions{Only: []string{StageStats}}) },
		"apply":  func() (*Stream, error) { return f.Apply(ctx, ApplyRequest{PlanID: "p"}) },
		"open":   func() (*Stream, error) { return f.Open(ctx, OpenRequest{Dirs: []string{"/d"}}) },
		"new":    func() (*Stream, error) { return f.New(ctx, NewRequest{}) },
		"dry":    func() (*Stream, error) { return f.New(ctx, NewRequest{DryRun: true}) },
	}
	for name, start := range starts {
		s, err := start()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		values := collect(t, s)
		if r := s.Wait(); r.Err != nil || r.Stopped || len(values) == 0 {
			t.Fatalf("%s: %+v, %d events", name, r, len(values))
		}
		for _, value := range values {
			if _, bad := value.(BadLine); bad {
				t.Fatalf("%s: %#v", name, value)
			}
		}
	}
}

func TestFakeRecordsTheCommandLineOfTheRealClient(t *testing.T) {
	f := NewFakeClient()
	request := ApplyRequest{PlanRequest: PlanRequest{Action: ActionDeleteStacks, Volumes: true, Targets: []string{"p", "q"}}, PlanID: "id", Choice: "stacks", Phrases: []string{"2 stacks"}}
	s, err := f.Apply(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	s.Wait()
	calls := f.Calls()
	if len(calls) != 1 || calls[0].Op != "apply:delete-stacks" || strings.Join(calls[0].Args, " ") != strings.Join(applyArgs(request), " ") {
		t.Fatalf("%+v", calls)
	}
}

func TestFakeHasTheExitCodesOfTheRecordings(t *testing.T) {
	cases := []struct {
		fixture string
		exit    int
		check   func(error) bool
	}{
		{"apply-ok.ndjson", 0, isNil},
		{"apply-failed.ndjson", 1, isNil},
		{"apply-cancelled.ndjson", 1, isNil},
		{"apply-refused-stale.ndjson", 4, is[*RefusedError]},
		{"apply-refused-phrase.ndjson", 4, is[*RefusedError]},
		{"error-usage.ndjson", 2, is[*CoreError]},
		{"error-not-a-repo.ndjson", 3, is[*CoreError]},
		{"new-refused-taken.ndjson", 4, is[*RefusedError]},
	}
	for _, c := range cases {
		t.Run(c.fixture, func(t *testing.T) {
			f := NewFakeClient()
			f.On("apply", FakeScript{Fixture: c.fixture})
			s, err := f.Apply(context.Background(), ApplyRequest{PlanID: "p"})
			if err != nil {
				t.Fatal(err)
			}
			collect(t, s)
			r := s.Wait()
			if r.Exit != c.exit || !c.check(r.Err) {
				t.Fatalf("%+v", r)
			}
		})
	}
}

func TestFakeDocumentsHaveTheirFailures(t *testing.T) {
	f := NewFakeClient()
	f.On("version", FakeScript{Data: []byte(`{"v":1,"script":"9","contract":2}`)})
	if _, err := f.Version(context.Background()); !is[*ContractError](err) {
		t.Fatalf("%v", err)
	}
	f.On("plan", FakeScript{Fixture: "error-usage.ndjson"})
	f.On("plan:"+ActionOpen, FakeScript{Fixture: "error-usage.ndjson"})
	_, err := f.Plan(context.Background(), PlanRequest{Action: ActionOpen})
	var coreErr *CoreError
	if !errors.As(err, &coreErr) || coreErr.Exit != ExitUsage {
		t.Fatalf("%v", err)
	}
	f.On("plan:"+ActionOpen, FakeScript{Fixture: "nosuch.json"})
	if _, err := f.Plan(context.Background(), PlanRequest{Action: ActionOpen}); err == nil {
		t.Fatal("a fixture that does not exist must fail")
	}
}

func TestFakeGenerations(t *testing.T) {
	f := NewFakeClient()
	f.On("list", FakeScript{Fixture: "list-stream.ndjson", PauseAfter: 3})
	ctx := context.Background()
	first, err := f.List(ctx, ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	stats, err := f.List(ctx, ListOptions{Only: []string{StageStats}})
	if err != nil {
		t.Fatal(err)
	}
	if first.Gen() != 1 || stats.Gen() != 1 {
		t.Fatalf("gens %d, %d", first.Gen(), stats.Gen())
	}
	collect(t, stats)
	second, err := f.List(ctx, ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if second.Gen() != 2 {
		t.Fatalf("gen %d", second.Gen())
	}
	if r := first.Wait(); !errors.Is(r.Err, ErrSuperseded) || !r.Stopped {
		t.Fatalf("%+v", r)
	}
	second.Stop()
	second.Wait()
}

func TestFakePausesARead(t *testing.T) {
	f := NewFakeClient()
	f.On("list", FakeScript{Fixture: "list-stream.ndjson", PauseAfter: 2})
	s, err := f.List(context.Background(), ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	<-s.Events()
	<-s.Events()
	select {
	case ev := <-s.Events():
		t.Fatalf("an event in the pause: %#v", ev)
	case <-time.After(50 * time.Millisecond):
	}
	f.Release()
	collect(t, s)
	if r := s.Wait(); r.Err != nil || r.End == nil {
		t.Fatalf("%+v", r)
	}
}

func TestFakeCancelOfAReadEndsIt(t *testing.T) {
	f := NewFakeClient()
	f.On("list", FakeScript{Fixture: "list-stream.ndjson", PauseAfter: -1})
	ctx, cancel := context.WithCancel(context.Background())
	s, err := f.List(ctx, ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if values := collect(t, s); len(values) != 0 {
		t.Fatalf("%d events", len(values))
	}
	if r := s.Wait(); !r.Stopped || !errors.Is(r.Err, context.Canceled) {
		t.Fatalf("%+v", r)
	}
}

func TestFakeCancelOfAnApplyPlaysTheRecordedCancel(t *testing.T) {
	f := NewFakeClient()
	f.On("apply", FakeScript{Fixture: "apply-ok.ndjson", OnCancel: "apply-cancelled.ndjson", PauseAfter: 3})
	s, err := f.Apply(context.Background(), ApplyRequest{PlanID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		<-s.Events()
	}
	s.Stop()
	values := collect(t, s)
	want := StepEvent{ID: "s2", State: StepSkipped, Because: "cancelled"}
	if len(values) != 2 || values[0].(StepEvent).Because != want.Because {
		t.Fatalf("%#v", values)
	}
	r := s.Wait()
	if r.Err != nil || r.Stopped || r.Exit != ExitFailure || r.End == nil || !r.End.Cancelled {
		t.Fatalf("%+v", r)
	}
}

func TestFakeChildThatDies(t *testing.T) {
	f := NewFakeClient()
	f.On("apply", FakeScript{Fixture: "apply-ok.ndjson", DieAfter: 2, Truncate: true, Stderr: "boom"})
	s, err := f.Apply(context.Background(), ApplyRequest{PlanID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	values := collect(t, s)
	if len(values) != 3 {
		t.Fatalf("%#v", values)
	}
	if bad, ok := values[2].(BadLine); !ok || !bad.Truncated {
		t.Fatalf("%#v", values[2])
	}
	r := s.Wait()
	var died *ChildDiedError
	if !errors.As(r.Err, &died) || died.Signal != "killed" || died.Partial == "" || died.Stderr != "boom" {
		t.Fatalf("%+v", r)
	}
}

func TestFakeCancelledContextStartsNothing(t *testing.T) {
	f := NewFakeClient()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.Apply(ctx, ApplyRequest{PlanID: "p"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("%v", err)
	}
	if _, err := f.List(ctx, ListOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("%v", err)
	}
	if calls := f.Calls(); len(calls) != 0 {
		t.Fatalf("%+v", calls)
	}
	if len(f.lists.running) != 0 {
		t.Fatal("a list is still registered")
	}
}

func TestFakeNoScript(t *testing.T) {
	f := &FakeClient{files: Fixtures(), release: make(chan struct{}), scripts: map[string]FakeScript{}}
	if _, err := f.Open(context.Background(), OpenRequest{Dirs: []string{"/d"}}); err == nil {
		t.Fatal("no script must fail")
	}
}
