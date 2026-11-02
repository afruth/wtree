package model

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/afruth/wtree/tui/internal/core"
	"github.com/afruth/wtree/tui/internal/term"
	"github.com/afruth/wtree/tui/internal/theme"
	"github.com/charmbracelet/x/ansi"
)

// harness drives a Model with a FakeClient, as the Bubble Tea loop does: one Update at a
// time, each Cmd on its own goroutine. Timers do not run: a test fires them.
type harness struct {
	t      *testing.T
	m      *Model
	fake   *core.FakeClient
	msgs   chan tea.Msg
	timers []timerEntry
	quit   bool
	clock  time.Time
}

type timerEntry struct {
	d   time.Duration
	msg tea.Msg
}

type harnessOption func(*Options)

func withRex(o *Options)    { o.Caps.Rex = true }
func withASCII(o *Options)  { o.Theme.ASCII = true; o.ASCIIForced = true }
func withGlobal(o *Options) { o.Global = true }
func withSlow(o *Options)   { o.Caps.Slow = true }

func newHarness(t *testing.T, w, h int, options ...harnessOption) *harness {
	t.Helper()
	hs := &harness{t: t, fake: core.NewFakeClient(), msgs: make(chan tea.Msg, 64), clock: time.Unix(1767323045+3600, 0)}
	o := Options{
		Client: hs.fake, Cwd: "/fixture/home/code/proj", Home: "/fixture/home",
		Theme: theme.Options{Profile: theme.TrueColor}, ThemeForced: true, Caps: term.Caps{},
		Now: func() time.Time { return hs.clock },
		Timer: func(d time.Duration, msg tea.Msg) tea.Cmd {
			hs.timers = append(hs.timers, timerEntry{d, msg})
			return nil
		},
	}
	for _, f := range options {
		f(&o)
	}
	hs.m = New(o)
	t.Cleanup(func() { close(hs.m.uiDone); hs.m.cancel() })
	hs.send(tea.WindowSizeMsg{Width: w, Height: h})
	return hs
}

// start runs Init and pumps until the list has come.
func (h *harness) start() *harness {
	h.exec(h.m.Init())
	h.pump()
	return h
}

func (h *harness) exec(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	go func() {
		defer func() { _ = recover() }()
		if msg := cmd(); msg != nil {
			h.msgs <- msg
		}
	}()
}

func (h *harness) send(msg tea.Msg) {
	_, cmd := h.m.Update(msg)
	h.exec(cmd)
}

func (h *harness) handle(msg tea.Msg) {
	switch msg := msg.(type) {
	case tea.BatchMsg:
		for _, cmd := range msg {
			h.exec(cmd)
		}
	case tea.QuitMsg:
		h.quit = true
	default:
		h.send(msg)
	}
}

// pump delivers messages until none comes for a moment.
func (h *harness) pump() {
	h.t.Helper()
	for {
		select {
		case msg := <-h.msgs:
			h.handle(msg)
		case <-time.After(30 * time.Millisecond):
			return
		}
	}
}

func (h *harness) key(names ...string) {
	h.t.Helper()
	for _, name := range names {
		h.m.sheetHold = time.Time{} // the clock holds still, and a test is a person who has read the sheet
		h.send(keyOf(name))
		h.pump()
	}
}

// rapid sends keys with no time between them and no message in between: a held key, a buffered
// key or a second send. The plan of the first has not come when the next one lands.
func (h *harness) rapid(names ...string) {
	h.t.Helper()
	for _, name := range names {
		h.send(keyOf(name))
	}
}

// letGo lets the time pass in which a repeat of the last key counts as that key held down.
func (h *harness) letGo() { h.clock = h.clock.Add(2 * heldKey) }

func (h *harness) typeText(text string) {
	for _, r := range text {
		h.send(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	h.pump()
}

func keyOf(name string) tea.KeyPressMsg {
	switch name {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	}
	r := []rune(name)[0]
	return tea.KeyPressMsg{Code: r, Text: name}
}

func (h *harness) tap(x, y int) {
	h.t.Helper()
	h.m.sheetHold = time.Time{}
	h.send(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	h.send(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
	h.pump()
}

// fire delivers the timers whose message has this type, and forgets them.
func fire[T tea.Msg](h *harness) int {
	n := 0
	timers := h.timers
	h.timers = nil
	for _, e := range timers {
		if _, ok := e.msg.(T); ok {
			h.send(e.msg)
			n++
		} else {
			h.timers = append(h.timers, e)
		}
	}
	h.pump()
	return n
}

func armed[T tea.Msg](h *harness) int {
	n := 0
	for _, e := range h.timers {
		if _, ok := e.msg.(T); ok {
			n++
		}
	}
	return n
}

func (h *harness) screen() string { return ansi.Strip(h.m.View().Content) }

func (h *harness) expect(text string) {
	h.t.Helper()
	if !strings.Contains(h.screen(), text) {
		h.t.Fatalf("the screen does not show %q:\n%s", text, h.screen())
	}
}

func (h *harness) expectNot(text string) {
	h.t.Helper()
	if strings.Contains(h.screen(), text) {
		h.t.Fatalf("the screen shows %q:\n%s", text, h.screen())
	}
}

func (h *harness) calls(op string) []core.FakeCall {
	var out []core.FakeCall
	for _, c := range h.fake.Calls() {
		if strings.HasPrefix(c.Op, op) {
			out = append(out, c)
		}
	}
	return out
}

// mildPlans are the plans without force or volumes: a sheet with levels reads the other level
// ahead, and these are the plans that the keys asked for.
func (h *harness) mildPlans() []core.FakeCall {
	var out []core.FakeCall
	for _, c := range h.calls("plan") {
		if !hasArgs(c.Args, "--force") && !hasArgs(c.Args, "--volumes") {
			out = append(out, c)
		}
	}
	return out
}

func (h *harness) cursorTo(id string) {
	h.t.Helper()
	if !h.m.table.SetCursor(id) {
		h.t.Fatalf("no row %s", id)
	}
}

// findLine is the first line of the screen with this text, and the cell of the text.
func (h *harness) findLine(text string) (x, y int) {
	h.t.Helper()
	for i, line := range strings.Split(h.screen(), "\n") {
		if at := strings.Index(line, text); at >= 0 {
			return ansi.StringWidth(line[:at]), i
		}
	}
	h.t.Fatalf("no %q on the screen:\n%s", text, h.screen())
	return 0, 0
}

// checkFrame asserts that the frame has exactly the size of the terminal.
func (h *harness) checkFrame() {
	h.t.Helper()
	lines := strings.Split(h.m.View().Content, "\n")
	if len(lines) != h.m.layout.Height {
		h.t.Fatalf("%d lines at height %d:\n%s", len(lines), h.m.layout.Height, h.screen())
	}
	for i, line := range lines {
		if w := ansi.StringWidth(line); w != h.m.layout.Width {
			h.t.Fatalf("line %d has %d cells at width %d: %q\n%s", i, w, h.m.layout.Width, ansi.Strip(line), h.screen())
		}
	}
}

func hasArgs(args []string, want ...string) bool {
	return strings.Contains(" "+strings.Join(args, " ")+" ", " "+strings.Join(want, " ")+" ")
}

var sizes = [][2]int{{120, 35}, {80, 24}, {48, 40}, {93, 14}, {100, 30}, {60, 20}, {48, 23}, {40, 30}, {41, 12}, {200, 60}, {39, 20}}

const (
	wtAlpha  = "worktree:/fixture/home/code/worktrees/alpha"
	wtBeta   = "worktree:/fixture/home/code/worktrees/beta"
	wtMerged = "worktree:/fixture/home/code/worktrees/merged"
	wtMain   = "worktree:/fixture/home/code/proj"
	stAlpha  = "stack:proj-alpha"
	stOld    = "stack:proj-old"
	stGone   = "stack:proj-gone"
	stVols   = "stack:proj-vols"
)
