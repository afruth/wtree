package model

import (
	"fmt"
	"strings"

	"github.com/afruth/wtree/tui/internal/dialog"
)

// openHelp fills the overlay with the keys of the list. It says why a key does nothing
// now, and it starts with what a phone user needs to see why something is missing.
func (m *Model) openHelp() {
	m.helpReturn = m.mode
	if m.mode == ModeHelp {
		m.helpReturn = ModeList
	}
	m.refreshHelp()
	m.mode = ModeHelp
}

func (m *Model) refreshHelp() {
	switch m.helpReturn {
	case ModeMenu:
		m.fillHelp([]dialog.HelpEntry{
			{Key: "1-9", Text: "pick an item"},
			{Key: "j k", Text: "move"},
			{Key: "enter", Text: "pick item under cursor"},
			{Key: "backspace", Text: "back"},
			{Key: "0", Text: "cancel"},
			{Key: "?", Text: "this help"},
		})
		return
	case ModeConfirm:
		m.fillHelp([]dialog.HelpEntry{
			{Key: "1-9", Text: "pick a level, a choice"},
			{Key: "enter", Text: "focus confirm, confirm"},
			{Key: "y", Text: "confirm"},
			{Key: "tab", Text: "cancel or confirm"},
			{Key: "h", Text: "unmark hidden by filter"},
			{Key: "j k", Text: "scroll"},
			{Key: "backspace", Text: "cancel: nothing runs"},
			{Key: "?", Text: "this help"},
		})
		return
	}
	loading := ""
	if !m.structureReady() {
		loading = "still loading"
	}
	reason := func(action string) string {
		for _, b := range m.barButtons() {
			if b.Action == action && b.Reason != "" {
				return b.Reason
			}
		}
		return loading
	}
	deleteText, deleteReason := "delete: pick level", reason(actDelete)
	if info, ok := m.cursorInfo(); ok && neverReason(info) != "" {
		deleteText, deleteReason = "", neverReason(info) // the reason needs the room
	}
	entries := []dialog.HelpEntry{
		{Key: "j k", Text: "move"},
		{Key: "enter", Text: "open or jump", Reason: reason(actOpen)},
		{Key: "a", Text: "actions of the row"},
		{Key: "space", Text: "mark"},
		{Key: "s", Text: "stop", Reason: reason(actStop)},
		{Key: "d", Text: deleteText, Reason: deleteReason},
		{Key: "/", Text: "search"},
		{Key: "c", Text: "cleanup view"},
		{Key: "g", Text: "repo or machine"},
		{Key: "q", Text: "quit"},
		{Key: "J K", Text: "move by a page"},
		{Key: "h l", Text: "collapse, expand"},
		{Key: "z", Text: "all open or closed"},
		{Key: "tab", Text: "mark"},
		{Key: "A", Text: "mark all that is shown"},
		{Key: "u", Text: "unmark all"},
		{Key: "n", Text: "new worktree", Reason: loading},
		{Key: "o", Text: "cycle the order"},
		{Key: "p", Text: "detail panel"},
		{Key: "r", Text: "read everything again"},
		{Key: "L", Text: "log of the last batch"},
		{Key: "y", Text: "copy the path"},
		{Key: "backspace", Text: "filter, cleanup, marks"},
		{Key: "?", Text: "this help"},
	}
	m.fillHelp(entries)
}

func (m *Model) fillHelp(entries []dialog.HelpEntry) {
	glyphs := "unicode"
	if m.th.ASCII {
		glyphs = "ascii"
	}
	shade := "dark"
	if m.th.Light {
		shade = "light"
	}
	line := fmt.Sprintf("%s %dx%d %s %s %s %s", m.layout.Tier, m.layout.Width, m.layout.Height, m.th.G.Dot, glyphs, m.th.G.Dot, shade)
	if hidden := m.table.HiddenColumns(); len(hidden) > 0 {
		line += " " + m.th.G.Dot + " hides " + strings.Join(hidden, ",")
	}
	m.help = dialog.Help{
		Line:    line,
		Entries: entries,
	}
}
