package dialog

import (
	"strings"

	"github.com/afruth/wtree/tui/internal/theme"
	"github.com/afruth/wtree/tui/internal/treetable"
)

// HelpEntry is one key.
type HelpEntry struct {
	Key    string
	Text   string
	Reason string // why it does nothing now
}

// Help is the help overlay: the keys of the current context, disabled ones dimmed with
// their reason. It closes with ?, backspace, q or a tap.
type Help struct {
	Line    string // tier, size, glyphs and theme
	Entries []HelpEntry
	Scroll  int
	More    bool // on a short screen: all keys and not the ten most used
}

// MostUsed is how many keys the short screen shows before `more`.
const MostUsed = 10

// Render draws the overlay: three columns on wide, two on medium, one when two would cut it.
func (h Help) Render(th theme.Theme, l theme.Layout) Frame {
	const want = 112
	inner := shapeFor(l, want).w - 4
	columns := 3
	switch {
	case l.Short, l.Tier == theme.TierNarrow:
		columns = 1
	case l.Tier == theme.TierMedium:
		columns = 2
	}
	switch {
	case inner < 66: // two columns of this size cut the words
		columns = 1
	case columns == 3 && inner < 106:
		columns = 2
	}
	entries := h.Entries
	if l.Short && !h.More && len(entries) > MostUsed {
		entries = entries[:MostUsed]
	}
	colW := (inner - (columns-1)*2) / columns
	keyW := 0
	for _, e := range entries {
		keyW = max(keyW, Width(e.Key))
	}
	keyW = min(keyW, max(colW/3, 6))
	cell := func(e HelpEntry) string {
		text := e.Text
		tone := theme.ToneFg
		keyStyle := th.Bold(theme.ToneAccent)
		if e.Reason != "" {
			text = strings.TrimPrefix(text+": "+e.Reason, ": ")
			tone = theme.ToneDim
			keyStyle = th.Style(theme.ToneDim)
		}
		return fitTo(keyStyle.Render(treetable.TruncateEnd(e.Key, keyW, th.G.Ellipsis)), keyW) + " " +
			th.Style(tone).Render(treetable.TruncateEnd(text, max(colW-keyW-1, 1), th.G.Ellipsis))
	}
	rowsN := (len(entries) + columns - 1) / columns
	var body []Row
	for r := 0; r < rowsN; r++ {
		parts := make([]string, 0, columns)
		for c := 0; c < columns; c++ {
			i := r + c*rowsN
			if i < len(entries) {
				parts = append(parts, fitTo(cell(entries[i]), colW))
			}
		}
		body = append(body, Row{Lines: []string{strings.Join(parts, "  ")}, Action: "close"})
	}
	var foot []Row
	foot = append(foot, textRow(""))
	hints := "u ascii glyphs  " + th.G.Dot + "  t theme  " + th.G.Dot + "  ? close"
	if l.Short && !h.More && len(h.Entries) > MostUsed {
		hints = "j more  " + th.G.Dot + "  " + hints
	}
	foot = append(foot, Row{Lines: []string{th.Style(theme.ToneDim).Render(treetable.TruncateEnd(hints, inner, th.G.Ellipsis))}, Action: "close"})
	title := "help"
	if h.Line != "" {
		title += " " + th.G.Dot + " " + h.Line
	}
	f, _ := assemble(th, l, want, title, theme.ToneAccent, body, foot, h.Scroll, -1)
	return f
}

// Info is a read-only sheet: the detail panel when there is no room beside the table.
type Info struct {
	Title  string
	Rows   []Row
	Scroll int
}

// Render draws the sheet.
func (i Info) Render(th theme.Theme, l theme.Layout) Frame {
	b := button(th, "backspace close", theme.ToneAccent, true)
	foot := []Row{{Lines: []string{b}, Spans: []Span{{0, Width(b), "close"}}}}
	f, _ := assemble(th, l, 60, i.Title, theme.ToneFg, i.Rows, foot, i.Scroll, -1)
	return f
}
