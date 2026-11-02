package tree

import (
	"sort"
	"strconv"
	"strings"

	"github.com/afruth/wtree/tui/internal/core"
	"github.com/afruth/wtree/tui/internal/theme"
	"github.com/afruth/wtree/tui/internal/treetable"
)

const staleDiskSeconds = 30 * 60

// dnode is one row of the display tree, before the filter and the expansion.
type dnode struct {
	row      treetable.Row
	facts    *facts
	expanded bool
	kids     []*dnode
	disk     int64 // the bytes behind row.Disk, because a sum of rounded text drifts
}

func (n *dnode) collectMatches(s *State, out map[string]bool) {
	if n.facts != nil && s.filter.match(n.facts) {
		out[n.row.ID] = true
	}
	for _, k := range n.kids {
		k.collectMatches(s, out)
	}
}

// Visible returns the rows to draw, in order. It is a pure read of the state, and
// the ids in it are stable, so a cursor by id survives any change.
func (s *State) Visible() []treetable.Row {
	var out []treetable.Row
	active := s.filter.Active()
	for _, n := range s.displayTree() {
		rows := s.flatten(n, nil, active)
		if n.row.Kind == treetable.KindGroup && len(out) > 0 && len(rows) > 0 && s.view == ViewTree {
			out = append(out, treetable.Row{ID: "sep:" + n.row.ID, Kind: treetable.KindSeparator})
		}
		out = append(out, rows...)
	}
	return out
}

func (s *State) flatten(n, parent *dnode, active bool) []treetable.Row {
	if !active {
		row := n.row
		row.Expanded, row.Marked = n.expanded, s.marks[row.ID]
		rows := []treetable.Row{row}
		if n.expanded {
			for _, k := range n.kids {
				rows = append(rows, s.flatten(k, n, false)...)
			}
		}
		return rows
	}
	matched := n.facts != nil && s.filter.match(n.facts)
	var kids []treetable.Row
	for _, k := range n.kids {
		kids = append(kids, s.flatten(k, n, true)...)
	}
	parentMatched := parent != nil && parent.facts != nil && s.filter.match(parent.facts)
	// A row that matches only by what its parent has stays folded with that parent.
	self := matched && (!parentMatched || parent.expanded)
	if !self && len(kids) == 0 {
		return nil
	}
	row := n.row
	row.Expanded = n.expanded || len(kids) > 0
	row.Marked = s.marks[row.ID]
	if !self {
		row.Dim = true
	}
	return append([]treetable.Row{row}, kids...)
}

// displayTree builds the rows of the current view with no filter yet.
func (s *State) displayTree() []*dnode {
	if s.view == ViewCleanup {
		return s.cleanupViewTree()
	}
	if s.sortMode != SortTree {
		return s.flatTree()
	}
	var main, plain, clean []*dnode
	type placed struct {
		rank int
		node *dnode
	}
	var mainP, plainP, cleanP []placed
	for path, n := range s.worktrees {
		if n.wt.Bare {
			continue
		}
		p := s.places["worktree:"+path]
		if p.zone == zoneCleanup {
			cleanP = append(cleanP, placed{1<<30 + p.rank, s.worktreeNode(n, true)})
		} else {
			mainP = append(mainP, placed{p.rank, s.worktreeNode(n, false)})
		}
	}
	for project, n := range s.stacks {
		p := s.places["stack:"+project]
		// A child whose worktree is gone from the data shows with what has no repo.
		if p.zone == zoneChild && n.st.Worktree != nil {
			if _, ok := s.worktrees[*n.st.Worktree]; ok {
				continue
			}
		}
		if p.zone == zoneCleanup {
			cleanP = append(cleanP, placed{p.rank, s.stackNode(n, 1, true, nil)})
		} else {
			plainP = append(plainP, placed{p.rank, s.stackNode(n, 1, false, nil)})
		}
	}
	take := func(in []placed) []*dnode {
		sort.Slice(in, func(i, j int) bool { return in[i].rank < in[j].rank })
		out := make([]*dnode, len(in))
		for i, p := range in {
			out[i] = p.node
		}
		return out
	}
	main, plain, clean = take(mainP), take(plainP), take(cleanP)

	out := append([]*dnode{}, main...)
	if len(plain) > 0 {
		group := s.groupNode(GroupPlain, "no repo", treetable.MarkerNone, plain)
		group.row.Disk, group.disk = rollDisk(plain)
		out = append(out, group)
	}
	group := s.groupNode(GroupCleanup, "cleanup", treetable.MarkerCleanup, clean)
	if len(clean) == 0 {
		if s.StageDone(core.StageCleanup) {
			group.row.NameNote = "nothing to clean"
			group.row.Disk, group.row.CPU, group.row.Mem = treetable.Cell{}, treetable.Cell{}, treetable.Cell{}
		} else {
			group.row.Disk = treetable.Loading()
		}
	} else {
		group.row.Disk, group.disk = rollDisk(clean)
	}
	return append(out, group)
}

func (s *State) groupNode(id, name string, marker treetable.Marker, kids []*dnode) *dnode {
	return &dnode{
		row: treetable.Row{
			ID: id, Kind: treetable.KindGroup, Name: name, Marker: marker,
			CPU: treetable.NA(), Mem: treetable.NA(),
		},
		expanded: true,
		kids:     kids,
	}
}

// rollDisk adds up the disk of some rows.
func rollDisk(nodes []*dnode) (treetable.Cell, int64) {
	cells := make([]treetable.Cell, 0, len(nodes))
	var total int64
	for _, n := range nodes {
		cells = append(cells, n.row.Disk)
		total += n.disk
	}
	return rollup(cells, FormatDisk(total)), total
}

// flatTree is the list for a sort by disk, cpu or name: stack rows only, with their
// worktree name as a dim prefix.
func (s *State) flatTree() []*dnode {
	type placed struct {
		rank int
		node *dnode
	}
	var in []placed
	for project, n := range s.stacks {
		rank, ok := s.flat[project]
		if !ok {
			rank = s.nextFlat + 1<<20
		}
		parent := s.parentOf(n)
		node := s.stackNode(n, 0, n.st.Kind == core.StackGone || n.st.Kind == core.StackVolumes, parent)
		if parent != nil {
			node.row.Prefix = s.worktreeName(parent)
		}
		in = append(in, placed{rank, node})
	}
	sort.Slice(in, func(i, j int) bool {
		if in[i].rank != in[j].rank {
			return in[i].rank < in[j].rank
		}
		return in[i].node.row.ID < in[j].node.row.ID
	})
	out := make([]*dnode, len(in))
	for i, p := range in {
		out[i] = p.node
	}
	return out
}

// cleanupViewTree is ux.md 4.3: the same rows under a heading for each tier, with the
// reason of the core on each row.
func (s *State) cleanupViewTree() []*dnode {
	byTier := map[string][]core.CleanupRow{}
	for _, row := range s.cleanup {
		if s.lookupExists(row.ID) {
			byTier[row.Tier] = append(byTier[row.Tier], row)
		}
	}
	var out []*dnode
	tiers := []struct {
		tier, id, name string
		marker         treetable.Marker
	}{
		{core.TierSafe, GroupSafe, "safe", treetable.MarkerSafe},
		{core.TierNeedsLook, GroupLook, "needs a look", treetable.MarkerLook},
		{core.TierNever, GroupNever, "never", treetable.MarkerNone},
	}
	for _, tier := range tiers {
		rows := byTier[tier.tier]
		if len(rows) == 0 {
			continue
		}
		sort.Slice(rows, func(i, j int) bool {
			var a, b int64
			if rows[i].Bytes != nil {
				a = *rows[i].Bytes
			}
			if rows[j].Bytes != nil {
				b = *rows[j].Bytes
			}
			if a != b {
				return a > b
			}
			return rows[i].ID < rows[j].ID
		})
		var kids []*dnode
		for _, row := range rows {
			var node *dnode
			if row.Kind == "worktree" {
				node = s.worktreeNode(s.worktrees[strings.TrimPrefix(row.ID, "worktree:")], false)
			} else {
				st := s.stacks[strings.TrimPrefix(row.ID, "stack:")]
				node = s.stackNode(st, 1, false, s.parentOf(st))
			}
			if len(row.Reasons) > 0 {
				node.row.Reason = treetable.Sanitize(row.Reasons[0].Text)
				if row.Reasons[0].Code == "volumes_only" {
					node.row.Reason = "volumes only · origin unknown" // the panel has the sentence
				}
				node.row.ReasonTone = theme.ToneWarn
				if row.Tier == core.TierNever {
					node.row.ReasonTone = theme.ToneDim
				}
			}
			if row.Tier == core.TierNever {
				node.row.Dim = true
			}
			kids = append(kids, node)
		}
		group := s.groupNode(tier.id, tier.name, tier.marker, kids)
		group.row.Disk, group.disk = rollDisk(kids)
		if tier.tier == core.TierNever {
			group.row.Dim = true
		}
		out = append(out, group)
	}
	if len(byTier[core.TierSafe]) == 0 && len(byTier[core.TierNeedsLook]) == 0 {
		group := s.groupNode(GroupCleanup, "cleanup", treetable.MarkerCleanup, nil)
		group.row.CPU, group.row.Mem = treetable.Cell{}, treetable.Cell{}
		if s.StageDone(core.StageCleanup) {
			group.row.NameNote = "nothing to clean"
		} else {
			group.row.Disk = treetable.Loading()
		}
		out = append([]*dnode{group}, out...)
	}
	return out
}

// ---- rows

func (s *State) repoName(n *wtNode) string {
	if n.wt.Repo != "" {
		return treetable.Sanitize(n.wt.Repo)
	}
	return s.repos[n.wt.Common].Name
}

func (s *State) worktreeName(n *wtNode) string {
	name := treetable.Sanitize(base(n.wt.Path))
	// the main checkout is usually named as its repo: "proj", and not "proj/proj"
	if repo := s.repoName(n); s.scope == ScopeMachine && repo != name {
		return repo + "/" + name
	}
	return name
}

func (s *State) worktreeNode(n *wtNode, inCleanup bool) *dnode {
	id := "worktree:" + n.wt.Path
	branch := s.branchText(n)

	kids := []*dnode{}
	children := s.childStacks(n.wt.Path)
	cpu, mem := make([]treetable.Cell, 0, len(children)), make([]treetable.Cell, 0, len(children))
	var cpuSum float64
	var memSum int64
	running, total := 0, 0
	for _, c := range children {
		kid := s.stackNode(c, 1, false, n)
		kids = append(kids, kid)
		r, t := countStates(c.st.States)
		running += r
		total += t
		cpu = append(cpu, kid.row.CPU)
		mem = append(mem, kid.row.Mem)
		if c.stats != nil {
			cpuSum += c.stats.CPUPct
			memSum += c.stats.MemBytes
		}
	}

	row := treetable.Row{
		ID: id, Kind: treetable.KindWorktree, Depth: 0,
		Name: s.worktreeName(n), Branch: branch, Markable: s.rowMarkable(id),
		Tab: treetable.Sanitize(s.tabs[n.wt.Path]),
	}
	row.Expandable = len(children) > 0
	if branch == treetable.Sanitize(base(n.wt.Path)) {
		row.Branch = "" // the name already says it
	}
	switch {
	case n.wt.Current:
		row.Marker = treetable.MarkerCurrent
	case n.wt.Main && (s.scope == ScopeRepo || n.wt.Common == s.currentCommon()):
		row.Marker = treetable.MarkerMain
	}
	if inCleanup {
		// the name, as the sheets and the cleanup view say it; the panel shows the path
		row.Name = treetable.Sanitize(base(n.wt.Path))
		if s.scope == ScopeMachine && s.repoName(n) != row.Name {
			row.Prefix = s.repoName(n)
		}
		row.Reason, row.ReasonTone = s.worktreeReason(n), theme.ToneWarn
	} else if n.wt.Missing {
		row.Reason, row.ReasonTone = "directory gone", theme.ToneWarn
	}
	row.Git = s.gitOf(n, inCleanup)
	if total > 0 {
		row.Count = strconv.Itoa(running) + "/" + strconv.Itoa(total)
		switch {
		case running == total:
			row.Run = treetable.RunUp
		case running > 0:
			row.Run = treetable.RunPartial
		default:
			row.Run = treetable.RunExited
		}
	}
	var diskTotal int64
	if len(children) == 0 {
		cell := treetable.NA()
		if !s.StructureDone() {
			cell = treetable.Loading()
		}
		row.CPU, row.Mem, row.Disk = cell, cell, cell
	} else {
		row.CPU = rollup(cpu, FormatCPU(cpuSum))
		row.Mem = rollup(mem, FormatMem(memSum))
		row.Disk, diskTotal = rollDisk(kids)
	}
	row.Age = s.worktreeAge(n)
	return &dnode{row: row, facts: s.worktreeFacts(n, branch, row.Reason), expanded: s.isExpanded(id), kids: kids, disk: diskTotal}
}

func (s *State) worktreeReason(n *wtNode) string {
	row := s.cleanup["worktree:"+n.wt.Path]
	for _, r := range row.Reasons {
		switch r.Code {
		case "merged", "merged_running", "merged_recent_stack", "stacks_unknown":
			if n.git != nil && n.git.MergedInto != nil {
				return "merged into " + treetable.Sanitize(*n.git.MergedInto)
			}
		case "upstream_gone":
			return "branch gone"
		case "missing_dir":
			return "directory gone"
		}
	}
	if len(row.Reasons) > 0 {
		return treetable.Sanitize(row.Reasons[0].Text)
	}
	return ""
}

func (s *State) gitOf(n *wtNode, inCleanup bool) treetable.Git {
	g := treetable.Git{Locked: n.wt.Locked != nil}
	switch {
	case n.wt.Missing || (n.git != nil && n.git.Missing):
		g.State = treetable.CellNA
	case n.git == nil:
		g.State = treetable.CellLoading
		if s.StageDone(core.StageGit) {
			g.State = treetable.CellFailed
		}
	case n.git.Unreadable || n.git.TimedOut:
		g.State = treetable.CellFailed
	default:
		g.State = treetable.CellValue
		g.Ahead, g.Behind, g.Changed = deref(n.git.Ahead), deref(n.git.Behind), deref(n.git.Changed)
		// In the cleanup group the reason already says that it is merged.
		g.Merged = n.git.MergedInto != nil && !inCleanup
	}
	return g
}

func deref(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func (s *State) worktreeAge(n *wtNode) treetable.Cell {
	switch {
	case n.wt.Missing:
		return treetable.NA()
	case n.git == nil:
		if s.StageDone(core.StageGit) {
			return treetable.Failed().WithTone(theme.ToneDim)
		}
		return treetable.Loading()
	case n.git.LastCommitAt == nil:
		if n.git.Unreadable || n.git.TimedOut {
			return treetable.Failed()
		}
		return treetable.NA()
	}
	return treetable.Value(FormatRowAge(s.now().Unix() - *n.git.LastCommitAt)).WithTone(theme.ToneDim)
}

func (s *State) worktreeFacts(n *wtNode, branch, reason string) *facts {
	w := n.wt
	repo := strings.ToLower(s.repoName(n))
	f := &facts{
		repo:   repo,
		branch: strings.ToLower(branch),
		text:   strings.ToLower(strings.Join([]string{repo, base(w.Path), treetable.Sanitize(w.Path), branch, reason}, "\x00")),
		is: map[string]bool{
			"current": w.Current,
			"main":    w.Main,
			"locked":  w.Locked != nil,
			"marked":  s.marks["worktree:"+w.Path],
			"open":    s.tabs[w.Path] != "",
			"merged":  n.git != nil && n.git.MergedInto != nil,
			"changed": n.git != nil && deref(n.git.Changed) > 0,
		},
	}
	if len(s.childStacks(w.Path)) == 0 {
		f.state = "none"
	}
	return f
}

func countStates(states map[string]int) (running, total int) {
	for state, n := range states {
		total += n
		if state == "running" {
			running += n
		}
	}
	return running, total
}

func stackWord(st core.Stack) (string, treetable.Run) {
	if st.Kind == core.StackVolumes {
		return "volumes", treetable.RunVolumes
	}
	running, total := countStates(st.States)
	switch {
	case total == 0:
		return "down", treetable.RunDown
	case running == total:
		return "up", treetable.RunUp
	case running > 0:
		return "partial", treetable.RunPartial
	case st.States["exited"] > 0 || st.States["dead"] > 0:
		return "exited", treetable.RunExited
	case st.States["paused"] > 0:
		return "paused", treetable.RunExited
	}
	return "down", treetable.RunDown
}

// stateFilterWord is the value of state: for a stack.
func stateFilterWord(run treetable.Run) string {
	switch run {
	case treetable.RunUp:
		return "running"
	case treetable.RunPartial:
		return "partial"
	case treetable.RunExited:
		return "exited"
	case treetable.RunVolumes:
		return "volumes"
	}
	return "down"
}

func (s *State) stackNode(n *stNode, depth int, inCleanup bool, parent *wtNode) *dnode {
	st := n.st
	id := "stack:" + st.Project
	word, run := stackWord(st)
	running, total := countStates(st.States)
	row := treetable.Row{
		ID: id, Kind: treetable.KindStack, Depth: depth,
		Name: treetable.Sanitize(st.Project), Word: word, Run: run,
		Markable:   s.rowMarkable(id),
		Expandable: len(st.Containers) > 0,
	}
	if total > 0 {
		row.Count = strconv.Itoa(running) + "/" + strconv.Itoa(total)
	}
	if inCleanup {
		switch st.Kind {
		case core.StackGone:
			row.Reason, row.ReasonTone = "worktree gone", theme.ToneWarn
		case core.StackVolumes:
			row.Reason, row.ReasonTone = "volumes only", theme.ToneWarn
		}
	}
	row.CPU, row.Mem = s.liveCells(n, running)
	row.Disk = s.diskCell(n)
	row.Age = s.stackAgeCell(n)

	expanded := s.isExpanded(id)
	kids := make([]*dnode, 0, len(st.Containers))
	containers := append([]core.Container{}, st.Containers...)
	sort.Slice(containers, func(i, j int) bool {
		if containers[i].Service != containers[j].Service {
			return containers[i].Service < containers[j].Service
		}
		return containers[i].Name < containers[j].Name
	})
	for _, c := range containers {
		kids = append(kids, s.containerNode(n, c, depth+1, parent))
	}
	var bytes int64
	if n.disk != nil {
		bytes = n.disk.Bytes
	}
	return &dnode{row: row, facts: s.stackFacts(n, run, row.Reason, parent), expanded: expanded, kids: kids, disk: bytes}
}

func (s *State) stackFacts(n *stNode, run treetable.Run, reason string, parent *wtNode) *facts {
	st := n.st
	repo, branch, inherited := "", "", ""
	if parent != nil {
		pf := s.worktreeFacts(parent, s.branchText(parent), "")
		repo, branch, inherited = pf.repo, pf.branch, pf.text
	} else if st.Common != nil {
		repo = strings.ToLower(s.repos[*st.Common].Name)
	}
	dir := ""
	if st.Dir != nil {
		dir = treetable.Sanitize(*st.Dir)
	}
	f := &facts{
		repo:   repo,
		branch: branch,
		text:   strings.ToLower(strings.Join([]string{inherited, repo, treetable.Sanitize(st.Project), dir, reason}, "\x00")),
		state:  stateFilterWord(run),
		is: map[string]bool{
			"orphan": parent == nil,
			"stale":  n.age != nil && n.age.Stale,
			"marked": s.marks["stack:"+st.Project],
		},
	}
	return f
}

// branchText is what the branch column says: the branch, or "(detached) 9ac1e2".
func (s *State) branchText(n *wtNode) string {
	switch {
	case n.wt.Detached:
		head := ""
		if n.wt.Head != nil {
			head = *n.wt.Head
			if len(head) > 6 {
				head = head[:6]
			}
		}
		return strings.TrimSpace("(detached) " + treetable.Sanitize(head))
	case n.wt.Branch != nil:
		return treetable.Sanitize(*n.wt.Branch)
	}
	return ""
}

// parentOf is the worktree that a stack hangs under, or nil.
func (s *State) parentOf(n *stNode) *wtNode {
	if n.st.Worktree != nil {
		return s.worktrees[*n.st.Worktree]
	}
	return nil
}

func (s *State) containerNode(n *stNode, c core.Container, depth int, parent *wtNode) *dnode {
	word, run := containerWord(c.State)
	row := treetable.Row{
		ID:   "container:" + n.st.Project + "/" + c.Name,
		Kind: treetable.KindContainer, Depth: depth,
		Name: treetable.Sanitize(c.Service), Word: word, Run: run,
		Disk: treetable.NA(),
	}
	if row.Name == "" {
		row.Name = treetable.Sanitize(c.Name)
	}
	if c.Health != nil {
		row.Health = treetable.Sanitize(*c.Health)
	}
	for _, p := range c.Published {
		row.Ports = append(row.Ports, ":"+treetable.Sanitize(p))
	}
	row.CPU, row.Mem = s.containerLive(n, c)
	row.Age = treetable.NA()
	if v, ok := statusSeconds(c.Status); ok {
		row.Age = treetable.Value(FormatRowAge(v)).WithTone(theme.ToneDim)
	}
	repo, branch, inherited := "", "", ""
	if parent != nil {
		pf := s.worktreeFacts(parent, s.branchText(parent), "")
		repo, branch, inherited = pf.repo, pf.branch, pf.text
	}
	state := ""
	switch run {
	case treetable.RunUp:
		state = "running"
	case treetable.RunExited:
		state = "exited"
	}
	f := &facts{
		repo: repo, branch: branch, state: state,
		text: strings.ToLower(strings.Join([]string{inherited, treetable.Sanitize(n.st.Project), treetable.Sanitize(c.Service), treetable.Sanitize(c.Name)}, "\x00")),
		is:   map[string]bool{},
	}
	return &dnode{row: row, facts: f}
}

func containerWord(state string) (string, treetable.Run) {
	switch state {
	case "running":
		return "running", treetable.RunUp
	case "restarting":
		return "restart", treetable.RunPartial
	case "paused":
		return "paused", treetable.RunExited
	case "removing":
		return "remove", treetable.RunDown
	case "exited", "dead", "created":
		return state, treetable.RunExited
	}
	return treetable.Sanitize(state), treetable.RunNone
}

// ---- cells

// liveCells is CPU and memory of a stack. What does not run is not applicable. What
// runs is loading until the stats stage ends; then a number that this round did not
// renew is stale, and a number that never came is failed.
func (s *State) liveCells(n *stNode, running int) (treetable.Cell, treetable.Cell) {
	if running == 0 {
		return treetable.NA(), treetable.NA()
	}
	switch s.stages[core.StageStats] {
	case stageDone:
		if n.stats == nil {
			return treetable.Failed(), treetable.Failed()
		}
		if !n.statsFresh {
			return treetable.Stale(FormatCPU(n.stats.CPUPct)), treetable.Stale(FormatMem(n.stats.MemBytes))
		}
	default:
		if n.stats == nil {
			return treetable.Loading(), treetable.Loading()
		}
	}
	return treetable.Value(FormatCPU(n.stats.CPUPct)), treetable.Value(FormatMem(n.stats.MemBytes))
}

func (s *State) containerLive(n *stNode, c core.Container) (treetable.Cell, treetable.Cell) {
	if c.State != "running" {
		return treetable.NA(), treetable.NA()
	}
	if n.stats != nil {
		for _, v := range n.stats.Containers {
			if v.ID == c.ID {
				if s.stages[core.StageStats] == stageDone && !n.statsFresh {
					return treetable.Stale(FormatCPU(v.CPUPct)), treetable.Stale(FormatMem(v.MemBytes))
				}
				return treetable.Value(FormatCPU(v.CPUPct)), treetable.Value(FormatMem(v.MemBytes))
			}
		}
	}
	if s.stages[core.StageStats] == stageDone {
		return treetable.Failed(), treetable.Failed()
	}
	return treetable.Loading(), treetable.Loading()
}

func (s *State) diskCell(n *stNode) treetable.Cell {
	if n.diskPending {
		return treetable.Loading()
	}
	if n.disk != nil {
		text := FormatDisk(n.disk.Bytes)
		if n.disk.AgeS != nil && *n.disk.AgeS > staleDiskSeconds {
			return treetable.Stale(text)
		}
		return treetable.Value(text)
	}
	if s.stages[core.StageDisk] != stageDone {
		return treetable.Loading()
	}
	// The cache has no size for this project. Before the first scan a size will come;
	// after it, none will.
	if s.diskTotal == nil || s.diskTotal.AgeS == nil {
		if s.diskTotal != nil && s.diskTotal.Refreshing {
			return treetable.Loading()
		}
		return treetable.Failed()
	}
	return treetable.NA()
}

func (s *State) stackAgeCell(n *stNode) treetable.Cell {
	if n.age == nil {
		if s.stages[core.StageStats] == stageDone || n.st.Kind == core.StackVolumes {
			return treetable.NA()
		}
		return treetable.Loading()
	}
	var at *int64
	if n.age.Running {
		at = n.age.LastStartedAt
	} else {
		at = n.age.LastFinishedAt
	}
	if at == nil {
		return treetable.NA()
	}
	cell := treetable.Value(FormatRowAge(s.now().Unix() - *at)).WithTone(theme.ToneDim)
	if n.age.Stale {
		cell = cell.WithTone(theme.ToneWarn)
	}
	return cell
}

// rollup picks the state of a parent cell from its children: one failed child makes it
// failed, then a loading one makes it loading, because a sum without them would be a
// wrong number that reads as a right one. All not applicable stays not applicable.
func rollup(cells []treetable.Cell, sum string) treetable.Cell {
	values, stale := 0, false
	for _, c := range cells {
		switch c.State {
		case treetable.CellFailed:
			return treetable.Failed()
		case treetable.CellLoading:
			return treetable.Loading()
		case treetable.CellStale:
			stale = true
			values++
		case treetable.CellValue:
			values++
		}
	}
	switch {
	case values == 0:
		return treetable.NA()
	case stale:
		return treetable.Stale(sum)
	}
	return treetable.Value(sum)
}
