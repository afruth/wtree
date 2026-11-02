// Package dialog draws the sheets of wtree: the action sheet, the consequences sheet
// with its typed confirmation, the progress and results screen, the command log, the
// new worktree sheet and the help overlay. Each holds its own state and draws from it.
// None does I/O, and none decides what is safe: the plan of the core says that.
//
// A dialog draws to a Frame: the lines, where they go, and the zones that a tap can hit.
// Each zone has an action that also has a key.
package dialog

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/afruth/wtree/tui/internal/theme"
	"github.com/afruth/wtree/tui/internal/treetable"
	"github.com/charmbracelet/x/ansi"
)

// Zone is a tappable area, in screen coordinates. X1 and Y1 are exclusive.
type Zone struct {
	X0, Y0, X1, Y1 int
	Action         string
}

// Frame is a dialog that is drawn and placed on the screen.
type Frame struct {
	X, Y, W, H int
	Lines      []string
	Zones      []Zone
	Scroll     int // the first body row that it drew
}

// MoveX moves the frame and its zones sideways.
func (f *Frame) MoveX(dx int) {
	f.X += dx
	for i := range f.Zones {
		f.Zones[i].X0 += dx
		f.Zones[i].X1 += dx
	}
}

// Hit finds the action at a position. inside says that the position is on the frame.
func (f Frame) Hit(x, y int) (action string, inside bool) {
	inside = x >= f.X && x < f.X+f.W && y >= f.Y && y < f.Y+f.H
	for _, z := range f.Zones {
		if x >= z.X0 && x < z.X1 && y >= z.Y0 && y < z.Y1 {
			return z.Action, inside
		}
	}
	return "", inside
}

// Row is one logical row of a sheet body. All its lines are one target.
type Row struct {
	Lines  []string
	Action string
	Spans  []Span // parts of the first line with their own action
}

// Span is a part of a line, in cells from the start of the text.
type Span struct {
	X0, X1 int
	Action string
}

func textRow(lines ...string) Row { return Row{Lines: lines} }

// Boxes are the border glyphs.
type boxGlyphs struct{ tl, tr, bl, br, h, v string }

func glyphs(th theme.Theme) boxGlyphs {
	if th.ASCII {
		return boxGlyphs{"+", "+", "+", "+", "-", "|"}
	}
	return boxGlyphs{"┌", "┐", "└", "┘", "─", "│"}
}

// Sheet shape: how a dialog sits on the screen in a layout.
type shape struct {
	w, maxH int
	x       int
	bottom  bool // a bottom sheet of full width
	full    bool // the whole screen, on a short screen
}

func shapeFor(l theme.Layout, want int) shape {
	switch {
	case l.Short:
		return shape{w: l.Width, maxH: l.Height, full: true}
	case l.Tier == theme.TierNarrow:
		return shape{w: l.Width, maxH: l.Height, bottom: true}
	}
	w := min(want, l.Width-4)
	return shape{w: w, maxH: l.Height - 2, x: (l.Width - w) / 2}
}

// assemble draws a box with a title, a body that scrolls and a foot that does not.
// focus is the index of the body row that must stay in view, or -1. It returns the frame
// and the scroll it used.
func assemble(th theme.Theme, l theme.Layout, want int, title string, titleTone theme.Tone, body, foot []Row, scroll, focus int) (Frame, int) {
	return assembleSheet(th, l, shapeFor(l, want), sheetParts{title: title, tone: titleTone, body: body, foot: foot, scroll: scroll, focus: focus})
}

// sheetParts are what a box holds. head stays under the title while the body scrolls, and foot
// stays at the bottom. styled replaces the title: it has its own colours, part by part.
type sheetParts struct {
	title, styled    string
	tone             theme.Tone
	head, body, foot []Row
	scroll, focus    int
}

func assembleSheet(th theme.Theme, l theme.Layout, s shape, p sheetParts) (Frame, int) {
	title, titleTone, body, foot, scroll, focus := p.title, p.tone, p.body, p.foot, p.scroll, p.focus
	g := glyphs(th)
	border := th.Style(theme.ToneDim)
	inner := s.w - 4 // two borders and two spaces

	footH := 0
	for _, r := range p.head {
		footH += len(r.Lines)
	}
	for _, r := range foot {
		footH += len(r.Lines)
	}
	bodyH := 0
	for _, r := range body {
		bodyH += len(r.Lines)
	}
	room := max(s.maxH-2-footH, 1)
	if s.full {
		bodyH = max(bodyH, room) // a full screen sheet fills the screen
	}
	clipped := bodyH > room

	// window the body by rows, with the focus row in view
	start := max(scroll, 0)
	if start >= len(body) {
		start = max(len(body)-1, 0)
	}
	fit := func(from, to int) int {
		n := 0
		for i := from; i < to && i < len(body); i++ {
			n += len(body[i].Lines)
		}
		return n
	}
	if focus >= 0 {
		start = min(start, focus)
		for start < focus && fit(start, focus+1) > room {
			start++
		}
	}
	// no scrolling past the end
	for start > 0 && fit(start-1, len(body)) <= room {
		start--
	}

	lines := make([]string, 0, s.maxH)
	var zones []Zone
	titleText := treetable.TruncateEnd(title, max(s.w-6, 1), th.G.Ellipsis)
	drawn := th.Bold(titleTone).Render(titleText)
	if p.styled != "" {
		drawn = ansi.Truncate(p.styled, max(s.w-6, 1), th.G.Ellipsis)
		titleText = drawn
	}
	fill := max(s.w-Width(titleText)-5, 0)
	lines = append(lines, border.Render(g.tl+g.h+" ")+drawn+border.Render(" "+strings.Repeat(g.h, fill)+g.tr))

	put := func(text string) {
		text = fitTo(text, inner)
		lines = append(lines, border.Render(g.v)+" "+text+" "+border.Render(g.v))
	}
	for _, r := range p.head {
		for _, t := range r.Lines {
			put(t)
		}
	}
	used := 0
	last := start
	for i := start; i < len(body); i++ {
		r := body[i]
		if used+len(r.Lines) > room {
			break
		}
		y := len(lines)
		if r.Action != "" {
			zones = append(zones, Zone{0, y, s.w, y + len(r.Lines), r.Action})
		}
		for _, span := range r.Spans {
			zones = append(zones, Zone{2 + span.X0, y, 2 + span.X1, y + 1, span.Action})
		}
		for _, t := range r.Lines {
			put(t)
		}
		used += len(r.Lines)
		last = i
	}
	more := clipped && last < len(body)-1
	for used < room && (used < bodyH || s.full) {
		put("")
		used++
	}
	if more || start > 0 {
		// mark the last body line, so that a long sheet says that it goes on
		hint := th.Style(theme.ToneDim).Render(moreHint(th, start > 0, more))
		lines[len(lines)-1] = border.Render(g.v) + " " + fitTo(hint, inner) + " " + border.Render(g.v)
	}
	for _, r := range foot {
		y := len(lines)
		if r.Action != "" {
			zones = append(zones, Zone{0, y, s.w, y + len(r.Lines), r.Action})
		}
		for _, span := range r.Spans {
			zones = append(zones, Zone{2 + span.X0, y, 2 + span.X1, y + 1, span.Action})
		}
		for _, t := range r.Lines {
			put(t)
		}
	}
	lines = append(lines, border.Render(g.bl+strings.Repeat(g.h, max(s.w-2, 0))+g.br))

	y := (l.Height - len(lines)) / 2
	switch {
	case s.full:
		y = 0
	case s.bottom:
		y = l.Height - len(lines)
	}
	y = max(y, 0)
	f := Frame{X: s.x, Y: y, W: s.w, H: len(lines), Lines: lines, Scroll: start}
	for _, z := range zones {
		f.Zones = append(f.Zones, Zone{z.X0 + s.x, z.Y0 + y, z.X1 + s.x, z.Y1 + y, z.Action})
	}
	return f, start
}

func moreHint(th theme.Theme, above, below bool) string {
	arrow := func(u, a string) string {
		if th.ASCII {
			return a
		}
		return u
	}
	switch {
	case above && below:
		return arrow("↑↓", "^v") + " more"
	case above:
		return arrow("↑", "^") + " more"
	}
	return arrow("↓", "v") + " more"
}

// Width is the width of styled text in cells.
func Width(s string) int { return ansi.StringWidth(s) }

// fitTo pads or cuts styled text to exactly w cells.
func fitTo(s string, w int) string {
	n := Width(s)
	switch {
	case n > w:
		return ansi.Truncate(s, w, "")
	case n < w:
		return s + strings.Repeat(" ", w-n)
	}
	return s
}

// Wrap breaks plain text on spaces, into lines of at most w cells.
func Wrap(text string, w int) []string {
	if w <= 0 {
		return nil
	}
	var out []string
	cur := ""
	for _, word := range strings.Fields(text) {
		for Width(word) > w {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			head := ansi.Truncate(word, w, "")
			out = append(out, head)
			word = ansi.Cut(word, w, Width(word))
		}
		switch {
		case cur == "":
			cur = word
		case Width(cur)+1+Width(word) <= w:
			cur += " " + word
		default:
			out = append(out, cur)
			cur = word
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

// button draws one button. Every button is a block in its tone, so that its colour is on the
// screen before any key. Focus is a marker and bold, and it takes no width, so nothing moves.
func button(th theme.Theme, text string, tone theme.Tone, focused bool) string {
	if focused {
		return th.Button(tone).Bold(true).Render(th.G.InProgress + " " + text + " ")
	}
	return th.Button(tone).Render("  " + text + " ")
}

// buttonOff is a button that does nothing now.
func buttonOff(th theme.Theme, text string) string {
	return th.ButtonOff().Render("  " + text + " ")
}

func style(th theme.Theme, tone theme.Tone) lipgloss.Style { return th.Style(tone) }

// rightAlign puts b at the right end of a line of w cells that starts with a.
func rightAlign(a, b string, w int) string {
	gap := w - Width(a) - Width(b)
	if gap < 1 {
		return fitTo(a, max(w-Width(b)-1, 0)) + " " + b
	}
	return a + strings.Repeat(" ", gap) + b
}
