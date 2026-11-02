package model

import (
	"strings"

	"github.com/afruth/wtree/tui/internal/dialog"
	"github.com/afruth/wtree/tui/internal/theme"
	"github.com/afruth/wtree/tui/internal/tree"
	"github.com/afruth/wtree/tui/internal/treetable"
)

type panelLine struct {
	text   string
	action string
}

// panelOn says that the panel is wanted: what the user chose, and else the width decides.
func (m *Model) panelOn() bool {
	if m.panelSet {
		return m.panel
	}
	return m.layout.Width >= theme.PanelAuto
}

func (m *Model) panelOnScreen() bool {
	return m.panelOn() && m.layout.PanelAllowed() && !m.layout.TooSmall
}

// panelContent describes the cursor row in the detail panel, or in the sheet that takes
// its place on a smaller screen.
func (m *Model) panelContent(width int) (string, []panelLine) {
	info, ok := m.cursorInfo()
	if !ok {
		return "", nil
	}
	th := m.th
	dim := th.Style(theme.ToneDim)
	fg := th.Style(theme.ToneFg)
	heading := th.Bold(theme.ToneFg)
	ell := th.G.Ellipsis
	dot := " " + th.G.Dot + " "
	var out []panelLine
	add := func(text string) { out = append(out, panelLine{text: text}) }
	wrap := func(text string, style func(...string) string) {
		for _, line := range dialog.Wrap(text, width) {
			add(style(line))
		}
	}
	title := ""
	path := func(p string) string {
		return treetable.TruncatePath(treetable.Sanitize(tildeOf(p, m.o.Home)), width, ell)
	}

	stackLine := func(st coreStack) {
		glyph, tone := m.stackGlyph(st)
		name := treetable.TruncateStart(treetable.Sanitize(st.Project), width-2, ell)
		out = append(out, panelLine{text: th.Style(tone).Render(glyph) + " " + fg.Render(name), action: "panel:stack:" + st.Project})
		text := m.stackSummary(st)
		if text != "" {
			out = append(out, panelLine{text: "  " + dim.Render(treetable.TruncateEnd(text, width-2, ell)), action: "panel:stack:" + st.Project})
		}
	}
	containers := func(st coreStack) {
		for _, c := range st.Containers {
			name := treetable.Sanitize(c.Service)
			if name == "" {
				name = treetable.Sanitize(c.Name)
			}
			ports := ""
			for _, p := range c.Published {
				ports += " :" + treetable.Sanitize(p)
			}
			word := treetable.Sanitize(c.State)
			if c.Health != nil {
				word += " " + treetable.Sanitize(*c.Health)
			}
			line := name + "  " + word + ports
			out = append(out, panelLine{text: fg.Render(treetable.TruncateEnd(line, width, ell)), action: "panel:container:" + st.Project + "/" + c.Name})
		}
	}

	switch info.Kind {
	case treetable.KindWorktree:
		w := info.Worktree
		title = lastSegment(treetable.Sanitize(w.Path))
		add(heading.Render(treetable.TruncateStart(title, width, ell)))
		add(dim.Render(path(w.Path)))
		var parts []string
		if w.Branch != nil {
			parts = append(parts, treetable.Sanitize(*w.Branch))
		} else if w.Detached {
			head := ""
			if w.Head != nil {
				head = treetable.Sanitize(*w.Head)
				if len(head) > 7 {
					head = head[:7]
				}
			}
			parts = append(parts, "detached "+head)
		}
		if g := info.Git; g != nil {
			if g.Ahead != nil && *g.Ahead > 0 {
				parts = append(parts, th.G.Ahead+itoa(*g.Ahead))
			}
			if g.Behind != nil && *g.Behind > 0 {
				parts = append(parts, th.G.Behind+itoa(*g.Behind))
			}
			if g.Changed != nil && *g.Changed > 0 {
				parts = append(parts, itoa(*g.Changed)+" changed")
			}
			if g.Untracked != nil && *g.Untracked > 0 {
				parts = append(parts, itoa(*g.Untracked)+" new")
			}
		}
		if len(parts) > 0 {
			wrap(strings.Join(parts, dot), fg.Render)
		}
		if g := info.Git; g != nil {
			if g.LastCommitSubject != nil {
				last := "last: " + treetable.Sanitize(*g.LastCommitSubject)
				if g.LastCommitAt != nil {
					last += dot + tree.FormatRowAge(m.nowAt.Unix()-*g.LastCommitAt)
				}
				wrap(last, dim.Render)
			}
			if g.MergedInto != nil {
				wrap("merged into "+treetable.Sanitize(*g.MergedInto), th.Style(theme.ToneOk).Render)
			}
			if g.UpstreamGone {
				wrap("its upstream is gone", th.Style(theme.ToneWarn).Render)
			}
			if g.HeadHeld != nil && !*g.HeadHeld {
				wrap("no branch or tag holds its commit", th.Style(theme.ToneWarn).Render)
			}
			if g.Unreadable || g.TimedOut {
				wrap("git could not read it", th.Style(theme.ToneDanger).Render)
			}
		}
		if w.Locked != nil {
			text := "locked"
			if *w.Locked != "" {
				text += ": " + treetable.Sanitize(*w.Locked)
			}
			wrap(text, th.Style(theme.ToneWarn).Render)
		}
		if w.Missing {
			wrap("its directory is gone", th.Style(theme.ToneWarn).Render)
		}
		if info.Cleanup != nil {
			m.cleanupLines(info, width, &out)
		}
		if len(info.Stacks) > 0 {
			add("")
			add(heading.Render("STACKS"))
			for i := range info.Stacks {
				stackLine(info.Stacks[i])
			}
		}
		first := true
		for i := range info.Stacks {
			if len(info.Stacks[i].Containers) == 0 {
				continue
			}
			if first {
				add("")
				add(heading.Render("CONTAINERS"))
				first = false
			}
			containers(info.Stacks[i])
		}
	case treetable.KindStack:
		st := *info.Stack
		title = treetable.Sanitize(st.Project)
		add(heading.Render(treetable.TruncateStart(title, width, ell)))
		if st.Dir != nil {
			add(dim.Render(path(*st.Dir)))
		}
		add(fg.Render(treetable.TruncateEnd(m.stackSummary(st), width, ell)))
		if st.Volumes != nil {
			word := " volumes"
			if *st.Volumes == 1 {
				word = " volume"
			}
			add(dim.Render(itoa(*st.Volumes) + word))
		}
		if info.Cleanup != nil {
			m.cleanupLines(info, width, &out)
		}
		if len(st.Containers) > 0 {
			add("")
			add(heading.Render("CONTAINERS"))
			containers(st)
		}
	case treetable.KindContainer:
		c := info.Container
		title = treetable.Sanitize(c.Name)
		add(heading.Render(treetable.TruncateStart(title, width, ell)))
		add(dim.Render(treetable.TruncateStart(treetable.Sanitize(info.Stack.Project), width, ell)))
		wrap(treetable.Sanitize(c.Status), fg.Render)
		if c.Ports != "" {
			add("")
			add(heading.Render("PORTS"))
			wrap(treetable.Sanitize(c.Ports), dim.Render)
		}
	default:
		title = "cleanup"
		add(heading.Render("cleanup"))
		wrap("Stacks whose directory is gone, volumes that nothing uses, and worktrees that are merged or whose branch is gone.", dim.Render)
		add("")
		sum := m.tree.Summary()
		if sum.ReclaimableKnown {
			add(th.Style(theme.ToneOk).Render("safe ") + fg.Render(tree.FormatDisk(sum.CleanupSafe)))
			add(th.Style(theme.ToneWarn).Render("needs a look ") + fg.Render(tree.FormatDisk(sum.CleanupNeedsLook)))
		}
	}
	return title, out
}

func (m *Model) cleanupLines(info tree.Info, width int, out *[]panelLine) {
	tone := theme.ToneOk
	word := "safe"
	switch info.Cleanup.Tier {
	case "needs_look":
		tone, word = theme.ToneWarn, "needs a look"
	case "never":
		tone, word = theme.ToneDim, "never"
	}
	*out = append(*out, panelLine{})
	*out = append(*out, panelLine{text: m.th.Bold(tone).Render("cleanup: " + word)})
	for _, r := range info.Cleanup.Reasons {
		for _, line := range dialog.Wrap(treetable.Sanitize(r.Text), width) {
			*out = append(*out, panelLine{text: m.th.Style(theme.ToneDim).Render(line)})
		}
	}
}

// panelLines is the panel beside the table. The bar is right under it, so it does not repeat
// the actions.
func (m *Model) panelLines(height int) []panelLine {
	width := theme.PanelWidth - 3
	_, lines := m.panelContent(width)
	start := min(m.panelScroll, max(len(lines)-height, 0))
	if start > 0 {
		lines = lines[start:]
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return lines
}

func (m *Model) panelSheet() dialog.Info {
	width := min(max(m.layout.Width-4, 20), 56)
	title, lines := m.panelContent(width)
	rows := make([]dialog.Row, 0, len(lines))
	for _, line := range lines {
		rows = append(rows, dialog.Row{Lines: []string{line.text}})
	}
	return dialog.Info{Title: title, Rows: rows}
}

func tildeOf(path, home string) string {
	if home != "" && (path == home || strings.HasPrefix(path, home+"/")) {
		return "~" + path[len(home):]
	}
	return path
}
