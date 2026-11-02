package dialog

import (
	"path/filepath"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/afruth/wtree/tui/internal/theme"
	"github.com/afruth/wtree/tui/internal/treetable"
)

// NewFocus is the part of the new worktree sheet that has the focus.
type NewFocus int

// The parts. In the field, digits type; on an option, they pick.
const (
	NewFocusField NewFocus = iota
	NewFocusOpen
	NewFocusStay
)

// NewWorktree is the sheet of `n`: one field, filled with the name that wtree picked,
// so that enter alone does what `wtree -n` does.
type NewWorktree struct {
	Repo     string // the common dir to create in, or ""
	RepoName string
	Path     string // the path that the core reported for its name
	Head     string
	Input    textinput.Model
	Open     bool // open after creating
	Focus    NewFocus
	Err      string
	Busy     bool
	Flash    bool // enter came on a name that cannot be: the field says so for a moment
}

// NewWorktreeSheet opens the sheet.
func NewWorktreeSheet(th theme.Theme, repo, repoName, path, name, head string) (NewWorktree, tea.Cmd) {
	n := NewWorktree{Repo: repo, RepoName: treetable.Sanitize(repoName), Path: path, Head: head, Open: true}
	n.Input = NewInput(th, "name  ")
	n.Input.SetValue(name)
	n.Input.CursorEnd()
	return n, n.Input.Focus()
}

// NameProblem says what is wrong with a name, in the rule of the core, or "". It only tells
// early, and the core still decides on each name that goes to it.
func NameProblem(name string) string {
	if name == "" {
		return ""
	}
	bad := name[0] == '.' || name[0] == '-'
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
		default:
			bad = true
		}
	}
	if bad {
		return "only letters, digits, dots, dashes, underscores; it starts with a letter or a digit"
	}
	return ""
}

// TargetPath is where the worktree goes with the name in the field.
func (n NewWorktree) TargetPath() string {
	return filepath.Join(filepath.Dir(n.Path), n.Input.Value())
}

// Render draws the sheet.
func (n NewWorktree) Render(th theme.Theme, l theme.Layout) Frame {
	const want = 66
	inner := shapeFor(l, want).w - 4
	ell := th.G.Ellipsis
	dim := th.Style(theme.ToneDim)
	narrow := l.Tier == theme.TierNarrow && !l.Short

	var body []Row
	field := n.Input
	problem := NameProblem(n.Input.Value())
	if problem != "" {
		styles := field.Styles()
		styles.Focused.Text = th.Style(theme.ToneDanger)
		styles.Blurred.Text = th.Style(theme.ToneDanger)
		if n.Flash {
			styles.Focused.Text = th.Style(theme.ToneDanger).Reverse(true)
			styles.Blurred.Text = styles.Focused.Text
		}
		field.SetStyles(styles)
	}
	body = append(body, Row{Lines: []string{field.View()}, Action: "field"})
	if problem != "" {
		for _, line := range Wrap(problem, inner) {
			body = append(body, textRow(th.Style(theme.ToneDanger).Render(line)))
		}
	} else {
		body = append(body, textRow(dim.Render(treetable.TruncatePath(treetable.Sanitize(n.TargetPath()), inner, ell))))
	}
	if n.Head != "" {
		head := n.Head
		if len(head) > 8 {
			head = head[:8]
		}
		body = append(body, textRow(dim.Render("detached at "+treetable.Sanitize(head))))
	}
	body = append(body, textRow(""))
	option := func(key, label string, on, focused bool, action string) Row {
		mark := "( )"
		if on {
			mark = "(" + th.G.Running + ")"
		}
		text := key + " " + mark + " " + label
		line := th.Style(theme.ToneFg).Render(text)
		if on {
			line = th.Bold(theme.ToneAccent).Render(text)
		}
		if focused {
			line = th.Cursor().Render(fitTo(text, inner))
		}
		lines := []string{line}
		if narrow {
			lines = append(lines, "")
		}
		return Row{Lines: lines, Action: action}
	}
	body = append(body,
		option("1", "open after creating", n.Open, n.Focus == NewFocusOpen, "open"),
		option("2", "stay here", !n.Open, n.Focus == NewFocusStay, "stay"))
	if n.Err != "" {
		body = append(body, textRow(""))
		for _, line := range Wrap(n.Err, inner) {
			body = append(body, textRow(th.Style(theme.ToneDanger).Render(line)))
		}
	}
	label := "enter create"
	if n.Busy {
		label = "creating" + ell
	}
	cancel := "backspace cancel"
	hint := "digits type into the name " + th.G.Dot + " tab moves to the options"
	if n.Focus != NewFocusField {
		hint = "1 and 2 pick " + th.G.Dot + " tab goes back to the name"
	} else if n.Input.Value() != "" {
		cancel = "backspace clears, then cancels"
	}
	for _, line := range Wrap(hint, inner) {
		body = append(body, textRow(dim.Render(line)))
	}
	a, b := button(th, cancel, theme.ToneDim, false), button(th, label, theme.ToneAccent, !n.Busy)
	if problem != "" && !n.Busy {
		b = buttonOff(th, "name invalid")
	}
	var foot []Row
	if !l.Short {
		foot = append(foot, textRow(""))
	}
	if Width(a)+2+Width(b) > inner {
		if narrow && l.Height > 24 { // a phone with rows to spare: each is a target of two rows
			foot = append(foot, Row{Lines: []string{a, ""}, Action: "cancel"}, Row{Lines: []string{b, ""}, Action: "create"})
		} else {
			foot = append(foot, Row{Lines: []string{a}, Action: "cancel"}, Row{Lines: []string{b}, Action: "create"})
		}
	} else {
		foot = append(foot, Row{Lines: []string{a + "  " + b}, Spans: []Span{{0, Width(a), "cancel"}, {Width(a) + 2, Width(a) + 2 + Width(b), "create"}}})
	}
	title := "new worktree"
	if n.RepoName != "" {
		title += " in " + n.RepoName
	}
	f, _ := assemble(th, l, want, title, theme.ToneFg, body, foot, 0, -1)
	return f
}
