package treetable

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/afruth/wtree/tui/internal/theme"
	"github.com/charmbracelet/x/ansi"
)

// lineStyle is how a whole line draws. The cursor and a flash have one flat style, so
// that the bar reads the same in monochrome.
type lineStyle int

const (
	lineNormal lineStyle = iota
	lineCursor
	lineFlash
)

type seg struct {
	x     int
	text  string
	style lipgloss.Style
}

// Render draws the column titles and the rows: exactly the height of SetSize, each
// line exactly its width. It only reads the state.
func (m Model) Render() string {
	if m.h <= 0 || m.w <= 0 {
		return ""
	}
	out := make([]string, 0, m.h)
	blank := strings.Repeat(" ", m.w)
	if m.layout.TooSmall {
		out = append(out, m.th.Style(theme.ToneFg).Render(pad(" "+clip(theme.TooSmallText, m.w-1), m.w)))
		for len(out) < m.h {
			out = append(out, blank)
		}
		return strings.Join(out, "\n")
	}
	if m.titleLines() > 0 {
		out = append(out, m.renderTitles())
	}
	bar := m.scrollbarCells()
	lineNo := 0
	appendLine := func(segs []seg, bar0 lineStyle) {
		out = append(out, m.joinLine(segs, bar0, bar, lineNo))
		lineNo++
	}
	if len(m.rows) == 0 && m.emptyText != "" && m.bodyLines > 0 {
		appendLine([]seg{{x: 3, text: m.emptyText, style: m.th.Style(theme.ToneDim)}}, lineNormal)
	}
	for _, g := range m.geom {
		r := m.rows[g.Index]
		look := lineNormal
		switch {
		case r.ID == m.cursor && r.selectable():
			look = lineCursor
		case m.flash[r.ID] && m.layout.Tier == theme.TierWide:
			look = lineFlash
		}
		var lines [][]seg
		if m.layout.Tier == theme.TierNarrow {
			lines = m.narrowLines(r)
		} else {
			lines = [][]seg{m.columnLine(r)}
		}
		for i := 0; i < len(lines) && i < g.Height; i++ {
			appendLine(lines[i], look)
		}
	}
	for lineNo < m.bodyLines {
		appendLine(nil, lineNormal)
	}
	return strings.Join(out, "\n")
}

// joinLine places the segments at their cells. A line is padded to the full width, so
// a stale cell of the last frame cannot stay, and the cursor bar has one width.
func (m Model) joinLine(segs []seg, look lineStyle, bar []string, lineNo int) string {
	limit := m.w
	hasBar := lineNo < len(bar)
	if hasBar {
		limit = m.w - 1
	}
	var plain, styled strings.Builder
	x := 0
	for _, s := range segs {
		if s.text == "" {
			continue
		}
		if s.x > x {
			gap := min(s.x, limit) - x
			if gap > 0 {
				sp := strings.Repeat(" ", gap)
				plain.WriteString(sp)
				styled.WriteString(sp)
				x += gap
			}
		}
		if s.x < x || x >= limit { // the slots keep this from happening; never overdraw
			continue
		}
		text := clip(s.text, limit-x)
		plain.WriteString(text)
		styled.WriteString(s.style.Render(text))
		x += Width(text)
	}
	rest := strings.Repeat(" ", max(limit-x, 0))
	var line string
	switch look {
	case lineCursor:
		line = m.th.Cursor().Render(plain.String() + rest)
	case lineFlash:
		line = m.th.Button(theme.ToneDim).Render(plain.String() + rest)
	default:
		line = styled.String() + rest
	}
	if hasBar {
		line += bar[lineNo]
	}
	return line
}

func pad(s string, w int) string {
	return s + strings.Repeat(" ", max(w-Width(s), 0))
}

// scrollbarCells draws the scrollbar in the last column, one cell per body line, or
// returns nil when everything fits. All tiers keep that column free of data.
func (m Model) scrollbarCells() []string {
	if !m.hasScrollbar() || m.bodyLines < 1 {
		return nil
	}
	h := m.bodyLines
	thumb := max(h*h/m.total, 1)
	span := max(m.total-h, 1)
	start := min(m.topLines, span) * (h - thumb) / span
	cells := make([]string, h)
	for i := range cells {
		if i >= start && i < start+thumb {
			cells[i] = m.th.Style(theme.ToneAccent).Render(m.th.G.ScrollThumb)
		} else {
			cells[i] = m.th.Style(theme.ToneDim).Render(m.th.G.ScrollTrack)
		}
	}
	return cells
}

func (m Model) renderTitles() string {
	style := m.th.Bold(theme.ToneFg)
	var b strings.Builder
	x := 0
	if m.layout.Short {
		limit := m.w
		for _, c := range m.cols {
			if c.id != colName && c.id != colBranch {
				limit = c.x - 2
				break
			}
		}
		prefix := m.titlePrefix
		if Width(prefix) > limit {
			prefix = ansi.Truncate(prefix, max(limit, 0), "")
		}
		b.WriteString(prefix)
		x = Width(prefix)
	}
	for _, c := range m.cols {
		if m.layout.Short && (c.id == colName || c.id == colBranch) {
			continue
		}
		tx := c.x
		if c.right {
			tx = c.x + c.w - Width(c.title)
		}
		if tx < x {
			continue
		}
		b.WriteString(strings.Repeat(" ", tx-x))
		b.WriteString(style.Render(c.title))
		x = tx + Width(c.title)
	}
	b.WriteString(strings.Repeat(" ", max(m.w-x, 0)))
	return b.String()
}

// ---- text of the parts of a row

func (m Model) tone(r Row, t theme.Tone) lipgloss.Style {
	if r.Dim {
		return m.th.Style(theme.ToneDim)
	}
	return m.th.Style(t)
}

func (m Model) cellText(c Cell) string {
	switch c.State {
	case CellValue:
		return c.Text
	case CellNA:
		return "" // not applicable: nothing to read, on every tier
	case CellLoading:
		return m.th.G.Loading
	case CellFailed:
		return m.th.G.Unknown
	case CellStale:
		return m.th.G.Stale + c.Text
	}
	return ""
}

func (m Model) cellStyle(r Row, c Cell) lipgloss.Style {
	switch c.State {
	case CellNA, CellLoading, CellStale:
		return m.th.Style(theme.ToneDim)
	case CellFailed:
		return m.tone(r, theme.ToneDanger)
	}
	return m.tone(r, c.Tone)
}

func runTone(run Run) theme.Tone {
	switch run {
	case RunUp:
		return theme.ToneOk
	case RunPartial:
		return theme.ToneWarn
	case RunExited, RunDown, RunVolumes:
		return theme.ToneDim
	}
	return theme.ToneFg
}

func (m Model) runGlyph(r Row) string {
	g := m.th.G
	switch r.Run {
	case RunUp:
		return g.Running
	case RunPartial:
		if r.Kind == KindWorktree { // the mock-ups show anything that runs as running here
			return g.Running
		}
		return g.Partial
	case RunExited:
		return g.Exited
	case RunDown:
		return g.Down
	case RunVolumes:
		return g.Volumes
	}
	return ""
}

func healthWord(h string) (string, theme.Tone) {
	switch h {
	case "healthy":
		return "ok", theme.ToneOk
	case "unhealthy":
		return "sick", theme.ToneDanger
	case "starting":
		return "start", theme.ToneWarn
	}
	return "", theme.ToneFg
}

// stateCompact is the one cell of the state on medium and narrow: "partial 1/2",
// "1/1 up", "running".
func (m Model) stateCompact(r Row) string {
	switch r.Kind {
	case KindWorktree:
		if r.Run == RunUp && r.Count != "" {
			return r.Count + " up"
		}
		return r.Count
	case KindStack:
		if r.Run == RunVolumes {
			if m.layout.Tier == theme.TierNarrow {
				return "volumes"
			}
			return ""
		}
		if r.Run == RunUp {
			return r.Count + " up"
		}
		return strings.TrimSpace(r.Word + " " + r.Count)
	case KindContainer:
		h, _ := healthWord(r.Health)
		return strings.TrimSpace(r.Word + " " + h)
	}
	return ""
}

type part struct {
	text string
	tone theme.Tone
}

// gitParts is the git state in full: ↑3 ↓1 ±4. The second line on narrow has more room
// for words and less for noise: "✓ merged", and nothing at all for a clean tree.
func (m Model) gitParts(g Git, line2 bool) []part {
	gl := m.th.G
	var out []part
	if g.Locked { // the worktree list knows this, whatever the git stage says
		out = append(out, part{gl.Locked, theme.ToneWarn})
	}
	switch g.State {
	case CellBlank:
		return out
	case CellNA:
		return out
	case CellLoading:
		return append(out, part{gl.Loading, theme.ToneDim})
	case CellFailed:
		return append(out, part{gl.Unknown, theme.ToneDanger})
	}
	if g.Ahead > 0 {
		out = append(out, part{gl.Ahead + strconv.Itoa(g.Ahead), theme.ToneAccent})
	}
	if g.Behind > 0 {
		out = append(out, part{gl.Behind + strconv.Itoa(g.Behind), theme.ToneFg})
	}
	if g.Changed > 0 {
		out = append(out, part{gl.Changed + strconv.Itoa(g.Changed), theme.ToneWarn})
	}
	if g.Ahead > 0 || g.Behind > 0 || g.Changed > 0 {
		return out
	}
	if g.Merged {
		text := gl.Merged
		if line2 {
			text += " merged"
		}
		return append(out, part{text, theme.ToneOk})
	}
	return out
}

// gitGlyph folds the git state into one glyph after the name, on medium.
func (m Model) gitGlyph(g Git) (string, theme.Tone) {
	gl := m.th.G
	if g.Locked {
		return gl.Locked, theme.ToneWarn
	}
	if g.State != CellValue {
		return "", theme.ToneFg
	}
	switch {
	case g.Changed > 0:
		return gl.Changed, theme.ToneWarn
	case g.Ahead > 0:
		return gl.Ahead, theme.ToneAccent
	case g.Behind > 0:
		return gl.Behind, theme.ToneFg
	case g.Merged:
		return gl.Merged, theme.ToneOk
	}
	return "", theme.ToneFg
}

// lead is the first cells: the mark, then the marker or the chevron. On narrow the mark goes on
// the second line, where a tap toggles it.
func (m Model) lead(r Row) []seg {
	g := m.th.G
	var out []seg
	glyphX := 2
	if m.layout.Tier == theme.TierNarrow {
		glyphX = 1
	} else if r.Marked {
		out = append(out, seg{0, g.Mark, m.th.Style(theme.ToneAccent)})
	}
	glyph, tone := "", theme.ToneDim
	switch r.Marker {
	case MarkerSafe:
		glyph, tone = g.Merged, theme.ToneOk // one cell in both glyph sets, where Done is not
	case MarkerLook:
		glyph, tone = g.Cleanup, theme.ToneWarn
	case MarkerCurrent:
		glyph, tone = g.Current, theme.ToneAccent
	case MarkerMain:
		glyph, tone = g.Main, theme.ToneFg
	case MarkerCleanup:
		glyph, tone = g.Cleanup, theme.ToneDanger
	default:
		if r.Expandable {
			glyph = g.Collapsed
			if r.Expanded {
				glyph = g.Expanded
			}
		}
	}
	if glyph != "" {
		out = append(out, seg{glyphX, glyph, m.tone(r, tone)})
	}
	return out
}

// nameSegs draws the name part from x with room for avail cells.
func (m Model) nameSegs(r Row, x, avail int, suffix string, suffixTone theme.Tone) []seg {
	ell := m.th.G.Ellipsis
	if avail <= 0 {
		return nil
	}
	nameStyle := m.tone(r, theme.ToneFg)
	switch r.Kind {
	case KindWorktree, KindGroup:
		if !r.Dim {
			nameStyle = nameStyle.Bold(true)
		}
	}
	if r.Marked {
		nameStyle = m.th.Bold(theme.ToneAccent)
	}
	room := avail
	if suffix != "" {
		if Width(suffix)+1 < room-4 {
			room -= Width(suffix) + 1
		} else {
			suffix = ""
		}
	}
	tab := r.Tab
	if tab != "" {
		if Width(tab)+1 < room-6 && Width(r.Name)+Width(tab)+1 <= room {
			room -= Width(tab) + 1
		} else {
			tab = ""
		}
	}
	name := TruncateStart(r.Name, room, ell)
	var out []seg
	if r.Prefix != "" {
		left := room - Width(name) - 1
		if left >= 4 {
			p := TruncateStart(r.Prefix, left, ell)
			out = append(out, seg{x, p, m.th.Style(theme.ToneDim)})
			x += Width(p) + 1
		}
	}
	out = append(out, seg{x, name, nameStyle})
	x += Width(name)
	if suffix != "" {
		out = append(out, seg{x + 1, suffix, m.tone(r, suffixTone)})
		x += Width(suffix) + 1
	}
	if tab != "" {
		out = append(out, seg{x + 1, tab, m.th.Style(theme.ToneDim)})
		x += Width(tab) + 1
	}
	if r.NameNote != "" {
		out = append(out, seg{x + 2, r.NameNote, m.th.Style(theme.ToneDim)})
	}
	return out
}

// branchOrReason is the second column of a row.
func (m Model) branchOrReason(r Row, w int) (string, lipgloss.Style) {
	ell := m.th.G.Ellipsis
	if r.Reason != "" {
		return TruncateEnd(m.reasonText(r), w, ell), m.tone(r, r.ReasonTone)
	}
	return shortBranch(r.Branch, w, ell), m.th.Style(theme.ToneDim)
}

// reasonText puts the dot of the glyph set in a reason.
func (m Model) reasonText(r Row) string {
	return strings.ReplaceAll(r.Reason, "·", m.th.G.Dot)
}

// shortBranch cuts a branch at its end, where a left cut would lose what it is. A detached head
// keeps its commit, which is all that it has.
func shortBranch(branch string, w int, ell string) string {
	const detached = "(detached) "
	if Width(branch) > w && strings.HasPrefix(branch, detached) {
		branch = strings.TrimPrefix(branch, detached)
	}
	return TruncateEnd(branch, w, ell)
}

// ---- wide and medium

func (m Model) columnLine(r Row) []seg {
	if r.Kind == KindSeparator {
		return nil
	}
	segs := m.lead(r)
	byID := make(map[colID]column, len(m.cols))
	for _, c := range m.cols {
		byID[c.id] = c
	}
	name, branch := byID[colName], byID[colBranch]
	nameX := name.x + indentOf(r.Depth)
	avail := name.x + name.w - nameX
	// A row with nothing in the branch column can use it: a project name is what the
	// user reads before a delete, so it is not cut while there is room.
	hasBranch := r.Branch != "" || r.Reason != ""
	if !hasBranch {
		avail = branch.x + branch.w - nameX
	}
	suffix, suffixTone := "", theme.ToneFg
	if r.Kind == KindWorktree && m.layout.Tier != theme.TierWide {
		suffix, suffixTone = m.gitGlyph(r.Git)
	}
	segs = append(segs, m.nameSegs(r, nameX, avail, suffix, suffixTone)...)
	if hasBranch {
		text, style := m.branchOrReason(r, branch.w)
		segs = append(segs, seg{branch.x, text, style})
	}
	for _, c := range m.cols {
		switch c.id {
		case colGit:
			segs = append(segs, m.wideGitOrState(r, c)...)
		case colStack:
			segs = append(segs, m.stackCell(r, c))
		case colCPU:
			segs = append(segs, m.numberSeg(r, r.CPU, c))
		case colMem:
			segs = append(segs, m.numberSeg(r, r.Mem, c))
		case colDisk:
			s := m.numberSeg(r, r.Disk, c)
			if r.Kind == KindGroup && r.Disk.State == CellValue {
				s.style = s.style.Bold(true)
			}
			segs = append(segs, s)
		case colAge:
			segs = append(segs, seg{c.x, clip(m.cellText(r.Age), c.w), m.cellStyle(r, r.Age)})
		}
	}
	return segs
}

func (m Model) numberSeg(r Row, cell Cell, c column) seg {
	text := m.cellText(cell)
	// The tilde of a stale value hangs in the gap, and the digits keep their place.
	w := Width(text)
	return seg{c.x + c.w - w, text, m.cellStyle(r, cell)}
}

// wideGitOrState is the GIT column on wide: git state on a worktree, and the state
// word on a stack or a container.
func (m Model) wideGitOrState(r Row, c column) []seg {
	switch r.Kind {
	case KindWorktree:
		parts := m.gitParts(r.Git, false)
		total := 0
		for _, p := range parts {
			total += Width(p.text)
		}
		sep := 1
		if total+(len(parts)-1) > c.w {
			sep = 0 // ↑3↓1±12: the numbers stay readable before anything is cut
		}
		var out []seg
		x := c.x
		for _, p := range parts {
			left := c.x + c.w - x
			if left <= 0 {
				break
			}
			text := clip(p.text, left)
			out = append(out, seg{x, text, m.tone(r, p.tone)})
			x += Width(text) + sep
		}
		return out
	case KindStack:
		if r.Run == RunVolumes || r.Word == "" {
			return nil
		}
		return []seg{{c.x, clip(r.Word, c.w), m.tone(r, runTone(r.Run))}}
	case KindContainer:
		return []seg{{c.x, clip(r.Word, c.w), m.tone(r, runTone(r.Run))}}
	}
	return nil
}

// stackCell is the STACK column: the roll-up, the count of a stack, or the health of
// a container on wide; and the whole state on medium.
func (m Model) stackCell(r Row, c column) seg {
	wide := m.layout.Tier == theme.TierWide
	switch {
	case r.Kind == KindGroup:
		return seg{}
	case wide && r.Kind == KindStack:
		return seg{c.x, clip(r.Count, c.w), m.tone(r, runTone(r.Run))}
	case wide && r.Kind == KindContainer:
		h, tone := healthWord(r.Health)
		return seg{c.x, clip(h, c.w), m.tone(r, tone)}
	}
	text := m.stateCompact(r)
	if Width(text) > c.w && r.Kind == KindWorktree {
		text = r.Count
	}
	return seg{c.x, clip(text, c.w), m.tone(r, runTone(r.Run))}
}

// ---- narrow

// narrowStateX is the column of the state on line 1: 27 at 48 columns as in the
// mock-up. It keeps 21 cells for the state from there up, and gives names a share of
// the width below.
func narrowStateX(w int) int {
	if w >= 48 {
		return w - 21
	}
	return 27 - (48-w)/4
}

func (m Model) narrowLines(r Row) [][]seg {
	if r.Kind == KindSeparator {
		return [][]seg{nil}
	}
	stateX := narrowStateX(m.w)
	nameX := narrowCells + indentOf(r.Depth)
	line1 := m.lead(r)
	avail := stateX - 1 - nameX
	groupDisk := r.Kind == KindGroup && r.Disk.State != CellBlank && r.Disk.State != CellNA
	if r.Kind == KindGroup && !groupDisk {
		avail = m.w - 1 - nameX
	}
	line1 = append(line1, m.nameSegs(r, nameX, avail, "", theme.ToneFg)...)

	switch r.Kind {
	case KindGroup:
		if groupDisk {
			s := seg{stateX, m.cellText(r.Disk), m.cellStyle(r, r.Disk)}
			if r.Disk.State == CellValue {
				s.style = s.style.Bold(true)
			}
			line1 = append(line1, s)
		}
		return [][]seg{line1}
	}

	state := m.stateCompact(r)
	if g := m.runGlyph(r); g != "" && state != "" {
		state += " " + g
	}
	line1 = append(line1, seg{stateX, TruncateEnd(state, m.w-1-stateX, m.th.G.Ellipsis), m.tone(r, runTone(r.Run))})

	x := nameX
	if r.Depth >= 1 {
		x += 2
	}
	return [][]seg{line1, m.narrowLine2(r, x)}
}

// narrowLine2 is the second line: numbers in a fixed order, then the branch or the reason. A
// cell that does not apply takes no room, and a cell with a value to come keeps its place.
func (m Model) narrowLine2(r Row, x int) []seg {
	var out []seg
	if r.Marked {
		out = append(out, seg{1, m.th.G.Mark, m.th.Style(theme.ToneAccent)})
	}
	add := func(c Cell) {
		if c.State == CellNA || c.State == CellBlank {
			return
		}
		text := m.cellText(c)
		out = append(out, seg{x, text, m.cellStyle(r, c)})
		x += Width(text) + 2
	}
	add(r.CPU)
	add(r.Mem)
	right := m.w - 1 // the last column stays free for the scrollbar
	switch r.Kind {
	case KindWorktree:
		add(r.Disk)
		if r.Reason != "" {
			out = append(out, seg{x, TruncateEnd(m.reasonText(r), right-x, m.th.G.Ellipsis), m.tone(r, r.ReasonTone)})
			return out
		}
		parts := m.gitParts(r.Git, true)
		gitW := 0
		for _, p := range parts {
			gitW += Width(p.text) + 1
		}
		room := right - x - gitW
		// Too little room for a branch that says anything: it goes, as ux.md 2.4 asks.
		if room >= min(8, Width(r.Branch)) && r.Branch != "" {
			text := shortBranch(r.Branch, room, m.th.G.Ellipsis)
			out = append(out, seg{x, text, m.th.Style(theme.ToneDim)})
			x += Width(text) + 1
		}
		for _, p := range parts {
			out = append(out, seg{x, p.text, m.tone(r, p.tone)})
			x += Width(p.text) + 1
		}
	case KindStack:
		add(r.Disk)
		if r.Reason != "" {
			reason := m.reasonText(r)
			ageW := 0
			if r.Age.State != CellNA && r.Age.State != CellBlank {
				ageW = Width(m.cellText(r.Age)) + 1
			}
			text := TruncateEnd(reason, right-x-ageW, m.th.G.Ellipsis)
			out = append(out, seg{x, text, m.tone(r, r.ReasonTone)})
			x += Width(text) + 2
		}
		add(r.Age)
	case KindContainer:
		if len(r.Ports) > 0 {
			text := TruncateEnd(strings.Join(r.Ports, " "), right-x, m.th.G.Ellipsis)
			out = append(out, seg{x, text, m.th.Style(theme.ToneDim)})
		} else {
			add(r.Age)
		}
	}
	return out
}
