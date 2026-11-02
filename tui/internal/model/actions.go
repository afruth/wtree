package model

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/afruth/wtree/tui/internal/core"
	"github.com/afruth/wtree/tui/internal/dialog"
	"github.com/afruth/wtree/tui/internal/theme"
	"github.com/afruth/wtree/tui/internal/tree"
	"github.com/afruth/wtree/tui/internal/treetable"
)

// Action ids. A key, a button and a sheet item all come to run with one of these.
const (
	actOpen       = "open"
	actStop       = "stop"
	actDelete     = "delete"
	actDeleteKeep = "delete-keep"
	actDeleteVols = "delete-volumes"
	actExpand     = "expand"
	actMark       = "mark"
	actMarkAll    = "mark-all"
	actUnmarkAll  = "unmark-all"
	actCopy       = "copy"
	actNew        = "new"
	actPrint      = "print"
	actPanel      = "panel"
	actCleanup    = "cleanup"
	actSearch     = "search"
	actActions    = "actions"
	actOpenMarks  = "open-marks"
	actClearFilt  = "clear-filter"
	actApplySrch  = "apply-search"
	actClearSrch  = "clear-search"
)

func (m *Model) loadingText() string { return "loading" + m.th.G.Ellipsis + " try again" }

func (m *Model) cursorInfo() (tree.Info, bool) {
	id := m.table.Cursor()
	if id == "" {
		return tree.Info{}, false
	}
	return m.tree.Lookup(id)
}

func runningOf(st *core.Stack) int {
	if st == nil {
		return 0
	}
	return st.States["running"]
}

func stackHasDir(st *core.Stack) (bool, string) {
	if st == nil {
		return false, ""
	}
	switch st.Kind {
	case core.StackGone:
		return false, "its directory is gone"
	case core.StackVolumes:
		return false, "only volumes are left"
	}
	if st.Dir == nil {
		return false, "it has no directory"
	}
	return true, ""
}

// neverReason is the reason of the core when it says that a row can never go. The tier
// comes from the core, and the TUI only repeats it.
func neverReason(info tree.Info) string {
	if info.Cleanup != nil && info.Cleanup.Tier == core.TierNever && len(info.Cleanup.Reasons) > 0 {
		return treetable.Sanitize(info.Cleanup.Reasons[0].Text)
	}
	return ""
}

// neverPhrase is the same reason as a sentence about the row: "proj is the main checkout".
func (m *Model) neverPhrase(info tree.Info) string {
	reason := neverReason(info)
	if rest, ok := strings.CutPrefix(reason, "it "); ok && info.Worktree != nil {
		return lastSegment(treetable.Sanitize(info.Worktree.Path)) + " " + rest
	}
	return reason
}

func (m *Model) stopItem(info tree.Info) dialog.Item {
	it := dialog.Item{Label: "stop", Action: actStop, Tone: theme.ToneAccent}
	if !m.structureReady() {
		it.Disabled, it.Reason = true, "still loading"
		return it
	}
	running := 0
	switch info.Kind {
	case treetable.KindWorktree:
		it.Label = "stop its stacks"
		for i := range info.Stacks {
			running += runningOf(&info.Stacks[i])
		}
	case treetable.KindStack:
		running = runningOf(info.Stack)
	default:
		it.Disabled, it.Reason = true, "stop acts on a whole stack"
		return it
	}
	if running == 0 {
		it.Disabled, it.Reason = true, "nothing is running"
	} else {
		it.Right = itoa(running) + " running"
	}
	return it
}

// sheetItems lists the actions of the cursor row, with the contextual primary first.
func (m *Model) sheetItems(info tree.Info) []dialog.Item {
	var items []dialog.Item
	switch info.Kind {
	case treetable.KindWorktree:
		w := info.Worktree
		path := treetable.Sanitize(tildeOf(w.Path, m.o.Home))
		open := dialog.Item{Label: "open", Right: path, Path: true, Action: actOpen, Tone: theme.ToneAccent}
		switch {
		case w.Odd:
			open.Disabled, open.Reason = true, "its path cannot be passed on"
		case w.Missing:
			open.Disabled, open.Reason = true, "its directory is gone"
		}
		del := dialog.Item{Label: "delete worktree", Right: m.deleteWorktreeText(info), Action: actDelete, Tone: theme.ToneWarn}
		switch reason := neverReason(info); {
		case w.Odd:
			del.Disabled, del.Reason = true, "its path cannot be passed on"
		case reason != "":
			del.Disabled, del.Reason = true, reason
		case !m.structureReady():
			del.Disabled, del.Reason = true, "still loading"
		}
		items = append(items, open, m.stopItem(info), del)
		if len(info.Stacks) > 0 {
			items = append(items, m.expandItem(info.ID, dialog.Plural(len(info.Stacks), "stack")))
		}
		items = append(items, m.markItem(info.ID), dialog.Item{Label: "copy path", Right: path, Path: true, Action: actCopy}, m.newItem(), m.printItem(w))
	case treetable.KindStack:
		st := info.Stack
		stop := m.stopItem(info)
		path := ""
		if st.Dir != nil {
			path = treetable.Sanitize(tildeOf(*st.Dir, m.o.Home))
		}
		open := dialog.Item{Label: "open directory", Right: path, Path: true, Action: actOpen}
		if ok, reason := stackHasDir(st); !ok {
			open.Disabled, open.Reason = true, reason
		}
		keep := dialog.Item{Label: "delete, keep volumes", Right: "its volumes stay", Action: actDeleteKeep, Tone: theme.ToneWarn}
		vols := dialog.Item{Label: "delete with volumes", Right: "the data goes", Action: actDeleteVols, Tone: theme.ToneLoss}
		if info.Disk != nil {
			vols.Right = treetable.Sanitize(tree.FormatDisk(info.Disk.Bytes)) + " lost"
		}
		if st.Volumes != nil && *st.Volumes == 0 {
			// nothing to lose, so not the colour of loss. The plan of this level says the same.
			keep.Right, vols.Right, vols.Tone = "it has no volumes", "it has no volumes", theme.ToneWarn
		}
		if st.Kind == core.StackVolumes {
			keep.Disabled, keep.Reason = true, "only volumes are left"
		}
		if !m.structureReady() {
			keep.Disabled, keep.Reason = true, "still loading"
			vols.Disabled, vols.Reason = true, "still loading"
		}
		switch {
		case st.Kind == core.StackGone || st.Kind == core.StackVolumes:
			items = append(items, keep, vols, stop, open)
		case runningOf(st) > 0:
			items = append(items, stop, open, keep, vols)
		default:
			items = append(items, open, stop, keep, vols)
		}
		if len(st.Containers) > 0 {
			items = append(items, m.expandItem(info.ID, dialog.Plural(len(st.Containers), "container")))
		}
		items = append(items, m.markItem(info.ID))
		if st.Dir != nil {
			items = append(items, dialog.Item{Label: "copy path", Right: path, Path: true, Action: actCopy})
		}
	case treetable.KindContainer:
		items = append(items,
			dialog.Item{Label: "show in panel", Right: treetable.Sanitize(info.Container.Status), Action: actPanel, Tone: theme.ToneAccent},
			dialog.Item{Label: "copy name", Right: treetable.Sanitize(info.Container.Name), Action: actCopy},
			dialog.Item{Label: "stop", Action: actStop, Disabled: true, Reason: "stop acts on a whole stack"},
			dialog.Item{Label: "delete", Action: actDelete, Disabled: true, Reason: "delete acts on a whole stack"},
			dialog.Item{Label: "mark", Action: actMark, Disabled: true, Reason: "only worktrees and stacks take a mark"})
	default:
		items = append(items, dialog.Item{Label: "cleanup view", Right: "what is safe to delete", Action: actCleanup, Tone: theme.ToneAccent},
			dialog.Item{Label: "mark", Action: actMark, Disabled: true, Reason: "only worktrees and stacks take a mark"})
	}
	return items
}

// deleteWorktreeText says what stays with the level and the choice that the sheet opens on. The
// size is of its stacks: the list has no size for the worktree itself.
func (m *Model) deleteWorktreeText(info tree.Info) string {
	if len(info.Stacks) > 0 {
		for _, r := range m.table.Rows() {
			if r.ID == info.ID && r.Disk.State == treetable.CellValue {
				return "stacks stay (" + r.Disk.Text + ")"
			}
		}
		return "stacks stay"
	}
	if info.Worktree.Branch != nil {
		return "the branch stays"
	}
	return "its files go"
}

// printItem prints the path on the terminal after wtree ends. A path with a control character
// cannot be printed as it is, and a clean copy of it is another path.
func (m *Model) printItem(w *core.Worktree) dialog.Item {
	it := dialog.Item{Label: "print path and quit", Action: actPrint}
	if treetable.Sanitize(w.Path) != w.Path {
		it.Disabled, it.Reason = true, "its path cannot be passed on"
	}
	return it
}

func (m *Model) expandItem(id, what string) dialog.Item {
	label := "expand"
	if m.tree.IsExpanded(id) {
		label = "collapse"
	}
	return dialog.Item{Label: label, Right: what, Action: actExpand}
}

func (m *Model) markItem(id string) dialog.Item {
	label := "mark"
	if m.tree.IsMarked(id) {
		label = "unmark"
	}
	return dialog.Item{Label: label, Right: itoa(m.tree.MarkCounts().Total) + " marked", Action: actMark}
}

func (m *Model) newItem() dialog.Item {
	it := dialog.Item{Label: "new worktree", Action: actNew}
	if _, name := m.newRepo(); name != "" {
		it.Right = "in " + name
	}
	if !m.structureReady() {
		it.Disabled, it.Reason = true, "still loading"
	}
	return it
}

// markedInScope are the marks with a row in the data of this scope. A mark out of scope is
// kept, and it takes no part in an action.
func (m *Model) markedInScope() (worktrees, stacks []tree.Info) {
	for _, id := range m.tree.Marked() {
		info, ok := m.tree.Lookup(id)
		if !ok {
			continue
		}
		switch info.Kind {
		case treetable.KindWorktree:
			worktrees = append(worktrees, info)
		case treetable.KindStack:
			stacks = append(stacks, info)
		}
	}
	return worktrees, stacks
}

func (m *Model) markSentence() string {
	c := m.tree.MarkCounts()
	if c.Total == 0 {
		return ""
	}
	text := itoa(c.Total) + " marked"
	var extra []string
	if c.HiddenByFilter > 0 {
		extra = append(extra, itoa(c.HiddenByFilter)+" hidden by filter")
	}
	if c.OutOfScope > 0 {
		extra = append(extra, itoa(c.OutOfScope)+" out of scope")
	}
	if len(extra) > 0 {
		text += " (" + strings.Join(extra, ", ") + ")"
	}
	return text
}

// openMenu opens the action sheet of the marks, or of the cursor row.
func (m *Model) openMenu() tea.Cmd {
	if len(m.tree.Marked()) > 0 {
		m.menu = m.marksMenu()
		m.menuReturn = m.mode
		m.mode = ModeMenu
		return nil
	}
	info, ok := m.cursorInfo()
	if !ok {
		return m.say("no row here", theme.ToneDim)
	}
	title := m.rowTitle(info)
	m.menu = dialog.NewMenu(title, "", m.sheetItems(info))
	m.menuReturn = m.mode
	m.mode = ModeMenu
	return nil
}

func (m *Model) rowTitle(info tree.Info) string {
	switch info.Kind {
	case treetable.KindWorktree:
		return lastSegment(treetable.Sanitize(info.Worktree.Path))
	case treetable.KindStack:
		word := ""
		for _, r := range m.table.Rows() {
			if r.ID == info.ID {
				word = strings.TrimSpace(r.Word + " " + r.Count)
			}
		}
		return strings.TrimSpace(treetable.Sanitize(info.Stack.Project) + "  " + word)
	case treetable.KindContainer:
		return treetable.Sanitize(info.Container.Name)
	}
	return "cleanup"
}

func lastSegment(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[i+1:]
	}
	return path
}

// cursorNote is the first line of a sheet of the marks when the row under the cursor is none of them.
func (m *Model) cursorNote() string {
	if len(m.tree.Marked()) == 0 {
		return ""
	}
	if id := m.table.Cursor(); id != "" && !m.tree.IsMarked(id) {
		return "The row under the cursor is not marked, so it is not included."
	}
	return ""
}

func (m *Model) marksMenu() dialog.Menu {
	worktrees, stacks := m.markedInScope()
	var note string
	note = m.cursorNote()
	items := []dialog.Item{}
	running := 0
	for i := range stacks {
		running += runningOf(stacks[i].Stack)
	}
	for i := range worktrees {
		for j := range worktrees[i].Stacks {
			running += runningOf(&worktrees[i].Stacks[j])
		}
	}
	stop := dialog.Item{Label: "stop", Action: actStop, Tone: theme.ToneAccent}
	del := dialog.Item{Label: "delete", Action: actDelete, Tone: theme.ToneWarn}
	switch {
	case !m.structureReady():
		stop.Disabled, stop.Reason = true, "still loading"
		del.Disabled, del.Reason = true, "still loading"
	case running == 0:
		stop.Disabled, stop.Reason = true, "nothing is running"
	}
	if running > 0 {
		stop.Right = itoa(running) + " running"
	}
	if len(worktrees)+len(stacks) == 0 {
		del.Disabled, del.Reason = true, "all marks are out of scope"
	}
	var what []string
	if len(worktrees) > 0 {
		what = append(what, dialog.Plural(len(worktrees), "worktree"))
	}
	if len(stacks) > 0 {
		what = append(what, dialog.Plural(len(stacks), "stack"))
	}
	del.Right = strings.Join(what, ", ")
	items = append(items, stop, del)
	if len(stacks) == 0 && len(worktrees) > 0 {
		items = append(items, dialog.Item{Label: "open them all", Right: dialog.Plural(len(worktrees), "worktree"), Action: actOpenMarks})
	}
	items = append(items, dialog.Item{Label: "unmark all", Right: itoa(m.tree.MarkCounts().Total) + " marked", Action: actUnmarkAll})
	return dialog.NewMenu(m.markSentence(), note, items)
}

// run does an action. It is the one way in for a key, a button and a sheet item. A
// destructive action that comes early is dropped, not queued.
func (m *Model) run(action string) tea.Cmd {
	gated := action == actStop || action == actDelete || action == actDeleteKeep || action == actDeleteVols || action == actNew
	if gated && !m.structureReady() {
		return m.say(m.loadingText(), theme.ToneWarn)
	}
	if gated && m.planPending {
		return nil
	}
	switch action {
	case actOpen:
		return m.doOpen()
	case actStop:
		return m.doStop()
	case actDelete:
		return m.doDelete(false)
	case actDeleteKeep:
		return m.doDelete(false)
	case actDeleteVols:
		return m.doDelete(true)
	case actExpand:
		if id := m.table.Cursor(); id != "" {
			m.tree.Toggle(id)
		}
	case actMark:
		return m.doMark()
	case actMarkAll:
		n := m.tree.MarkAll()
		return m.say("marked "+itoa(n)+" more", theme.ToneAccent)
	case actUnmarkAll:
		m.tree.ClearMarks()
	case actCopy:
		return m.doCopy()
	case actNew:
		return m.doNew()
	case actPrint:
		return m.doPrint()
	case actPanel:
		return m.doPanel()
	case actCleanup:
		return m.toggleCleanup()
	case actSearch:
		return m.openSearch()
	case actActions:
		return m.openMenu()
	case actOpenMarks:
		return m.doOpenMarks()
	case actClearFilt:
		m.tree.SetFilter("")
		m.search.SetValue("")
	case actApplySrch:
		return m.applySearch()
	case actClearSrch:
		return m.clearSearch()
	}
	return nil
}

func (m *Model) doOpen() tea.Cmd {
	info, ok := m.cursorInfo()
	if !ok {
		return nil
	}
	dir, name := "", ""
	switch info.Kind {
	case treetable.KindWorktree:
		w := info.Worktree
		switch {
		case w.Odd:
			return m.say("its path cannot be passed on", theme.ToneDim)
		case w.Missing:
			return m.say("open: its directory is gone", theme.ToneDim)
		case w.Current:
			return m.sayFor("already here", theme.ToneDim, 2*time.Second)
		}
		dir, name = w.Path, lastSegment(w.Path)
	case treetable.KindStack:
		if ok, reason := stackHasDir(info.Stack); !ok {
			return m.openMenuWithStatus("open: " + reason)
		}
		dir, name = *info.Stack.Dir, info.Stack.Project
	case treetable.KindContainer:
		return m.doPanel()
	default:
		return nil
	}
	return m.startOpen(dir, treetable.Sanitize(name))
}

func (m *Model) openMenuWithStatus(text string) tea.Cmd {
	return tea.Batch(m.openMenu(), m.say(text, theme.ToneDim))
}

func (m *Model) startOpen(dir, name string) tea.Cmd {
	if m.writer != nil || m.writerOn {
		return m.say("wtree is busy", theme.ToneDim)
	}
	m.opening = name
	m.openLines, m.openOK = nil, false
	m.writerKind, m.writerOn = kindOpen, true
	client, cwd := m.client, m.o.Cwd
	cmd := startWriter(m.writerBox, kindOpen, func() (*core.Stream, error) {
		return client.Open(context.Background(), core.OpenRequest{Cwd: cwd, Dirs: []string{dir}})
	})
	return tea.Batch(cmd, m.say("opening "+name+m.th.G.Ellipsis, theme.ToneAccent))
}

func (m *Model) doPrint() tea.Cmd {
	info, ok := m.cursorInfo()
	if !ok || info.Worktree == nil {
		return nil
	}
	if treetable.Sanitize(info.Worktree.Path) != info.Worktree.Path {
		return m.say("its path cannot be passed on", theme.ToneDim)
	}
	m.printOnEnd = info.Worktree.Path
	return m.quitNow()
}

func (m *Model) doStop() tea.Cmd {
	worktrees, stacks := m.markedInScope()
	// The sheet shows marks that the filter hides, and what the cursor row is not. Only one row
	// that the user is on, by key, runs at once.
	sheet := m.inTap || len(worktrees)+len(stacks) > 0
	if len(worktrees)+len(stacks) == 0 {
		info, ok := m.cursorInfo()
		if !ok {
			return nil
		}
		switch info.Kind {
		case treetable.KindWorktree:
			worktrees = []tree.Info{info}
		case treetable.KindStack:
			stacks = []tree.Info{info}
		default:
			return m.say("stop: this acts on a whole stack", theme.ToneDim)
		}
	}
	seen := map[string]bool{}
	var targets []string
	add := func(st *core.Stack) {
		if runningOf(st) > 0 && !seen[st.Project] {
			seen[st.Project] = true
			targets = append(targets, st.Project)
		}
	}
	for i := range stacks {
		add(stacks[i].Stack)
	}
	for i := range worktrees {
		for j := range worktrees[i].Stacks {
			add(&worktrees[i].Stacks[j])
		}
	}
	if len(targets) == 0 {
		return m.say("stop: nothing is running", theme.ToneDim)
	}
	m.queue, m.queueTotal, m.queuePart = nil, 0, 0
	cmd := m.planCmd(core.PlanRequest{Action: core.ActionStopStacks, Cwd: m.o.Cwd, Targets: targets}, false)
	m.planSheet = sheet
	return cmd
}

func (m *Model) worktreeRequest(infos []tree.Info) core.PlanRequest {
	req := core.PlanRequest{Action: core.ActionDeleteWorktrees, Cwd: m.o.Cwd}
	for _, info := range infos {
		req.Targets = append(req.Targets, info.Worktree.Path)
		if m.machine() {
			req.Repos = append(req.Repos, info.Worktree.Common)
		}
	}
	return req
}

func stackRequest(infos []tree.Info, cwd string, volumes bool) core.PlanRequest {
	req := core.PlanRequest{Action: core.ActionDeleteStacks, Cwd: cwd, Volumes: volumes}
	for _, info := range infos {
		req.Targets = append(req.Targets, info.Stack.Project)
	}
	return req
}

// doDelete asks for the plans of a delete. Marks of both kinds make two plans, each with
// its own sheet: the stacks first, so that the worktrees see what stacks are left.
func (m *Model) doDelete(volumes bool) tea.Cmd {
	worktrees, stacks := m.markedInScope()
	if len(m.tree.Marked()) > 0 && len(worktrees)+len(stacks) == 0 {
		return m.say("all marks are out of scope", theme.ToneDim)
	}
	if len(worktrees)+len(stacks) == 0 {
		info, ok := m.cursorInfo()
		if !ok {
			return nil
		}
		switch info.Kind {
		case treetable.KindWorktree:
			if info.Worktree.Odd {
				return m.say("delete: its path cannot be passed on", theme.ToneDim)
			}
			// The core says that this row can never go, whatever the level: no sheet to end in.
			if phrase := m.neverPhrase(info); phrase != "" {
				return m.say("delete: "+phrase, theme.ToneDim)
			}
			worktrees = []tree.Info{info}
		case treetable.KindStack:
			stacks = []tree.Info{info}
		default:
			return m.say("delete: this acts on a whole stack", theme.ToneDim)
		}
	}
	m.queue, m.queueTotal, m.queuePart = nil, 0, 0
	if len(stacks) > 0 {
		m.queue = append(m.queue, stackRequest(stacks, m.o.Cwd, volumes))
	}
	if len(worktrees) > 0 {
		m.queue = append(m.queue, m.worktreeRequest(worktrees))
	}
	m.queueTotal = len(m.queue)
	return m.nextInQueue()
}

func (m *Model) doOpenMarks() tea.Cmd {
	worktrees, _ := m.markedInScope()
	req := core.PlanRequest{Action: core.ActionOpen, Cwd: m.o.Cwd}
	for _, info := range worktrees {
		req.Targets = append(req.Targets, info.Worktree.Path)
	}
	m.queue, m.queueTotal, m.queuePart = nil, 0, 0
	return m.planCmd(req, false)
}

func (m *Model) doMark() tea.Cmd { return m.markRow(m.table.Cursor()) }

func (m *Model) markRow(id string) tea.Cmd {
	if id == "" {
		return nil
	}
	info, ok := m.tree.Lookup(id)
	if !ok {
		return nil
	}
	if info.Kind == treetable.KindContainer || info.Kind == treetable.KindGroup {
		return m.say("only worktrees and stacks take a mark", theme.ToneDim)
	}
	was := m.tree.IsMarked(id)
	if m.tree.ToggleMark(id) == was && !was {
		reason := neverReason(info)
		if reason == "" {
			reason = "wtree cannot act on this row"
		}
		return m.say("no mark: "+reason, theme.ToneDim)
	}
	return nil
}

func (m *Model) doCopy() tea.Cmd {
	info, ok := m.cursorInfo()
	if !ok {
		return nil
	}
	text := ""
	switch info.Kind {
	case treetable.KindWorktree:
		text = info.Worktree.Path
	case treetable.KindStack:
		if info.Stack.Dir != nil {
			text = *info.Stack.Dir
		} else {
			text = info.Stack.Project
		}
	case treetable.KindContainer:
		text = info.Container.Name
	default:
		return nil
	}
	clean := treetable.Sanitize(text)
	// OSC 52 cannot say if it worked, so the text is on the screen too.
	return tea.Batch(tea.SetClipboard(clean), m.sayFor("copied "+clean, theme.ToneDim, 2*statusShort))
}

// newRepo says which repo a new worktree goes in: in machine scope the repo of the row.
func (m *Model) newRepo() (common, name string) {
	if !m.machine() {
		return "", m.repoName
	}
	info, ok := m.cursorInfo()
	if !ok || info.Worktree == nil {
		return "", ""
	}
	return info.Worktree.Common, treetable.Sanitize(info.Worktree.Repo)
}

func (m *Model) newBlocked() string {
	if m.machine() {
		if info, ok := m.cursorInfo(); !ok || info.Worktree == nil {
			return "new worktree: choose a repo row first"
		}
	}
	return ""
}

func (m *Model) doNew() tea.Cmd {
	if reason := m.newBlocked(); reason != "" {
		return m.say(reason, theme.ToneDim)
	}
	if m.newPending {
		return nil
	}
	repo, _ := m.newRepo()
	m.newPending = true
	m.planSeq++
	m.planPending = false
	req := core.NewRequest{Cwd: m.o.Cwd, Repo: repo, DryRun: true}
	client, ctx := m.client, m.ctx
	return startStream(kindDryRun, m.planSeq, func() (*core.Stream, error) { return client.New(ctx, req) })
}

func (m *Model) submitNew() tea.Cmd {
	s := &m.newSheet
	if s.Busy || m.writer != nil {
		return nil
	}
	if dialog.NameProblem(s.Input.Value()) != "" {
		// The field is red and says why, so no second message: it flashes, and nothing is sent.
		s.Flash = true
		return m.timer(flashFor, newFlashMsg{})
	}
	if m.writerOn {
		s.Err = "wtree is busy: an open still runs"
		return nil
	}
	s.Busy, s.Err = true, ""
	req := core.NewRequest{Cwd: m.o.Cwd, Repo: s.Repo, Name: s.Input.Value(), NoOpen: !s.Open}
	m.newRequest = req
	m.openLines, m.openOK = nil, false
	m.writerKind, m.writerOn = kindNew, true
	client := m.client
	return startWriter(m.writerBox, kindNew, func() (*core.Stream, error) { return client.New(context.Background(), req) })
}

func (m *Model) doPanel() tea.Cmd {
	if m.layout.PanelAllowed() {
		m.panel, m.panelSet = !m.panelOn(), true
		m.panelScroll = 0
		return nil
	}
	m.info = m.panelSheet()
	m.mode = ModePanel
	return nil
}

// toggleCleanup opens the view on the first row that it is about, and goes back to the row
// that it came from. It only moves the cursor: a delete still has its sheet.
func (m *Model) toggleCleanup() tea.Cmd {
	if m.tree.View() == tree.ViewCleanup {
		m.tree.SetView(tree.ViewTree)
		m.wantCursor, m.cleanupFrom = m.cleanupFrom, ""
		return nil
	}
	m.cleanupFrom = m.table.Cursor()
	m.tree.SetView(tree.ViewCleanup)
	for _, r := range m.tree.Visible() {
		if (r.Kind == treetable.KindWorktree || r.Kind == treetable.KindStack) && !r.Dim {
			m.wantCursor = r.ID
			break
		}
	}
	return nil
}

func (m *Model) setScope(machine bool) tea.Cmd {
	if machine == m.machine() {
		return nil
	}
	if !machine && m.noRepo {
		return m.say("wtree did not start in a repo", theme.ToneDim)
	}
	m.scopeSet = true
	m.wantCursor = m.table.Cursor()
	scope := tree.ScopeRepo
	if machine {
		scope = tree.ScopeMachine
	}
	m.tree.SetScope(scope)
	return m.startFullList()
}

func (m *Model) openSearch() tea.Cmd {
	m.mode = ModeSearch
	return m.search.Focus()
}

func (m *Model) applySearch() tea.Cmd {
	m.mode = ModeList
	m.search.Blur()
	f := m.tree.SetFilter(m.search.Value())
	m.seekMatch = true
	if f.Invalid() {
		return m.say(f.Error(), theme.ToneDanger)
	}
	return nil
}

func (m *Model) clearSearch() tea.Cmd {
	m.mode = ModeList
	m.search.Blur()
	m.search.SetValue("")
	m.tree.SetFilter("")
	return nil
}

// onCycleSort: the header says what the order is, so the status line does not.
func (m *Model) onCycleSort() tea.Cmd {
	m.sortSet = true
	m.tree.CycleSort()
	return nil
}

func (m *Model) sortByTitle(title string) tea.Cmd {
	var want tree.SortMode
	switch title {
	case "NAME":
		m.sortSet = true
		m.tree.SetSort(tree.SortTree, false)
		return nil
	case "CPU":
		want = tree.SortCPU
	case "DISK":
		want = tree.SortDisk
	default:
		return m.say("the list cannot sort by "+title, theme.ToneDim)
	}
	m.sortSet = true
	mode, reversed := m.tree.Sort()
	if mode == want {
		m.tree.SetSort(want, !reversed)
		return nil
	}
	m.tree.SetSort(want, false)
	return nil
}
