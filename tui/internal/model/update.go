package model

import (
	"context"
	"errors"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/afruth/wtree/tui/internal/core"
	"github.com/afruth/wtree/tui/internal/dialog"
	"github.com/afruth/wtree/tui/internal/theme"
	"github.com/afruth/wtree/tui/internal/tree"
	"github.com/afruth/wtree/tui/internal/treetable"
	"github.com/charmbracelet/x/ansi"
)

// Update takes one message. It changes the model and returns Cmds, and it does no I/O.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.nowAt = m.now()
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		cmd = m.onResize(msg.Width, msg.Height)
	case settleMsg:
		if msg.gen == m.resizeGen {
			m.settling = false
			// #1780: a frame can stay stale one tick after a resize. A clear and a second
			// render 50 ms later make a rotation exactly two frames.
			cmd = tea.Batch(tea.ClearScreen, m.timer(redrawAfter, redrawMsg{}))
		}
	case redrawMsg:
	case tea.KeyPressMsg:
		cmd = m.onKey(msg)
	case tea.PasteMsg:
		cmd = m.onPaste(msg)
	case tea.MouseClickMsg:
		cmd = m.onPress(tea.Mouse(msg))
	case tea.MouseReleaseMsg:
		cmd = m.onRelease(tea.Mouse(msg))
	case tea.MouseMotionMsg:
		m.onMotion(tea.Mouse(msg))
	case tea.MouseWheelMsg:
		m.onWheel(tea.Mouse(msg))
	case pressExpireMsg:
		cmd = m.onPressExpire(msg.gen)
	case streamMsg:
		cmd = m.onStream(msg)
	case planMsg:
		cmd = m.onPlan(msg)
	case statsTickMsg:
		m.tickArmed = false
		cmd = m.onTick()
	case statusExpireMsg:
		if msg.gen == m.statusGen {
			m.status = ""
		}
	case spinMsg:
		m.spinArmed = false
		m.spin++
	case newFlashMsg:
		m.newSheet.Flash = false
	case flashClearMsg:
		if msg.gen == m.flashGen {
			m.table.SetFlash(nil)
		}
	case tea.FocusMsg:
		m.blurred = false
	case tea.BlurMsg:
		m.blurred = true
	case tea.BackgroundColorMsg:
		if !m.o.ThemeForced && m.o.Prefs.Theme == "" {
			m.setLight(!msg.IsDark())
		}
	case logSavedMsg:
		m.lastLogPath = msg.path
		if m.mode == ModeLog {
			m.logView.Path = msg.path
		}
	case SignalMsg:
		cmd = m.quitNow()
	case panicMsg:
		panic("wtree-tui: --panic-after")
	}
	m.sync()
	return m, tea.Batch(cmd, m.keepTicking(), m.keepSpinning(), m.savePrefs())
}

func (m *Model) clearWriter() {
	m.writer = nil
	m.writerOn = false
	m.writerBox.Store(nil)
}

func (m *Model) quitNow() tea.Cmd {
	m.quitting = true
	return tea.Quit
}

// ---- resize

func (m *Model) onResize(w, h int) tea.Cmd {
	first := m.w == 0
	changed := w != m.w || h != m.h
	m.w, m.h = w, h
	if changed && !first {
		// The buttons move. A focus on confirm and a press from before that mean nothing now.
		if m.mode == ModeConfirm {
			m.confirm.Focus = dialog.FocusCancel
		}
		m.press = nil
		m.pressGen++
	}
	m.layout = theme.LayoutFor(w, h)
	m.table.SetLayout(m.layout)
	m.resizeGen++
	if m.mode == ModeHelp {
		m.refreshHelp() // its top line names the tier and the size
	}
	settle := m.timer(settleAfter, settleMsg{m.resizeGen})
	if first {
		return nil
	}
	if !m.settling {
		m.settling = true
		return tea.Batch(tea.ClearScreen, settle)
	}
	return settle
}

// ---- status line

func (m *Model) say(text string, tone theme.Tone) tea.Cmd {
	return m.sayFor(text, tone, statusShort)
}

func (m *Model) sayFor(text string, tone theme.Tone, d time.Duration) tea.Cmd {
	m.status, m.statusTone = treetable.Sanitize(text), tone
	m.statusMark = false
	m.statusGen++
	return m.timer(d, statusExpireMsg{m.statusGen})
}

// ---- the list

func (m *Model) listOptions() core.ListOptions {
	return core.ListOptions{Global: m.machine(), Cwd: m.o.Cwd}
}

// startFullList starts a list of all stages. All earlier lists stop.
func (m *Model) startFullList() tea.Cmd {
	m.token++
	m.loading = true
	m.loadStarted = m.nowAt
	m.statsRunning = false
	m.listErr = ""
	// The warnings stay until this list has said what is true now, so that a banner does
	// not go away and come back on every reload, and move the rows twice.
	m.warnSeen = map[string]bool{}
	client, ctx, opts := m.client, m.ctx, m.listOptions()
	return startStream(kindList, m.token, func() (*core.Stream, error) { return client.List(ctx, opts) })
}

func (m *Model) keepTicking() tea.Cmd {
	if m.tickArmed || m.quitting || !m.tree.StructureDone() || m.tickPaused() {
		return nil
	}
	m.tickArmed = true
	return m.timer(statsEvery, statsTickMsg{})
}

// tickPaused holds the stats ticker: on blur, when a sheet that shows a plan is open or a
// batch runs, and when docker is absent, because a tick then reads nothing.
func (m *Model) tickPaused() bool {
	if m.blurred {
		return true
	}
	switch m.mode {
	case ModeConfirm, ModeProgress, ModeNew:
		return true
	}
	_, missing := m.warnings[core.WarnDockerMissing]
	_, down := m.warnings[core.WarnDockerDown]
	return missing || down
}

func (m *Model) onTick() tea.Cmd {
	if m.tickPaused() || m.statsRunning || m.loading {
		return nil
	}
	m.ticks++
	only := []string{core.StageStats}
	if m.ticks%diskEveryTick == 0 || m.diskScan {
		only = append(only, core.StageDisk)
	}
	if m.cleanStale {
		// The totals of the cleanup view were made before there were sizes. The contract runs
		// that stage only with git.
		m.cleanStale = false
		only = []string{core.StageGit, core.StageStats, core.StageDisk, core.StageCleanup}
	}
	opts := m.listOptions()
	opts.Only = only
	opts.NoRefresh = true
	m.statsRunning = true
	client, ctx := m.client, m.ctx
	return startStream(kindStats, m.token, func() (*core.Stream, error) { return client.List(ctx, opts) })
}

func (m *Model) keepSpinning() tea.Cmd {
	if m.spinArmed || m.quitting || !m.motionAllowed() || !m.busy() {
		return nil
	}
	m.spinArmed = true
	return m.timer(spinEvery, spinMsg{})
}

// motionAllowed: no spinners on narrow or on a slow link.
func (m *Model) motionAllowed() bool {
	return !m.o.Caps.Slow && m.layout.Tier != theme.TierNarrow
}

func (m *Model) onStream(msg streamMsg) tea.Cmd {
	switch msg.kind {
	case kindList, kindStats:
		return m.onListStream(msg)
	case kindApply:
		return m.onApplyStream(msg)
	case kindOpen:
		return m.onOpenStream(msg)
	case kindNew:
		return m.onNewStream(msg)
	case kindDryRun:
		return m.onDryRunStream(msg)
	}
	return nil
}

func (m *Model) onListStream(msg streamMsg) tea.Cmd {
	if msg.token != m.token {
		if msg.stream != nil {
			msg.stream.Close()
		}
		return nil
	}
	var cmds []tea.Cmd
	for _, ev := range msg.events {
		if cmd := m.applyListEvent(ev); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	// Only a live update of a list that holds still: a scope switch, a reload and the first fill
	// bring new values all over, and that is no news.
	if len(msg.events) > 0 && m.everLoaded && msg.kind == kindStats && !m.loading {
		m.flashArmed = true
	}
	if !msg.done {
		cmds = append(cmds, readBatch(msg.kind, msg.token, msg.stream, m.uiDone))
		return tea.Batch(cmds...)
	}
	if msg.kind == kindStats {
		m.statsRunning = false
		if mode, _ := m.tree.Sort(); mode != tree.SortTree && m.nowAt.Sub(m.lastMove) >= sortStill {
			m.tree.Resort()
		}
		return tea.Batch(cmds...)
	}
	m.loading = false
	m.everLoaded = true
	if cmd := m.onListEnd(msg); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

func (m *Model) applyListEvent(ev any) tea.Cmd {
	switch e := ev.(type) {
	case core.BadLine:
		text := e.Text
		if e.Err != nil {
			text = e.Err.Error()
		}
		return m.say(text, theme.ToneDanger)
	case core.RepoEvent:
		m.repoNames[e.Repo.Common] = treetable.Sanitize(e.Repo.Name)
		if m.repoName == "" && !m.machine() {
			m.repoName = treetable.Sanitize(e.Repo.Name)
		}
	case core.WorktreeEvent:
		if e.Worktree.Current {
			m.repoName = treetable.Sanitize(e.Worktree.Repo)
		}
	case core.Warning:
		m.warnings[e.Code] = treetable.Sanitize(e.Message)
		m.warnSeen[e.Code] = true
	case core.StageEvent:
		if e.Stage == core.StageStructure && e.Done {
			for code := range m.warnings {
				if !m.warnSeen[code] {
					delete(m.warnings, code)
				}
			}
		}
	case core.DiskTotals:
		if e.AgeS != nil && (!m.diskKnown || (m.diskScan && !e.Refreshing)) && m.everLoaded {
			m.cleanStale = true
		}
		m.diskAt = m.nowAt
		m.diskKnown = e.AgeS != nil
		if e.AgeS != nil {
			m.diskAgeBase = *e.AgeS
			m.diskScan = e.Refreshing
		} else {
			m.diskScan = e.Refreshing
		}
	}
	m.tree.Apply(ev)
	return nil
}

func (m *Model) onListEnd(msg streamMsg) tea.Cmd {
	err := msg.err
	if err == nil {
		err = msg.result.Err
	}
	if err == nil || errors.Is(err, core.ErrSuperseded) || errors.Is(err, context.Canceled) {
		return nil
	}
	var coreErr *core.CoreError
	if errors.As(err, &coreErr) && coreErr.Code == core.ErrorNotARepo && !m.machine() {
		// outside a repo the default scope is the machine
		m.noRepo = true
		m.tree.SetScope(tree.ScopeMachine)
		return m.startFullList()
	}
	m.listErr = treetable.Sanitize(err.Error())
	return nil
}

// ---- plans

func (m *Model) planCmd(req core.PlanRequest, level bool) tea.Cmd {
	if m.planCancel != nil {
		m.planCancel()
	}
	m.planSeq++
	m.planPending = true
	m.planSheet = false
	ctx, cancel := context.WithCancel(m.ctx)
	m.planCancel = cancel
	seq, client := m.planSeq, m.client
	return func() tea.Msg {
		defer cancel()
		plan, err := client.Plan(ctx, req)
		return planMsg{seq: seq, req: req, plan: plan, err: err, level: level}
	}
}

func (m *Model) onPlan(msg planMsg) tea.Cmd {
	if msg.seq != m.planSeq || !m.planPending {
		return nil
	}
	m.planPending = false
	if msg.err != nil {
		if msg.level && m.mode == ModeConfirm {
			// Marks left the sheet, so the plan on it is not what the user asks for any more.
			if !sameTargets(msg.req.Targets, m.confirm.Req.Targets) {
				m.cancelConfirm()
				return m.say(msg.err.Error(), theme.ToneDanger)
			}
			m.confirm.Busy = false
			m.confirm.Notice = treetable.Sanitize(msg.err.Error())
			return nil
		}
		return m.say(msg.err.Error(), theme.ToneDanger)
	}
	if msg.level && m.mode == ModeConfirm {
		m.confirm.Req = msg.req
		m.confirm.SetPlan(msg.plan)
		m.confirm.Lonely = m.lonelyOf(msg.plan)
		m.confirm.Notice = ""
		m.armSheet()
		return nil
	}
	if m.mode != ModeList {
		return nil // the user went on; a plan only reads, so nothing is lost
	}
	return m.showPlan(msg.req, msg.plan)
}

func sameTargets(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// showPlan opens the consequences sheet, unless the plan says that it needs none: only a
// stop or an open of one target does, and it is run at once as the contract says.
func (m *Model) showPlan(req core.PlanRequest, plan core.Plan) tea.Cmd {
	if plan.NothingToDo && (plan.Action == core.ActionStopStacks || plan.Action == core.ActionOpen) {
		m.mode = ModeList
		text := "nothing to do"
		if len(plan.Items) > 0 && plan.Items[0].Reason != nil {
			text = treetable.Sanitize(plan.Items[0].Reason.Text)
		}
		return m.say(text, theme.ToneDim)
	}
	// Only a stop or an open can skip the sheet. A delete never does, whatever a plan says.
	reversible := plan.Action == core.ActionStopStacks || plan.Action == core.ActionOpen
	sheet := m.planSheet
	m.planSheet = false
	if reversible && plan.Confirm.Kind == core.ConfirmNone && !plan.NothingToDo && !sheet {
		if m.writerOn {
			return m.say("wtree is busy: an open still runs", theme.ToneDim)
		}
		m.mode = ModeList
		c := dialog.NewConfirm(req, plan, nil, nil)
		m.confirm = c
		m.autoClose = true
		return m.startApply(nil)
	}
	c := dialog.NewConfirm(req, plan, m.hiddenFor(plan), m.volumesOf(plan))
	c.Lonely = m.lonelyOf(plan)
	c.Note = m.cursorNote()
	if m.machine() {
		c.Repos = m.repoNames
	}
	if m.queueTotal > 1 {
		c.Part, c.Parts = m.queuePart, m.queueTotal
		if len(m.queue) > 0 {
			noun := "stack"
			if m.queue[0].Action == core.ActionDeleteWorktrees {
				noun = "worktree"
			}
			c.Next = dialog.Plural(len(m.queue[0].Targets), noun)
		}
	}
	m.confirm = c
	m.mode = ModeConfirm
	m.armSheet() // it came by itself, and what the user types now was not meant for it
	return nil
}

func (m *Model) volumesOf(plan core.Plan) map[string]int {
	out := map[string]int{}
	for _, it := range plan.Items {
		if info, ok := m.tree.Lookup(it.ID); ok && info.Stack != nil && info.Stack.Volumes != nil {
			out[it.ID] = *info.Stack.Volumes
		}
	}
	return out
}

// lonelyOf are the sizes that the list knows of the stacks of which the plan says that only
// volumes are left. That they are is for the plan to say: the list can be old.
func (m *Model) lonelyOf(plan core.Plan) map[string]*int64 {
	out := map[string]*int64{}
	for _, it := range plan.Items {
		if !it.OnlyVolumes {
			continue
		}
		if info, ok := m.tree.Lookup(it.ID); ok && info.Disk != nil {
			bytes := info.Disk.Bytes
			out[it.ID] = &bytes
		}
	}
	return out
}

// nextInQueue goes on to the next plan of a batch of mixed marks. Each gets its own sheet.
func (m *Model) nextInQueue() tea.Cmd {
	if len(m.queue) == 0 {
		m.queueTotal, m.queuePart = 0, 0
		return nil
	}
	req := m.queue[0]
	m.queue = m.queue[1:]
	m.queuePart++
	return m.planCmd(req, false)
}

// ---- apply

// startApply sends the plan of the sheet back, with the phrases that the core gave. It
// passes the plan unchanged: the same action, flags and targets.
func (m *Model) startApply(phrases []string) tea.Cmd {
	if m.writerOn {
		// An open runs. The end of it must not find a batch, or the batch its end.
		m.confirm.Said = "wtree is busy: an open still runs. Try again in a moment."
		m.confirm.Wrong = m.confirm.Said
		return nil
	}
	c := m.confirm
	ch := c.Selected()
	req := core.ApplyRequest{PlanRequest: c.Req, PlanID: c.Plan.PlanID, Phrases: phrases}
	if ch != nil {
		req.Choice = ch.ID
	}
	m.applyReq = req
	m.progress = dialog.NewProgress(c.Plan, ch, commandLine(req))
	m.mode = ModeProgress
	m.quitMenu = nil
	m.writerKind, m.writerOn = kindApply, true
	client := m.client
	// Not the context of the list: nothing here may end an apply but an explicit Stop.
	return startWriter(m.writerBox, kindApply, func() (*core.Stream, error) { return client.Apply(context.Background(), req) })
}

func commandLine(r core.ApplyRequest) string {
	args := []string{"wtree", "core", "apply", r.Action, "--plan", r.PlanID}
	if r.Choice != "" {
		args = append(args, "--choice", r.Choice)
	}
	for _, p := range r.Phrases {
		args = append(args, "--phrase", quoteArg(p))
	}
	if r.Force {
		args = append(args, "--force")
	}
	if r.Volumes {
		args = append(args, "--volumes")
	}
	for _, repo := range r.Repos {
		args = append(args, "--repo", quoteArg(repo))
	}
	for _, dir := range r.Dirs {
		args = append(args, "--dir", quoteArg(dir))
	}
	if r.Cwd != "" {
		args = append(args, "--cwd", quoteArg(r.Cwd))
	}
	args = append(args, "--")
	for _, t := range r.Targets {
		args = append(args, quoteArg(t))
	}
	return strings.Join(args, " ")
}

func quoteArg(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\"'\\$`*?[](){}<>|&;!#~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func (m *Model) onApplyStream(msg streamMsg) tea.Cmd {
	if msg.stream != nil {
		m.writer = msg.stream
		if m.progress.Cancelling {
			msg.stream.Stop() // the choice came before the stream did
		}
	}
	var refused *core.RefusedEvent
	for _, ev := range msg.events {
		switch e := ev.(type) {
		case core.RefusedEvent:
			r := e
			refused = &r
		case core.BadLine:
			m.progress.Log = append(m.progress.Log, "bad line: "+treetable.Sanitize(e.Text))
		default:
			m.progress.Apply(ev)
		}
	}
	if !msg.done {
		return readBatch(kindApply, 0, msg.stream, m.uiDone)
	}
	m.clearWriter()
	result := msg.result
	if msg.err != nil {
		result.Err = msg.err
	}
	var refusal *core.RefusedError
	if refused == nil && errors.As(result.Err, &refusal) {
		refused = &core.RefusedEvent{Code: refusal.Code, Message: refusal.Message, Plan: refusal.Plan}
	}
	if refused != nil {
		return m.onRefused(*refused)
	}
	m.progress.Finish(result)
	return m.onApplyFinished()
}

// onRefused handles a refusal: nothing ran. The sheet comes back with the plain truth and
// the focus on cancel. A destructive action is never tried again on its own.
func (m *Model) onRefused(r core.RefusedEvent) tea.Cmd {
	m.autoClose = false
	m.armSheet() // what comes back is a sheet that nobody has read, and keys can be on their way
	reload := m.startFullList()
	switch r.Code {
	case core.RefusedPlanStale:
		m.mode = ModeConfirm
		if r.Plan != nil {
			m.confirm.SetPlan(*r.Plan)
			m.confirm.Lonely = m.lonelyOf(*r.Plan)
		} else {
			m.confirm.Busy = true
			m.confirm.Focus = dialog.FocusCancel
			reload = tea.Batch(reload, m.planCmd(m.confirm.Req, true))
		}
		m.confirm.Hidden = m.hiddenFor(m.confirm.Plan)
		m.confirm.Notice = "Something changed since this sheet opened, so wtree did nothing. Below is what is true now. Read it again before you confirm."
		return reload
	case core.RefusedPhraseMismatch:
		m.mode = ModeConfirm
		m.confirm.Typing = false
		m.confirm.Focus = dialog.FocusCancel
		m.confirm.Notice = "wtree did not accept the typed phrase, so it did nothing. Nothing ran."
		return reload
	}
	m.mode = ModeConfirm
	m.confirm.Typing = false
	m.confirm.Focus = dialog.FocusCancel
	m.confirm.Notice = "wtree refused and did nothing: " + treetable.Sanitize(r.Message)
	return reload
}

// hiddenFor is the marks that the filter hides, by the ids of this plan. In a stop, a mark on a
// worktree row stands for its stacks, and those are the items of the plan.
func (m *Model) hiddenFor(plan core.Plan) map[string]bool {
	hidden := m.hiddenNow()
	if plan.Action != core.ActionStopStacks {
		return hidden
	}
	for id := range m.hiddenNow() {
		if info, ok := m.tree.Lookup(id); ok && info.Kind == treetable.KindWorktree {
			for i := range info.Stacks {
				hidden["stack:"+info.Stacks[i].Project] = true
			}
		}
	}
	return hidden
}

func (m *Model) hiddenNow() map[string]bool {
	counts := m.tree.MarkCounts()
	hidden := make(map[string]bool, len(counts.Hidden))
	for _, id := range counts.Hidden {
		hidden[id] = true
	}
	return hidden
}

// onApplyFinished keeps the marks true to the outcome: what went is no longer marked, and
// what failed stays marked, or gets a mark, so that a retry is one key.
func (m *Model) onApplyFinished() tea.Cmd {
	covered := m.settleWorktreeMarks()
	m.tree.Unmark(m.progress.Succeeded()...)
	for _, id := range m.progress.Unfinished() {
		if !m.tree.IsMarked(id) && !covered[id] {
			m.tree.ToggleMark(id)
		}
	}
	m.lastLog = append([]string(nil), m.progress.Log...)
	lines, stateDir, at := m.lastLog, m.o.StateDir, m.nowAt
	save := func() tea.Msg { return logSavedMsg{writeLog(stateDir, at, lines)} }
	var projects []string
	for _, r := range m.progress.Rows {
		if strings.HasPrefix(r.ItemID, "stack:") {
			projects = append(projects, strings.TrimPrefix(r.ItemID, "stack:"))
		}
	}
	m.tree.InvalidateDisk(projects)
	if m.progress.End != nil && m.progress.End.DiskRefresh {
		m.diskScan = true
	}
	if m.quitAfter {
		m.logUnsaved = true
		return m.quitNow()
	}
	reload := m.startFullList()
	if m.quitMenu != nil {
		m.quitMenu = nil
	}
	if m.autoClose && m.progress.OK() {
		m.autoClose = false
		m.mode = ModeList
		text := "done"
		if m.progress.Action == core.ActionStopStacks && len(m.progress.Rows) > 0 {
			text = "stopped " + m.progress.Rows[0].Label
		}
		return tea.Batch(save, reload, m.say(text, theme.ToneOk))
	}
	m.autoClose = false
	return tea.Batch(save, reload)
}

// settleWorktreeMarks is for a stop. The steps are of stacks, and a mark on a worktree row stands
// for its stacks: it goes when they all stopped, and it stays when one did not. That stack then
// needs no mark of its own, since a retry of the worktree finds it. It says which stacks those are.
func (m *Model) settleWorktreeMarks() map[string]bool {
	covered := map[string]bool{}
	if m.progress.Action != core.ActionStopStacks {
		return covered
	}
	stopped, unfinished := map[string]bool{}, map[string]bool{}
	for _, id := range m.progress.Succeeded() {
		stopped[id] = true
	}
	for _, id := range m.progress.Unfinished() {
		unfinished[id] = true
	}
	for _, id := range m.tree.Marked() {
		info, ok := m.tree.Lookup(id)
		if !ok || info.Kind != treetable.KindWorktree {
			continue
		}
		hit, failed := false, false
		for i := range info.Stacks {
			stack := "stack:" + info.Stacks[i].Project
			if stopped[stack] {
				hit = true
			}
			if unfinished[stack] {
				failed = true
				covered[stack] = true
			}
		}
		if hit && !failed {
			m.tree.Unmark(id)
		}
	}
	return covered
}

// closeResults goes back to the list, or on to the next part of a batch.
func (m *Model) closeResults() tea.Cmd {
	m.mode = ModeList
	m.progress.Scroll = 0
	if !m.progress.OK() {
		_, failed, skipped, _, _ := m.progress.Counts()
		if failed+skipped > 0 {
			word := " failed"
			if skipped > 0 {
				word = " did not finish"
			}
			text := itoa(failed+skipped) + word
			marked := len(m.tree.Marked()) > 0
			if retry := m.progress.RetryKey(); retry != "" && marked {
				text += ", still marked " + m.th.G.Dot + " " + retry + " retries"
				if long := text + " " + m.th.G.Dot + " /is:marked shows it"; ansi.StringWidth(long)+2 <= m.layout.Width {
					text = long
				}
			}
			cmd := m.sayFor(text, theme.ToneWarn, 3*statusShort)
			m.statusMark = m.progress.RetryKey() != "" && marked
			return tea.Batch(cmd, m.nextInQueue())
		}
	}
	return m.nextInQueue()
}

// ---- open and new

func (m *Model) onOpenStream(msg streamMsg) tea.Cmd {
	if msg.stream != nil {
		m.writer = msg.stream
	}
	for _, ev := range msg.events {
		switch e := ev.(type) {
		case core.OutputEvent:
			m.openLines = append(m.openLines, treetable.Sanitize(e.Line))
		case core.StepEvent:
			if e.State == core.StepOK {
				m.openOK = true
			}
			if e.State == core.StepFailed {
				m.openOK = false
				for _, line := range e.Tail {
					m.openLines = append(m.openLines, treetable.Sanitize(line))
				}
			}
		}
	}
	if !msg.done {
		return readBatch(kindOpen, 0, msg.stream, m.uiDone)
	}
	m.clearWriter()
	return m.openFinished(msg.err, msg.result)
}

// openFinished: a jump that worked leaves wtree, as today. What the terminal could not do
// comes as an output line, and then wtree stays and says it.
func (m *Model) openFinished(err error, result core.Result) tea.Cmd {
	name := m.opening
	m.opening = ""
	if err == nil {
		err = result.Err
	}
	lines := m.openLines
	m.openLines = nil
	switch {
	case err != nil:
		return m.say("open: "+err.Error(), theme.ToneDanger)
	case !m.openOK:
		text := "could not open " + name
		if len(lines) > 0 {
			text = lines[len(lines)-1]
		}
		return m.say(text, theme.ToneDanger)
	case len(lines) > 0:
		text := strings.TrimPrefix(lines[len(lines)-1], "wtree: ")
		if strings.Contains(text, "no tab support") {
			// y copies this path, and the actions sheet of the row has "print path and quit"
			text = "no tab support " + m.th.G.Dot + " y copy path " + m.th.G.Dot + " a print path"
		}
		return m.sayFor(text, theme.ToneWarn, 3*statusShort)
	}
	if m.mode != ModeList && m.mode != ModeSearch {
		// A sheet is open since then: what it asks is not to be dropped for a jump.
		return m.say("opened "+name, theme.ToneDim)
	}
	return m.quitNow()
}

func (m *Model) onNewStream(msg streamMsg) tea.Cmd {
	if msg.stream != nil {
		m.writer = msg.stream
	}
	var cmds []tea.Cmd
	for _, ev := range msg.events {
		switch e := ev.(type) {
		case core.NewWorktreeEvent:
			if e.Created {
				m.wantCursor = "worktree:" + e.Path
				m.mode = ModeList
				cmds = append(cmds, m.startFullList())
			}
		case core.OutputEvent:
			m.openLines = append(m.openLines, treetable.Sanitize(e.Line))
		case core.StepEvent:
			if e.State == core.StepOK {
				m.openOK = true
			}
		}
	}
	if !msg.done {
		cmds = append(cmds, readBatch(kindNew, 0, msg.stream, m.uiDone))
		return tea.Batch(cmds...)
	}
	m.clearWriter()
	result := msg.result
	if msg.err != nil {
		result.Err = msg.err
	}
	var refusal *core.RefusedError
	if errors.As(result.Err, &refusal) {
		m.newSheet.Busy = false
		m.newSheet.Err = treetable.Sanitize(refusal.Message)
		return tea.Batch(cmds...)
	}
	if result.Err != nil && result.Created == nil {
		m.newSheet.Busy = false
		m.newSheet.Err = treetable.Sanitize(result.Err.Error())
		return tea.Batch(cmds...)
	}
	switch {
	case result.Created != nil && m.newRequest.NoOpen:
		cmds = append(cmds, m.say("created "+treetable.Sanitize(result.Created.Name), theme.ToneOk))
	case result.Created != nil:
		m.opening = treetable.Sanitize(result.Created.Name)
		cmds = append(cmds, m.openFinished(nil, result))
	}
	return tea.Batch(cmds...)
}

func (m *Model) onDryRunStream(msg streamMsg) tea.Cmd {
	if !msg.done {
		return readBatch(kindDryRun, msg.token, msg.stream, m.uiDone)
	}
	m.newPending = false
	err := msg.err
	if err == nil {
		err = msg.result.Err
	}
	if err != nil || msg.result.Created == nil {
		text := "new worktree: wtree picked no name"
		if err != nil {
			text = "new worktree: " + err.Error()
		}
		return m.say(text, theme.ToneDanger)
	}
	if msg.token != m.planSeq || (m.mode != ModeList && m.mode != ModeSearch) {
		return nil
	}
	c := msg.result.Created
	repo, repoName := m.newRepo()
	sheet, cmd := dialog.NewWorktreeSheet(m.th, repo, repoName, c.Path, c.Name, c.Head)
	m.newSheet = sheet
	m.mode = ModeNew
	return cmd
}

// ---- theme

func (m *Model) setLight(light bool) {
	opts := m.th.Options
	opts.Light = light
	m.applyTheme(opts)
}

func (m *Model) applyTheme(opts theme.Options) {
	m.th = theme.New(opts)
	m.table.SetTheme(m.th)
	value := m.search.Value()
	focused := m.search.Focused()
	m.search = dialog.NewInput(m.th, "/ ")
	m.search.SetValue(value)
	if focused {
		m.search.Focus()
	}
}
