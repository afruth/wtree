package model

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/afruth/wtree/tui/internal/tree"
)

// Prefs is $XDG_STATE_HOME/wtree/ui.json. A missing or corrupt file means no prefs. It
// never holds a delete level or a consent.
type Prefs struct {
	Scope    string          `json:"scope,omitempty"`
	Expanded map[string]bool `json:"expanded,omitempty"`
	Sort     string          `json:"sort,omitempty"`
	Panel    *bool           `json:"panel,omitempty"`
	ASCII    *bool           `json:"ascii,omitempty"`
	Theme    string          `json:"theme,omitempty"`
}

// LoadPrefs reads ui.json. Any problem gives the empty value.
func LoadPrefs(stateDir string) Prefs {
	var p Prefs
	if stateDir == "" {
		return p
	}
	data, err := os.ReadFile(filepath.Join(stateDir, "ui.json"))
	if err != nil {
		return Prefs{}
	}
	if json.Unmarshal(data, &p) != nil {
		return Prefs{}
	}
	return p
}

func (m *Model) prefs() Prefs {
	p := Prefs{Expanded: m.tree.ExpandedState(), Scope: m.o.Prefs.Scope, Sort: m.o.Prefs.Sort}
	// -g, -d and the fallback outside a repo are for one run. Only a choice of the user is kept.
	if m.scopeSet {
		p.Scope = "repo"
		if m.machine() {
			p.Scope = "machine"
		}
	}
	if m.sortSet {
		p.Sort = ""
		if mode, _ := m.tree.Sort(); mode != tree.SortTree {
			p.Sort = mode.String()
		}
	}
	if m.panelSet {
		panel := m.panel
		p.Panel = &panel
	}
	ascii := m.th.ASCII
	if !m.o.ASCIIForced {
		p.ASCII = &ascii
	}
	if !m.o.ThemeForced && m.o.Prefs.Theme != "" || m.light {
		p.Theme = "dark"
		if m.th.Light {
			p.Theme = "light"
		}
	}
	return p
}

func (m *Model) prefsJSON() string {
	data, err := json.Marshal(m.prefs())
	if err != nil {
		return ""
	}
	return string(data)
}

// savePrefs returns a Cmd that writes ui.json if something changed.
func (m *Model) savePrefs() tea.Cmd {
	if m.o.StateDir == "" {
		return nil
	}
	data := m.prefsJSON()
	if data == m.savedPrefs {
		return nil
	}
	m.savedPrefs = data
	dir := m.o.StateDir
	return func() tea.Msg {
		_ = writePrefs(dir, data)
		return nil
	}
}

func writePrefs(dir, data string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".ui-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, "ui.json"))
}

// writeLog keeps the log of a batch for a week.
func writeLog(stateDir string, at time.Time, lines []string) string {
	if stateDir == "" {
		return ""
	}
	dir := filepath.Join(stateDir, "log")
	if os.MkdirAll(dir, 0o755) != nil {
		return ""
	}
	entries, _ := os.ReadDir(dir)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		info, err := e.Info()
		if err == nil && strings.HasSuffix(e.Name(), ".log") && at.Sub(info.ModTime()) > 7*24*time.Hour {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
	path := filepath.Join(dir, at.Format("20060102-150405.000")+".log")
	if os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644) != nil {
		return ""
	}
	return path
}
