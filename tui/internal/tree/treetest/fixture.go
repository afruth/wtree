// Package treetest has event fixtures that match the mock-ups of the UX design, for the
// tests of tree, treetable and the model. It runs no command and reads no file.
package treetest

import (
	"time"

	"github.com/afruth/wtree/tui/internal/core"
)

// Home is the home directory of the fixtures, for ~ in paths.
const Home = "/home/dev"

// Now is the clock of the fixtures.
var Now = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

const (
	minute = int64(60)
	hour   = 60 * minute
	day    = 24 * hour
	mib    = int64(1024 * 1024)
	gib    = 1024 * mib
)

func ptr[T any](v T) *T { return &v }

func ago(seconds int64) *int64 { return ptr(Now.Unix() - seconds) }

// WT is a worktree of a fixture with its git facts.
type WT struct {
	Repo, Path, Common string
	Main, Current      bool
	Branch             string // "" is detached
	Head               string
	Ahead, Behind      int
	Changed            int
	CommitAgo          int64
	Merged             string
	Locked             *string
}

func (w WT) events() (core.WorktreeEvent, core.WorktreeGit) {
	wt := core.Worktree{Path: w.Path, Common: w.Common, Repo: w.Repo, Main: w.Main, Current: w.Current, Head: ptr(w.Head), Locked: w.Locked}
	if w.Branch == "" {
		wt.Detached = true
	} else {
		wt.Branch = ptr(w.Branch)
	}
	g := core.WorktreeGit{
		Path: w.Path, Changed: ptr(w.Changed), Untracked: ptr(0), Ahead: ptr(w.Ahead), Behind: ptr(w.Behind),
		LastCommitAt: ago(w.CommitAgo), IndexMtime: ago(w.CommitAgo), HeadHeld: ptr(true),
	}
	if w.Merged != "" {
		g.MergedInto = ptr(w.Merged)
	}
	return core.WorktreeEvent{Worktree: wt}, g
}

// ST is a stack of a fixture with its numbers. Zero numbers mean no event. The e2e stacks
// have no container list, because the mock-ups draw them with no chevron.
type ST struct {
	Project  string
	Worktree string // "" for an orphan
	Kind     string
	States   map[string]int
	Conts    []core.Container
	CPU      float64
	Mem      int64
	ContStat []core.ContainerStats
	Disk     int64
	Started  int64 // seconds ago, for a running stack
	Finished int64 // seconds ago, for a stopped stack
}

func (s ST) events() []any {
	st := core.Stack{Project: s.Project, Kind: s.Kind, States: s.States, Containers: s.Conts, Volumes: ptr(0)}
	if s.Worktree != "" {
		st.Worktree, st.Dir = ptr(s.Worktree), ptr(s.Worktree)
		st.Dirs, st.Worktrees = []string{s.Worktree}, []string{s.Worktree}
	}
	return []any{core.StackEvent{Stack: st}}
}

func (s ST) stats() []any {
	var out []any
	if s.Mem > 0 {
		out = append(out, core.StackStats{Project: s.Project, CPUPct: s.CPU, MemBytes: s.Mem, Containers: s.ContStat})
	}
	age := core.StackAge{Project: s.Project}
	switch {
	case s.Started > 0:
		age.Running, age.LastStartedAt = true, ago(s.Started)
		out = append(out, age)
	case s.Finished > 0:
		age.LastFinishedAt, age.Stale = ago(s.Finished), s.Finished >= 7*day
		out = append(out, age)
	}
	return out
}

// Set is a whole fixture.
type Set struct {
	Repos    []core.Repo
	Trees    []WT
	Stacks   []ST
	Cleanup  []core.CleanupRow
	Totals   core.CleanupTotals
	Expanded []string // ids to open, as the mock-ups show them
}

// Structure returns the events up to the end of the structure stage.
func (x Set) Structure() []any {
	out := []any{core.StageEvent{Stage: core.StageStructure}}
	for _, r := range x.Repos {
		out = append(out, core.RepoEvent{Repo: r})
	}
	for _, w := range x.Trees {
		e, _ := w.events()
		out = append(out, e)
	}
	for _, s := range x.Stacks {
		out = append(out, s.events()...)
	}
	return append(out, core.StageEvent{Stage: core.StageStructure, Done: true})
}

// Git returns the git stage.
func (x Set) Git() []any {
	out := []any{core.StageEvent{Stage: core.StageGit}}
	for _, w := range x.Trees {
		_, g := w.events()
		out = append(out, g)
	}
	return append(out, core.StageEvent{Stage: core.StageGit, Done: true})
}

// Stats returns the stats stage.
func (x Set) Stats() []any {
	out := []any{core.StageEvent{Stage: core.StageStats}}
	for _, s := range x.Stacks {
		out = append(out, s.stats()...)
	}
	return append(out, core.StageEvent{Stage: core.StageStats, Done: true})
}

// DiskStage returns the disk stage.
func (x Set) DiskStage() []any {
	out := []any{core.StageEvent{Stage: core.StageDisk}}
	for _, s := range x.Stacks {
		if s.Disk > 0 {
			out = append(out, core.StackDisk{Project: s.Project, Bytes: s.Disk, AgeS: ptr(int64(18 * 60))})
		}
	}
	out = append(out, core.DiskTotals{AgeS: ptr(int64(18 * 60))})
	return append(out, core.StageEvent{Stage: core.StageDisk, Done: true})
}

// CleanupStage returns the cleanup stage.
func (x Set) CleanupStage() []any {
	out := []any{core.StageEvent{Stage: core.StageCleanup}}
	for _, r := range x.Cleanup {
		out = append(out, r)
	}
	out = append(out, x.Totals)
	return append(out, core.StageEvent{Stage: core.StageCleanup, Done: true})
}

// Events returns the whole stream, in the order of the contract.
func (x Set) Events() []any {
	var out []any
	out = append(out, x.Structure()...)
	out = append(out, x.Git()...)
	out = append(out, x.Stats()...)
	out = append(out, x.DiskStage()...)
	out = append(out, x.CleanupStage()...)
	return append(out, core.EndEvent{})
}

func runningCont(service string, ports ...string) core.Container {
	return core.Container{ID: "id-" + service, Name: "x-" + service + "-1", Service: service, State: "running", Status: "Up 2 hours", Published: ports}
}

func exitedCont(service string) core.Container {
	return core.Container{ID: "id-" + service, Name: "x-" + service + "-1", Service: service, State: "exited", Status: "Exited (0) 25 hours ago"}
}

// Machine is the data of the 120x35 mock-up: machine scope, from feat-login.
func Machine() Set {
	foundry, metycle, wtree := "/home/dev/dev/foundry/.git", "/home/dev/dev/metycle/.git", "/home/dev/dev/wtree/.git"
	login, fmain, race := "/home/dev/dev/worktrees/feat-login", "/home/dev/dev/foundry/main", "/home/dev/dev/worktrees/fix-race"
	hot := "/home/dev/dev/wt/hotfix-1"
	mf, pv, wm, wt := "/home/dev/dev/metycle/foundry", "/home/dev/dev/worktrees/pricing-v2", "/home/dev/dev/wtree/main", "/home/dev/dev/worktrees/tui"
	return Set{
		Repos: []core.Repo{
			{Common: foundry, Name: "foundry", Main: fmain, DefaultBranch: ptr("main")},
			{Common: metycle, Name: "metycle", Main: mf, DefaultBranch: ptr("main")},
			{Common: wtree, Name: "wtree", Main: wm, DefaultBranch: ptr("main")},
		},
		Trees: []WT{
			{Repo: "foundry", Path: fmain, Common: foundry, Main: true, Branch: "main", Head: "aaaaaa11", CommitAgo: 1 * hour},
			{Repo: "foundry", Path: login, Common: foundry, Current: true, Branch: "feat/login", Head: "bbbbbb22", Ahead: 3, Changed: 4, CommitAgo: 2 * hour},
			{Repo: "foundry", Path: race, Common: foundry, Head: "9ac1e2ff00", Ahead: 1, CommitAgo: 5 * day},
			{Repo: "foundry", Path: hot, Common: foundry, Branch: "hotfix-1", Head: "cccccc33", CommitAgo: 9 * day, Merged: "main"},
			{Repo: "metycle", Path: mf, Common: metycle, Main: true, Branch: "main", Head: "dddddd44", Behind: 2, CommitAgo: 10 * minute},
			{Repo: "metycle", Path: pv, Common: metycle, Branch: "feat/pricing-v2", Head: "eeeeee55", Changed: 12, CommitAgo: 3 * hour},
			{Repo: "wtree", Path: wm, Common: wtree, Main: true, Branch: "main", Head: "ffffff66", CommitAgo: 20 * minute, Merged: "main"},
			{Repo: "wtree", Path: wt, Common: wtree, Branch: "tui-and-hardening", Head: "111111aa", Ahead: 2, CommitAgo: 1 * minute},
		},
		Stacks: []ST{
			{Project: "foundry-feat-login-api", Worktree: login, Kind: core.StackLive, States: map[string]int{"running": 1, "exited": 1},
				Conts: []core.Container{runningCont("api", "8080", "5555"), exitedCont("db")}, CPU: 4.1, Mem: 612 * mib,
				ContStat: []core.ContainerStats{{ID: "id-api", CPUPct: 3.9, MemBytes: 540 * mib}}, Disk: 840e6, Started: 2 * hour},
			{Project: "foundry-feat-login-e2e", Worktree: login, Kind: core.StackLive, States: map[string]int{"exited": 1},
				Disk: 380e6, Finished: 3 * day},
			{Project: "foundry-main", Worktree: fmain, Kind: core.StackLive, States: map[string]int{"running": 1},
				Conts: []core.Container{runningCont("web")}, CPU: 12, Mem: 1434 * mib, Disk: 3100e6, Started: hour},
			{Project: "metycle-foundry", Worktree: mf, Kind: core.StackLive, States: map[string]int{"running": 3},
				Conts: []core.Container{runningCont("a"), runningCont("b"), runningCont("c")}, CPU: 22.4, Mem: 2970 * mib, Disk: 6000e6, Started: 30},
			{Project: "metycle-pricing-v2", Worktree: pv, Kind: core.StackLive, States: map[string]int{"running": 1},
				Conts: []core.Container{runningCont("p")}, CPU: 1.2, Mem: 220 * mib, Disk: 900e6, Started: 3 * hour},
			{Project: "foundry-old-spike", Kind: core.StackGone, States: map[string]int{"exited": 2},
				Conts: []core.Container{exitedCont("a"), exitedCont("b")}, Disk: 5200e6, Finished: 12 * day},
			{Project: "metycle-demo", Kind: core.StackVolumes, States: map[string]int{}, Disk: 2200e6, Finished: 40 * day},
		},
		Cleanup: []core.CleanupRow{
			{ID: "worktree:" + hot, Kind: "worktree", Label: "hotfix-1", Tier: core.TierSafe, Reasons: []core.Reason{{Code: "merged", Text: "merged into main"}}},
			{ID: "stack:foundry-old-spike", Kind: "stack", Label: "foundry-old-spike", Tier: core.TierSafe, Reasons: []core.Reason{{Code: "worktree_gone", Text: "its directory is gone"}}, Bytes: ptr(int64(5200e6))},
			{ID: "stack:metycle-demo", Kind: "stack", Label: "metycle-demo", Tier: core.TierNeedsLook, Reasons: []core.Reason{{Code: "volumes_only", Text: "only volumes are left"}}, Bytes: ptr(int64(2200e6))},
			{ID: "worktree:" + login, Kind: "worktree", Label: "feat-login", Tier: core.TierNever, Reasons: []core.Reason{{Code: "current", Text: "you are in this worktree"}}, Bytes: ptr(int64(1220e6))},
			{ID: "worktree:" + fmain, Kind: "worktree", Label: "main", Tier: core.TierNever, Reasons: []core.Reason{{Code: "main_checkout", Text: "it is the main checkout"}}, Bytes: ptr(int64(3100e6))},
		},
		Totals:   core.CleanupTotals{Safe: 5200e6, NeedsLook: 2200e6, Never: 4320e6},
		Expanded: []string{"stack:foundry-feat-login-api"},
	}
}

// Repo is the data of the 80x24, 48x40 and 93x14 mock-ups: repo scope, from feat-login.
func Repo() Set {
	foundry := "/home/dev/dev/foundry/.git"
	login, fmain := "/home/dev/dev/worktrees/feat-login", "/home/dev/dev/foundry/main"
	race, pv, hot := "/home/dev/dev/worktrees/fix-race", "/home/dev/dev/worktrees/pricing-v2", "/home/dev/dev/worktrees/hotfix-1"
	return Set{
		Repos: []core.Repo{{Common: foundry, Name: "foundry", Main: fmain, DefaultBranch: ptr("main")}},
		Trees: []WT{
			{Repo: "foundry", Path: fmain, Common: foundry, Main: true, Branch: "main", Head: "aaaaaa11", CommitAgo: 1 * hour},
			{Repo: "foundry", Path: login, Common: foundry, Current: true, Branch: "feat/login", Head: "bbbbbb22", Changed: 4, CommitAgo: 2 * hour},
			{Repo: "foundry", Path: race, Common: foundry, Head: "9ac1e2ff00", Ahead: 1, CommitAgo: 1 * hour},
			{Repo: "foundry", Path: pv, Common: foundry, Branch: "feat/pricing-v2", Head: "eeeeee55", Changed: 12, CommitAgo: 2 * hour},
			{Repo: "foundry", Path: hot, Common: foundry, Branch: "hotfix-1", Head: "cccccc33", CommitAgo: 3 * hour, Merged: "main"},
		},
		Stacks: []ST{
			{Project: "feat-login-api", Worktree: login, Kind: core.StackLive, States: map[string]int{"running": 1, "exited": 1},
				Conts: []core.Container{runningCont("api", "8080", "5555"), exitedCont("db")}, CPU: 4.1, Mem: 612 * mib,
				ContStat: []core.ContainerStats{{ID: "id-api", CPUPct: 3.9, MemBytes: 540 * mib}}, Disk: 840e6, Started: 2 * hour},
			{Project: "feat-login-e2e", Worktree: login, Kind: core.StackLive, States: map[string]int{"exited": 1},
				Disk: 380e6, Finished: 3 * day},
			{Project: "foundry-main", Worktree: fmain, Kind: core.StackLive, States: map[string]int{"running": 1},
				Conts: []core.Container{runningCont("web")}, CPU: 12, Mem: 1434 * mib, Disk: 3100e6, Started: hour},
			{Project: "pricing-v2", Worktree: pv, Kind: core.StackLive, States: map[string]int{"running": 1},
				Conts: []core.Container{runningCont("p")}, CPU: 1.2, Mem: 220 * mib, Disk: 900e6, Started: 2 * hour},
			{Project: "old-spike", Kind: core.StackGone, States: map[string]int{"exited": 2},
				Conts: []core.Container{exitedCont("a"), exitedCont("b")}, Disk: 5200e6, Finished: 12 * day},
			{Project: "foundry-demo", Kind: core.StackVolumes, States: map[string]int{}, Disk: 380e6, Finished: 40 * day},
		},
		// hotfix-1 has no cleanup row here, because the mock-ups at these sizes keep it
		// in the list. See the report on the divergence from the 120x35 mock-up.
		Cleanup: []core.CleanupRow{
			{ID: "stack:old-spike", Kind: "stack", Label: "old-spike", Tier: core.TierSafe, Reasons: []core.Reason{{Code: "worktree_gone", Text: "its directory is gone"}}, Bytes: ptr(int64(5200e6))},
			{ID: "stack:foundry-demo", Kind: "stack", Label: "foundry-demo", Tier: core.TierNeedsLook, Reasons: []core.Reason{{Code: "volumes_only", Text: "only volumes are left"}}, Bytes: ptr(int64(380e6))},
		},
		Totals:   core.CleanupTotals{Safe: 5200e6, NeedsLook: 380e6},
		Expanded: []string{"stack:feat-login-api"},
	}
}
