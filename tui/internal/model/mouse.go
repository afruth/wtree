package model

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/afruth/wtree/tui/internal/dialog"
	"github.com/afruth/wtree/tui/internal/theme"
	"github.com/afruth/wtree/tui/internal/treetable"
)

// pressState is a press that has no release yet. Some clients never send one, so a press
// counts as a tap when its release is late. A tap never needs a timing gesture.
type pressState struct{ x, y int }

func (m *Model) onPress(mouse tea.Mouse) tea.Cmd {
	if mouse.Button != tea.MouseLeft {
		return nil
	}
	m.press = &pressState{mouse.X, mouse.Y}
	m.dragY = mouse.Y
	m.pressGen++
	return m.timer(pressTimeout, pressExpireMsg{m.pressGen})
}

func (m *Model) onRelease(mouse tea.Mouse) tea.Cmd {
	p := m.press
	m.press = nil
	m.pressGen++
	if p == nil {
		return nil // the timeout already counted it, or a drag used it
	}
	return m.tap(p.x, p.y)
}

func (m *Model) onPressExpire(gen int) tea.Cmd {
	if gen != m.pressGen || m.press == nil {
		return nil
	}
	p := m.press
	m.press = nil
	return m.tap(p.x, p.y)
}

// onMotion is a drag, for a client that sends one. The list follows it, and no feature
// needs it. A press that moved is no tap.
func (m *Model) onMotion(mouse tea.Mouse) {
	if mouse.Button != tea.MouseLeft || m.overlay() != nil {
		return
	}
	dy := mouse.Y - m.dragY
	if dy == 0 {
		return
	}
	m.press = nil
	m.pressGen++
	m.dragY = mouse.Y
	step := dy
	if m.layout.Tier == theme.TierNarrow {
		step = dy / 2
		if step == 0 {
			m.dragY -= dy
			return
		}
	}
	m.table.Scroll(-step)
}

func (m *Model) onWheel(mouse tea.Mouse) {
	delta := 0
	switch mouse.Button {
	case tea.MouseWheelUp:
		delta = -1
	case tea.MouseWheelDown:
		delta = 1
	default:
		return
	}
	switch m.mode {
	case ModeMenu:
		m.menu.Move(delta)
	case ModeConfirm:
		m.confirm.Scroll = max(m.confirm.Scroll+delta, 0)
	case ModeProgress:
		m.progress.Move(delta)
	case ModeLog:
		m.logView.Scroll = max(m.logView.Scroll+delta*3, 0)
	case ModeHelp:
		m.help.Scroll = max(m.help.Scroll+delta, 0)
	case ModePanel:
		m.info.Scroll = max(m.info.Scroll+delta, 0)
	default:
		if m.panelOnScreen() && mouse.X >= m.layout.TableWidth(true) {
			m.panelScroll = max(m.panelScroll+delta, 0)
			return
		}
		m.table.Scroll(delta * m.table.WheelStep())
	}
}

// tap routes a tap through what is drawn now. Each zone does what a plain key does.
func (m *Model) tap(x, y int) tea.Cmd {
	if m.layout.TooSmall {
		return nil
	}
	// The layout is moving: a tap aimed at what was here lands on what comes here.
	if m.settling {
		return nil
	}
	if m.planWaiting() {
		if f := m.overlay(); f != nil {
			if action, _ := f.Hit(x, y); action == "cancel" {
				return m.cancelWait()
			}
		}
		m.ignoredWhileWaiting = true
		return nil
	}
	m.inTap = true
	defer func() { m.inTap = false }()
	if f := m.overlay(); f != nil {
		action, inside := f.Hit(x, y)
		return m.tapOverlay(action, inside)
	}
	c := m.compose()
	for _, z := range c.zones {
		if x >= z.X0 && x < z.X1 && y >= z.Y0 && y < z.Y1 {
			return m.tapBase(z.Action)
		}
	}
	return m.tapTable(x, y)
}

func (m *Model) tapBase(action string) tea.Cmd {
	switch {
	case action == "scope:toggle":
		return m.setScope(!m.machine())
	case action == "scope:repo":
		return m.setScope(false)
	case action == "scope:machine":
		return m.setScope(true)
	case action == "help":
		m.openHelp()
		return nil
	case action == "back":
		return m.back(false)
	case strings.HasPrefix(action, "bar:"):
		return m.tapBar(strings.TrimPrefix(action, "bar:"))
	case strings.HasPrefix(action, "panel:"):
		return m.revealRow(strings.TrimPrefix(action, "panel:"))
	}
	return nil
}

// revealRow moves the cursor to a line of the panel, and opens what its row is under: a worktree
// with one stack is collapsed by default, so this is the usual case.
func (m *Model) revealRow(id string) tea.Cmd {
	if m.table.SetCursor(id) {
		m.lastMove = m.nowAt
		return nil
	}
	info, ok := m.tree.Lookup(id)
	if !ok || info.Stack == nil {
		return nil
	}
	if info.Kind == treetable.KindContainer {
		m.tree.SetExpanded("stack:"+info.Stack.Project, true)
	}
	if info.Stack.Worktree != nil {
		m.tree.SetExpanded("worktree:"+*info.Stack.Worktree, true)
	}
	for _, r := range m.tree.Visible() {
		if r.ID == id {
			m.wantCursor = id
			m.lastMove = m.nowAt
			return nil
		}
	}
	return m.say("the filter hides this row", theme.ToneDim)
}

func (m *Model) tapBar(action string) tea.Cmd {
	// as for a key: a cursor that a wheel scrolled away comes back before it is acted on
	if m.table.Place() != treetable.CursorVisible {
		m.table.EnsureVisible(m.table.Cursor())
	}
	for _, b := range m.barButtons() {
		if b.Action != action {
			continue
		}
		if b.Reason != "" {
			return m.say(b.Label+": "+b.Reason, theme.ToneDim)
		}
		return m.run(action)
	}
	return nil
}

// tapTable: a tap on another row moves the cursor and does nothing else. A tap on the
// cursor row opens the action sheet. Neither one ever deletes or opens a tab.
func (m *Model) tapTable(x, y int) tea.Cmd {
	hit := m.table.HitTest(x, y)
	switch hit.Zone {
	case treetable.ZoneBody:
		if hit.ID == m.table.Cursor() {
			return m.openMenu()
		}
		m.table.SetCursor(hit.ID)
		m.lastMove = m.nowAt
	case treetable.ZoneGutter:
		return m.markRow(hit.ID) // the cursor and the view stay, so several rows can take a mark
	case treetable.ZoneChevron:
		m.tree.Toggle(hit.ID)
	case treetable.ZoneScrollbar:
		m.table.ScrollTo(hit.Fraction)
	case treetable.ZoneTitle:
		return m.sortByTitle(hit.Column)
	}
	return nil
}

func (m *Model) tapOverlay(action string, inside bool) tea.Cmd {
	switch m.mode {
	case ModeMenu:
		switch {
		case strings.HasPrefix(action, "item:"):
			if it, ok := m.menu.ByAction(strings.TrimPrefix(action, "item:")); ok {
				return m.pickMenu(it)
			}
		case !inside:
			m.closeMenu()
		}
	case ModeConfirm:
		return m.tapConfirm(action)
	case ModeNew:
		return m.tapNew(action)
	case ModeProgress:
		return m.tapProgress(action)
	case ModeLog:
		if action == "close" || !inside {
			m.mode = m.logReturn
		}
	case ModeHelp:
		m.mode = m.helpReturn
	case ModePanel:
		if action == "close" || !inside {
			m.mode = ModeList
		}
	}
	return nil
}

func (m *Model) tapConfirm(action string) tea.Cmd {
	c := &m.confirm
	c.Said = ""
	switch {
	case action == "cancel":
		return m.cancelConfirm()
	case strings.HasPrefix(action, "reason:"):
		c.Said = c.DisabledReason(action)
		return nil
	case action == "confirm":
		// the same two steps as the keys, so a stray tap cannot confirm
		if m.sheetHeld() {
			return nil
		}
		if c.Focus == dialog.FocusCancel {
			c.Focus = dialog.FocusConfirm
			m.armSheet()
			return nil
		}
		return m.confirmNow()
	case action == "hidden":
		return m.unmarkHidden()
	case strings.HasPrefix(action, "option:"):
		if o, ok := c.OptionByDigit(strings.TrimPrefix(action, "option:")); ok {
			return m.pickOption(o)
		}
	}
	return nil
}

func (m *Model) tapNew(action string) tea.Cmd {
	s := &m.newSheet
	if s.Busy {
		return nil
	}
	switch action {
	case "cancel":
		m.mode = ModeList
	case "create":
		return m.submitNew()
	case "open":
		s.Input.Blur()
		s.Open, s.Focus = true, dialog.NewFocusOpen
	case "stay":
		s.Input.Blur()
		s.Open, s.Focus = false, dialog.NewFocusStay
	case "field":
		s.Focus = dialog.NewFocusField
		return s.Input.Focus()
	}
	return nil
}

func (m *Model) tapProgress(action string) tea.Cmd {
	if m.quitMenu != nil {
		if strings.HasPrefix(action, "item:") {
			what := strings.TrimPrefix(action, "item:")
			if what == dialog.CancelAction {
				m.quitMenu = nil
				return nil
			}
			return m.pickQuit(what)
		}
		return nil
	}
	switch {
	case action == "log":
		return m.openLog()
	case action == "close":
		return m.closeResults()
	case action == "stop":
		m.openQuitMenu()
	case strings.HasPrefix(action, "row:"):
		if i, err := strconv.Atoi(strings.TrimPrefix(action, "row:")); err == nil {
			m.progress.ToggleID(i)
		}
	}
	return nil
}
