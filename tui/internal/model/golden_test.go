package model

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/afruth/wtree/tui/internal/core"
	"github.com/afruth/wtree/tui/internal/term"
	"github.com/afruth/wtree/tui/internal/theme"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/teatest/v2"
)

// Golden frames. Each scene runs in a real Bubble Tea program under teatest, with the fake
// client and a fixed clock, at each size. A frame is what View gave after the last Update,
// and it is stored twice: .ansi is raw, so a change of style shows as a diff, and .txt has
// no escape sequences, so a change of layout is readable.
//
//	go test ./internal/model -run Golden -update
//
// The flag -update belongs to the golden package that teatest brings in.
func updating() bool {
	f := flag.Lookup("update")
	return f != nil && f.Value.String() == "true"
}

var goldenSizes = [][2]int{{120, 40}, {100, 30}, {80, 24}, {60, 20}, {48, 23}, {48, 40}, {93, 14}}

// frameModel keeps the frame of the event loop for the test goroutine, and it counts
// updates, so that a test can tell when the program has gone quiet.
type frameModel struct {
	*Model
	frame   atomic.Pointer[string]
	updates atomic.Int64
}

// setCursorMsg moves the cursor to a row by its id, inside the event loop.
type setCursorMsg struct{ id string }

// readSheetMsg is the time in which a person reads a sheet, on a clock that holds still.
type readSheetMsg struct{}

func (f *frameModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if c, ok := msg.(setCursorMsg); ok {
		f.Model.table.SetCursor(c.id)
	} else if _, ok := msg.(readSheetMsg); ok {
		f.Model.sheetHold = time.Time{}
	} else {
		_, cmd = f.Model.Update(msg)
	}
	content := f.Model.View().Content
	f.frame.Store(&content)
	f.updates.Add(1)
	return f, cmd
}

// goldenTimer drops the timers that would make a frame depend on the moment: the spinner,
// the stats tick, the expiry of a status. A resize keeps its real timers.
func goldenTimer(d time.Duration, msg tea.Msg) tea.Cmd {
	switch msg.(type) {
	case settleMsg, redrawMsg:
		return tea.Tick(d, func(time.Time) tea.Msg { return msg })
	}
	return nil
}

type driver struct {
	t    testing.TB
	f    *frameModel
	tm   *teatest.TestModel
	fake *core.FakeClient
}

type driverOption func(*Options)

func startDriver(t testing.TB, w, h int, setup func(*core.FakeClient), options ...driverOption) *driver {
	t.Helper()
	fake := core.NewFakeClient()
	if setup != nil {
		setup(fake)
	}
	clock := time.Unix(1767323045+3600, 0)
	o := Options{
		Client: fake, Cwd: "/fixture/home/code/proj", Home: "/fixture/home",
		Theme: theme.Options{Profile: theme.TrueColor}, ThemeForced: true, ASCIIForced: true, Caps: term.Caps{},
		Now: func() time.Time { return clock }, Timer: goldenTimer,
	}
	for _, f := range options {
		f(&o)
	}
	f := &frameModel{Model: New(o)}
	tm := teatest.NewTestModel(t, f, teatest.WithInitialTermSize(w, h),
		teatest.WithProgramOptions(tea.WithColorProfile(colorprofile.TrueColor)))
	d := &driver{t: t, f: f, tm: tm, fake: fake}
	t.Cleanup(func() {
		fake.Release()
		_ = tm.Quit()
		tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
		f.Model.cancel()
	})
	return d
}

func (d *driver) raw() string {
	if p := d.f.frame.Load(); p != nil {
		return *p
	}
	return ""
}

func (d *driver) text() string { return stripTrailing(ansi.Strip(d.raw())) }

// settle waits until no update came for a moment.
func (d *driver) settle() {
	last := d.f.updates.Load()
	still := time.Now()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		time.Sleep(5 * time.Millisecond)
		now := d.f.updates.Load()
		if now != last {
			last, still = now, time.Now()
			continue
		}
		if time.Since(still) >= 60*time.Millisecond {
			return
		}
	}
}

func (d *driver) wait(text string) {
	d.t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		if strings.Contains(ansi.Strip(d.raw()), text) {
			d.settle()
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	d.t.Fatalf("the screen never showed %q:\n%s", text, d.text())
}

func (d *driver) key(names ...string) {
	d.t.Helper()
	for _, name := range names {
		d.tm.Send(readSheetMsg{})
		d.tm.Send(keyOf(name))
		d.settle()
	}
}

func (d *driver) typeText(text string) {
	for _, r := range text {
		d.tm.Send(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	d.settle()
}

func (d *driver) cursor(id string) {
	d.tm.Send(setCursorMsg{id})
	d.settle()
}

// loaded waits until the whole recording of the list is in.
func (d *driver) loaded() {
	d.t.Helper()
	d.wait("alpha")
	d.settle()
}

type goldenScene struct {
	name  string
	setup func(*core.FakeClient)
	run   func(*driver)
	opts  []driverOption
	sizes [][2]int // nil: all of goldenSizes
}

// the plan of two stacks with volumes, and an apply of its four steps
func stacksPlan(apply core.FakeScript) func(*core.FakeClient) {
	return func(f *core.FakeClient) {
		f.On("plan:"+core.ActionDeleteStacks, core.FakeScript{Fixture: "plan-delete-stacks-volumes.json"})
		f.On("apply", apply)
	}
}

func (d *driver) applyFailing() {
	d.t.Helper()
	d.loaded()
	d.cursor(stGone)
	d.key("d")
	d.wait("GOES")
	d.key("4", "y")
	d.wait("Type ")
	d.typeText("2 stacks w")
	d.key("enter")
	d.wait("failed: volume")
}

func goldenScenes() []goldenScene {
	light := func(o *Options) { o.Theme.Light = true }
	ascii := func(o *Options) { o.Theme.ASCII = true }
	return []goldenScene{
		{
			name:  "loading",
			setup: func(f *core.FakeClient) { f.On("list", core.FakeScript{Fixture: "list-stream.ndjson", PauseAfter: -1}) },
			run:   func(d *driver) { d.wait("loading worktrees") },
		},
		{
			name:  "structure",
			setup: func(f *core.FakeClient) { f.On("list", core.FakeScript{Fixture: "list-stream.ndjson", PauseAfter: 14}) },
			run:   func(d *driver) { d.wait("alpha") },
		},
		{name: "loaded", run: func(d *driver) { d.loaded() }},
		{name: "filtered", run: func(d *driver) {
			d.loaded()
			d.key("/")
			d.typeText("alpha")
			d.key("enter")
			d.wait("filter")
		}},
		{name: "action_sheet", run: func(d *driver) {
			d.loaded()
			d.cursor(wtAlpha)
			d.key("a")
			d.wait("delete worktree")
		}},
		{name: "consequences", run: func(d *driver) {
			d.loaded()
			d.cursor(wtAlpha)
			d.key("d")
			d.wait("GOES")
		}},
		{
			name:  "phrase",
			setup: stacksPlan(core.FakeScript{Fixture: "apply-failed.ndjson"}),
			run: func(d *driver) {
				d.loaded()
				d.cursor(stGone)
				d.key("d")
				d.wait("GOES")
				d.key("y")
				d.wait("Type ")
			},
		},
		{
			name:  "phrase_near_miss",
			setup: stacksPlan(core.FakeScript{Fixture: "apply-failed.ndjson"}),
			run: func(d *driver) {
				d.loaded()
				d.cursor(stGone)
				d.key("d")
				d.wait("GOES")
				d.key("y")
				d.wait("Type ")
				d.typeText("2 stack")
				d.key("enter")
				d.wait("expected: 2 stacks")
			},
		},
		{
			// the stream holds after the second step: one failed, one ok, two still to come
			name:  "applying_failure",
			setup: stacksPlan(core.FakeScript{Fixture: "apply-failed.ndjson", PauseAfter: 6}),
			run:   func(d *driver) { d.applyFailing() },
		},
		{
			name:  "results",
			setup: stacksPlan(core.FakeScript{Fixture: "apply-failed.ndjson"}),
			run: func(d *driver) {
				d.applyFailing()
				d.wait("skipped")
			},
		},
		{name: "cleanup", run: func(d *driver) {
			d.loaded()
			d.key("c")
			d.wait("c back")
		}},
		{name: "help", run: func(d *driver) {
			d.loaded()
			d.key("?")
			d.settle()
		}},
		{
			name:  "docker_down",
			setup: func(f *core.FakeClient) { f.On("list", core.FakeScript{Fixture: "list-docker-down.ndjson"}) },
			run:   func(d *driver) { d.wait("docker is not running") },
		},
		{
			name:  "new_sheet",
			sizes: [][2]int{{48, 23}, {80, 24}, {93, 14}},
			run: func(d *driver) {
				d.loaded()
				d.key("n")
				d.wait("open after creating")
			},
		},
		{
			name:  "new_sheet_bad_name",
			sizes: [][2]int{{48, 23}},
			run: func(d *driver) {
				d.loaded()
				d.key("n")
				d.wait("open after creating")
				d.typeText("/zzz")
				d.wait("only letters")
			},
		},
		{
			name:  "volumes_only",
			setup: func(f *core.FakeClient) { f.On("plan:"+core.ActionDeleteStacks, core.FakeScript{Data: lonelyPlan()}) },
			sizes: [][2]int{{48, 23}, {120, 40}},
			run: func(d *driver) {
				d.loaded()
				d.cursor(stVols)
				d.key("d")
				d.wait("nothing goes")
			},
		},
		{
			name:  "light",
			opts:  []driverOption{light},
			sizes: [][2]int{{120, 40}},
			run:   func(d *driver) { d.loaded() },
		},
		{
			name:  "ascii",
			opts:  []driverOption{ascii},
			sizes: [][2]int{{80, 24}, {48, 40}},
			run:   func(d *driver) { d.loaded() },
		},
	}
}

func TestGoldenFrames(t *testing.T) {
	for _, scene := range goldenScenes() {
		sizes := scene.sizes
		if sizes == nil {
			sizes = goldenSizes
		}
		for _, s := range sizes {
			name := fmt.Sprintf("%s_%dx%d", scene.name, s[0], s[1])
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				d := startDriver(t, s[0], s[1], scene.setup, scene.opts...)
				scene.run(d)
				checkFrameSize(t, d.raw(), s[0], s[1])
				d.compare(name)
			})
		}
	}
}

// Rotation: the frame at the new size has nothing of the old one. It must be the very
// frame that a program that started at that size draws.
func TestGoldenRotationHasNoLeftovers(t *testing.T) {
	for _, c := range []struct {
		name string
		run  func(*driver)
	}{
		{"list", func(*driver) {}},
		{"sheet", func(d *driver) {
			d.cursor(wtAlpha)
			d.key("d")
			d.wait("GOES")
		}},
		{"help", func(d *driver) { d.key("?") }},
	} {
		t.Run(c.name, func(t *testing.T) {
			old := startDriver(t, 93, 14, nil)
			old.loaded()
			c.run(old)
			before := old.text()

			var quiet bytes.Buffer
			_, _ = io.Copy(&quiet, old.tm.Output())
			old.tm.Send(tea.WindowSizeMsg{Width: 48, Height: 41})
			old.settle()
			time.Sleep(2 * (settleAfter + redrawAfter))
			old.settle()
			var drawn bytes.Buffer
			_, _ = io.Copy(&drawn, old.tm.Output())
			if !bytes.Contains(drawn.Bytes(), []byte("\x1b[2J")) {
				t.Errorf("a rotation must clear the screen, so that a terminal keeps nothing: %q", tail(drawn.String(), 200))
			}

			checkFrameSize(t, old.raw(), 48, 41)
			fresh := startDriver(t, 48, 41, nil)
			fresh.loaded()
			c.run(fresh)
			if old.raw() != fresh.raw() {
				t.Errorf("the rotated frame differs from a fresh one:\n%s", firstDiff(fresh.text(), old.text()))
			}
			// the header of the short rule merges the title line: none of it may stay
			if strings.Contains(old.text(), "CPU") && !strings.Contains(fresh.text(), "CPU") {
				t.Errorf("a column title of the old tier stayed:\n%s", old.text())
			}
			if before == old.text() {
				t.Error("the frame did not change")
			}
			if c.name == "list" {
				old.compare("rotation_93x14_to_48x41")
			}
		})
	}
}

// compare checks the frame against its two files. A frame can still change for a moment
// after the program went quiet on a loaded machine, so a difference gets some time.
func (d *driver) compare(name string) {
	d.t.Helper()
	dir := filepath.Join("testdata", "frames")
	if updating() {
		time.Sleep(150 * time.Millisecond)
		d.settle()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			d.t.Fatal(err)
		}
		for _, f := range []struct{ ext, data string }{{".ansi", d.raw()}, {".txt", d.text()}} {
			if err := os.WriteFile(filepath.Join(dir, name+f.ext), []byte(f.data+"\n"), 0o644); err != nil {
				d.t.Fatal(err)
			}
		}
		return
	}
	read := func(ext string) string {
		data, err := os.ReadFile(filepath.Join(dir, name+ext))
		if err != nil {
			d.t.Fatalf("no golden frame, run: go test ./internal/model -run Golden -update: %v", err)
		}
		return strings.TrimSuffix(string(data), "\n")
	}
	wantRaw, wantText := read(".ansi"), read(".txt")
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
		if d.raw() == wantRaw && d.text() == wantText {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	if d.text() != wantText {
		d.t.Errorf("%s.txt differs:\n%s", name, firstDiff(wantText, d.text()))
		return
	}
	d.t.Errorf("%s.ansi differs in style only:\n%s", name, firstDiff(wantRaw, d.raw()))
}

func checkFrameSize(t testing.TB, frame string, w, h int) {
	t.Helper()
	lines := strings.Split(frame, "\n")
	if len(lines) != h {
		t.Errorf("%d lines at height %d:\n%s", len(lines), h, ansi.Strip(frame))
	}
	for i, line := range lines {
		if got := ansi.StringWidth(line); got != w {
			t.Errorf("line %d has %d cells at width %d: %q", i, got, w, ansi.Strip(line))
		}
	}
}

func stripTrailing(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
	}
	return strings.Join(lines, "\n")
}

func firstDiff(want, got string) string {
	a, b := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := 0; i < len(a) || i < len(b); i++ {
		var x, y string
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			return fmt.Sprintf("line %d\nwant %q\ngot  %q\n--- whole frame\n%s", i, x, y, got)
		}
	}
	return "same lines"
}

func tail(s string, n int) string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}

// A short screen has no status line: its footer still says that a filter hides rows and
// how many marks there are.
func TestShortFooterShowsMarksAndFilter(t *testing.T) {
	h := newHarness(t, 93, 14).start()
	h.cursorTo(wtAlpha)
	h.key("space")
	h.cursorTo(wtBeta)
	h.key("space", "/")
	h.typeText("alpha")
	h.key("enter")
	h.expect("2 marked (1 hidden by")
	h.expect("a actions")
	h.checkFrame()
	h.key("u")
	h.expect("filter: alpha")
	h.checkFrame()
}

// A row is a target of the full width, also where a short screen has no column left to draw.
func TestTapRightOfTheColumnsIsStillTheRow(t *testing.T) {
	for _, s := range [][2]int{{93, 14}, {60, 20}} {
		h := newHarness(t, s[0], s[1]).start()
		_, y := h.findLine("beta")
		h.tap(s[0]-2, y)
		if h.m.table.Cursor() != wtBeta {
			t.Fatalf("%v: cursor %s\n%s", s, h.m.table.Cursor(), h.screen())
		}
	}
}

// With docker down nothing is live: the header must not say that the numbers are fresh.
func TestDockerDownIsNotLive(t *testing.T) {
	h := newHarness(t, 120, 40)
	h.fake.On("list", core.FakeScript{Fixture: "list-docker-down.ndjson"})
	h.start()
	h.expect("docker is not running")
	h.expectNot("live ")
	h.expectNot("disk ?")
}
