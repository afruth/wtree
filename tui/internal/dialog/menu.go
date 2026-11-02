package dialog

import (
	"strconv"

	"github.com/afruth/wtree/tui/internal/theme"
	"github.com/afruth/wtree/tui/internal/treetable"
)

// Item is one line of an action sheet.
type Item struct {
	Key      string // the digit, set by NewMenu
	Label    string
	Right    string // dim text: "840M lost". At the right, and on the second row on narrow
	Path     bool   // Right is a path, so a cut keeps its end
	Reason   string // why it is disabled
	Tone     theme.Tone
	Action   string
	Disabled bool
}

// CancelAction is the action of the last item of each sheet.
const CancelAction = "cancel"

// Menu is the action sheet: all actions of a row with the primary first, disabled ones
// dimmed with their reason, and cancel last.
type Menu struct {
	Title  string
	Note   string // the first line, such as "the cursor row is not marked, so it is not included"
	Items  []Item
	Cursor int
	Scroll int
}

// NewMenu numbers the enabled items and adds cancel. 0 is always cancel.
func NewMenu(title, note string, items []Item) Menu {
	m := Menu{Title: treetable.Sanitize(title), Note: treetable.Sanitize(note)}
	n := 1
	for _, it := range items {
		it.Key = "-"
		if !it.Disabled && n <= 9 {
			it.Key = strconv.Itoa(n)
			n++
		}
		m.Items = append(m.Items, it)
	}
	m.Items = append(m.Items, Item{Key: "0", Label: "cancel", Action: CancelAction, Tone: theme.ToneDim})
	for i, it := range m.Items {
		if !it.Disabled {
			m.Cursor = i
			break
		}
	}
	return m
}

// Move moves the cursor over the items, also the disabled ones, because their reason is
// there to read.
func (m *Menu) Move(delta int) {
	m.Cursor = min(max(m.Cursor+delta, 0), len(m.Items)-1)
}

// Current is the item under the cursor.
func (m Menu) Current() Item { return m.Items[m.Cursor] }

// Pick finds the item of a digit.
func (m Menu) Pick(key string) (Item, bool) {
	for _, it := range m.Items {
		if it.Key == key && key != "-" {
			return it, true
		}
	}
	return Item{}, false
}

// ByAction finds an item from its zone action.
func (m Menu) ByAction(action string) (Item, bool) {
	for _, it := range m.Items {
		if it.Action == action {
			return it, true
		}
	}
	return Item{}, false
}

// contentWidth is the width that the sheet needs: its longest item, up to 60 cells. A sheet
// that fits its content keeps a sub-text next to its label.
func (m Menu) contentWidth() int {
	w := Width(m.Title) + 6
	if m.Note != "" {
		w = max(w, 44)
	}
	for _, it := range m.Items {
		side := it.Right
		if it.Disabled {
			side = it.Reason
		}
		n := 2 + Width(it.Label)
		if side != "" {
			n += 2 + Width(side)
		}
		w = max(w, n+4)
	}
	return min(max(w, 36), 60)
}

// Render draws the sheet: a centred sheet of its content, and a bottom sheet on narrow, where
// an item has a second row for its sub-text. An item with no sub-text has one row.
func (m Menu) Render(th theme.Theme, l theme.Layout) Frame {
	width := m.contentWidth()
	s := shapeFor(l, width)
	if l.Short && l.Tier != theme.TierNarrow {
		s = shape{w: min(width, l.Width), maxH: l.Height, x: (l.Width - min(width, l.Width)) / 2}
	}
	inner := s.w - 4
	narrow := l.Tier == theme.TierNarrow && !l.Short
	var body []Row
	if m.Note != "" {
		for _, line := range Wrap(m.Note, inner) {
			body = append(body, textRow(th.Style(theme.ToneDim).Render(line)))
		}
	}
	focus := -1
	for i, it := range m.Items {
		if i == m.Cursor {
			focus = len(body)
		}
		body = append(body, m.itemRow(th, it, i == m.Cursor, inner, narrow))
	}
	f, _ := assembleSheet(th, l, s, sheetParts{title: m.Title, tone: theme.ToneFg, body: body, scroll: m.Scroll, focus: focus})
	return f
}

func (m Menu) itemRow(th theme.Theme, it Item, selected bool, inner int, narrow bool) Row {
	ell := th.G.Ellipsis
	side := it.Right
	if it.Disabled {
		side = it.Reason
	}
	cut := treetable.TruncateEnd
	if it.Path && !it.Disabled {
		cut = treetable.TruncateStart
	}
	key, label := it.Key, it.Label
	var first, second string
	if narrow {
		// two rows: the sub-text gets the second, so that a phone shows all of it
		first = key + " " + treetable.TruncateEnd(label, inner-2, ell)
		second = "  " + cut(side, inner-2, ell)
	} else if side != "" {
		side = cut(side, max(inner-2-Width(label)-2, 0), ell)
		first = rightAlign(key+" "+label, side, inner)
	} else {
		first = key + " " + label
	}
	lines := []string{first}
	if narrow && side != "" {
		lines = append(lines, second)
	}
	for i, line := range lines {
		switch {
		case selected:
			lines[i] = th.Cursor().Render(fitTo(line, inner))
		case it.Disabled:
			lines[i] = th.Style(theme.ToneDim).Render(line)
		case i == 0:
			styled := th.Bold(theme.ToneAccent).Render(key) + " " + th.Style(it.Tone).Render(treetable.TruncateEnd(label, inner-2, ell))
			if !narrow && side != "" {
				styled = rightAlign(styled, th.Style(theme.ToneDim).Render(side), inner)
			}
			lines[i] = styled
		default:
			lines[i] = th.Style(theme.ToneDim).Render(line)
		}
	}
	return Row{Lines: lines, Action: "item:" + it.Action}
}
