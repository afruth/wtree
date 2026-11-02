package model

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/afruth/wtree/tui/internal/core"
	"github.com/afruth/wtree/tui/internal/dialog"
	"github.com/afruth/wtree/tui/internal/theme"
	"github.com/afruth/wtree/tui/internal/tree"
	"github.com/afruth/wtree/tui/internal/treetable"
	"github.com/charmbracelet/x/ansi"
)

// geom says where the parts of the screen are.
type geom struct {
	tableY, tableH, tableW int
	bannerY                int // -1: no banner
	statusY                int // -1: no status line
	barY, barH             int
}

func (m *Model) geometry() geom {
	l := m.layout
	g := geom{tableW: l.TableWidth(m.panelOn()), bannerY: -1, statusY: -1}
	banner := m.banner() != ""
	switch {
	case l.Short:
		g.barH = 1
		g.barY = l.Height - 1
		g.tableY = 0
		g.tableH = l.Height - 1
	case l.Tier == theme.TierNarrow:
		// A phone has no row to spare: the list goes from the header to the bar, and the
		// status line is there only when it has something to say.
		g.barH = 2
		g.barY = l.Height - 2
		g.tableY = 1
		g.tableH = g.barY - 1
		if m.statusLine() != "" {
			g.statusY = g.barY - 1
			g.tableH--
		}
	default:
		g.barH = 1
		g.barY = l.Height - 1
		g.statusY = l.Height - 2
		g.tableY = 1
		g.tableH = g.statusY - 1
	}
	if banner && !l.Short {
		g.bannerY = 1
		g.tableY = 2
		g.tableH--
	}
	g.tableH = max(g.tableH, 0)
	return g
}

// sync brings the table in line with the state, at the end of each Update. So the table
// geometry always answers for the frame that View draws.
func (m *Model) sync() {
	if m.statusMark && len(m.tree.Marked()) == 0 {
		m.status, m.statusMark = "", false // it said that rows are still marked
	}
	m.table.SetTitlePrefix(m.shortHeader(1 << 20))
	m.table.SetReasons(m.tree.View() == tree.ViewCleanup)
	rows := m.tree.Visible()
	sum := m.tree.Summary()
	switch {
	case m.listErr != "" && sum.Worktrees == 0 && sum.Stacks == 0:
		rows = nil
		m.table.SetEmptyText(m.listErr)
	case !m.tree.StructureDone() && sum.Worktrees == 0 && sum.Stacks == 0:
		rows = nil
		m.table.SetEmptyText("loading worktrees" + m.th.G.Ellipsis)
	case !m.tree.StructureDone() && m.flatSort():
		m.table.SetEmptyText("loading worktrees" + m.th.G.Ellipsis) // no rows yet says nothing yet
	case m.tree.Filter().Active():
		m.table.SetEmptyText(`nothing matches "` + m.tree.Filter().String() + `" ` + m.th.G.Dot + ` backspace clear`)
	case m.flatSort():
		// a sort shows only stack rows, and here there are none
		m.table.SetEmptyText("no stacks here " + m.th.G.Dot + " o shows the tree")
	default:
		m.table.SetEmptyText("")
	}
	if m.tree.Filter().Active() && len(rows) > 0 && !hasDataRow(rows) {
		rows = nil
	}
	flash := m.flashIDs(rows)
	m.table.SetRows(rows)
	// the header on a short screen gives way by whole segments, and never cuts into a number
	if room := m.table.TitleRoom(); m.layout.Short && ansi.StringWidth(m.shortHeader(1<<20)) > room {
		m.table.SetTitlePrefix(m.shortHeader(room))
	}
	m.syncGeometry()
	asked := m.wantCursor != ""
	if asked {
		if m.table.SetCursor(m.wantCursor) || m.tree.StructureDone() {
			m.wantCursor = ""
		}
	}
	m.placeFirstCursor(asked, len(rows) > 0)
	if flash != nil {
		m.table.SetFlash(flash)
	}
	if m.seekMatch {
		m.seekMatch = false
		m.cursorToMatch()
	}
	m.syncGeometry() // the cursor moved, and the status line can say where it went
	m.clampSheetScroll()
	room := m.layout.Width - 6
	if f := m.tree.Filter(); f.Invalid() {
		room -= ansi.StringWidth(f.Error()) + 2
	}
	m.search.SetWidth(max(room, 8))
}

// placeFirstCursor keeps the cursor on the top row during the first load. Rows arrive in scan
// order and the tree sorts the current worktree to the top, while the cursor follows the id of
// the first row that came, which can be any repo. Any move by the user ends this.
func (m *Model) placeFirstCursor(asked, hasRows bool) {
	if m.cursorPlaced {
		return
	}
	if asked || (m.autoCursor != "" && m.table.Cursor() != m.autoCursor) {
		m.cursorPlaced = true
		return
	}
	if hasRows {
		m.table.CursorHome()
		m.autoCursor = m.table.Cursor()
	}
	m.cursorPlaced = m.tree.StructureDone()
}

// syncGeometry gives the table its place. Not on every update: SetSize scrolls to the cursor,
// and a wheel leaves it behind. When only the status line came or went, a cursor that was in
// view stays in view, and one that a wheel left behind stays behind.
func (m *Model) syncGeometry() {
	g := m.geometry()
	old := m.lastGeom
	if g == old {
		return
	}
	m.lastGeom = g
	if g.tableW == old.tableW && g.tableY == old.tableY && g.barY == old.barY && g.bannerY == old.bannerY {
		place := m.table.Place()
		m.table.SetHeight(g.tableH)
		if place == treetable.CursorVisible {
			m.table.EnsureVisible(m.table.Cursor())
		}
		return
	}
	m.table.SetOrigin(0, g.tableY)
	m.table.SetSize(g.tableW, g.tableH)
}

// clampSheetScroll keeps the scroll of a sheet at what it can show, so that a key that goes
// back always moves something.
func (m *Model) clampSheetScroll() {
	f := m.overlay()
	if f == nil {
		return
	}
	switch m.mode {
	case ModeConfirm:
		m.confirm.Scroll = f.Scroll
	case ModeLog:
		m.logView.Scroll = f.Scroll
	case ModeHelp:
		m.help.Scroll = f.Scroll
	case ModePanel:
		m.info.Scroll = f.Scroll
	}
}

// cursorToMatch puts the cursor on a row that the filter matched, if it is on context.
func (m *Model) cursorToMatch() {
	rows := m.table.Rows()
	for _, r := range rows {
		if r.ID == m.table.Cursor() && !r.Dim && r.Kind != treetable.KindGroup {
			return
		}
	}
	for _, r := range rows {
		if !r.Dim && (r.Kind == treetable.KindWorktree || r.Kind == treetable.KindStack || r.Kind == treetable.KindContainer) {
			m.table.SetCursor(r.ID)
			return
		}
	}
}

func (m *Model) flatSort() bool {
	mode, _ := m.tree.Sort()
	return mode != tree.SortTree
}

func hasDataRow(rows []treetable.Row) bool {
	for _, r := range rows {
		if r.Kind != treetable.KindSeparator && r.Kind != treetable.KindGroup {
			return true
		}
	}
	return false
}

// flashIDs says what changed in a live update, on wide only and not on a slow link. It
// looks at the state of a row, and not at its CPU, memory or age: those move on every tick,
// and a list that flashes all the time says nothing.
func (m *Model) flashIDs(rows []treetable.Row) []string {
	if !m.flashArmed {
		return nil
	}
	m.flashArmed = false
	if m.layout.Tier != theme.TierWide || m.o.Caps.Slow {
		return nil
	}
	plain := func(in []treetable.Row) []treetable.Row {
		out := make([]treetable.Row, len(in))
		for i, r := range in {
			r.CPU, r.Mem, r.Age = treetable.Cell{}, treetable.Cell{}, treetable.Cell{}
			r.Expanded, r.Marked, r.Dim = false, false, false
			out[i] = r
		}
		return out
	}
	ids := treetable.ChangedIDs(plain(m.table.Rows()), plain(rows))
	if len(ids) == 0 {
		return nil
	}
	m.flashGen++
	return ids
}

// overlay is the sheet on top of the list, if there is one.
func (m *Model) overlay() *dialog.Frame {
	var f dialog.Frame
	defer func() {
		// In the list area, and not across the divider of the panel that it hides.
		if m.panelOnScreen() && f.W <= m.layout.TableWidth(true) {
			f.MoveX((m.layout.TableWidth(true)-f.W)/2 - f.X)
		}
	}()
	switch m.mode {
	case ModeMenu:
		f = m.menu.Render(m.th, m.layout)
	case ModeConfirm:
		f = m.confirm.Render(m.th, m.layout)
	case ModeNew:
		f = m.newSheet.Render(m.th, m.layout)
	case ModeProgress:
		if m.quitMenu != nil {
			f = m.quitMenu.Render(m.th, m.layout)
		} else {
			f = m.progress.Render(m.th, m.layout)
		}
	case ModeLog:
		f = m.logView.Render(m.th, m.layout)
	case ModeHelp:
		f = m.help.Render(m.th, m.layout)
	case ModePanel:
		f = m.info.Render(m.th, m.layout)
	default:
		if !m.planWaiting() {
			return nil
		}
		f = m.waitSheet().Render(m.th, m.layout)
	}
	return &f
}

// overlayOpen says that a sheet is on the list, without drawing it.
func (m *Model) overlayOpen() bool {
	switch m.mode {
	case ModeMenu, ModeConfirm, ModeNew, ModeProgress, ModeLog, ModeHelp, ModePanel:
		return true
	}
	return false
}

type composed struct {
	lines []string
	zones []dialog.Zone
}

// compose draws the whole screen from the state, and says what a tap can hit. It changes
// nothing: View and the hit test both call it, so they cannot disagree.
func (m *Model) compose() composed {
	l := m.layout
	blank := strings.Repeat(" ", max(l.Width, 0))
	c := composed{lines: make([]string, l.Height)}
	for i := range c.lines {
		c.lines[i] = blank
	}
	if l.TooSmall {
		copy(c.lines, strings.Split(m.table.Render(), "\n"))
		return c
	}
	g := m.geometry()
	if !l.Short {
		line, zones := m.header()
		c.lines[0] = line
		c.zones = append(c.zones, zones...)
	}
	if g.bannerY >= 0 {
		c.lines[g.bannerY] = fit(m.th.Bold(theme.ToneWarn).Render(" "+m.banner()), l.Width)
	}
	table := strings.Split(m.table.Render(), "\n")
	panel := m.panelOnScreen() // behind a sheet it goes dim, like the list, and nothing jumps
	var panelLines []panelLine
	if panel {
		panelLines = m.panelLines(g.tableH)
	}
	for i, line := range table {
		y := g.tableY + i
		if y < 0 || y >= l.Height {
			continue
		}
		if panel {
			line = fit(line, g.tableW) + m.th.Style(theme.ToneDim).Render(m.th.G.Rule) + " "
			if i < len(panelLines) {
				line += fit(panelLines[i].text, theme.PanelWidth-2)
				if panelLines[i].action != "" {
					c.zones = append(c.zones, dialog.Zone{X0: g.tableW, Y0: y, X1: l.Width, Y1: y + 1, Action: panelLines[i].action})
				}
			}
		}
		c.lines[y] = fit(line, l.Width)
	}
	if l.Short {
		// the short rule merges the header with the titles: the same scope zones
		_, zones := m.shortHeaderZones()
		c.zones = append(c.zones, zones...)
	}
	if g.statusY >= 0 {
		c.lines[g.statusY] = fit(m.statusLine(), l.Width)
	}
	bar, zones := m.bar(g)
	for i, line := range bar {
		c.lines[g.barY+i] = fit(line, l.Width)
	}
	c.zones = append(c.zones, zones...)
	return c
}

func fit(s string, w int) string {
	n := ansi.StringWidth(s)
	switch {
	case n > w:
		return ansi.Truncate(s, w, "")
	case n < w:
		return s + strings.Repeat(" ", w-n)
	}
	return s
}

// View draws from the state.
func (m *Model) View() tea.View {
	var v tea.View
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.ReportFocus = true
	v.WindowTitle = "wtree"
	if m.w == 0 {
		return v
	}
	if m.quitting {
		v.Content = ""
		m.setHostStatus(&v)
		return v
	}
	c := m.compose()
	lines := c.lines
	if f := m.overlay(); f != nil {
		keep := 0
		if m.panelOnScreen() && f.X+f.W <= m.layout.TableWidth(true) {
			keep = m.layout.TableWidth(true)
		}
		lines = stamp(m.th, lines, *f, m.layout.Width, keep)
	}
	v.Content = strings.Join(lines, "\n")
	m.setHostStatus(&v)
	return v
}

// stamp draws a frame over the list, which goes dim behind it. The lines that the frame is on
// have nothing beside it: a piece of a row at its edge reads as a stray glyph. From cell keep on
// the line stays, when that is the panel, which the frame stops short of.
func stamp(th theme.Theme, base []string, f dialog.Frame, width, keep int) []string {
	out := make([]string, len(base))
	dim := th.Style(theme.ToneDim)
	for i, line := range base {
		out[i] = dim.Render(ansi.Strip(line))
	}
	for i, line := range f.Lines {
		y := f.Y + i
		if y < 0 || y >= len(out) {
			continue
		}
		if keep > 0 {
			out[y] = fit(strings.Repeat(" ", max(f.X, 0))+line, keep) + dim.Render(ansi.Strip(ansi.Cut(base[y], keep, width)))
			continue
		}
		out[y] = fit(strings.Repeat(" ", max(f.X, 0))+line, width)
	}
	return out
}

// ---- header

func (m *Model) repoSegmentText() string {
	if m.layout.Tier == theme.TierNarrow || m.repoName == "" {
		return "repo"
	}
	return "repo " + treetable.Sanitize(m.repoName)
}

// segments is the scope control: the key g, then the two scopes, each a block like the buttons
// of the bar, so that it reads as a control and not as a title. The chosen one is in accent.
// A tap on g switches, a tap on a scope picks it.
func (m *Model) segments(x int) (string, []dialog.Zone) {
	g := m.th.G
	draw := func(text string, on, off bool) string {
		if m.th.ASCII {
			if on {
				return m.th.Bold(theme.ToneAccent).Render(g.ScopeOpen + text + g.ScopeClose)
			}
			return m.th.Style(theme.ToneFg).Render(" " + text + " ")
		}
		switch {
		case on:
			return m.th.Button(theme.ToneAccent).Bold(true).Render(" " + text + " ")
		case off:
			return m.th.ButtonOff().Render(" " + text + " ")
		}
		return m.th.Button(theme.ToneDim).Render(" " + text + " ")
	}
	key := m.th.Button(theme.ToneDim).Bold(true).Render(" g")
	if m.th.ASCII {
		key = m.th.Bold(theme.ToneAccent).Render("g ")
	}
	repoText, machine := m.repoSegmentText(), "machine"
	if repoText == "repo" {
		repoText = " repo " // each segment is a target of at least 8 cells
	}
	repo := draw(repoText, !m.machine(), m.noRepo)
	mach := draw(machine, m.machine(), false)
	sep := m.th.Button(theme.ToneDim).Render(" ")
	if m.th.ASCII {
		sep = ""
	}
	kw, rw, sw, mw := ansi.StringWidth(key), ansi.StringWidth(repo), ansi.StringWidth(sep), ansi.StringWidth(mach)
	y := 0
	zones := []dialog.Zone{
		{X0: x, Y0: y, X1: x + kw + sw, Y1: y + 1, Action: "scope:toggle"},
		{X0: x + kw + sw, Y0: y, X1: x + kw + sw + rw, Y1: y + 1, Action: "scope:repo"},
		{X0: x + kw + sw + rw, Y0: y, X1: x + kw + sw + rw + mw, Y1: y + 1, Action: "scope:machine"},
	}
	return key + sep + repo + mach, zones
}

func (m *Model) spinner() string {
	if !m.busy() || !m.motionAllowed() {
		return "   "
	}
	return []string{m.th.G.Dot + "  ", m.th.G.Dot + m.th.G.Dot + " ", m.th.G.Dot + m.th.G.Dot + m.th.G.Dot}[m.spin%3]
}

// reclaimable is the total, or "" while there is no number to show, and then its segment goes
// whole. It is stale while a part of it has no size, because that number can only be too low.
func (m *Model) reclaimable(sum tree.Summary) string {
	return m.sizeText(sum, sum.Reclaimable)
}

func (m *Model) sizeText(sum tree.Summary, v int64) string {
	switch {
	case !sum.ReclaimableKnown:
		return ""
	case sum.ReclaimableUnknown && v == 0:
		return "" // a tilde on nothing says "stale zero"
	case sum.ReclaimableUnknown:
		return m.th.G.Stale + tree.FormatDisk(v)
	}
	return tree.FormatDisk(v)
}

// agoWord is a data age. Zero seconds is "now": "0s" reads as a bug.
func agoWord(seconds int64) string {
	if seconds < 1 {
		return "now"
	}
	return tree.FormatAge(seconds)
}

func (m *Model) sortWord() string {
	mode, rev := m.tree.Sort()
	if mode == tree.SortTree {
		return ""
	}
	text := "sort: " + mode.String()
	if rev {
		text += " up"
	}
	return text
}

// firstLoad says that no list has ended yet.
func (m *Model) firstLoad() bool { return m.loading && !m.everLoaded }

// compact is the numbers where room is short. It says nothing of the total before it is known.
func (m *Model) compact(sum tree.Summary) string { return m.compactAs(sum, "clean ") }

func (m *Model) compactAs(sum tree.Summary, word string) string {
	dot := " " + m.th.G.Dot + " "
	if m.layout.Short {
		dot = m.th.G.Dot
	}
	return strings.Join(m.compactParts(sum, word), dot)
}

func (m *Model) compactParts(sum tree.Summary, word string) []string {
	var parts []string
	if sum.Worktrees > 0 || m.tree.StructureDone() {
		parts = append(parts, itoa(sum.Worktrees)+" wt")
	}
	if sum.Stacks > 0 {
		parts = append(parts, itoa(sum.Stacks)+" st")
	}
	if size := m.reclaimable(sum); size != "" {
		parts = append(parts, word+size)
	}
	if m.layout.Short {
		if word := m.sortWord(); word != "" {
			parts = append(parts, word)
		}
	}
	return parts
}

// ageParts is the data age: `live 2s`, `disk 18m`, in amber past 30 minutes, and
// `disk updating…` while a scan runs.
func (m *Model) dockerDown() bool {
	_, down := m.warnings[core.WarnDockerDown]
	return down
}

func (m *Model) ageParts() (live string, disk string, diskTone theme.Tone) {
	dockerless := false
	if _, ok := m.warnings[core.WarnDockerMissing]; ok {
		dockerless = true
	}
	_, timedOut := m.warnings[core.WarnDockerTimeout]
	sum := m.tree.Summary()
	switch {
	case dockerless:
	case m.dockerDown(), timedOut: // the stage ended, and it read nothing: no age to give
	case !sum.StatsAt.IsZero():
		live = "live " + agoWord(int64(m.nowAt.Sub(sum.StatsAt).Seconds()))
	}
	diskTone = theme.ToneDim
	switch {
	case dockerless:
	case m.diskScan:
		disk = "disk updating" + m.th.G.Ellipsis
	case m.diskKnown:
		age := m.diskAgeBase + int64(m.nowAt.Sub(m.diskAt).Seconds())
		disk = "disk " + agoWord(age)
		if age > 30*60 {
			diskTone = theme.ToneWarn
		}
	}
	return live, disk, diskTone
}

func (m *Model) ageText(withDisk bool) string {
	live, disk, diskTone := m.ageParts()
	dim := m.th.Style(theme.ToneDim)
	var parts []string
	if live != "" {
		parts = append(parts, dim.Render(live))
	}
	if withDisk && disk != "" {
		parts = append(parts, m.th.Style(diskTone).Render(disk))
	}
	return strings.Join(parts, dim.Render(" "+m.th.G.Dot+" "))
}

// headerPart is a segment of the header right of the scope control. It gives way whole, so
// that the header never cuts into a number.
type headerPart struct {
	text  string // with its style
	right bool
	drop  int // the lowest goes first
	spin  bool
}

func (m *Model) header() (string, []dialog.Zone) {
	l := m.layout
	dim := m.th.Style(theme.ToneDim)
	if m.tree.View() == tree.ViewCleanup {
		return m.cleanupHeader()
	}
	name := " " + m.th.Bold(theme.ToneAccent).Render("wtree") + " "
	x := ansi.StringWidth(name)
	if l.Tier != theme.TierNarrow {
		name += " "
		x++
	}
	seg, zones := m.segments(x)
	line := name + seg
	sum := m.tree.Summary()
	if l.Tier == theme.TierNarrow {
		return joinEnds(line, m.narrowRight(sum, l.Width-ansi.StringWidth(line)-2)+" ", l.Width), zones
	}

	fg := dim // numbers are information; only the blocks act
	var parts []headerPart
	wtWord := " worktrees"
	if sum.Worktrees == 1 {
		wtWord = " worktree"
	}
	if l.Tier != theme.TierWide {
		wtWord = " wt"
	}
	if sum.Worktrees > 0 || m.tree.StructureDone() {
		parts = append(parts, headerPart{text: fg.Render(itoa(sum.Worktrees) + wtWord), drop: 5})
	}
	if sum.Stacks > 0 {
		stacks := " stacks"
		if sum.Stacks == 1 {
			stacks = " stack"
		}
		parts = append(parts, headerPart{text: fg.Render(itoa(sum.Stacks) + stacks), drop: 4})
	}
	if size := m.reclaimable(sum); size != "" {
		parts = append(parts, headerPart{text: fg.Render("cleanup " + size), drop: 6})
	}
	if word := m.sortWord(); word != "" {
		parts = append(parts, headerPart{text: fg.Render(word), drop: 7})
	}
	if m.firstLoad() {
		parts = append(parts, headerPart{text: dim.Render("loading" + m.th.G.Ellipsis), right: true, drop: 8})
	} else if spin := strings.TrimRight(m.spinner(), " "); spin != "" {
		parts = append(parts, headerPart{text: dim.Render(spin), right: true, drop: 1, spin: true})
	}
	live, disk, diskTone := m.ageParts()
	if live != "" {
		parts = append(parts, headerPart{text: dim.Render(live), right: true, drop: 3})
	}
	if disk != "" && l.Tier == theme.TierWide {
		parts = append(parts, headerPart{text: m.th.Style(diskTone).Render(disk), right: true, drop: 2})
	}
	helpOn := l.Tier == theme.TierWide
	if helpOn {
		parts = append(parts, headerPart{text: m.th.Button(theme.ToneDim).Bold(true).Render(" ?") + m.th.Button(theme.ToneDim).Render(" help "), right: true, drop: 9})
	}
	dot := dim.Render(" " + m.th.G.Dot + " ")
	build := func(ps []headerPart) (string, string) {
		var left, right []string
		spin := ""
		for _, p := range ps {
			switch {
			case p.spin:
				spin = p.text + " "
			case p.right:
				right = append(right, p.text)
			default:
				left = append(left, p.text)
			}
		}
		return line + "  " + strings.Join(left, dot), spin + strings.Join(right, dot) + " "
	}
	for {
		left, right := build(parts)
		if ansi.StringWidth(left)+1+ansi.StringWidth(right) <= l.Width || len(parts) == 0 {
			if helpOn && strings.Contains(ansi.Strip(right), "? help") {
				zones = append(zones, dialog.Zone{X0: l.Width - 9, Y0: 0, X1: l.Width, Y1: 1, Action: "help"})
			}
			return joinEnds(left, right, l.Width), zones
		}
		low := 0
		for i, p := range parts {
			if p.drop < parts[low].drop {
				low = i
			}
		}
		parts = append(parts[:low:low], parts[low+1:]...)
	}
}

// narrowRight is the right half of the header on a phone: the sort if there is one, and the
// numbers, with the age of the data when it fits as well.
func (m *Model) narrowRight(sum tree.Summary, room int) string {
	dim := m.th.Style(theme.ToneDim)
	dot := " " + m.th.G.Dot + " "
	var lead string
	if word := m.sortWord(); word != "" {
		lead = word
	}
	counts := m.compact(sum)
	if ansi.StringWidth(counts) > room {
		counts = m.compactAs(sum, m.th.G.Cleanup+" ") // the mark of the cleanup group in the list
	}
	if m.firstLoad() {
		text := "loading" + m.th.G.Ellipsis
		if counts != "" && ansi.StringWidth(counts)+2+ansi.StringWidth(text) <= room {
			text = counts + "  " + text
		}
		return dim.Render(text)
	}
	join := func(parts ...string) string {
		var kept []string
		for _, p := range parts {
			if p != "" {
				kept = append(kept, p)
			}
		}
		return strings.Join(kept, dot)
	}
	if lead == "" {
		for _, withDisk := range []bool{true, false} {
			age := m.ageText(withDisk)
			if age != "" && ansi.StringWidth(counts)+3+ansi.StringWidth(age) <= room {
				return dim.Render(counts) + dim.Render(dot) + age
			}
		}
	}
	clean := ""
	if size := m.reclaimable(sum); size != "" {
		clean = "clean " + size
	}
	for _, text := range []string{join(lead, counts), join(lead, clean), lead} {
		if text != "" && ansi.StringWidth(text) <= room {
			return dim.Render(text)
		}
	}
	return ""
}

// ageFits says that the header of a phone has room for the age of the data beside the numbers.
func (m *Model) ageFits(sum tree.Summary, room int) bool {
	age := ansi.StringWidth(m.ageText(true))
	return age > 0 && ansi.StringWidth(m.compactAs(sum, m.th.G.Cleanup+" "))+3+age <= room
}

func joinEnds(left, right string, w int) string {
	gap := w - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 1 {
		// the left side wins: the scope control and the name must stay
		room := w - ansi.StringWidth(left) - 1
		if room < 1 {
			return fit(left, w)
		}
		return left + " " + ansi.TruncateLeft(right, ansi.StringWidth(right)-room, "")
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m *Model) cleanupHeader() (string, []dialog.Zone) {
	sum := m.tree.Summary()
	dim := m.th.Style(theme.ToneDim)
	dot := dim.Render(" " + m.th.G.Dot + " ")
	// A tier with nothing in it has a dash, and not a zero that reads as a number to look into.
	size := func(v int64) string {
		text := m.sizeText(sum, v)
		if v == 0 || text == "" {
			text = m.th.G.Kept
		}
		return " " + m.th.Bold(theme.ToneFg).Render(text)
	}
	left := " " + m.th.Bold(theme.ToneAccent).Render("cleanup") + "   "
	if m.layout.Tier == theme.TierNarrow {
		left = " " // the numbers need the room, and the bar says that this is cleanup
	}
	back := " c back "
	if sum.ReclaimableKnown {
		tiers := func(a, b, c string) string {
			return m.th.Style(theme.ToneOk).Render("safe") + a + dot +
				m.th.Style(theme.ToneWarn).Render("needs a look") + b + dot +
				dim.Render("never") + c
		}
		full := tiers(size(sum.CleanupSafe), size(sum.CleanupNeedsLook), size(sum.CleanupNever))
		// On a phone the words stay, and the sizes go: the headings of the rows have them.
		if ansi.StringWidth(left)+ansi.StringWidth(full)+len(back)+1 > m.layout.Width {
			full = tiers("", "", "")
		}
		left += full
	} else {
		left += dim.Render("reading" + m.th.G.Ellipsis)
	}
	right := dim.Render(back)
	if m.layout.Tier != theme.TierNarrow {
		if _, disk, tone := m.ageParts(); disk != "" {
			// the age goes whole, and it is never cut to a number with no word
			with := m.th.Style(tone).Render(disk) + dim.Render(" "+m.th.G.Dot+back)
			if ansi.StringWidth(left)+1+ansi.StringWidth(with) <= m.layout.Width {
				right = with
			}
		}
	}
	w := m.layout.Width
	line := joinEnds(left, right, w)
	bw := ansi.StringWidth(back)
	return line, []dialog.Zone{{X0: w - bw, Y0: 0, X1: w, Y1: 1, Action: "back"}}
}

// shortHeader is the status line that shares its line with the column titles.
func (m *Model) shortHeader(room int) string {
	if !m.layout.Short {
		return ""
	}
	if m.tree.View() == tree.ViewCleanup {
		line, _ := m.cleanupHeader()
		return strings.TrimRight(ansi.Truncate(line, m.shortRoom(), ""), " ")
	}
	line, _ := m.shortHeaderZones()
	if ansi.StringWidth(line) <= room {
		return line
	}
	// the segments give way whole, the last first, and the scope control stays
	name := " " + m.th.Bold(theme.ToneAccent).Render("wtree") + " "
	seg, _ := m.segments(ansi.StringWidth(name))
	base := name + seg
	parts := m.compactParts(m.tree.Summary(), "clean ")
	for len(parts) > 0 {
		parts = parts[:len(parts)-1]
		if len(parts) == 0 {
			break
		}
		text := base + " " + m.th.Style(theme.ToneDim).Render(strings.Join(parts, m.th.G.Dot))
		if ansi.StringWidth(text) <= room {
			return text
		}
	}
	return base
}

func (m *Model) shortRoom() int { return m.layout.Width }

func (m *Model) shortHeaderZones() (string, []dialog.Zone) {
	name := " " + m.th.Bold(theme.ToneAccent).Render("wtree") + " "
	seg, zones := m.segments(ansi.StringWidth(name))
	sum := m.tree.Summary()
	return name + seg + " " + m.th.Style(theme.ToneDim).Render(m.compact(sum)), zones
}

// ---- banner and status

func (m *Model) banner() string {
	dot := " " + m.th.G.Dot + " "
	if m.listErr != "" && m.tree.Summary().Worktrees > 0 {
		return m.listErr + dot + "r retry"
	}
	if _, ok := m.warnings[core.WarnDockerMissing]; ok {
		return "no docker on PATH, so no stacks" + dot + "r retry"
	}
	if _, ok := m.warnings[core.WarnDockerDown]; ok {
		return "docker is not running" + dot + "r retry"
	}
	if text, ok := m.warnings[core.WarnDockerTimeout]; ok {
		if ansi.StringWidth(text)+len(" r retry")+3 > m.layout.Width {
			text = "Docker did not answer" // the short words, and no sentence cut in a word
		}
		return text + dot + "r retry"
	}
	return ""
}

func (m *Model) statusLine() string {
	dim := m.th.Style(theme.ToneDim)
	w := m.layout.Width
	if m.planWaiting() {
		return " " + m.th.Style(theme.ToneAccent).Render(m.planWaitText())
	}
	if m.mode == ModeSearch {
		f := m.tree.Filter()
		field := " " + m.search.View()
		if f.Invalid() {
			return joinEnds(field, m.th.Style(theme.ToneDanger).Render(f.Error())+" ", w)
		}
		return field
	}
	if m.status != "" {
		return " " + m.th.Style(m.statusTone).Render(treetable.TruncateEnd(m.status, w-2, m.th.G.Ellipsis))
	}
	var left []string
	if text := m.markSentence(); text != "" {
		left = append(left, m.th.Bold(theme.ToneAccent).Render(text))
	}
	if f := m.tree.Filter(); f.Active() {
		left = append(left, dim.Render("filter: ")+m.th.Style(theme.ToneFg).Render(treetable.Sanitize(f.String()))+dim.Render(" "+m.th.G.Dot+" backspace clear"))
	}
	switch m.table.Place() {
	case treetable.CursorAbove:
		left = append(left, dim.Render("cursor: "+m.cursorName()+" (above)"))
	case treetable.CursorBelow:
		left = append(left, dim.Render("cursor: "+m.cursorName()+" (below)"))
	}
	if len(left) == 0 {
		if hint := m.hintLine(); hint != "" {
			left = append(left, dim.Render(hint))
		}
	}
	if m.layout.Tier == theme.TierNarrow && len(left) == 0 {
		// The age has its place in the header when it fits. Here only what a phone must see.
		sum := m.tree.Summary()
		if _, disk, tone := m.ageParts(); disk != "" && (tone == theme.ToneWarn || m.diskScan) && !m.ageFits(sum, w-26) {
			return " " + m.th.Style(tone).Render(disk)
		}
		return ""
	}
	line := " " + strings.Join(left, dim.Render(" "+m.th.G.Dot+" "))
	var right []string
	switch m.layout.Tier {
	case theme.TierMedium:
		_, disk, tone := m.ageParts()
		if disk != "" && m.tree.View() != tree.ViewCleanup { // the header of cleanup says it
			right = append(right, m.th.Style(tone).Render(disk))
		}
	}
	if len(right) == 0 {
		return line
	}
	return joinEnds(line, strings.Join(right, dim.Render(" "+m.th.G.Dot+" "))+" ", w)
}

// hintLine fills the status line with what the empty states say.
func (m *Model) hintLine() string {
	sum := m.tree.Summary()
	dot := " " + m.th.G.Dot + " "
	if m.tree.View() == tree.ViewCleanup {
		// a total of zero that does not know all the sizes is no "nothing": it says that only
		// when no row has a thing to clean
		if sum.ReclaimableKnown && sum.Reclaimable == 0 && sum.CleanupRows == 0 {
			return "nothing to clean" + dot + "0 B reclaimable"
		}
		return ""
	}
	if m.tree.StructureDone() && !m.machine() && sum.Worktrees == 1 {
		// all of it, or fewer words: an action is never cut in half
		for _, text := range []string{
			"no worktrees besides main" + dot + "n new worktree" + dot + "g machine scope",
			"no worktrees besides main" + dot + "n new" + dot + "g machine",
			"only main" + dot + "n new" + dot + "g machine",
		} {
			if ansi.StringWidth(text)+2 <= m.layout.Width {
				return text
			}
		}
	}
	return ""
}

// planWaitText says that a key was heard, and how to take it back. A plan can take a long time.
func (m *Model) planWaitText() string {
	return "reading the plan" + m.th.G.Ellipsis + " " + m.th.G.Dot + " backspace cancels"
}

func (m *Model) cursorName() string {
	for _, r := range m.table.Rows() {
		if r.ID == m.table.Cursor() {
			return r.Name
		}
	}
	return ""
}

// ---- action bar

type barButton struct {
	Key, Short, Label string
	Action            string
	Reason            string
	Primary           bool
}

func (m *Model) gated(b barButton) barButton {
	if !m.structureReady() {
		b.Reason = m.loadingText()
	}
	return b
}

// barButtons is the bar for the cursor row, most relevant first. It always ends with the
// actions sheet. Disabled buttons stay and give their reason on a tap.
func (m *Model) barButtons() []barButton {
	if m.mode == ModeSearch {
		return []barButton{
			{Key: "enter", Label: "apply", Action: actApplySrch, Primary: true},
			{Key: "backspace", Label: "clear", Action: actClearSrch},
		}
	}
	actions := barButton{Key: "a", Label: "actions", Action: actActions}
	search := barButton{Key: "/", Label: "search", Action: actSearch}
	cleanup := barButton{Key: "c", Label: "cleanup", Action: actCleanup}
	markBtn := barButton{Key: "space", Label: "mark", Action: actMark}
	stop := m.gated(barButton{Key: "s", Label: "stop", Action: actStop})
	del := m.gated(barButton{Key: "d", Label: "delete", Action: actDelete})
	marks := m.tree.MarkCounts().Total
	if m.tree.View() == tree.ViewCleanup {
		del.Primary = true
		if marks > 0 {
			del.Label, del.Short = "delete "+itoa(marks), "del "+itoa(marks)
		} else if info, ok := m.cursorInfo(); !ok || info.Kind == treetable.KindGroup {
			del.Primary, del.Reason = false, "choose a worktree or a stack"
		} else if phrase := m.neverPhrase(info); phrase != "" {
			del.Primary, del.Reason = false, phrase
		}
		return []barButton{del, markBtn, {Key: "A", Label: "mark safe", Short: "mk safe", Action: actMarkAll}, {Key: "c", Label: "back", Action: actCleanup}, search, actions}
	}
	if marks > 0 {
		del.Label, del.Short = "delete "+itoa(marks), "del "+itoa(marks)
		stop.Label = "stop " + itoa(marks)
		del.Primary = true
		return []barButton{del, stop, {Key: "u", Label: "unmark", Action: actUnmarkAll}, markBtn, cleanup, search, actions}
	}
	info, ok := m.cursorInfo()
	open := barButton{Key: "enter", Label: "open", Action: actOpen, Primary: true}
	if !ok {
		open.Reason = "no row here"
		for _, b := range []*barButton{&stop, &del, &markBtn} {
			if b.Reason == "" {
				b.Reason = "no row here"
			}
		}
		return []barButton{open, stop, del, markBtn, cleanup, search, actions}
	}
	switch info.Kind {
	case treetable.KindWorktree:
		if info.Worktree.Missing {
			open.Reason = "its directory is gone"
		}
		running := 0
		for i := range info.Stacks {
			running += runningOf(&info.Stacks[i])
		}
		if stop.Reason == "" && running == 0 {
			stop.Reason = "nothing is running"
		}
		if phrase := m.neverPhrase(info); phrase != "" && del.Reason == "" {
			del.Reason = phrase
		}
		return []barButton{open, stop, del, markBtn, cleanup, search, actions}
	case treetable.KindStack:
		st := info.Stack
		if ok, reason := stackHasDir(st); !ok {
			open.Reason = reason
		}
		open.Primary = false
		if stop.Reason == "" && runningOf(st) == 0 {
			stop.Reason = "nothing is running"
		}
		switch {
		case st.Kind == core.StackGone || st.Kind == core.StackVolumes:
			del.Primary = true
			return []barButton{del, stop, open, markBtn, cleanup, search, actions}
		case runningOf(st) > 0:
			stop.Primary = true
			return []barButton{stop, open, del, markBtn, cleanup, search, actions}
		}
		open.Primary = true
		return []barButton{open, stop, del, markBtn, cleanup, search, actions}
	case treetable.KindContainer:
		open.Label = "panel"
		open.Action = actPanel
		stop.Reason = "stop acts on a whole stack"
		del.Reason = "delete acts on a whole stack"
		markBtn.Reason = "only worktrees and stacks take a mark"
		return []barButton{open, stop, del, markBtn, cleanup, search, actions}
	}
	cleanup.Primary = true
	open.Primary, open.Reason = false, "choose a worktree or a stack"
	stop.Reason, del.Reason = "choose a worktree or a stack", "choose a worktree or a stack"
	return []barButton{cleanup, open, stop, del, search, actions}
}

func (m *Model) bar(g geom) ([]string, []dialog.Zone) {
	buttons := m.barButtons()
	l := m.layout
	y := g.barY
	if l.Tier == theme.TierNarrow && !l.Short {
		return m.narrowBar(buttons, y)
	}
	limit := 7
	if l.Tier != theme.TierWide {
		limit = 5
	}
	if len(buttons) > limit {
		// the most relevant first, and the actions sheet always stays
		last := buttons[len(buttons)-1]
		buttons = append(append([]barButton{}, buttons[:limit-1]...), last)
	}
	right := ""
	if l.Short {
		right = m.ageText(true)
		if text := m.banner(); text != "" {
			right = m.th.Style(theme.ToneWarn).Render(treetable.TruncateEnd(text, l.Width/2, m.th.G.Ellipsis))
		} else if m.status != "" {
			right = m.th.Style(m.statusTone).Render(treetable.TruncateEnd(m.status, l.Width/2, m.th.G.Ellipsis))
		}
		if m.planWaiting() {
			right = m.th.Style(theme.ToneAccent).Render(treetable.TruncateEnd(m.planWaitText(), l.Width/2, m.th.G.Ellipsis))
		}
		if m.mode == ModeSearch {
			return []string{fit(" "+m.search.View(), l.Width)}, nil
		}
		if l.Tier == theme.TierNarrow {
			limit = 3
			last := buttons[len(buttons)-1]
			if len(buttons) > limit {
				buttons = append(append([]barButton{}, buttons[:limit-1]...), last)
			}
		}
		if text := m.shortState(); text != "" && m.status == "" && m.banner() == "" {
			// what stays is for this text: the actions button must not fall off
			room := l.Width - 3
			for _, b := range buttons {
				room -= ansi.StringWidth(m.buttonText(b)) + 1
			}
			if room >= 8 {
				right = m.th.Bold(theme.ToneAccent).Render(treetable.TruncateEnd(text, room, m.th.G.Ellipsis))
			}
		}
	}
	rightW := ansi.StringWidth(right)
	// Whole buttons give way, the last of the others first: the bar always ends with actions,
	// because all that fell off is in that sheet. The text on the right has a space after it.
	width := func(bs []barButton) int {
		n := 1
		for _, b := range bs {
			n += ansi.StringWidth(m.buttonText(b)) + 1
		}
		return n
	}
	for len(buttons) > 1 && width(buttons)+rightW+1 > l.Width-1 {
		buttons = append(buttons[:len(buttons)-2:len(buttons)-2], buttons[len(buttons)-1])
	}
	x := 1
	line := " "
	var zones []dialog.Zone
	for _, b := range buttons {
		text := m.buttonText(b)
		w := ansi.StringWidth(text)
		line += text + " "
		zones = append(zones, dialog.Zone{X0: x, Y0: y, X1: x + max(w, 9), Y1: y + 1, Action: "bar:" + b.Action})
		x += w + 1
	}
	if right != "" {
		line = joinEnds(line, right+" ", l.Width)
	}
	return []string{line}, zones
}

// shortState is what the status line says on other screens, and a short screen has none:
// the marks, also those that the filter hides, and the filter.
func (m *Model) shortState() string {
	var parts []string
	if text := m.markSentence(); text != "" {
		parts = append(parts, text)
	}
	if f := m.tree.Filter(); f.Active() {
		parts = append(parts, "filter: "+treetable.Sanitize(f.String()))
	}
	switch m.table.Place() {
	case treetable.CursorAbove:
		parts = append(parts, "cursor: "+m.cursorName()+" (above)")
	case treetable.CursorBelow:
		parts = append(parts, "cursor: "+m.cursorName()+" (below)")
	}
	return strings.Join(parts, " "+m.th.G.Dot+" ")
}

// buttonStyle is a block in neutral grey, the primary in accent, and a disabled one a block
// that is darker still: its target stays, and so does the row of them.
func (m *Model) buttonStyle(b barButton) (lipgloss.Style, lipgloss.Style) {
	switch {
	case b.Reason != "":
		s := m.th.ButtonOff()
		return s, s
	case b.Primary:
		s := m.th.Button(theme.ToneAccent)
		return s.Bold(true), s
	}
	s := m.th.Button(theme.ToneDim)
	return s.Bold(true), s
}

func (m *Model) buttonText(b barButton) string {
	key, label := m.buttonStyle(b)
	if b.Reason != "" {
		return key.Render(" " + b.Key + " " + b.Label + " ")
	}
	return key.Render(" "+b.Key) + label.Render(" "+b.Label+" ")
}

// narrowBar is the thumb zone of a phone: two rows tall, four buttons, the key under its
// label, so that each is a 2 by 8 target and a reminder.
func (m *Model) narrowBar(buttons []barButton, y int) ([]string, []dialog.Zone) {
	w := m.layout.Width
	if len(buttons) > 4 {
		last := buttons[len(buttons)-1]
		buttons = append(append([]barButton{}, buttons[:3]...), last)
	}
	cell := (w - 2 - (len(buttons) - 1)) / len(buttons)
	top, bottom := " ", " "
	var zones []dialog.Zone
	x := 1
	for i, b := range buttons {
		key, label := m.buttonStyle(b)
		if i > 0 {
			top += " "
			bottom += " "
			x++
		}
		text := b.Label
		if b.Short != "" && ansi.StringWidth(text) > cell {
			text = b.Short
		}
		// a count on a button that is cut says another number, so a cut has its mark
		top += label.Render(center(treetable.TruncateEnd(text, cell, m.th.G.Ellipsis), cell))
		bottom += key.Render(center(b.Key, cell))
		zones = append(zones, dialog.Zone{X0: x, Y0: y, X1: x + cell, Y1: y + 2, Action: "bar:" + b.Action})
		x += cell
	}
	return []string{top, bottom}, zones
}

func center(s string, w int) string {
	n := ansi.StringWidth(s)
	if n >= w {
		return ansi.Truncate(s, w, "")
	}
	left := (w - n) / 2
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", w-n-left)
}

// ---- the status of the host

// setHostStatus sets OSC 7501 and OSC 9;4, in Rex only.
func (m *Model) setHostStatus(v *tea.View) {
	if !m.o.Caps.Rex {
		return
	}
	status := &tea.ProgramStatus{State: tea.ProgramStateIdle, App: "wtree"}
	switch {
	case m.quitting:
		status.State = tea.ProgramStateClear
	case m.mode == ModeProgress && !m.progress.Finished:
		status.State = tea.ProgramStateWorking
		status.Message = m.progress.StatusText()
		status.Progress, status.HasProgress = int(m.progress.Fraction()*100), true
		v.ProgressBar = tea.NewProgressBar(tea.ProgressBarDefault, status.Progress)
	case m.mode == ModeProgress:
		if m.progress.OK() {
			status.State = tea.ProgramStateDone
		} else {
			status.State = tea.ProgramStateError
			status.Message = "a step failed"
		}
	case m.mode == ModeConfirm:
		status.State = tea.ProgramStateBlocked
		status.Kind = tea.ProgramStatusKindQuestion
		status.Message = "confirm: " + m.confirm.ConfirmLabel()
	case m.loading && m.nowAt.Sub(m.loadStarted).Seconds() >= 1:
		status.State = tea.ProgramStateWorking
		status.Message = "loading worktrees"
	}
	v.ProgramStatus = status
}
