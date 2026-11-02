package model

import (
	"github.com/afruth/wtree/tui/internal/core"
	"github.com/afruth/wtree/tui/internal/theme"
	"github.com/afruth/wtree/tui/internal/tree"
)

type coreStack = core.Stack

func (m *Model) stackGlyph(st core.Stack) (string, theme.Tone) {
	g := m.th.G
	if st.Kind == core.StackVolumes {
		return g.Volumes, theme.ToneDim
	}
	running, total := 0, 0
	for state, n := range st.States {
		total += n
		if state == "running" {
			running += n
		}
	}
	switch {
	case total > 0 && running == total:
		return g.Running, theme.ToneOk
	case running > 0:
		return g.Partial, theme.ToneWarn
	case total > 0:
		return g.Exited, theme.ToneDim
	}
	return g.Down, theme.ToneDim
}

// stackSummary is the second line of a stack in the panel: state and disk.
func (m *Model) stackSummary(st core.Stack) string {
	running, total := 0, 0
	for state, n := range st.States {
		total += n
		if state == "running" {
			running += n
		}
	}
	word := ""
	switch {
	case st.Kind == core.StackVolumes:
		word = "volumes only"
	case st.Kind == core.StackGone:
		word = "worktree gone " + itoa(running) + "/" + itoa(total)
	case total == 0:
		word = "down"
	case running == total:
		word = itoa(total) + "/" + itoa(total) + " up"
	case running > 0:
		word = "partial " + itoa(running) + "/" + itoa(total)
	default:
		word = "exited " + itoa(running) + "/" + itoa(total)
	}
	if info, ok := m.tree.Lookup("stack:" + st.Project); ok && info.Disk != nil {
		word += " " + m.th.G.Dot + " " + tree.FormatDisk(info.Disk.Bytes)
	}
	return word
}
