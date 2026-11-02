package model

import (
	"strconv"

	tea "charm.land/bubbletea/v2"
	"github.com/afruth/wtree/tui/internal/dialog"
	"github.com/afruth/wtree/tui/internal/theme"
	"github.com/afruth/wtree/tui/internal/tree"
	"github.com/afruth/wtree/tui/internal/treetable"
)

func itoa(n int) string { return strconv.Itoa(n) }

// keyName gives a key its plain name. Phone clients send backspace and enter in more than
// one way, and all of them count. CSI 27 u decodes to esc already.
func keyName(msg tea.KeyPressMsg) string {
	k := msg.String()
	switch k {
	case "ctrl+h", "delete":
		return "backspace"
	case "ctrl+j", "ctrl+m":
		return "enter"
	}
	return k
}

func (m *Model) onKey(msg tea.KeyPressMsg) tea.Cmd {
	k := keyName(msg)
	if m.layout.TooSmall {
		if k == "q" || k == "ctrl+c" || k == "esc" {
			return m.requestQuit()
		}
		return nil
	}
	if k != "ctrl+c" && k == m.heldName && m.nowAt.Before(m.heldUntil) {
		m.heldUntil = m.nowAt.Add(heldKey) // still held
		return nil
	}
	m.heldName = ""
	// While a plan is on its way the user can go on, and that is the answer to it: its sheet must
	// not open over what the user does next, and take the keys of that as its answer.
	if m.planWaiting() {
		switch k {
		case "d", "D", "s", "n":
			return nil // the status line says that it is on its way
		case "backspace", "esc":
			m.cancelPlan()
			m.holdKey(k)
			return m.say("cancelled", theme.ToneDim)
		}
		m.cancelPlan()
	}
	mode, typing := m.mode, m.confirm.Typing
	cmd := m.dispatchKey(msg, k)
	closed := m.mode != mode && (m.mode == ModeList || mode == ModeHelp || mode == ModeLog)
	if closed || (typing && !m.confirm.Typing && m.mode == ModeConfirm) {
		m.holdKey(k)
	}
	return cmd
}

// holdKey is for a key that just closed something. A phone repeats a held backspace, and the
// repeats must not go on to what was under it: a filter, the marks, or a tab that opens. A digit
// is a pick: the next sheet has its own digits, and the same one again is a choice.
func (m *Model) holdKey(k string) {
	if isDigit(k) {
		return
	}
	m.heldName, m.heldUntil = k, m.nowAt.Add(heldKey)
}

func (m *Model) dispatchKey(msg tea.KeyPressMsg, k string) tea.Cmd {
	switch m.mode {
	case ModeSearch:
		return m.keySearch(msg, k)
	case ModeMenu:
		return m.keyMenu(k)
	case ModeConfirm:
		return m.keyConfirm(msg, k)
	case ModeNew:
		return m.keyNew(msg, k)
	case ModeProgress:
		return m.keyProgress(k)
	case ModeLog:
		return m.keyLog(k)
	case ModeHelp:
		return m.keyHelp(k)
	case ModePanel:
		return m.keyPanel(k)
	}
	return m.keyList(k)
}

func (m *Model) onPaste(msg tea.PasteMsg) tea.Cmd {
	var cmd tea.Cmd
	switch {
	case m.mode == ModeSearch:
		m.search, cmd = m.search.Update(msg)
		m.tree.SetFilter(m.search.Value())
	case m.mode == ModeConfirm && m.confirm.Typing:
		m.confirm.Input, cmd = m.confirm.Input.Update(msg)
	case m.mode == ModeNew && m.newSheet.Focus == dialog.NewFocusField:
		m.newSheet.Input, cmd = m.newSheet.Input.Update(msg)
	}
	return cmd
}

// requestQuit leaves, unless a batch runs. A batch is never left alone without a word. An open or
// a new that runs is told to stop on the way out, and the way out says so.
func (m *Model) requestQuit() tea.Cmd {
	if m.applyRunning() {
		m.openQuitMenu()
		return nil
	}
	return m.quitNow()
}

func (m *Model) openQuitMenu() {
	menu := dialog.NewMenu("still "+m.progress.Verb(), "The step that runs always finishes. What is queued can still stop.", []dialog.Item{
		{Label: "keep going", Action: "keep", Tone: theme.ToneAccent},
		{Label: "stop after this step", Action: "stop-step", Tone: theme.ToneWarn},
		{Label: "stop after this step, then quit", Action: "stop-quit", Tone: theme.ToneWarn},
	})
	m.quitMenu = &menu
}

// keyList is the list in command mode. Letters are commands, and / opens the search.
func (m *Model) keyList(k string) tea.Cmd {
	// any key brings a cursor that a wheel scrolled away back into view
	if m.table.Place() != treetable.CursorVisible {
		m.table.EnsureVisible(m.table.Cursor())
	}
	switch k {
	case "q", "ctrl+c":
		return m.requestQuit()
	case "backspace":
		return m.back(false)
	case "esc":
		return m.back(true)
	case "?":
		m.openHelp()
		return nil
	case "g":
		return m.setScope(!m.machine())
	case "/":
		return m.openSearch()
	case "s":
		return m.run(actStop)
	case "d", "D":
		return m.run(actDelete)
	case "n":
		return m.run(actNew)
	}
	if !m.structureReady() && len(m.table.Rows()) == 0 {
		return nil // the first frame: only the keys above work
	}
	switch k {
	case "j", "down":
		m.table.MoveCursor(1)
	case "k", "up":
		m.table.MoveCursor(-1)
	case "J", "pgdown":
		m.table.MovePage(1)
	case "K", "pgup":
		m.table.MovePage(-1)
	case "home":
		m.table.CursorHome()
	case "end":
		m.table.CursorEnd()
	case "h", "left":
		m.collapseOrParent()
	case "l", "right":
		if id := m.table.Cursor(); id != "" {
			m.tree.SetExpanded(id, true)
		}
	case "z":
		m.toggleAll()
	case "enter":
		return m.keyEnterOnList()
	case "a":
		return m.openMenu()
	case "tab":
		if !m.lastTab.IsZero() && m.nowAt.Sub(m.lastTab) < doubleTab {
			m.lastTab = m.nowAt
			return nil
		}
		m.lastTab = m.nowAt
		return m.run(actMark)
	case "space":
		return m.run(actMark)
	case "A":
		return m.run(actMarkAll)
	case "u":
		m.tree.ClearMarks()
	case "c":
		return m.toggleCleanup()
	case "o":
		return m.onCycleSort()
	case "p":
		return m.doPanel()
	case "r":
		m.tree.BeginReload()
		return m.startFullList()
	case "R":
		m.tree.BeginReload()
		return m.startFullList()
	case "y":
		return m.run(actCopy)
	case "L":
		return m.openLog()
	default:
		return nil
	}
	m.lastMove = m.nowAt
	return nil
}

// keyEnterOnList is the primary action of the row. Enter never stops and never deletes:
// where open is not possible, it opens the action sheet.
func (m *Model) keyEnterOnList() tea.Cmd {
	info, ok := m.cursorInfo()
	if !ok {
		return nil
	}
	if info.Kind == treetable.KindGroup {
		return m.openMenu()
	}
	return m.run(actOpen)
}

func (m *Model) collapseOrParent() {
	id := m.table.Cursor()
	if id == "" {
		return
	}
	rows := m.table.Rows()
	if m.tree.IsExpanded(id) {
		for _, r := range rows {
			if r.ID == id && r.Expandable {
				m.tree.SetExpanded(id, false)
				return
			}
		}
	}
	depth, index := 0, -1
	for i, r := range rows {
		if r.ID == id {
			depth, index = r.Depth, i
			break
		}
	}
	for i := index - 1; i >= 0; i-- {
		if rows[i].Depth < depth && rows[i].Kind != treetable.KindSeparator {
			m.table.SetCursor(rows[i].ID)
			return
		}
	}
}

func (m *Model) toggleAll() {
	open := false
	for _, r := range m.table.Rows() {
		if r.Expandable && !r.Expanded {
			open = true
			break
		}
	}
	m.tree.SetAllExpanded(open)
}

// back clears the last of what is there, in order. With nothing left, esc quits, and backspace
// does not: it is the only "back" on a phone, and it repeats while it is held.
func (m *Model) back(quit bool) tea.Cmd {
	key := "backspace"
	if quit {
		key = "esc"
	}
	switch {
	case m.tree.Filter().Active():
		m.holdKey(key)
		m.search.SetValue("")
		m.tree.SetFilter("")
		return nil
	case m.tree.View() == tree.ViewCleanup:
		m.holdKey(key)
		return m.toggleCleanup()
	case len(m.tree.Marked()) > 0:
		m.holdKey(key)
		m.tree.ClearMarks()
		return nil
	case quit:
		return m.requestQuit()
	}
	return m.say("q quits", theme.ToneDim)
}

func (m *Model) keySearch(msg tea.KeyPressMsg, k string) tea.Cmd {
	switch k {
	case "enter":
		return m.run(actApplySrch)
	case "esc", "ctrl+c":
		return m.run(actClearSrch)
	case "backspace":
		if m.search.Value() == "" {
			return m.run(actClearSrch)
		}
	}
	var cmd tea.Cmd
	m.search, cmd = m.search.Update(msg)
	m.tree.SetFilter(m.search.Value())
	m.seekMatch = true
	return cmd
}

func isDigit(k string) bool { return len(k) == 1 && k[0] >= '0' && k[0] <= '9' }

func (m *Model) keyMenu(k string) tea.Cmd {
	switch k {
	case "?":
		m.openHelp()
	case "j", "down":
		m.menu.Move(1)
	case "k", "up":
		m.menu.Move(-1)
	case "backspace", "q", "esc", "0", "ctrl+c":
		m.closeMenu()
	case "enter":
		return m.pickMenu(m.menu.Current())
	default:
		if isDigit(k) {
			if it, ok := m.menu.Pick(k); ok {
				return m.pickMenu(it)
			}
		}
	}
	return nil
}

func (m *Model) closeMenu() { m.mode = ModeList }

func (m *Model) pickMenu(it dialog.Item) tea.Cmd {
	if it.Disabled {
		return m.say(it.Label+": "+it.Reason, theme.ToneDim)
	}
	m.closeMenu()
	if it.Action == dialog.CancelAction {
		return nil
	}
	return m.run(it.Action)
}

// ---- the consequences sheet

func (m *Model) keyConfirm(msg tea.KeyPressMsg, k string) tea.Cmd {
	c := &m.confirm
	c.Said = ""
	if c.Typing {
		switch k {
		case "enter":
			return m.submitPhrase()
		case "esc":
			c.Typing = false
			return nil
		case "ctrl+c":
			return m.cancelConfirm()
		case "up":
			c.Scroll = max(c.Scroll-1, 0)
			return nil
		case "down":
			c.Scroll++
			return nil
		case "backspace":
			if c.Input.Value() == "" {
				c.Typing, c.Wrong = false, ""
				return nil
			}
		}
		var cmd tea.Cmd
		c.Input, cmd = c.Input.Update(msg)
		c.Wrong = ""
		return cmd
	}
	switch k {
	case "?":
		m.openHelp()
	case "backspace", "q", "esc", "0", "n", "ctrl+c":
		return m.cancelConfirm()
	case "enter":
		if m.sheetHeld() {
			return nil
		}
		if o, ok := c.LevelHint(); ok {
			return m.pickOption(o) // it only moves to that level, and focus is on cancel there
		}
		if c.Focus == dialog.FocusCancel {
			if can, reason := c.CanConfirm(); !can {
				c.Said = reason
				return nil
			}
			c.Focus = dialog.FocusConfirm
			m.armSheet() // the two steps are two presses, and not one that repeats
			return nil
		}
		return m.confirmNow()
	case "y":
		if m.sheetHeld() {
			return nil
		}
		return m.confirmNow()
	case "tab", "left", "right":
		if c.Focus == dialog.FocusCancel {
			if can, _ := c.CanConfirm(); can {
				c.Focus = dialog.FocusConfirm
			}
		} else {
			c.Focus = dialog.FocusCancel
		}
	case "j", "down":
		c.Scroll++
	case "k", "up":
		c.Scroll = max(c.Scroll-1, 0)
	case "h":
		return m.unmarkHidden()
	default:
		if isDigit(k) {
			if o, ok := c.OptionByDigit(k); ok {
				return m.pickOption(o)
			}
		}
	}
	return nil
}

func (m *Model) cancelConfirm() tea.Cmd {
	m.mode = ModeList
	m.cancelPlan()
	return nil
}

func (m *Model) confirmNow() tea.Cmd {
	c := &m.confirm
	if can, reason := c.CanConfirm(); !can {
		c.Said = reason
		return nil
	}
	if len(c.Phrases()) > 0 {
		return c.StartTyping(m.th)
	}
	return m.startApply(nil)
}

func (m *Model) submitPhrase() tea.Cmd {
	c := &m.confirm
	if c.Busy {
		// the targets of the old plan are not what the user asks for now
		c.Wrong = "wtree is reading the new plan"
		return nil
	}
	done, cmd := c.Submit(m.th)
	if !done {
		return cmd
	}
	return m.startApply(c.Typed)
}

func (m *Model) pickOption(o dialog.Option) tea.Cmd {
	c := &m.confirm
	if o.Disabled {
		c.Said = o.Reason
		return nil
	}
	c.Focus = dialog.FocusCancel
	c.Typing = false
	if !o.IsLevel {
		c.Choice = o.Index
		return nil
	}
	if o.Selected {
		return nil
	}
	req := c.Req
	switch c.Plan.Action {
	case "delete-worktrees":
		req.Force = o.Index == 1
	case "delete-stacks":
		req.Volumes = o.Index == 1
	}
	c.Busy = true
	return m.planCmd(req, true)
}

// unmarkHidden takes the marks under HIDDEN BY FILTER out of the plan, and asks again: another
// set of targets is another plan. It acts on what this sheet shows and on nothing else: a hidden
// mark of the next part of a batch has its own sheet, and it is listed there.
func (m *Model) unmarkHidden() tea.Cmd {
	c := &m.confirm
	_, _, lines := c.Sections()
	if len(lines) == 0 {
		return nil
	}
	gone := make(map[string]bool, len(lines))
	var ids []string
	for _, line := range lines {
		gone[line.ID] = true
		ids = append(ids, line.ID)
		delete(c.Hidden, line.ID)
	}
	// In a stop, a mark on a worktree row stands for its stacks.
	for id := range m.hiddenNow() {
		if info, ok := m.tree.Lookup(id); ok && info.Kind == treetable.KindWorktree {
			for i := range info.Stacks {
				if gone["stack:"+info.Stacks[i].Project] {
					ids = append(ids, id)
					break
				}
			}
		}
	}
	m.tree.Unmark(ids...)
	req := c.Req
	req.Targets, req.Repos = nil, nil
	for i, target := range c.Req.Targets {
		if gone["worktree:"+target] || gone["stack:"+target] {
			continue
		}
		req.Targets = append(req.Targets, target)
		if i < len(c.Req.Repos) {
			req.Repos = append(req.Repos, c.Req.Repos[i])
		}
	}
	if len(req.Targets) == 0 {
		return m.cancelConfirm()
	}
	c.Busy = true
	return m.planCmd(req, true)
}

// ---- new worktree

func (m *Model) keyNew(msg tea.KeyPressMsg, k string) tea.Cmd {
	s := &m.newSheet
	if s.Busy {
		if k == "ctrl+c" {
			return m.requestQuit()
		}
		return nil
	}
	if s.Focus == dialog.NewFocusField {
		switch k {
		case "enter":
			return m.submitNew()
		case "esc", "ctrl+c":
			m.mode = ModeList
			return nil
		case "backspace":
			if s.Input.Value() == "" {
				m.mode = ModeList
				return nil
			}
		case "tab", "down":
			s.Input.Blur()
			s.Focus = dialog.NewFocusOpen
			if !s.Open {
				s.Focus = dialog.NewFocusStay
			}
			return nil
		}
		var cmd tea.Cmd
		s.Input, cmd = s.Input.Update(msg)
		s.Err, s.Flash = "", false
		return cmd
	}
	switch k {
	case "enter":
		return m.submitNew()
	case "backspace", "esc", "q", "0", "ctrl+c":
		m.mode = ModeList
	case "1":
		s.Open, s.Focus = true, dialog.NewFocusOpen
	case "2":
		s.Open, s.Focus = false, dialog.NewFocusStay
	case "down", "j", "tab":
		if s.Focus == dialog.NewFocusOpen {
			s.Open, s.Focus = false, dialog.NewFocusStay
		} else {
			s.Focus = dialog.NewFocusField
			return s.Input.Focus()
		}
	case "up", "k":
		if s.Focus == dialog.NewFocusStay {
			s.Open, s.Focus = true, dialog.NewFocusOpen
		} else {
			s.Focus = dialog.NewFocusField
			return s.Input.Focus()
		}
	}
	return nil
}

// ---- progress and results

func (m *Model) keyProgress(k string) tea.Cmd {
	if m.quitMenu != nil {
		return m.keyQuitMenu(k)
	}
	p := &m.progress
	switch k {
	case "j", "down":
		p.Move(1)
		return nil
	case "k", "up":
		p.Move(-1)
		return nil
	case "l", "L":
		return m.openLog()
	}
	if p.Finished {
		switch k {
		case "enter", "backspace", "esc", "q", "ctrl+c":
			return m.closeResults()
		case "space":
			p.Toggle()
		}
		return nil
	}
	switch k {
	case "backspace", "q", "esc", "ctrl+c":
		m.openQuitMenu()
	case "enter", "space":
		p.Toggle()
	}
	return nil
}

func (m *Model) keyQuitMenu(k string) tea.Cmd {
	menu := m.quitMenu
	switch k {
	case "j", "down":
		menu.Move(1)
	case "k", "up":
		menu.Move(-1)
	case "backspace", "esc", "0", "q":
		m.quitMenu = nil
	case "enter":
		return m.pickQuit(menu.Current().Action)
	default:
		if isDigit(k) {
			if it, ok := menu.Pick(k); ok {
				return m.pickQuit(it.Action)
			}
		}
	}
	return nil
}

func (m *Model) pickQuit(action string) tea.Cmd {
	m.quitMenu = nil
	switch action {
	case "stop-quit":
		m.quitAfter = true
		fallthrough
	case "stop-step":
		m.progress.Cancelling = true
		m.queue, m.queueTotal, m.queuePart = nil, 0, 0
		if m.writer != nil {
			m.writer.Stop()
		}
	}
	return nil
}

func (m *Model) openLog() tea.Cmd {
	lines := m.lastLog
	if m.mode == ModeProgress {
		lines = m.progress.Log
	}
	if len(lines) == 0 {
		return m.say("no batch has run yet", theme.ToneDim)
	}
	m.logView = dialog.LogView{Title: "command log", Path: m.lastLogPath, Lines: lines}
	m.logReturn = m.mode
	m.mode = ModeLog
	return nil
}

func (m *Model) keyLog(k string) tea.Cmd {
	switch k {
	case "backspace", "q", "esc", "enter", "l", "L", "ctrl+c":
		m.mode = m.logReturn
	case "j", "down":
		m.logView.Scroll++
	case "k", "up":
		m.logView.Scroll = max(m.logView.Scroll-1, 0)
	case "J", "pgdown":
		m.logView.Scroll += 10
	case "K", "pgup":
		m.logView.Scroll = max(m.logView.Scroll-10, 0)
	}
	return nil
}

// ---- help and panel

func (m *Model) keyHelp(k string) tea.Cmd {
	switch k {
	case "?", "backspace", "q", "esc", "ctrl+c", "enter":
		m.mode = m.helpReturn
	case "j", "down":
		if m.layout.Short && !m.help.More {
			m.help.More = true
			return nil
		}
		m.help.Scroll++
	case "k", "up":
		m.help.Scroll = max(m.help.Scroll-1, 0)
	case "u":
		opts := m.th.Options
		opts.ASCII = !opts.ASCII
		m.applyTheme(opts)
		m.refreshHelp()
	case "t":
		opts := m.th.Options
		opts.Light = !opts.Light
		m.light = true
		m.applyTheme(opts)
		m.refreshHelp()
	}
	return nil
}

func (m *Model) keyPanel(k string) tea.Cmd {
	switch k {
	case "p", "backspace", "q", "esc", "enter", "ctrl+c":
		m.mode = ModeList
	case "j", "down":
		m.info.Scroll++
	case "k", "up":
		m.info.Scroll = max(m.info.Scroll-1, 0)
	}
	return nil
}
