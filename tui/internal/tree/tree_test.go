package tree

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/afruth/wtree/tui/internal/core"
	"github.com/afruth/wtree/tui/internal/tree/treetest"
	"github.com/afruth/wtree/tui/internal/treetable"
)

const (
	loginID = "worktree:/home/dev/dev/worktrees/feat-login"
	mainID  = "worktree:/home/dev/dev/foundry/main"
	raceID  = "worktree:/home/dev/dev/worktrees/fix-race"
	hotID   = "worktree:/home/dev/dev/worktrees/hotfix-1"
	pvID    = "worktree:/home/dev/dev/worktrees/pricing-v2"
	apiID   = "stack:feat-login-api"
	e2eID   = "stack:feat-login-e2e"
	spikeID = "stack:old-spike"
	demoID  = "stack:foundry-demo"
)

func newState(scope Scope) *State {
	return New(Options{Scope: scope, Home: treetest.Home, Now: func() time.Time { return treetest.Now }})
}

func feed(s *State, events []any) {
	for _, ev := range events {
		s.Apply(ev)
	}
}

func full(scope Scope, set treetest.Set) *State {
	s := newState(scope)
	feed(s, set.Events())
	return s
}

func rowOf(t *testing.T, s *State, id string) treetable.Row {
	t.Helper()
	for _, r := range s.Visible() {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("no row %s in %v", id, ids(s))
	return treetable.Row{}
}

func ids(s *State) []string {
	var out []string
	for _, r := range s.Visible() {
		out = append(out, r.ID)
	}
	return out
}

func names(s *State) []string {
	var out []string
	for _, r := range s.Visible() {
		if r.Kind != treetable.KindSeparator {
			out = append(out, r.Name)
		}
	}
	return out
}

func join(v []string) string { return strings.Join(v, " ") }

func TestStructureAloneGivesRowsWithLoadingCells(t *testing.T) {
	set := treetest.Repo()
	s := newState(ScopeRepo)
	if s.StructureDone() {
		t.Fatal("not done yet")
	}
	// Mid-structure: rows appear as they arrive.
	s.Apply(core.StageEvent{Stage: core.StageStructure})
	s.Apply(core.RepoEvent{Repo: set.Repos[0]})
	e := set.Structure()
	s.Apply(e[2])
	if len(ids(s)) < 2 {
		t.Fatalf("a worktree shows at once: %v", ids(s))
	}
	feed(s, set.Structure())
	if !s.StructureDone() {
		t.Fatal("structure is done")
	}
	r := rowOf(t, s, loginID)
	if r.Git.State != treetable.CellLoading || r.Age.State != treetable.CellLoading {
		t.Errorf("git stage pending: %+v", r)
	}
	if r.CPU.State != treetable.CellLoading || r.Mem.State != treetable.CellLoading || r.Disk.State != treetable.CellLoading {
		t.Errorf("a running stack loads: %+v", r)
	}
	if got := rowOf(t, s, e2eID); got.CPU.State != treetable.CellNA {
		t.Errorf("what does not run is not applicable: %+v", got.CPU)
	}
	if got := rowOf(t, s, raceID); got.Disk.State != treetable.CellNA || got.CPU.State != treetable.CellNA {
		t.Errorf("no stacks: not applicable: %+v", got)
	}

	feed(s, set.Git())
	r = rowOf(t, s, loginID)
	if r.Git.State != treetable.CellValue || r.Git.Changed != 4 || r.Age.Text != "2h" {
		t.Errorf("git: %+v", r)
	}
	if r.CPU.State != treetable.CellLoading {
		t.Errorf("stats still pending")
	}

	feed(s, set.Stats())
	r = rowOf(t, s, loginID)
	if r.CPU.Text != "4.1%" || r.Mem.Text != "612M" {
		t.Errorf("stats roll up: %+v", r)
	}
	if r.Disk.State != treetable.CellLoading {
		t.Errorf("disk still pending: %+v", r.Disk)
	}

	feed(s, set.DiskStage())
	if r = rowOf(t, s, loginID); r.Disk.Text != "1.2G" {
		t.Errorf("disk rolls up: %+v", r.Disk)
	}
}

func TestRollups(t *testing.T) {
	s := full(ScopeRepo, treetest.Repo())
	r := rowOf(t, s, loginID)
	if r.Count != "1/3" || r.Run != treetable.RunPartial {
		t.Errorf("count: %s %d", r.Count, r.Run)
	}
	if r.CPU.Text != "4.1%" || r.Mem.Text != "612M" || r.Disk.Text != "1.2G" {
		t.Errorf("sums: %+v", r)
	}
	if m := rowOf(t, s, mainID); m.Count != "1/1" || m.Run != treetable.RunUp {
		t.Errorf("main: %+v", m)
	}
	if g := rowOf(t, s, GroupCleanup); g.Disk.Text != "5.6G" || g.CPU.State != treetable.CellNA {
		t.Errorf("group: %+v", g)
	}
	if h := rowOf(t, s, raceID); h.Count != "" {
		t.Errorf("no stacks, no count: %q", h.Count)
	}
}

func TestRollupStatesAndStale(t *testing.T) {
	if got := rollup([]treetable.Cell{treetable.Value("1"), treetable.Failed()}, "x"); got.State != treetable.CellFailed {
		t.Errorf("failed wins: %+v", got)
	}
	if got := rollup([]treetable.Cell{treetable.Value("1"), treetable.Loading()}, "x"); got.State != treetable.CellLoading {
		t.Errorf("loading: %+v", got)
	}
	if got := rollup([]treetable.Cell{treetable.Value("1"), treetable.Stale("2")}, "x"); got.State != treetable.CellStale || got.Text != "x" {
		t.Errorf("stale: %+v", got)
	}
	if got := rollup([]treetable.Cell{treetable.NA(), treetable.NA()}, "x"); got.State != treetable.CellNA {
		t.Errorf("all na: %+v", got)
	}
	if got := rollup([]treetable.Cell{treetable.NA(), treetable.Value("1")}, "x"); got.State != treetable.CellValue {
		t.Errorf("na adds nothing: %+v", got)
	}

	// A stats round with no number for a stack keeps the old one as stale.
	s := full(ScopeRepo, treetest.Repo())
	s.Apply(core.StageEvent{Stage: core.StageStats})
	s.Apply(core.StageEvent{Stage: core.StageStats, Done: true})
	if r := rowOf(t, s, apiID); r.CPU.State != treetable.CellStale || r.CPU.Text != "4.1%" {
		t.Errorf("stale stats: %+v", r.CPU)
	}
	if r := rowOf(t, s, loginID); r.CPU.State != treetable.CellStale {
		t.Errorf("stale rolls up: %+v", r.CPU)
	}

	// With no number ever, it is failed.
	s = newState(ScopeRepo)
	set := treetest.Repo()
	feed(s, set.Structure())
	s.Apply(core.StageEvent{Stage: core.StageStats})
	s.Apply(core.Warning{Code: core.WarnStatsUnavailable, Message: "x"})
	s.Apply(core.StageEvent{Stage: core.StageStats, Done: true})
	if r := rowOf(t, s, apiID); r.CPU.State != treetable.CellFailed {
		t.Errorf("failed stats: %+v", r.CPU)
	}
	if len(s.Warnings()) != 1 {
		t.Errorf("warnings: %v", s.Warnings())
	}
}

func TestDiskCellStates(t *testing.T) {
	set := treetest.Repo()
	s := newState(ScopeRepo)
	feed(s, set.Structure())
	age := int64(40 * 60)
	s.Apply(core.StageEvent{Stage: core.StageDisk})
	s.Apply(core.StackDisk{Project: "feat-login-api", Bytes: 840e6, AgeS: &age, Refreshing: true})
	s.Apply(core.DiskTotals{AgeS: &age, Refreshing: true})
	s.Apply(core.StageEvent{Stage: core.StageDisk, Done: true})
	if r := rowOf(t, s, apiID); r.Disk.State != treetable.CellStale || r.Disk.Text != "840M" {
		t.Errorf("an old cache is stale: %+v", r.Disk)
	}
	if r := rowOf(t, s, e2eID); r.Disk.State != treetable.CellNA {
		t.Errorf("no size in a cache that exists: %+v", r.Disk)
	}

	// Before the first scan, all sizes are null: never show them as zero.
	s = newState(ScopeRepo)
	feed(s, set.Structure())
	s.Apply(core.StageEvent{Stage: core.StageDisk})
	s.Apply(core.DiskTotals{Refreshing: true})
	s.Apply(core.StageEvent{Stage: core.StageDisk, Done: true})
	if r := rowOf(t, s, apiID); r.Disk.State != treetable.CellLoading {
		t.Errorf("first scan running: %+v", r.Disk)
	}
	s.Apply(core.DiskTotals{})
	if r := rowOf(t, s, apiID); r.Disk.State != treetable.CellFailed {
		t.Errorf("no scan and none running: %+v", r.Disk)
	}

	s = full(ScopeRepo, set)
	s.InvalidateDisk([]string{"feat-login-api"})
	if r := rowOf(t, s, apiID); r.Disk.State != treetable.CellLoading {
		t.Errorf("after a delete: %+v", r.Disk)
	}
	if r := rowOf(t, s, loginID); r.Disk.State != treetable.CellLoading {
		t.Errorf("rolls up: %+v", r.Disk)
	}
}

func TestDefaultOrderRepoScope(t *testing.T) {
	s := full(ScopeRepo, treetest.Repo())
	want := "feat-login feat-login-api feat-login-e2e main fix-race pricing-v2 hotfix-1 cleanup old-spike foundry-demo"
	if got := join(names(s)); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	vis := s.Visible()
	if vis[0].Marker != treetable.MarkerCurrent {
		t.Errorf("first row is @: %+v", vis[0])
	}
	if r := rowOf(t, s, mainID); r.Marker != treetable.MarkerMain {
		t.Errorf("main is ⌂")
	}
}

func TestDefaultOrderMachineScope(t *testing.T) {
	s := full(ScopeMachine, treetest.Machine())
	want := []string{"foundry/feat-login", "foundry-feat-login-api", "foundry-feat-login-e2e", "foundry/main", "foundry/fix-race", "metycle/foundry", "metycle/pricing-v2", "wtree/main", "wtree/tui", "cleanup", "foundry-old-spike", "metycle-demo", "hotfix-1"}
	if got := names(s); join(got) != join(want) {
		t.Errorf("got  %s\nwant %s", join(got), join(want))
	}
	if r := rowOf(t, s, "worktree:/home/dev/dev/metycle/foundry"); r.Marker != treetable.MarkerNone || !r.Expandable {
		t.Errorf("the main of another repo has a chevron: %+v", r)
	}
	hot := rowOf(t, s, "worktree:/home/dev/dev/wt/hotfix-1")
	if hot.Prefix != "foundry" || hot.Reason != "merged into main" || hot.Git.Merged || hot.Expandable {
		t.Errorf("cleanup worktree: %+v", hot)
	}
}

func TestPositionsStableUnderLiveUpdates(t *testing.T) {
	set := treetest.Repo()
	s := full(ScopeRepo, set)
	before := join(ids(s))

	// A stats tick, git and new commit facts: cells change, and no row moves.
	s.Apply(core.StageEvent{Stage: core.StageStats})
	newer := treetest.Now.Unix()
	s.Apply(core.StackAge{Project: "pricing-v2", LastStartedAt: &newer, Running: true})
	s.Apply(core.StackStats{Project: "pricing-v2", CPUPct: 99, MemBytes: 1})
	s.Apply(core.StageEvent{Stage: core.StageStats, Done: true})
	s.Apply(core.WorktreeGit{Path: "/home/dev/dev/worktrees/hotfix-1", LastCommitAt: &newer})
	if got := join(ids(s)); got != before {
		t.Errorf("a live update moved rows\n got %s\nwant %s", got, before)
	}
	if r := rowOf(t, s, pvID); r.CPU.Text != "99" && r.CPU.Text != "99.0%" {
		t.Errorf("the cell changes: %+v", r.CPU)
	}

	// A new worktree goes after the others of its zone, and moves nothing.
	nw := core.Worktree{Path: "/home/dev/dev/worktrees/new", Common: set.Repos[0].Common, Repo: "foundry", Branch: strptr("new")}
	s.Apply(core.WorktreeEvent{Worktree: nw})
	got := names(s)
	if join(got) != "feat-login feat-login-api feat-login-e2e main fix-race pricing-v2 hotfix-1 new cleanup old-spike foundry-demo" {
		t.Errorf("new row: %s", join(got))
	}

	// `r` orders again.
	s.Resort()
	if got := names(s); got[6] != "pricing-v2" && got[3] != "main" {
		t.Errorf("resort: %v", got)
	}

	// So does a reload that the user asked for.
	s = full(ScopeRepo, set)
	s.BeginReload()
	feed(s, set.Structure())
	s.Apply(core.StageEvent{Stage: core.StageStats})
	s.Apply(core.StackAge{Project: "pricing-v2", LastStartedAt: &newer, Running: true})
	s.Apply(core.StageEvent{Stage: core.StageStats, Done: true})
	if got := names(s); got[4] != "pricing-v2" {
		t.Errorf("reload orders by the new activity: %v", got)
	}
}

func strptr(s string) *string { return &s }

func TestReloadRemovesWhatIsGone(t *testing.T) {
	set := treetest.Repo()
	s := full(ScopeRepo, set)
	s.ToggleMark(spikeID)

	smaller := set
	smaller.Stacks = set.Stacks[:len(set.Stacks)-1]
	smaller.Trees = set.Trees[:len(set.Trees)-1]
	feed(s, smaller.Structure())
	if _, ok := s.Lookup(demoID); ok {
		t.Errorf("a stack that left is gone")
	}
	if _, ok := s.Lookup(hotID); ok {
		t.Errorf("a worktree that left is gone")
	}
	if !s.IsMarked(spikeID) {
		t.Errorf("marks stay")
	}

	// An engine that does not answer says nothing about stacks.
	s = full(ScopeRepo, set)
	s.Apply(core.StageEvent{Stage: core.StageStructure})
	s.Apply(core.Warning{Code: core.WarnDockerTimeout, Message: "x"})
	for _, ev := range set.Structure() {
		if _, isStack := ev.(core.StackEvent); !isStack {
			if st, isStage := ev.(core.StageEvent); !isStage || st.Done {
				s.Apply(ev)
			}
		}
	}
	if _, ok := s.Lookup(apiID); !ok {
		t.Errorf("docker timed out: the stacks must stay")
	}
}

func TestDockerMissingStillShowsWorktrees(t *testing.T) {
	set := treetest.Repo()
	set.Stacks = nil
	s := newState(ScopeRepo)
	s.Apply(core.Warning{Code: core.WarnDockerMissing, Message: "x"})
	feed(s, set.Events())
	if r := rowOf(t, s, loginID); r.Count != "" || r.CPU.State != treetable.CellNA {
		t.Errorf("no docker: %+v", r)
	}
}

func TestExpansion(t *testing.T) {
	s := full(ScopeRepo, treetest.Repo())
	// Two stacks: open by default. One: closed. Stack: closed until asked.
	if !rowOf(t, s, loginID).Expanded || !s.IsExpanded(loginID) {
		t.Errorf("two stacks open")
	}
	if rowOf(t, s, mainID).Expanded {
		t.Errorf("one stack closed")
	}
	got := ids(s)
	if strings.Contains(join(got), "container:") {
		t.Errorf("containers only on an explicit expand: %v", got)
	}
	s.Toggle(apiID)
	if !strings.Contains(join(ids(s)), "container:feat-login-api/x-api-1") {
		t.Errorf("containers: %v", ids(s))
	}
	s.Toggle(loginID)
	if strings.Contains(join(ids(s)), apiID) {
		t.Errorf("collapse hides stacks")
	}
	s.Toggle(mainID)
	if !strings.Contains(join(ids(s)), "stack:foundry-main") {
		t.Errorf("expanding a one-stack worktree shows its stack: %v", ids(s))
	}
	// A worktree with no stacks has nothing to open: no chevron, and no placeholder child.
	if r := rowOf(t, s, raceID); r.Expandable {
		t.Errorf("a worktree with no stacks has a chevron: %+v", r)
	}
	before := len(s.Visible())
	s.Toggle(raceID)
	if got := len(s.Visible()); got != before {
		t.Errorf("toggling it added a child row: %d, was %d: %v", got, before, names(s))
	}
	saved := s.ExpandedState()
	other := full(ScopeRepo, treetest.Repo())
	other.RestoreExpanded(saved)
	if join(ids(other)) != join(ids(s)) {
		t.Errorf("restore: %v\n%v", ids(other), ids(s))
	}
	s.SetAllExpanded(false)
	for _, r := range s.Visible() {
		if r.Kind == treetable.KindStack && r.ID != spikeID && r.ID != demoID {
			t.Errorf("collapse all left %s", r.ID)
		}
	}
	s.SetAllExpanded(true)
	if !strings.Contains(join(ids(s)), "container:") {
		t.Errorf("expand all")
	}
}

func TestParseFilter(t *testing.T) {
	cases := []struct {
		q       string
		invalid bool
		err     string
		active  bool
	}{
		{"", false, "", false},
		{"   ", false, "", false},
		{"foo", false, "", true},
		{"is:main", false, "", true},
		{"-is:main", false, "", true},
		{"state:running", false, "", true},
		{"repo:foundry branch:feat", false, "", true},
		{`"a:b"`, false, "", true},
		{`-"a b"`, false, "", true},
		{`repo:"my repo"`, false, "", true},
		{"foo:", true, "unknown filter: foo:", true},
		{"foo:bar", true, "unknown filter: foo:", true},
		{"-foo:bar", true, "unknown filter: foo:", true},
		{"is:nope", true, "unknown filter: is:nope", true},
		{"-is:nope", true, "unknown filter: is:nope", true},
		{"state:nope", true, "unknown filter: state:nope", true},
		{"repo:", true, "unknown filter: repo:", true},
		{"branch:", true, "unknown filter: branch:", true},
		{"disk:>1g", true, "unknown filter: disk:", true},
		{"age:>7d", true, "unknown filter: age:", true},
		{"sort:disk", true, "unknown filter: sort:", true},
		{"good nope:x", true, "unknown filter: nope:", true},
		{"-", true, "unknown filter: -", true},
		{"\x1b[31mx:", true, "unknown filter: x:", true},
	}
	for _, c := range cases {
		f := ParseFilter(c.q)
		if f.Invalid() != c.invalid || f.Error() != c.err || f.Active() != c.active {
			t.Errorf("%q: invalid %v err %q active %v", c.q, f.Invalid(), f.Error(), f.Active())
		}
		if f.String() != c.q {
			t.Errorf("%q: String %q", c.q, f.String())
		}
	}
}

func TestFilterRows(t *testing.T) {
	s := full(ScopeRepo, treetest.Repo())

	check := func(q, want, dim string) {
		t.Helper()
		s.SetFilter(q)
		var shown, dimmed []string
		for _, r := range s.Visible() {
			if r.Kind == treetable.KindSeparator {
				continue
			}
			shown = append(shown, r.Name)
			if r.Dim {
				dimmed = append(dimmed, r.Name)
			}
		}
		if join(shown) != want || join(dimmed) != dim {
			t.Errorf("%q\n shown %q\n  want %q\n  dim %q\n want %q", q, join(shown), want, join(dimmed), dim)
		}
	}
	// A match on a stack keeps its worktree, dimmed, as context.
	check("FEAT-LOGIN-API", "feat-login feat-login-api", "feat-login")
	// A match that a collapsed parent has too stays folded under it.
	check("pricing", "pricing-v2", "")
	// AND, negation, quotes.
	check("feat-login -e2e", "feat-login feat-login-api", "")
	check(`"feat-login-e2e"`, "feat-login feat-login-e2e", "feat-login")
	check("nothing-like-this", "", "")
	// Reason text is searchable.
	check("gone worktree", "cleanup old-spike", "cleanup")
	check(`"worktree gone"`, "cleanup old-spike", "cleanup")
	check(`"gone worktree"`, "", "")
	// Field filters.
	check("is:current", "feat-login", "")
	check("is:main", "main", "")
	check("is:merged", "hotfix-1", "")
	check("is:changed", "feat-login pricing-v2", "")
	check("is:orphan", "cleanup old-spike foundry-demo", "cleanup")
	check("is:stale", "cleanup old-spike foundry-demo", "cleanup")
	check("state:volumes", "cleanup foundry-demo", "cleanup")
	check("state:partial", "feat-login feat-login-api", "feat-login")
	check("state:none", "fix-race hotfix-1", "")
	// A running container of a partial stack shows, with its stack as context. A container
	// of a stack that matches stays folded in it.
	check("state:running", "feat-login feat-login-api api main foundry-main pricing-v2 pricing-v2", "feat-login feat-login-api main pricing-v2")
	// A stack inherits repo: and branch:, and shows where its worktree is open.
	check("repo:found", "feat-login feat-login-api feat-login-e2e main fix-race pricing-v2 hotfix-1", "")
	check("branch:feat/", "feat-login feat-login-api feat-login-e2e pricing-v2", "")
	check("-is:main is:current", "feat-login", "")
	check("is:marked", "", "")
	s.ToggleMark(raceID)
	check("is:marked", "fix-race", "")
	// Invalid: nothing, negated or not.
	check("nope:x", "", "")
	check("-nope:x", "", "")
	check("is:typo", "", "")
	check("feat nope:", "", "")
	if s.Filter().Error() != "unknown filter: nope:" {
		t.Errorf("error: %q", s.Filter().Error())
	}
	// Containers.
	s.SetFilter("")
	s.SetExpanded(apiID, true)
	check("api", "feat-login feat-login-api api db", "feat-login")
	s.SetFilter("")
	if join(names(s)) == "" {
		t.Errorf("clearing the filter brings it all back")
	}
}

func TestTypoCannotWidenMarkAll(t *testing.T) {
	s := full(ScopeRepo, treetest.Repo())
	for _, q := range []string{"stat:running", "is:runnnig", "-state:typo", "foo:bar baz"} {
		s.SetFilter(q)
		if n := s.MarkAll(); n != 0 {
			t.Errorf("%q marked %d rows", q, n)
		}
	}
	if len(s.Marked()) != 0 {
		t.Errorf("marks: %v", s.Marked())
	}
}

func TestMarkAllTakesWhatMatchesAndNoContext(t *testing.T) {
	s := full(ScopeRepo, treetest.Repo())
	s.SetFilter("feat-login-e2e")
	if n := s.MarkAll(); n != 1 {
		t.Errorf("marked %d: %v", n, s.Marked())
	}
	if !s.IsMarked(e2eID) || s.IsMarked(loginID) {
		t.Errorf("the dim worktree is context and takes no mark: %v", s.Marked())
	}
	s.ClearMarks()
	s.SetFilter("")
	s.MarkAll()
	if s.IsMarked("group:cleanup") {
		t.Errorf("a group takes no mark")
	}
	s.ClearMarks()
	s.SetExpanded(apiID, true)
	s.SetFilter("api")
	s.MarkAll()
	for _, id := range s.Marked() {
		if strings.HasPrefix(id, "container:") {
			t.Errorf("a container takes no mark: %s", id)
		}
	}
}

func TestMarkCounts(t *testing.T) {
	s := full(ScopeRepo, treetest.Repo())
	s.ToggleMark(raceID)
	s.ToggleMark(pvID)
	s.ToggleMark(e2eID)
	if got := s.MarkCounts(); got.Total != 3 || got.HiddenByFilter != 0 || got.OutOfScope != 0 {
		t.Errorf("no filter: %+v", got)
	}
	s.SetFilter("pricing")
	got := s.MarkCounts()
	if got.Total != 3 || got.HiddenByFilter != 2 || join(got.Hidden) != e2eID+" "+raceID {
		t.Errorf("filter: %+v", got)
	}
	// A mark outlives the filter, and a mark under a collapsed row is not hidden by it.
	s.SetFilter("")
	s.SetExpanded(loginID, false)
	if got := s.MarkCounts(); got.HiddenByFilter != 0 {
		t.Errorf("collapsed: %+v", got)
	}
	// Out of scope.
	s.SetScope(ScopeMachine)
	feed(s, treetest.Machine().Events())
	if got := s.MarkCounts(); got.Total != 3 || got.OutOfScope != 1 {
		t.Errorf("scope: %+v, %v", got, s.Marked())
	}
	// Row flag.
	s = full(ScopeRepo, treetest.Repo())
	s.ToggleMark(raceID)
	if !rowOf(t, s, raceID).Marked {
		t.Errorf("row is marked")
	}
	if s.ToggleMark(raceID) {
		t.Errorf("second toggle unmarks")
	}
	s.ToggleMark(raceID)
	s.Unmark(raceID)
	if s.IsMarked(raceID) {
		t.Errorf("unmark")
	}
}

func TestMarkableRows(t *testing.T) {
	set := treetest.Repo()
	s := full(ScopeRepo, set)
	s.SetExpanded(apiID, true)
	for _, r := range s.Visible() {
		want := r.Kind == treetable.KindWorktree || r.Kind == treetable.KindStack
		if r.Markable != want {
			t.Errorf("%s: markable %v", r.ID, r.Markable)
		}
	}
	if s.ToggleMark("container:feat-login-api/x-api-1") || s.ToggleMark(GroupCleanup) || s.ToggleMark("worktree:/nope") {
		t.Errorf("mark on what cannot take one")
	}

	odd := core.Worktree{Path: "/home/dev/odd\tpath", Common: set.Repos[0].Common, Repo: "foundry", Odd: true}
	s.Apply(core.WorktreeEvent{Worktree: odd})
	if s.ToggleMark("worktree:" + odd.Path) {
		t.Errorf("an odd path takes no action")
	}
	bare := core.Worktree{Path: "/home/dev/bare", Common: set.Repos[0].Common, Repo: "foundry", Bare: true}
	s.Apply(core.WorktreeEvent{Worktree: bare})
	if strings.Contains(join(ids(s)), "/home/dev/bare") {
		t.Errorf("a bare entry is hidden")
	}
}

func TestCleanupGroup(t *testing.T) {
	// Always there, also when it is empty.
	set := treetest.Repo()
	set.Stacks = set.Stacks[:4]
	set.Cleanup, set.Totals = nil, core.CleanupTotals{}
	s := newState(ScopeRepo)
	feed(s, set.Structure())
	g := rowOf(t, s, GroupCleanup)
	if g.NameNote != "" || g.Disk.State != treetable.CellLoading {
		t.Errorf("cleanup stage pending: %+v", g)
	}
	feed(s, set.Events()[len(set.Structure()):])
	g = rowOf(t, s, GroupCleanup)
	if g.NameNote != "nothing to clean" || g.Disk.State != treetable.CellBlank || g.Marker != treetable.MarkerCleanup {
		t.Errorf("empty: %+v", g)
	}

	// It is last, with a separator before it.
	vis := s.Visible()
	if vis[len(vis)-1].ID != GroupCleanup || vis[len(vis)-2].Kind != treetable.KindSeparator {
		t.Errorf("order: %v", ids(s))
	}

	// A dirty worktree has a cleanup row and stays in the list; a merged one moves.
	s = newState(ScopeRepo)
	set = treetest.Repo()
	set.Cleanup = append(set.Cleanup,
		core.CleanupRow{ID: pvID, Kind: "worktree", Tier: core.TierNeedsLook, Reasons: []core.Reason{{Code: "dirty", Text: "it has 12 changed files"}}},
		core.CleanupRow{ID: hotID, Kind: "worktree", Tier: core.TierSafe, Reasons: []core.Reason{{Code: "merged", Text: "merged into main"}}},
	)
	feed(s, set.Events())
	if got := names(s); join(got) != "feat-login feat-login-api feat-login-e2e main fix-race pricing-v2 cleanup old-spike foundry-demo hotfix-1" {
		t.Errorf("zones: %s", join(got))
	}
	if r := rowOf(t, s, pvID); r.Reason != "" {
		t.Errorf("dirty stays a normal row: %+v", r)
	}
}

func TestPlainStacksHaveAGroup(t *testing.T) {
	set := treetest.Repo()
	set.Stacks = append(set.Stacks, treetest.ST{Project: "plain-app", Kind: core.StackPlain, States: map[string]int{"running": 1}, Mem: 1 << 20, CPU: 1, Disk: 1e6, Started: 60})
	s := full(ScopeMachine, set)
	vis := names(s)
	if join(vis) != "foundry/feat-login feat-login-api feat-login-e2e foundry/main foundry/fix-race foundry/pricing-v2 foundry/hotfix-1 no repo plain-app cleanup old-spike foundry-demo" {
		t.Errorf("got %s", join(vis))
	}
}

func TestCleanupView(t *testing.T) {
	s := full(ScopeMachine, treetest.Machine())
	s.SetView(ViewCleanup)
	got := join(names(s))
	if !strings.HasPrefix(got, "safe foundry-old-spike foundry/hotfix-1 needs a look metycle-demo never") {
		t.Errorf("view: %s", got)
	}
	if r := rowOf(t, s, "stack:foundry-old-spike"); r.Reason != "its directory is gone" {
		t.Errorf("reason of the core: %q", r.Reason)
	}
	never := rowOf(t, s, "worktree:/home/dev/dev/foundry/main")
	if !never.Dim || never.Markable {
		t.Errorf("never: dim and no mark: %+v", never)
	}
	if never.Marker != treetable.MarkerMain {
		t.Errorf("marker stays")
	}
	if s.ToggleMark("worktree:/home/dev/dev/foundry/main") {
		t.Errorf("a never row takes no mark")
	}
	if n := s.MarkAll(); n != 2 {
		t.Errorf("A marks safe rows only: %d %v", n, s.Marked())
	}
	if s.IsMarked("stack:metycle-demo") {
		t.Errorf("needs a look is not safe")
	}
	sum := s.Summary()
	if sum.CleanupSafe != 5200e6 || sum.CleanupNeedsLook != 2200e6 || sum.Reclaimable != 7400e6 || !sum.ReclaimableKnown {
		t.Errorf("totals: %+v", sum)
	}
	// Marks outside the view count as hidden.
	s.SetView(ViewTree)
	s.ToggleMark("stack:metycle-foundry")
	s.SetView(ViewCleanup)
	if got := s.MarkCounts(); got.HiddenByFilter != 1 {
		t.Errorf("hidden: %+v", got)
	}

	// Empty.
	set := treetest.Repo()
	set.Stacks = set.Stacks[:4]
	set.Cleanup = []core.CleanupRow{{ID: mainID, Kind: "worktree", Tier: core.TierNever, Reasons: []core.Reason{{Code: "main_checkout", Text: "it is the main checkout"}}}}
	set.Totals = core.CleanupTotals{}
	s = full(ScopeRepo, set)
	s.SetView(ViewCleanup)
	if got := join(names(s)); got != "cleanup never main" {
		t.Errorf("empty view: %s", got)
	}
	if r := rowOf(t, s, GroupCleanup); r.NameNote != "nothing to clean" {
		t.Errorf("note: %+v", r)
	}
}

func TestSort(t *testing.T) {
	s := full(ScopeRepo, treetest.Repo())
	if m := s.CycleSort(); m != SortDisk || m.String() != "disk" {
		t.Errorf("cycle: %v", m)
	}
	want := "old-spike foundry-main pricing-v2 feat-login-api feat-login-e2e foundry-demo"
	if got := join(names(s)); got != want {
		t.Errorf("disk:\n got %s\nwant %s", got, want)
	}
	if r := rowOf(t, s, apiID); r.Prefix != "feat-login" || r.Depth != 0 {
		t.Errorf("prefix: %+v", r)
	}
	if r := rowOf(t, s, spikeID); r.Prefix != "" || r.Reason != "worktree gone" {
		t.Errorf("orphan: %+v", r)
	}
	// Reversed, and what has no number stays last.
	s.SetSort(SortCPU, false)
	if got := names(s); got[0] != "foundry-main" {
		t.Errorf("cpu: %v", got)
	}
	s.SetSort(SortCPU, true)
	if got := names(s); got[0] != "pricing-v2" || got[len(got)-1] == "foundry-main" {
		t.Errorf("cpu reversed: %v", got)
	}

	// Live numbers do not re-order until Resort.
	s.SetSort(SortCPU, false)
	before := join(ids(s))
	s.Apply(core.StageEvent{Stage: core.StageStats})
	s.Apply(core.StackStats{Project: "pricing-v2", CPUPct: 500, MemBytes: 1})
	s.Apply(core.StageEvent{Stage: core.StageStats, Done: true})
	if join(ids(s)) != before {
		t.Errorf("stats moved rows")
	}
	s.Resort()
	if got := names(s); got[0] != "pricing-v2" {
		t.Errorf("resort: %v", got)
	}

	s.SetSort(SortName, false)
	if got := names(s); got[0] != "feat-login-api" {
		t.Errorf("name: %v", got)
	}
	s.SetSort(SortName, true)
	if got := names(s); got[0] != "pricing-v2" {
		t.Errorf("name reversed: %v", got)
	}
	// A filter still applies in a flat list.
	s.SetFilter("branch:feat/login")
	if got := join(names(s)); got != "feat-login-e2e feat-login-api" {
		t.Errorf("flat filter: %s", got)
	}
	s.SetFilter("")
	s.SetSort(SortName, false)
	if s.CycleSort() != SortTree {
		t.Errorf("name cycles to tree")
	}
	if m, rev := s.Sort(); m != SortTree || rev {
		t.Errorf("sort: %v %v", m, rev)
	}
}

func TestTextIsSanitized(t *testing.T) {
	set := treetest.Repo()
	set.Trees[1].Branch = "feat/\x1b[31mred\x1b[0m\x1b]0;pwn\x07"
	set.Stacks[0].Project = "evil\x1b[2Jstack"
	s := full(ScopeRepo, set)
	for _, r := range s.Visible() {
		for _, text := range []string{r.Name, r.Branch, r.Reason} {
			if strings.ContainsAny(text, "\x1b\x07") {
				t.Errorf("control in %q", text)
			}
		}
	}
	if r := rowOf(t, s, loginID); r.Branch != "feat/red" {
		t.Errorf("branch: %q", r.Branch)
	}
	if r := rowOf(t, s, "stack:evil\x1b[2Jstack"); r.Name != "evilstack" {
		t.Errorf("the id stays raw for a target, the name is clean: %q", r.Name)
	}
}

func TestLookup(t *testing.T) {
	s := full(ScopeRepo, treetest.Repo())
	info, ok := s.Lookup(loginID)
	if !ok || info.Kind != treetable.KindWorktree || info.Git == nil || len(info.Stacks) != 2 {
		t.Errorf("worktree: %+v", info)
	}
	info, ok = s.Lookup(apiID)
	if !ok || info.Stack == nil || info.Stats == nil || info.Disk == nil {
		t.Errorf("stack: %+v", info)
	}
	info, ok = s.Lookup("container:feat-login-api/x-api-1")
	if !ok || info.Container == nil || info.Container.Service != "api" || info.Stack == nil {
		t.Errorf("container: %+v", info)
	}
	info, ok = s.Lookup(spikeID)
	if !ok || info.Cleanup == nil || info.Cleanup.Tier != core.TierSafe {
		t.Errorf("cleanup: %+v", info)
	}
	if _, ok = s.Lookup(GroupCleanup); !ok {
		t.Errorf("group")
	}
	for _, id := range []string{"nope", "worktree:/x", "stack:x", "container:x/y", "container:feat-login-api/none"} {
		if _, ok := s.Lookup(id); ok {
			t.Errorf("%s found", id)
		}
	}
}

func TestSummaryAndTabs(t *testing.T) {
	s := full(ScopeRepo, treetest.Repo())
	sum := s.Summary()
	if sum.Worktrees != 5 || sum.Stacks != 6 || sum.Reclaimable != 5580e6 || sum.StatsAt.IsZero() || sum.DiskAgeS == nil || *sum.DiskAgeS != 18*60 {
		t.Errorf("summary: %+v", sum)
	}
	s.SetOpenTabs(map[string]string{"/home/dev/dev/worktrees/fix-race": "tab 4"})
	if r := rowOf(t, s, raceID); r.Tab != "tab 4" {
		t.Errorf("tab: %+v", r)
	}
	s.SetFilter("is:open")
	if got := join(names(s)); got != "fix-race" {
		t.Errorf("is:open: %s", got)
	}
}

func TestScopeChangeKeepsChoices(t *testing.T) {
	s := full(ScopeRepo, treetest.Repo())
	s.ToggleMark(raceID)
	s.SetExpanded(apiID, true)
	s.SetFilter("foo")
	s.SetSort(SortDisk, false)
	s.SetScope(ScopeMachine)
	if s.Scope() != ScopeMachine || s.StructureDone() {
		t.Errorf("new scope starts empty")
	}
	if !s.IsMarked(raceID) || s.Filter().String() != "foo" || !s.IsExpanded(apiID) {
		t.Errorf("choices are kept")
	}
	if m, _ := s.Sort(); m != SortDisk {
		t.Errorf("sort kept")
	}
}

func TestApplyDocMatchesStream(t *testing.T) {
	set := treetest.Repo()
	stream := full(ScopeRepo, set)
	var doc core.ListDoc
	for _, ev := range set.Events() {
		switch e := ev.(type) {
		case core.RepoEvent:
			doc.Repos = append(doc.Repos, e.Repo)
		case core.WorktreeEvent:
			doc.Worktrees = append(doc.Worktrees, e.Worktree)
		case core.StackEvent:
			doc.Stacks = append(doc.Stacks, e.Stack)
		case core.WorktreeGit:
			doc.WorktreeGit = append(doc.WorktreeGit, e)
		case core.StackStats:
			doc.StackStats = append(doc.StackStats, e)
		case core.StackAge:
			doc.StackAge = append(doc.StackAge, e)
		case core.StackDisk:
			doc.StackDisk = append(doc.StackDisk, e)
		case core.DiskTotals:
			v := e
			doc.DiskTotals = &v
		case core.CleanupRow:
			doc.Cleanup = append(doc.Cleanup, e)
		case core.CleanupTotals:
			v := e
			doc.CleanupTotals = &v
		}
	}
	s := newState(ScopeRepo)
	s.ApplyDoc(doc)
	a, _ := json.Marshal(stream.Visible())
	b, _ := json.Marshal(s.Visible())
	if string(a) != string(b) {
		t.Errorf("doc differs from stream\n%s\n%s", a, b)
	}
}

func TestContractExamplesDecodeAndApply(t *testing.T) {
	lines := []string{
		`{"v":1,"event":"stage","stage":"structure"}`,
		`{"v":1,"event":"repo","repo":{"common":"/abs/proj/.git","name":"proj","main":"/abs/proj","default_branch":"main"}}`,
		`{"v":1,"event":"worktree","worktree":{"path":"/abs/worktrees/alpha","common":"/abs/proj/.git","repo":"proj","main":false,"branch":"alpha","detached":false,"head":"44b5b6881c2abe459ea1310eee675287b79110d7","missing":false,"bare":false,"locked":"on the \u001b[31musb disk","current":false,"odd":false}}`,
		`{"v":1,"event":"stack","stack":{"project":"proj-alpha","dir":"/abs/worktrees/alpha","dirs":["/abs/worktrees/alpha"],"kind":"live","worktree":"/abs/worktrees/alpha","common":"/abs/proj/.git","worktrees":["/abs/worktrees/alpha"],"states":{"running":1,"exited":1},"containers":[{"id":"845341ef3380","name":"proj-alpha-api-1","service":"api","state":"running","health":"healthy","status":"Up 2 hours (healthy)","ports":"0.0.0.0:8080->80/tcp, 9000/tcp","published":["8080"]}],"volumes":1}}`,
		`{"v":1,"event":"stage","stage":"structure","done":true,"ms":118}`,
		`{"v":1,"event":"worktree_git","path":"/abs/worktrees/alpha","missing":false,"changed":2,"untracked":1,"upstream":"origin/alpha","upstream_gone":false,"ahead":3,"behind":0,"last_commit_at":1793553388,"last_commit_subject":"fix token refresh","index_mtime":1793553388,"merged_into":null,"head_held":true,"unreadable":false,"timed_out":false}`,
		`{"v":1,"event":"stack_stats","project":"proj-alpha","cpu_pct":2.50,"mem_bytes":209715200,"containers":[{"id":"845341ef3380","cpu_pct":2.50,"mem_bytes":209715200}]}`,
		`{"v":1,"event":"engine","ncpu":16,"mem_total":32848117760,"cpu_pct":166.05,"mem_bytes":6851539239}`,
		`{"v":1,"event":"stack_age","project":"proj-alpha","last_started_at":1793397786,"last_finished_at":1793517187,"running":true,"stale":false}`,
		`{"v":1,"event":"stack_disk","project":"proj-alpha","bytes":3000000000,"age_s":1080,"refreshing":false}`,
		`{"v":1,"event":"disk_totals","images":10500000000,"containers":1200000000,"volumes":5000000000,"build":200000000,"age_s":1080,"refreshing":false}`,
		`{"v":1,"event":"cleanup","id":"worktree:/abs/worktrees/alpha","kind":"worktree","label":"alpha","tier":"needs_look","reasons":[{"code":"merged_running","text":"merged into main, and a stack still runs"}],"bytes":3000000000,"stacks":["proj-alpha"]}`,
		`{"v":1,"event":"cleanup_totals","safe":0,"needs_look":3000000000,"never":0,"unknown":true}`,
		`{"v":1,"event":"warning","code":"docker_timeout","message":"Docker did not answer within 5 seconds."}`,
		`{"v":1,"event":"a_new_event_of_a_newer_script","x":1}`,
		`{"v":1,"event":"end","ms":260}`,
	}
	s := New(Options{Now: func() time.Time { return time.Unix(1793560000, 0) }})
	for _, line := range lines {
		ev, err := core.DecodeEvent([]byte(line))
		if err != nil {
			t.Fatalf("%s: %v", line, err)
		}
		s.Apply(ev)
	}
	r := rowOf(t, s, "worktree:/abs/worktrees/alpha")
	if r.Name != "alpha" || r.Branch != "" || r.Git.Ahead != 3 || r.Git.Changed != 2 || !r.Git.Locked || r.Count != "1/2" || r.CPU.Text != "2.5%" || r.Mem.Text != "200M" || r.Disk.Text != "3.0G" {
		t.Errorf("row: %+v", r)
	}
	s.SetExpanded("worktree:/abs/worktrees/alpha", true)
	s.SetExpanded("stack:proj-alpha", true)
	c := rowOf(t, s, "container:proj-alpha/proj-alpha-api-1")
	if c.Name != "api" || c.Word != "running" || c.Health != "healthy" || c.Age.Text != "2h" || len(c.Ports) != 1 || c.Ports[0] != ":8080" {
		t.Errorf("container: %+v", c)
	}
	if r := rowOf(t, s, "stack:proj-alpha"); r.Age.State != treetable.CellValue {
		t.Errorf("stack age: %+v", r.Age)
	}
}

func TestFormat(t *testing.T) {
	cases := []struct{ got, want string }{
		{FormatDisk(0), "0B"}, {FormatDisk(900), "900B"}, {FormatDisk(1500), "2K"},
		{FormatDisk(840e6), "840M"}, {FormatDisk(999e6), "999M"}, {FormatDisk(999.7e6), "1.0G"},
		{FormatDisk(1000e6), "1.0G"}, {FormatDisk(1.2e9), "1.2G"}, {FormatDisk(9.94e9), "9.9G"},
		{FormatDisk(9.96e9), "10G"}, {FormatDisk(123e9), "123G"}, {FormatDisk(999.9e9), "1.0T"},
		{FormatDisk(1.5e12), "1.5T"}, {FormatDisk(-5), "0B"},
		{FormatMem(612 << 20), "612M"}, {FormatMem(1000 << 20), "1.0G"}, {FormatMem(1 << 30), "1.0G"},
		{FormatMem(209715200), "200M"},
		{FormatCPU(4.1), "4.1%"}, {FormatCPU(12), "12.0%"}, {FormatCPU(0), "0.0%"}, {FormatCPU(99.94), "99.9%"},
		{FormatCPU(99.96), "100%"}, {FormatCPU(173.2), "173%"}, {FormatCPU(1632.1), "1632%"},
		{FormatAge(-5), "0s"}, {FormatAge(45), "45s"}, {FormatAge(600), "10m"}, {FormatAge(7200), "2h"},
		{FormatAge(86400), "1d"}, {FormatAge(12 * 86400), "12d"}, {FormatAge(99 * 86400), "99d"},
		{FormatAge(100 * 86400), "14w"}, {FormatAge(400 * 86400), "57w"}, {FormatAge(800 * 86400), "2y"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
	for _, v := range []int64{0, 5, 999, 1000, 9999, 1e5, 999e6, 1e9, 9.99e9, 99e9, 999e9, 1e12, 99e12, 999e12} {
		for _, text := range []string{FormatDisk(v), FormatMem(v)} {
			if len(text) > 4 {
				t.Errorf("%d: %q is wider than the column", v, text)
			}
		}
	}
	for s := int64(0); s < 10*365*86400; s += 3517 {
		if len(FormatAge(s)) > 3 {
			t.Fatalf("%d: %q is wider than the column", s, FormatAge(s))
		}
	}
}

func TestStatusSeconds(t *testing.T) {
	cases := []struct {
		in   string
		want int64
		ok   bool
	}{
		{"Up 2 hours (healthy)", 7200, true},
		{"Up About an hour", 3600, true},
		{"Up About a minute", 60, true},
		{"Up Less than a second", 0, true},
		{"Up 3 days", 3 * 86400, true},
		{"Up 2 weeks", 14 * 86400, true},
		{"Exited (0) 25 hours ago", 25 * 3600, true},
		{"Exited (137) 3 months ago", 90 * 86400, true},
		{"Created", 0, false},
		{"", 0, false},
		{"Restarting (1) 5 seconds ago", 5, true},
	}
	for _, c := range cases {
		got, ok := statusSeconds(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("%q: %d %v", c.in, got, ok)
		}
	}
}

// In machine scope the main checkout is named as its repo once, and a clean tree says nothing.
func TestNamesRepeatNothing(t *testing.T) {
	set := treetest.Machine()
	for i := range set.Trees {
		if w := &set.Trees[i]; w.Main {
			w.Repo = base(w.Path)
		}
	}
	s := full(ScopeMachine, set)
	for _, r := range s.Visible() {
		if parts := strings.Split(r.Name, "/"); len(parts) == 2 && parts[0] == parts[1] {
			t.Errorf("the name says its repo twice: %q", r.Name)
		}
	}
	for _, r := range full(ScopeRepo, treetest.Repo()).Visible() {
		if r.Branch != "" && r.Branch == r.Name {
			t.Errorf("the branch repeats the name: %+v", r)
		}
	}
}

func TestRowAgeHasAFloor(t *testing.T) {
	for v, want := range map[int64]string{0: "<1m", 9: "<1m", 59: "<1m", -3: "<1m", 60: "1m", 7200: "2h"} {
		if got := FormatRowAge(v); got != want {
			t.Errorf("%d: %q, want %q", v, got, want)
		}
	}
	if FormatAge(9) != "9s" {
		t.Error("the age of live data keeps its seconds")
	}
}
