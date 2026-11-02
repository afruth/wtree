// Package tree holds the data of the wtree TUI: it builds the tree from the events of
// `wtree core list` as they arrive, rolls numbers up, orders, filters and marks, and
// returns rows for the treetable component. It holds no policy: the tiers come from
// the core, and "safe" never skips a plan. It does no I/O, and the clock is injected.
package tree

import (
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/afruth/wtree/tui/internal/core"
	"github.com/afruth/wtree/tui/internal/treetable"
)

// Scope is repo (this repo and its worktrees) or machine (everything on the host).
type Scope int

// Scopes.
const (
	ScopeRepo Scope = iota
	ScopeMachine
)

// SortMode is the order of the list. All but SortTree flatten it to stack rows.
type SortMode int

// Sort modes, in the order that `o` cycles.
const (
	SortTree SortMode = iota
	SortDisk
	SortCPU
	SortName
	sortModes
)

// String is the name for the header: "sort: disk".
func (m SortMode) String() string {
	switch m {
	case SortDisk:
		return "disk"
	case SortCPU:
		return "cpu"
	case SortName:
		return "name"
	}
	return "tree"
}

// View is the list or the cleanup view.
type View int

// Views.
const (
	ViewTree View = iota
	ViewCleanup
)

// Options configure a State.
type Options struct {
	Scope Scope
	Home  string           // for ~ in paths that the list shows
	Now   func() time.Time // defaults to time.Now
}

// Group ids. The ids of the data rows are those of the contract: worktree:<path> and
// stack:<project>. A container is container:<project>/<name>.
const (
	GroupCleanup = "group:cleanup"
	GroupPlain   = "group:plain"
	GroupSafe    = "group:safe"
	GroupLook    = "group:needs_look"
	GroupNever   = "group:never"
)

const (
	zoneMain = iota
	zonePlain
	zoneCleanup
	zoneChild
)

type placement struct{ zone, rank int }

type wtNode struct {
	wt   core.Worktree
	git  *core.WorktreeGit
	seen bool
}

type stNode struct {
	st          core.Stack
	stats       *core.StackStats
	statsFresh  bool
	age         *core.StackAge
	disk        *core.StackDisk
	diskPending bool
	seen        bool
}

type stageState int

const (
	stageIdle stageState = iota
	stageRunning
	stageDone
)

// State is the tree. It is not safe for concurrent use: a Bubble Tea Update owns it.
type State struct {
	scope Scope
	home  string
	now   func() time.Time

	repos     map[string]core.Repo
	worktrees map[string]*wtNode
	stacks    map[string]*stNode
	cleanup   map[string]core.CleanupRow
	nextClean map[string]core.CleanupRow
	totals    *core.CleanupTotals
	engine    *core.Engine
	diskTotal *core.DiskTotals
	warnings  []core.Warning
	stages    map[string]stageState
	statsAt   time.Time
	dockerBad bool
	tabs      map[string]string

	settled  bool
	places   map[string]placement
	nextRank int
	flat     map[string]int
	nextFlat int

	filter   Filter
	sortMode SortMode
	reverse  bool
	view     View
	expanded map[string]bool
	marks    map[string]bool
}

// New returns an empty tree.
func New(o Options) *State {
	s := &State{scope: o.Scope, home: o.Home, now: o.Now, expanded: map[string]bool{}, marks: map[string]bool{}}
	if s.now == nil {
		s.now = time.Now
	}
	s.clearData()
	return s
}

func (s *State) clearData() {
	s.repos = map[string]core.Repo{}
	s.worktrees = map[string]*wtNode{}
	s.stacks = map[string]*stNode{}
	s.cleanup = map[string]core.CleanupRow{}
	s.nextClean = nil
	s.totals, s.engine, s.diskTotal = nil, nil, nil
	s.warnings = nil
	s.stages = map[string]stageState{}
	s.dockerBad = false
	s.settled = false
	s.places = map[string]placement{}
	s.flat = map[string]int{}
	s.nextRank, s.nextFlat = 0, 0
}

// SetScope changes the scope and forgets the data, which a new `core list` brings. It
// keeps the marks, the expansion, the filter and the sort, so a cursor by id stays on
// its row if the row is still in scope.
func (s *State) SetScope(scope Scope) {
	s.scope = scope
	s.clearData()
}

// Scope is the current scope.
func (s *State) Scope() Scope { return s.scope }

// BeginReload lets the next stages order the rows again. Call it for a reload that the
// user asked for, with `r`. A stats tick must not call it, so that live updates change
// cells and never positions.
func (s *State) BeginReload() { s.settled = false }

// Apply takes one decoded event of `core list --stream`, a value from core.DecodeEvent.
// Events of other commands and unknown events are ignored.
func (s *State) Apply(ev any) {
	switch e := ev.(type) {
	case core.StageEvent:
		s.applyStage(e)
	case core.RepoEvent:
		e.Repo.Name = treetable.Sanitize(e.Repo.Name)
		s.repos[e.Repo.Common] = e.Repo
	case core.WorktreeEvent:
		if n, ok := s.worktrees[e.Worktree.Path]; ok {
			n.wt, n.seen = e.Worktree, true
		} else {
			s.worktrees[e.Worktree.Path] = &wtNode{wt: e.Worktree, seen: true}
		}
		s.placeNew("worktree:" + e.Worktree.Path)
	case core.StackEvent:
		if n, ok := s.stacks[e.Stack.Project]; ok {
			n.st, n.seen = e.Stack, true
		} else {
			s.stacks[e.Stack.Project] = &stNode{st: e.Stack, seen: true}
		}
		s.placeNew("stack:" + e.Stack.Project)
	case core.WorktreeGit:
		if n, ok := s.worktrees[e.Path]; ok {
			g := e
			n.git = &g
		}
	case core.StackStats:
		if n, ok := s.stacks[e.Project]; ok {
			v := e
			n.stats, n.statsFresh = &v, true
			s.statsAt = s.now()
		}
	case core.StackAge:
		if n, ok := s.stacks[e.Project]; ok {
			v := e
			n.age = &v
		}
	case core.StackDisk:
		if n, ok := s.stacks[e.Project]; ok {
			v := e
			n.disk, n.diskPending = &v, false
		}
	case core.Engine:
		v := e
		s.engine = &v
	case core.DiskTotals:
		v := e
		s.diskTotal = &v
	case core.CleanupRow:
		if e.Tier == core.TierNever {
			delete(s.marks, e.ID) // it was marked before the core had said this
		}
		if s.nextClean != nil {
			s.nextClean[e.ID] = e
		} else {
			s.cleanup[e.ID] = e
		}
	case core.CleanupTotals:
		v := e
		s.totals = &v
	case core.Warning:
		s.warnings = append(s.warnings, e)
		switch e.Code {
		case core.WarnDockerTimeout, core.WarnDockerDown:
			s.dockerBad = true
		}
	case core.EndEvent:
		if !s.settled {
			s.reorder()
		}
		s.settled = true
	}
}

// ApplyDoc takes the answer of `core list` without --stream.
func (s *State) ApplyDoc(d core.ListDoc) {
	for _, stage := range []string{core.StageStructure, core.StageGit, core.StageStats, core.StageDisk, core.StageCleanup} {
		s.Apply(core.StageEvent{Stage: stage})
		switch stage {
		case core.StageStructure:
			for _, w := range d.Warnings {
				s.Apply(w)
			}
			for _, r := range d.Repos {
				s.Apply(core.RepoEvent{Repo: r})
			}
			for _, w := range d.Worktrees {
				s.Apply(core.WorktreeEvent{Worktree: w})
			}
			for _, st := range d.Stacks {
				s.Apply(core.StackEvent{Stack: st})
			}
		case core.StageGit:
			for _, g := range d.WorktreeGit {
				s.Apply(g)
			}
		case core.StageStats:
			for _, v := range d.StackStats {
				s.Apply(v)
			}
			for _, v := range d.StackAge {
				s.Apply(v)
			}
			if d.Engine != nil {
				s.Apply(*d.Engine)
			}
		case core.StageDisk:
			for _, v := range d.StackDisk {
				s.Apply(v)
			}
			if d.DiskTotals != nil {
				s.Apply(*d.DiskTotals)
			}
		case core.StageCleanup:
			for _, v := range d.Cleanup {
				s.Apply(v)
			}
			if d.CleanupTotals != nil {
				s.Apply(*d.CleanupTotals)
			}
		}
		s.Apply(core.StageEvent{Stage: stage, Done: true})
	}
	s.Apply(core.EndEvent{})
}

func (s *State) applyStage(e core.StageEvent) {
	if !e.Done {
		s.stages[e.Stage] = stageRunning
		switch e.Stage {
		case core.StageStructure:
			s.dockerBad = false
			for _, n := range s.worktrees {
				n.seen = false
			}
			for _, n := range s.stacks {
				n.seen = false
			}
		case core.StageStats:
			for _, n := range s.stacks {
				n.statsFresh = false
			}
		case core.StageCleanup:
			s.nextClean = map[string]core.CleanupRow{}
		}
		return
	}
	s.stages[e.Stage] = stageDone
	switch e.Stage {
	case core.StageStats:
		s.statsAt = s.now()
	case core.StageStructure:
		for path, n := range s.worktrees {
			if !n.seen {
				delete(s.worktrees, path)
				delete(s.places, "worktree:"+path)
			}
		}
		// An engine that does not answer says nothing about what is gone, so its
		// stacks stay until one that answers says so.
		if !s.dockerBad {
			for project, n := range s.stacks {
				if !n.seen {
					delete(s.stacks, project)
					delete(s.places, "stack:"+project)
					delete(s.flat, project)
				}
			}
		}
	case core.StageCleanup:
		if s.nextClean != nil {
			s.cleanup, s.nextClean = s.nextClean, nil
		}
	}
	if !s.settled {
		s.reorder()
	}
}

// StructureDone says that the rows are known. The keys that act on rows stay inert
// until it is true.
func (s *State) StructureDone() bool { return s.stages[core.StageStructure] == stageDone }

// StageDone says that a stage has ended at least once in this scope.
func (s *State) StageDone(stage string) bool { return s.stages[stage] == stageDone }

// Warnings lists the warnings so far.
func (s *State) Warnings() []core.Warning { return s.warnings }

// Summary is what the header says about the data.
type Summary struct {
	Worktrees, Stacks  int
	Reclaimable        int64 // safe and needs_look, from the core
	ReclaimableKnown   bool
	ReclaimableUnknown bool // a row has no size, so the total can be low
	CleanupRows        int  // the rows of the cleanup view that are not `never`
	CleanupSafe        int64
	CleanupNeedsLook   int64
	CleanupNever       int64
	StatsAt            time.Time // the time of the last live numbers
	DiskAgeS           *int64    // the age of the disk cache
	DiskRefreshing     bool
	Engine             *core.Engine
}

// Summary reads the totals for the header.
func (s *State) Summary() Summary {
	out := Summary{Stacks: len(s.stacks), StatsAt: s.statsAt, Engine: s.engine}
	for _, n := range s.worktrees {
		if !n.wt.Bare {
			out.Worktrees++
		}
	}
	if s.totals != nil {
		out.ReclaimableKnown = true
		out.Reclaimable = s.totals.Safe + s.totals.NeedsLook
		out.ReclaimableUnknown = s.totals.Unknown
		out.CleanupSafe, out.CleanupNeedsLook, out.CleanupNever = s.totals.Safe, s.totals.NeedsLook, s.totals.Never
	}
	for _, row := range s.cleanup {
		if row.Tier != core.TierNever {
			out.CleanupRows++
		}
	}
	if s.diskTotal != nil {
		out.DiskAgeS, out.DiskRefreshing = s.diskTotal.AgeS, s.diskTotal.Refreshing
	}
	return out
}

// SetOpenTabs says which worktree paths have a tab, with the hint to show: "tab 4".
func (s *State) SetOpenTabs(tabs map[string]string) { s.tabs = tabs }

// InvalidateDisk makes the disk cells of these stacks show loading until the next
// scan, as after a delete. The worktree cells follow by roll-up.
func (s *State) InvalidateDisk(projects []string) {
	for _, p := range projects {
		if n, ok := s.stacks[p]; ok {
			n.disk, n.diskPending = nil, true
		}
	}
}

// ---- order

func (s *State) worktreeActivity(n *wtNode) int64 {
	var newest int64
	if n.git != nil {
		if n.git.LastCommitAt != nil {
			newest = max(newest, *n.git.LastCommitAt)
		}
		if n.git.IndexMtime != nil {
			newest = max(newest, *n.git.IndexMtime)
		}
	}
	for _, st := range s.stacks {
		if st.age != nil && st.age.LastStartedAt != nil && st.st.Worktree != nil && *st.st.Worktree == n.wt.Path {
			newest = max(newest, *st.age.LastStartedAt)
		}
	}
	return newest
}

func (s *State) currentCommon() string {
	for _, n := range s.worktrees {
		if n.wt.Current {
			return n.wt.Common
		}
	}
	return ""
}

func (s *State) worktreeZone(n *wtNode) int {
	if n.wt.Main || n.wt.Current {
		return zoneMain
	}
	row, ok := s.cleanup["worktree:"+n.wt.Path]
	if !ok || row.Tier == core.TierNever {
		return zoneMain
	}
	for _, r := range row.Reasons {
		switch r.Code {
		case "merged", "merged_running", "merged_recent_stack", "stacks_unknown", "upstream_gone", "missing_dir":
			return zoneCleanup
		}
	}
	return zoneMain
}

func (s *State) stackZone(n *stNode) int {
	switch n.st.Kind {
	case core.StackGone, core.StackVolumes:
		return zoneCleanup
	case core.StackPlain:
		return zonePlain
	}
	if n.st.Worktree != nil {
		if _, ok := s.worktrees[*n.st.Worktree]; ok {
			return zoneChild
		}
	}
	return zonePlain
}

// placeNew gives a row that came after the order was settled a place after the others
// of its zone. Nothing that has a place moves for it.
func (s *State) placeNew(id string) {
	if _, ok := s.places[id]; ok {
		return
	}
	zone := zoneMain
	if strings.HasPrefix(id, "stack:") {
		zone = s.stackZone(s.stacks[strings.TrimPrefix(id, "stack:")])
	} else {
		zone = s.worktreeZone(s.worktrees[strings.TrimPrefix(id, "worktree:")])
	}
	s.places[id] = placement{zone, s.nextRank}
	s.nextRank++
}

// reorder is the smart default order of ux.md 2.3. Repos: the current one first, then
// by the newest activity of their worktrees. In a repo: the current worktree, the main
// checkout, then the others, newest first.
func (s *State) reorder() {
	activity := make(map[string]int64, len(s.worktrees))
	repoNewest := map[string]int64{}
	nodes := make([]*wtNode, 0, len(s.worktrees))
	for _, n := range s.worktrees {
		nodes = append(nodes, n)
		a := s.worktreeActivity(n)
		activity[n.wt.Path] = a
		repoNewest[n.wt.Common] = max(repoNewest[n.wt.Common], a)
	}
	current := s.currentCommon()
	rank := func(n *wtNode) int {
		switch {
		case n.wt.Current:
			return 0
		case n.wt.Main:
			return 1
		}
		return 2
	}
	sort.Slice(nodes, func(i, j int) bool {
		a, b := nodes[i], nodes[j]
		if a.wt.Common != b.wt.Common {
			if (a.wt.Common == current) != (b.wt.Common == current) {
				return a.wt.Common == current
			}
			if repoNewest[a.wt.Common] != repoNewest[b.wt.Common] {
				return repoNewest[a.wt.Common] > repoNewest[b.wt.Common]
			}
			return a.wt.Common < b.wt.Common
		}
		if rank(a) != rank(b) {
			return rank(a) < rank(b)
		}
		if activity[a.wt.Path] != activity[b.wt.Path] {
			return activity[a.wt.Path] > activity[b.wt.Path]
		}
		return a.wt.Path < b.wt.Path
	})
	s.places = make(map[string]placement, len(nodes)+len(s.stacks))
	s.nextRank = 0
	for _, n := range nodes {
		s.places["worktree:"+n.wt.Path] = placement{s.worktreeZone(n), s.nextRank}
		s.nextRank++
	}
	// Stacks, for the orphan zones: with a gone directory before volumes only, the
	// largest first. Children of a worktree are ordered by name when they are drawn.
	stacks := make([]*stNode, 0, len(s.stacks))
	for _, n := range s.stacks {
		stacks = append(stacks, n)
	}
	sort.Slice(stacks, func(i, j int) bool {
		a, b := stacks[i], stacks[j]
		if (a.st.Kind == core.StackVolumes) != (b.st.Kind == core.StackVolumes) {
			return b.st.Kind == core.StackVolumes
		}
		var da, db int64
		if a.disk != nil {
			da = a.disk.Bytes
		}
		if b.disk != nil {
			db = b.disk.Bytes
		}
		if da != db {
			return da > db
		}
		return a.st.Project < b.st.Project
	})
	for _, n := range stacks {
		s.places["stack:"+n.st.Project] = placement{s.stackZone(n), s.nextRank}
		s.nextRank++
	}
	s.resortFlat()
}

// Resort applies the order again, on `r`, or on a tick while the cursor has been still.
func (s *State) Resort() { s.reorder() }

// SetSort picks a sort mode. reverse is for a second tap on a column title.
func (s *State) SetSort(m SortMode, reverse bool) {
	s.sortMode, s.reverse = m, reverse
	s.resortFlat()
}

// CycleSort goes to the next mode: tree, disk, cpu, name.
func (s *State) CycleSort() SortMode {
	s.SetSort((s.sortMode+1)%sortModes, false)
	return s.sortMode
}

// Sort is the current mode, and whether it is reversed.
func (s *State) Sort() (SortMode, bool) { return s.sortMode, s.reverse }

func (s *State) resortFlat() {
	stacks := make([]*stNode, 0, len(s.stacks))
	for _, n := range s.stacks {
		stacks = append(stacks, n)
	}
	key := func(n *stNode) (float64, bool) {
		switch s.sortMode {
		case SortDisk:
			if n.disk != nil {
				return float64(n.disk.Bytes), true
			}
		case SortCPU:
			if n.stats != nil {
				return n.stats.CPUPct, true
			}
		}
		return 0, false
	}
	sort.SliceStable(stacks, func(i, j int) bool {
		a, b := stacks[i], stacks[j]
		if s.sortMode == SortName {
			if s.reverse {
				return a.st.Project > b.st.Project
			}
			return a.st.Project < b.st.Project
		}
		ka, oka := key(a)
		kb, okb := key(b)
		if oka != okb { // what has no number goes last in both directions
			return oka
		}
		if ka != kb {
			if s.reverse {
				return ka < kb
			}
			return ka > kb
		}
		return a.st.Project < b.st.Project
	})
	s.flat = make(map[string]int, len(stacks))
	s.nextFlat = 0
	for _, n := range stacks {
		s.flat[n.st.Project] = s.nextFlat
		s.nextFlat++
	}
}

// ---- filter, view, expansion

// SetFilter parses and applies a search, and returns it so the caller can show Error.
// The filter is the same while the cursor moves: a mark-all acts on what it shows.
func (s *State) SetFilter(query string) Filter {
	s.filter = ParseFilter(query)
	return s.filter
}

// Filter is the current filter.
func (s *State) Filter() Filter { return s.filter }

// SetView opens or closes the cleanup view.
func (s *State) SetView(v View) { s.view = v }

// View is the current view.
func (s *State) View() View { return s.view }

func (s *State) isExpanded(id string) bool {
	if v, ok := s.expanded[id]; ok {
		return v
	}
	if strings.HasPrefix(id, "worktree:") {
		return len(s.childStacks(strings.TrimPrefix(id, "worktree:"))) >= 2
	}
	return false
}

// Toggle flips the expansion of a row.
func (s *State) Toggle(id string) { s.expanded[id] = !s.isExpanded(id) }

// SetExpanded sets the expansion of a row.
func (s *State) SetExpanded(id string, open bool) { s.expanded[id] = open }

// IsExpanded says whether a row is expanded, with its default if nobody chose.
func (s *State) IsExpanded(id string) bool { return s.isExpanded(id) }

// SetAllExpanded opens or closes every worktree and stack, for `z`.
func (s *State) SetAllExpanded(open bool) {
	for p := range s.worktrees {
		s.expanded["worktree:"+p] = open
	}
	for p := range s.stacks {
		s.expanded["stack:"+p] = open
	}
}

// ExpandedState is what ui.json remembers: only what somebody chose.
func (s *State) ExpandedState() map[string]bool {
	out := make(map[string]bool, len(s.expanded))
	for k, v := range s.expanded {
		out[k] = v
	}
	return out
}

// RestoreExpanded puts back what ExpandedState returned.
func (s *State) RestoreExpanded(m map[string]bool) {
	for k, v := range m {
		s.expanded[k] = v
	}
}

// ---- marks

// markable says that a row can take a mark.
func (s *State) markable(id string) bool {
	if !s.markKind(id) {
		return false
	}
	row, ok := s.cleanup[id]
	return !ok || row.Tier != core.TierNever
}

// rowMarkable says that the gutter of a row is a target. In the tree a tap there on a row that can
// never go answers why, as the key does. In the cleanup view that row is listed only for the total.
func (s *State) rowMarkable(id string) bool {
	if s.view == ViewCleanup {
		return s.markable(id)
	}
	return s.markKind(id)
}

func (s *State) markKind(id string) bool {
	switch {
	case strings.HasPrefix(id, "worktree:"):
		n, ok := s.worktrees[strings.TrimPrefix(id, "worktree:")]
		if !ok || n.wt.Odd || n.wt.Bare {
			return false
		}
	case strings.HasPrefix(id, "stack:"):
		if _, ok := s.stacks[strings.TrimPrefix(id, "stack:")]; !ok {
			return false
		}
	default:
		return false
	}
	return true
}

// ToggleMark marks or unmarks a row and returns whether it is marked now. Only a
// worktree or a stack takes a mark, and no row that the core says can never go does.
func (s *State) ToggleMark(id string) bool {
	if s.marks[id] {
		delete(s.marks, id)
		return false
	}
	if !s.markable(id) {
		return false
	}
	s.marks[id] = true
	return true
}

// MarkAll is `A`. It marks the rows that match the filter and that the list shows, and
// no context row, so what you see is what you mark. In the cleanup view it marks `safe`
// rows only. An invalid filter shows nothing, and so marks nothing. It returns how many
// rows it added.
func (s *State) MarkAll() int {
	added := 0
	for _, r := range s.Visible() {
		if !s.markable(r.ID) || r.Dim || s.marks[r.ID] {
			continue
		}
		if s.view == ViewCleanup {
			if row, ok := s.cleanup[r.ID]; !ok || row.Tier != core.TierSafe {
				continue
			}
		}
		s.marks[r.ID] = true
		added++
	}
	return added
}

// ClearMarks unmarks everything, for `u`.
func (s *State) ClearMarks() { s.marks = map[string]bool{} }

// Unmark drops marks, such as those of rows that a batch deleted.
func (s *State) Unmark(ids ...string) {
	for _, id := range ids {
		delete(s.marks, id)
	}
}

// IsMarked says whether a row is marked.
func (s *State) IsMarked(id string) bool { return s.marks[id] }

// Marked lists the marked ids in byte order, also those that the list does not show.
func (s *State) Marked() []string {
	out := make([]string, 0, len(s.marks))
	for id := range s.marks {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// MarkCounts is the sentence `3 marked (1 hidden by filter)` in numbers.
type MarkCounts struct {
	Total          int
	HiddenByFilter int      // in scope, and the filter or the view does not show it
	OutOfScope     int      // not in the data of this scope
	Hidden         []string // the ids of HiddenByFilter, for HIDDEN BY FILTER in a sheet
}

// MarkCounts counts the marks. A mark under a collapsed row is not hidden by the filter.
func (s *State) MarkCounts() MarkCounts {
	out := MarkCounts{Total: len(s.marks)}
	matched := map[string]bool{}
	for _, n := range s.displayTree() {
		n.collectMatches(s, matched)
	}
	for _, id := range s.Marked() {
		if s.lookupExists(id) {
			if !matched[id] {
				out.HiddenByFilter++
				out.Hidden = append(out.Hidden, id)
			}
		} else {
			out.OutOfScope++
		}
	}
	return out
}

func (s *State) lookupExists(id string) bool {
	switch {
	case strings.HasPrefix(id, "worktree:"):
		_, ok := s.worktrees[strings.TrimPrefix(id, "worktree:")]
		return ok
	case strings.HasPrefix(id, "stack:"):
		_, ok := s.stacks[strings.TrimPrefix(id, "stack:")]
		return ok
	}
	return false
}

// Info is everything known about one row, for the panel and the sheets. The pointers
// are copies: nil is not known, or not this kind of row.
type Info struct {
	ID        string
	Kind      treetable.Kind
	Worktree  *core.Worktree
	Git       *core.WorktreeGit
	Stack     *core.Stack
	Stats     *core.StackStats
	Age       *core.StackAge
	Disk      *core.StackDisk
	Container *core.Container
	Cleanup   *core.CleanupRow
	Stacks    []core.Stack // the stacks under a worktree
}

// Lookup finds a row by id.
func (s *State) Lookup(id string) (Info, bool) {
	info := Info{ID: id}
	switch {
	case strings.HasPrefix(id, "worktree:"):
		n, ok := s.worktrees[strings.TrimPrefix(id, "worktree:")]
		if !ok {
			return info, false
		}
		info.Kind = treetable.KindWorktree
		w := n.wt
		info.Worktree, info.Git = &w, n.git
		for _, c := range s.childStacks(n.wt.Path) {
			info.Stacks = append(info.Stacks, c.st)
		}
	case strings.HasPrefix(id, "stack:"):
		n, ok := s.stacks[strings.TrimPrefix(id, "stack:")]
		if !ok {
			return info, false
		}
		info.Kind = treetable.KindStack
		st := n.st
		info.Stack, info.Stats, info.Age, info.Disk = &st, n.stats, n.age, n.disk
	case strings.HasPrefix(id, "container:"):
		project, name, _ := strings.Cut(strings.TrimPrefix(id, "container:"), "/")
		n, ok := s.stacks[project]
		if !ok {
			return info, false
		}
		for i := range n.st.Containers {
			if n.st.Containers[i].Name == name {
				c := n.st.Containers[i]
				st := n.st
				info.Kind = treetable.KindContainer
				info.Container, info.Stack = &c, &st
				return info, true
			}
		}
		return info, false
	case strings.HasPrefix(id, "group:"):
		info.Kind = treetable.KindGroup
		return info, true
	default:
		return info, false
	}
	if row, ok := s.cleanup[id]; ok {
		info.Cleanup = &row
	}
	return info, true
}

func (s *State) childStacks(path string) []*stNode {
	var out []*stNode
	for _, n := range s.stacks {
		if n.st.Worktree != nil && *n.st.Worktree == path && s.zoneOf("stack:"+n.st.Project) == zoneChild {
			out = append(out, n)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].st.Project < out[j].st.Project })
	return out
}

func (s *State) zoneOf(id string) int { return s.places[id].zone }

func (s *State) tilde(path string) string {
	if s.home != "" && (path == s.home || strings.HasPrefix(path, s.home+"/")) {
		return "~" + path[len(s.home):]
	}
	return path
}

func base(path string) string { return filepath.Base(path) }
