package dialog

import (
	"github.com/afruth/wtree/tui/internal/theme"
)

// Waiting is the sheet while the core reads a plan. A plan of many worktrees can take many
// seconds, and until it comes only cancel acts: a key meant for the list must not get lost, nor
// a part of a batch be dropped without a word.
type Waiting struct {
	Title string
	Text  string
}

// Render draws the sheet, with one button that cancels.
func (w Waiting) Render(th theme.Theme, l theme.Layout) Frame {
	s := shapeFor(l, 54)
	if l.Short && l.Tier != theme.TierNarrow {
		s = shape{w: min(54, l.Width), maxH: l.Height, x: (l.Width - min(54, l.Width)) / 2}
	}
	inner := max(s.w-4, 1)
	var body []Row
	for _, line := range Wrap(w.Text, inner) {
		body = append(body, textRow(th.Style(theme.ToneDim).Render(line)))
	}
	b := button(th, "backspace cancel", theme.ToneDim, true)
	foot := []Row{textRow(""), {Lines: []string{b}, Spans: []Span{{0, Width(b), "cancel"}}}}
	f, _ := assembleSheet(th, l, s, sheetParts{title: w.Title, tone: theme.ToneAccent, body: body, foot: foot, focus: -1})
	return f
}
