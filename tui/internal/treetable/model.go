// Package treetable draws the tree-table of wtree: rows of variable height, three
// layout tiers and the short rule, with a geometry table for hit-testing. It renders
// from state and touches no terminal.
//
// The geometry is computed by every call that changes the state, and not during
// Render. So Render is pure, and HitTest always answers for the frame that Render
// draws from the same state, also when a Bubble Tea View works on a copy of the model.
package treetable

import (
	"github.com/afruth/wtree/tui/internal/theme"
)

// Zone is a tappable region of the table.
type Zone int

// Zones. ux.md 3.2 gives each its action.
const (
	ZoneNone      Zone = iota
	ZoneBody           // moves the cursor; on the cursor row, opens the action sheet
	ZoneGutter         // toggles the mark
	ZoneChevron        // expands or collapses, without moving the cursor
	ZoneScrollbar      // scrolls to Hit.Fraction
	ZoneTitle          // a column title: sorts by Hit.Column
)

// Hit is the answer of HitTest.
type Hit struct {
	Zone     Zone
	ID       string  // the row, on a row zone
	Line     int     // which line of the row: 0 or 1
	Column   string  // the title, on ZoneTitle
	Fraction float64 // 0 to 1, on ZoneScrollbar
}

// RowGeom is one drawn row, in screen coordinates.
type RowGeom struct {
	ID     string
	Index  int // into Rows
	Top    int
	Height int
}

// ColumnGeom is one drawn column title, in screen coordinates.
type ColumnGeom struct {
	Title  string
	X0, X1 int // X1 is exclusive
}

// CursorPlace says where the cursor row is against the viewport.
type CursorPlace int

// Places of the cursor row.
const (
	CursorVisible CursorPlace = iota
	CursorAbove
	CursorBelow
	CursorNone
)

// Model is the table. Its zero value is not ready: use New.
type Model struct {
	th     theme.Theme
	layout theme.Layout
	w, h   int
	ox, oy int

	raw  []Row // as given
	rows []Row // what the layout draws: the short rule drops separators

	cursor string
	top    int // index of the first drawn row
	flash  map[string]bool

	titlePrefix string
	emptyText   string
	reasons     bool // the cleanup view: the reason takes the place of CPU and memory

	// computed by relayout
	cols      []column
	geom      []RowGeom
	colGeom   []ColumnGeom
	bodyLines int
	total     int
	topLines  int
}

// New returns a table with a theme and a layout for the size of the screen.
func New(th theme.Theme, layout theme.Layout) Model {
	m := Model{th: th, layout: layout, w: layout.Width, h: layout.Height}
	m.relayout()
	return m
}

// SetTheme changes the palette or the glyphs.
func (m *Model) SetTheme(th theme.Theme) { m.th = th; m.relayout() }

// SetLayout sets the tier and the short rule. Call it on every resize, with SetSize.
func (m *Model) SetLayout(l theme.Layout) {
	m.layout = l
	m.applyRows()
	m.relayout()
}

// SetSize sets the area of the table in cells: the column titles and the rows. The
// width is the table only: on wide with the panel on, use layout.TableWidth(true).
func (m *Model) SetSize(w, h int) {
	m.w, m.h = w, h
	m.relayout()
	m.EnsureVisible(m.cursor)
}

// SetHeight changes the height only, and leaves the view where it is: a status line that comes
// and goes must not scroll the list back to a cursor that a wheel left behind.
func (m *Model) SetHeight(h int) {
	m.h = h
	m.clampTop()
	m.relayout()
}

// SetReasons says that the rows are those of the cleanup view.
func (m *Model) SetReasons(on bool) {
	if m.reasons != on {
		m.reasons = on
		m.relayout()
	}
}

// SetOrigin says where the table starts on the screen, for HitTest and Geometry.
func (m *Model) SetOrigin(x, y int) { m.ox, m.oy = x, y; m.relayout() }

// SetTitlePrefix sets the text that shares the title line under the short rule: the
// status line of the header. It can hold styles. Other layouts ignore it.
func (m *Model) SetTitlePrefix(s string) { m.titlePrefix = s; m.relayout() }

// SetEmptyText sets the dim line to show when there are no rows, such as
// "loading worktrees…" or `nothing matches "foo" · backspace clear`.
func (m *Model) SetEmptyText(s string) { m.emptyText = Sanitize(s) }

// SetFlash sets the rows to highlight for this frame. It has an effect on wide only.
func (m *Model) SetFlash(ids []string) {
	m.flash = nil
	if len(ids) > 0 {
		m.flash = make(map[string]bool, len(ids))
		for _, id := range ids {
			m.flash[id] = true
		}
	}
}

// SetRows replaces the rows. Cursor and viewport follow the ids of rows and not their
// positions, so a reload or a live update moves nothing on the screen. If the cursor
// row is gone, the cursor takes the row that is now in its place.
func (m *Model) SetRows(rows []Row) {
	oldCursorIndex := m.indexOf(m.cursor)
	topID := ""
	if m.top >= 0 && m.top < len(m.rows) {
		topID = m.rows[m.top].ID
	}
	oldTop := m.top

	m.raw = rows
	m.applyRows()

	if i := m.indexOf(topID); i >= 0 {
		m.top = i
	} else {
		m.top = oldTop
	}
	if m.indexOf(m.cursor) < 0 {
		m.cursor = m.nearestSelectable(max(oldCursorIndex, 0))
	} else if i := m.indexOf(m.cursor); !m.rows[i].selectable() {
		m.cursor = m.nearestSelectable(i)
	}
	m.clampTop()
	m.relayout()
}

// Rows is what the layout draws.
func (m Model) Rows() []Row { return m.rows }

func (m *Model) applyRows() {
	if !m.layout.Short {
		m.rows = m.raw
		return
	}
	m.rows = make([]Row, 0, len(m.raw))
	for _, r := range m.raw {
		if r.Kind != KindSeparator {
			m.rows = append(m.rows, r)
		}
	}
}

func (m Model) indexOf(id string) int {
	if id == "" {
		return -1
	}
	for i := range m.rows {
		if m.rows[i].ID == id {
			return i
		}
	}
	return -1
}

func (m Model) nearestSelectable(i int) string {
	if len(m.rows) == 0 {
		return ""
	}
	i = min(i, len(m.rows)-1)
	for j := i; j < len(m.rows); j++ {
		if m.rows[j].selectable() {
			return m.rows[j].ID
		}
	}
	for j := i - 1; j >= 0; j-- {
		if m.rows[j].selectable() {
			return m.rows[j].ID
		}
	}
	return ""
}

// Cursor is the id of the cursor row, or "".
func (m Model) Cursor() string { return m.cursor }

// SetCursor puts the cursor on a row and scrolls to it. It reports false, and changes
// nothing, for a row that is not there or that takes no cursor.
func (m *Model) SetCursor(id string) bool {
	i := m.indexOf(id)
	if i < 0 || !m.rows[i].selectable() {
		return false
	}
	m.cursor = id
	m.EnsureVisible(id)
	return true
}

// MoveCursor moves the cursor by delta selectable rows and scrolls to it. A cursor
// that is out of view comes back into view, as ux.md 3.3 asks of any key press.
func (m *Model) MoveCursor(delta int) {
	i := m.indexOf(m.cursor)
	if i < 0 {
		m.cursor = m.nearestSelectable(0)
		m.EnsureVisible(m.cursor)
		return
	}
	step := 1
	if delta < 0 {
		step, delta = -1, -delta
	}
	for j := i + step; delta > 0 && j >= 0 && j < len(m.rows); j += step {
		if m.rows[j].selectable() {
			i = j
			delta--
		}
	}
	m.cursor = m.rows[i].ID
	m.EnsureVisible(m.cursor)
}

// MovePage moves the cursor by pages: one body height in lines for each.
func (m *Model) MovePage(pages int) {
	i := m.indexOf(m.cursor)
	if i < 0 {
		m.MoveCursor(0)
		return
	}
	lines := max(m.bodyLines-1, 1) * max(pages, -pages)
	step := 1
	if pages < 0 {
		step = -1
	}
	target := i
	for j := i + step; lines > 0 && j >= 0 && j < len(m.rows); j += step {
		lines -= m.rows[j].height(m.layout.Tier)
		if m.rows[j].selectable() {
			target = j
		}
	}
	m.cursor = m.rows[target].ID
	m.EnsureVisible(m.cursor)
}

// CursorHome and CursorEnd go to the first and the last selectable row.
func (m *Model) CursorHome() { m.cursor = m.nearestSelectable(0); m.EnsureVisible(m.cursor) }

// CursorEnd goes to the last selectable row.
func (m *Model) CursorEnd() {
	for j := len(m.rows) - 1; j >= 0; j-- {
		if m.rows[j].selectable() {
			m.cursor = m.rows[j].ID
			break
		}
	}
	m.EnsureVisible(m.cursor)
}

// Place says where the cursor row is against the viewport, for the status line
// `cursor: feat-login (above)`.
func (m Model) Place() CursorPlace {
	i := m.indexOf(m.cursor)
	if i < 0 {
		return CursorNone
	}
	if i < m.top {
		return CursorAbove
	}
	for _, g := range m.geom {
		if g.Index == i {
			return CursorVisible
		}
	}
	return CursorBelow
}

// Scroll moves the viewport by rows and leaves the cursor where it is. See WheelStep.
func (m *Model) Scroll(rows int) {
	m.top += rows
	m.clampTop()
	m.relayout()
}

// WheelStep is how many rows one wheel step scrolls in this tier.
func (m Model) WheelStep() int {
	if m.layout.Tier == theme.TierNarrow {
		return theme.WheelNarrow
	}
	return theme.WheelWide
}

// ScrollTo moves the viewport to a fraction of the list, from ZoneScrollbar.
func (m *Model) ScrollTo(fraction float64) {
	fraction = min(max(fraction, 0), 1)
	m.top = int(fraction*float64(m.maxTop()) + 0.5)
	m.clampTop()
	m.relayout()
}

// EnsureVisible scrolls as little as it can so that all lines of the row are drawn.
func (m *Model) EnsureVisible(id string) {
	i := m.indexOf(id)
	if i < 0 {
		m.clampTop()
		m.relayout()
		return
	}
	if i < m.top {
		m.top = i
	} else {
		body := m.bodyHeight()
		for m.top < i && m.linesBetween(m.top, i+1) > body {
			m.top++
		}
	}
	m.clampTop()
	m.relayout()
}

func (m Model) linesBetween(from, to int) int {
	n := 0
	for i := from; i < to && i < len(m.rows); i++ {
		n += m.rows[i].height(m.layout.Tier)
	}
	return n
}

// maxTop is the first row of the last full page.
func (m Model) maxTop() int {
	body := m.bodyHeight()
	lines := 0
	for i := len(m.rows) - 1; i >= 0; i-- {
		lines += m.rows[i].height(m.layout.Tier)
		if lines > body {
			return min(i+1, len(m.rows)-1)
		}
	}
	return 0
}

func (m *Model) clampTop() { m.top = max(min(m.top, m.maxTop()), 0) }

// titleLines is 1 where there are column titles. Narrow has none: only a short narrow screen
// has a line of it, because its header shares that line.
func (m Model) titleLines() int {
	if m.layout.Tier == theme.TierNarrow && (!m.layout.Short || m.titlePrefix == "") {
		return 0
	}
	return 1
}

func (m Model) bodyHeight() int { return max(m.h-m.titleLines(), 0) }

// BodyHeight is the number of lines for rows.
func (m Model) BodyHeight() int { return m.bodyHeight() }

// TotalLines is the height of all rows together.
func (m Model) TotalLines() int { return m.linesBetween(0, len(m.rows)) }

// Geometry is the table of rows drawn in this frame, in screen coordinates.
func (m Model) Geometry() []RowGeom { return m.geom }

// TitleRoom is how many cells a short header has before the first column title.
func (m Model) TitleRoom() int {
	for _, c := range m.cols {
		if c.id != colName && c.id != colBranch {
			return max(c.x-2, 0)
		}
	}
	return m.w
}

// Titles is the table of column titles drawn in this frame, in screen coordinates.
func (m Model) Titles() []ColumnGeom { return m.colGeom }

// HiddenColumns names what this layout does not show, for the help overlay. The repo stays
// on the name as a prefix, so it is not hidden.
func (m Model) HiddenColumns() []string {
	switch {
	case m.layout.Tier == theme.TierNarrow:
		return nil
	case m.reasons && m.layout.Tier == theme.TierWide:
		return []string{"CPU", "MEM"}
	case m.reasons:
		return []string{"CPU", "MEM", "AGE"}
	case m.layout.Tier == theme.TierMedium && m.layout.Short:
		return []string{"AGE", "MEM"}
	case m.layout.Tier == theme.TierMedium:
		return []string{"AGE"}
	case m.layout.Short:
		return []string{"MEM"}
	}
	return nil
}

func (m Model) hasScrollbar() bool {
	return m.layout.Width >= theme.MinWidth && m.total > m.bodyLines
}

func (m *Model) relayout() {
	m.cols = nil
	m.geom = m.geom[:0:0]
	m.colGeom = nil
	m.bodyLines = m.bodyHeight()
	m.total = m.TotalLines()
	m.topLines = m.linesBetween(0, m.top)
	if m.layout.TooSmall {
		return
	}
	if m.layout.Tier != theme.TierNarrow {
		m.cols = columnsFor(m.layout, m.w, m.rows, m.reasons, Width(m.titlePrefix))
		for _, c := range m.cols {
			if m.layout.Short && (c.id == colName || c.id == colBranch) {
				continue
			}
			x0, x1 := c.titleSpan()
			m.colGeom = append(m.colGeom, ColumnGeom{Title: c.title, X0: m.ox + x0, X1: m.ox + x1})
		}
	}
	y := 0
	for i := m.top; i >= 0 && i < len(m.rows); i++ {
		h := m.rows[i].height(m.layout.Tier)
		if y+h > m.bodyLines {
			if y == 0 && m.bodyLines > 0 { // a row taller than the screen shows what fits
				m.geom = append(m.geom, RowGeom{m.rows[i].ID, i, m.oy + m.titleLines(), m.bodyLines})
			}
			break
		}
		m.geom = append(m.geom, RowGeom{m.rows[i].ID, i, m.oy + m.titleLines() + y, h})
		y += h
	}
}

// HitTest says what is at a screen position in the frame of the current state.
//
// On wide and medium, cells 0 and 1 toggle the mark, and cells 2 and 3 are the chevron, also
// on the @ and ⌂ rows, whose marker sits where the chevron would. On a row that does not
// expand, the gutter also has cell 2. On narrow a row has two lines and uses them: in the
// first three cells, the first line is the chevron and the second is the mark, and a row that
// does not expand has its mark on both. A row that takes no mark has no gutter.
func (m Model) HitTest(x, y int) Hit {
	x -= m.ox
	y -= m.oy
	if m.layout.TooSmall || x < 0 || y < 0 || x >= m.w || y >= m.h {
		return Hit{}
	}
	tl := m.titleLines()
	if y < tl {
		for _, c := range m.colGeom {
			if x+m.ox >= c.X0 && x+m.ox < c.X1 {
				return Hit{Zone: ZoneTitle, Column: c.Title}
			}
		}
		return Hit{}
	}
	if m.hasScrollbar() && (x == m.w-1 || (m.layout.Tier == theme.TierNarrow && x == m.w-2)) {
		frac := 0.0
		if m.bodyLines > 1 {
			frac = float64(y-tl) / float64(m.bodyLines-1)
		}
		return Hit{Zone: ZoneScrollbar, Fraction: frac}
	}
	for _, g := range m.geom {
		sy := y + m.oy
		if sy < g.Top || sy >= g.Top+g.Height {
			continue
		}
		r := m.rows[g.Index]
		if !r.selectable() {
			return Hit{}
		}
		h := Hit{Zone: ZoneBody, ID: r.ID, Line: sy - g.Top}
		switch {
		case m.layout.Tier == theme.TierNarrow && x < narrowCells:
			switch {
			case r.Expandable && h.Line == 0:
				h.Zone = ZoneChevron
			case r.Markable:
				h.Zone = ZoneGutter
			}
		case m.layout.Tier == theme.TierNarrow:
		case r.Markable && x <= 1:
			h.Zone = ZoneGutter
		case r.Expandable && x >= 2 && x <= 3:
			h.Zone = ZoneChevron
		case r.Markable && !r.Expandable && x == 2:
			h.Zone = ZoneGutter
		}
		return h
	}
	return Hit{}
}
