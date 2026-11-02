package treetable_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/afruth/wtree/tui/internal/theme"
	"github.com/afruth/wtree/tui/internal/tree"
	"github.com/afruth/wtree/tui/internal/tree/treetest"
	"github.com/afruth/wtree/tui/internal/treetable"
	"github.com/charmbracelet/x/ansi"
	"github.com/clipperhouse/displaywidth"
)

var mono = theme.New(theme.Options{Profile: theme.Mono})

func tableFor(t *testing.T, w, h, tableW, tableH, top int, scope tree.Scope, set treetest.Set) treetable.Model {
	t.Helper()
	s := loaded(scope, set)
	m := treetable.New(mono, theme.LayoutFor(w, h))
	m.SetOrigin(0, top)
	m.SetSize(tableW, tableH)
	m.SetRows(s.Visible())
	return m
}

func TestHitTesting(t *testing.T) {
	const (
		login  = "worktree:/home/dev/dev/worktrees/feat-login"
		api    = "stack:foundry-feat-login-api"
		e2e    = "stack:foundry-feat-login-e2e"
		apiC   = "container:foundry-feat-login-api/x-api-1"
		hotfix = "worktree:/home/dev/dev/wt/hotfix-1"
		race   = "worktree:/home/dev/dev/worktrees/fix-race"
	)
	wide := tableFor(t, 120, 35, 83, 32, 1, tree.ScopeMachine, treetest.Machine())
	// Screen lines: 0 header, 1 titles, 2 feat-login, 3 api, 4 container api, 5 db, 6 e2e,
	// 7 main, 8 fix-race, 13 blank, 14 cleanup, 15 old-spike, 16 demo, 17 hotfix.
	cases := []struct {
		name string
		x, y int
		zone treetable.Zone
		id   string
	}{
		{"header is not the table", 5, 0, treetable.ZoneNone, ""},
		{"title gap", 0, 1, treetable.ZoneNone, ""},
		{"worktree body", 20, 2, treetable.ZoneBody, login},
		{"worktree far right", 82, 2, treetable.ZoneBody, login},
		{"gutter of an expandable row", 0, 2, treetable.ZoneGutter, login},
		{"second cell of the gutter", 1, 2, treetable.ZoneGutter, login},
		{"chevron under the @ marker", 2, 2, treetable.ZoneChevron, login},
		{"chevron second cell", 3, 2, treetable.ZoneChevron, login},
		{"first cell after the chevron", 4, 2, treetable.ZoneBody, login},
		{"stack chevron", 2, 3, treetable.ZoneChevron, api},
		{"stack gutter", 0, 3, treetable.ZoneGutter, api},
		{"container has no gutter or chevron", 2, 4, treetable.ZoneBody, apiC},
		{"stack with no containers: gutter takes 3 cells", 2, 6, treetable.ZoneGutter, e2e},
		{"stack with no containers: body at 3", 3, 6, treetable.ZoneBody, e2e},
		{"separator", 20, 13, treetable.ZoneNone, ""},
		{"group body", 20, 14, treetable.ZoneBody, tree.GroupCleanup},
		{"group has no gutter", 0, 14, treetable.ZoneBody, tree.GroupCleanup},
		{"cleanup worktree without stacks: gutter", 1, 17, treetable.ZoneGutter, hotfix},
		{"a worktree with no stacks has no chevron: gutter", 2, 8, treetable.ZoneGutter, race},
		{"below the rows", 20, 25, treetable.ZoneNone, ""},
		{"right of the table", 83, 2, treetable.ZoneNone, ""},
		{"below the table", 20, 33, treetable.ZoneNone, ""},
		{"negative", -1, 2, treetable.ZoneNone, ""},
	}
	for _, c := range cases {
		got := wide.HitTest(c.x, c.y)
		if got.Zone != c.zone || got.ID != c.id {
			t.Errorf("%s (%d,%d): got zone %d id %q, want zone %d id %q", c.name, c.x, c.y, got.Zone, got.ID, c.zone, c.id)
		}
	}

	titles := []struct {
		x    int
		want string
	}{{4, "NAME"}, {26, "BRANCH"}, {44, "GIT"}, {52, "STACK"}, {62, "CPU"}, {60, "CPU"}, {68, "MEM"}, {73, "DISK"}, {78, "AGE"}, {2, ""}}
	for _, c := range titles {
		got := wide.HitTest(c.x, 1)
		if got.Column != c.want {
			t.Errorf("title at %d: got %q, want %q", c.x, got.Column, c.want)
		}
		if (c.want != "") != (got.Zone == treetable.ZoneTitle) {
			t.Errorf("title at %d: zone %d", c.x, got.Zone)
		}
	}
	for _, g := range wide.Titles() {
		if g.X1-g.X0 < 5 && g.Title != "" {
			t.Errorf("title %s target is %d cells, ux.md asks for 5", g.Title, g.X1-g.X0)
		}
	}
}

func TestHitTestingNarrowHasTwoLineTargets(t *testing.T) {
	m := tableFor(t, 48, 40, 48, 36, 1, tree.ScopeRepo, treetest.Repo())
	// Narrow has no title line: the rows start under the header, and each has two lines. In the
	// first cells the first line is the chevron, and the second is the mark.
	first := "worktree:/home/dev/dev/worktrees/feat-login"
	for _, c := range []struct {
		x, y int
		zone treetable.Zone
		line int
	}{
		{0, 1, treetable.ZoneChevron, 0}, {1, 1, treetable.ZoneChevron, 0}, {2, 1, treetable.ZoneChevron, 0},
		{3, 1, treetable.ZoneBody, 0}, {40, 1, treetable.ZoneBody, 0},
		{0, 2, treetable.ZoneGutter, 1}, {1, 2, treetable.ZoneGutter, 1}, {2, 2, treetable.ZoneGutter, 1},
		{3, 2, treetable.ZoneBody, 1}, {40, 2, treetable.ZoneBody, 1},
	} {
		got := m.HitTest(c.x, c.y)
		if got.ID != first || got.Zone != c.zone || got.Line != c.line {
			t.Errorf("(%d,%d): %+v", c.x, c.y, got)
		}
	}
	if got := m.HitTest(5, 3); got.ID != "stack:feat-login-api" {
		t.Errorf("second row starts at line 3: %+v", got)
	}
	if geom := m.Geometry(); geom[0].Height != 2 || geom[1].Top != geom[0].Top+2 {
		t.Errorf("geometry: %+v", geom[:2])
	}
}

func TestShortRuleTitlesAreTappableAfterThePrefix(t *testing.T) {
	s := loaded(tree.ScopeRepo, treetest.Repo())
	m := treetable.New(mono, theme.LayoutFor(93, 14))
	m.SetSize(93, 13)
	m.SetTitlePrefix(" wtree status")
	m.SetRows(s.Visible())
	if got := m.HitTest(5, 0); got.Zone != treetable.ZoneNone {
		t.Errorf("the prefix belongs to the model: %+v", got)
	}
	if got := m.HitTest(48, 0); got.Column != "STACK" {
		t.Errorf("STACK: %+v", got)
	}
	got := strings.Join(strings.Fields(strings.SplitN(ansi.Strip(m.Render()), "\n", 2)[0]), " ")
	if got != "wtree status STACK CPU DISK" {
		t.Errorf("title line: %q", got)
	}
	if hidden := m.HiddenColumns(); strings.Join(hidden, ",") != "AGE,MEM" {
		t.Errorf("hidden: %v", hidden)
	}
}

func manyRows(n int) []treetable.Row {
	rows := make([]treetable.Row, 0, n)
	for i := range n {
		rows = append(rows, treetable.Row{
			ID: fmt.Sprintf("worktree:/w/%02d", i), Kind: treetable.KindWorktree, Name: fmt.Sprintf("wt-%02d", i),
			Branch: "b", Count: "1/1", Run: treetable.RunUp, Expandable: true, Markable: true,
			CPU: treetable.Value("1.0%"), Mem: treetable.Value("1M"), Disk: treetable.Value("1G"), Age: treetable.Value("1h"),
			Git: treetable.Git{State: treetable.CellValue},
		})
	}
	return rows
}

func TestCursorAndViewport(t *testing.T) {
	for _, tc := range []struct {
		name string
		w, h int
		line int
	}{{"wide", 120, 12, 1}, {"medium", 80, 12, 1}, {"narrow", 48, 12, 2}} {
		t.Run(tc.name, func(t *testing.T) {
			m := treetable.New(mono, theme.LayoutFor(tc.w, 30))
			m.SetSize(tc.w, tc.h) // 11 body lines
			rows := manyRows(30)
			m.SetRows(rows)
			if m.Cursor() != rows[0].ID {
				t.Fatalf("cursor starts at the first row: %q", m.Cursor())
			}
			check := func(step string) {
				t.Helper()
				if m.Place() != treetable.CursorVisible {
					t.Errorf("%s: cursor not visible", step)
				}
				last := m.Geometry()[len(m.Geometry())-1]
				if last.Top+last.Height > m.BodyHeight()+1 {
					t.Errorf("%s: row cut at the bottom: %+v", step, last)
				}
				if got := strings.Count(m.Render(), "\n") + 1; got != tc.h {
					t.Errorf("%s: %d lines", step, got)
				}
			}
			for range 25 {
				m.MoveCursor(1)
				check("down")
			}
			if m.Cursor() != rows[25].ID {
				t.Errorf("cursor at %q", m.Cursor())
			}
			m.CursorEnd()
			check("end")
			if m.Cursor() != rows[29].ID {
				t.Errorf("end: %q", m.Cursor())
			}
			m.MoveCursor(100)
			check("past the end")
			m.CursorHome()
			check("home")
			if m.Geometry()[0].ID != rows[0].ID {
				t.Errorf("home scrolls to the top")
			}
			m.MovePage(1)
			check("page down")
			if m.Cursor() == rows[0].ID {
				t.Errorf("page down did not move")
			}
			m.MovePage(-1)
			check("page up")
			if m.Cursor() != rows[0].ID {
				t.Errorf("page up: %q", m.Cursor())
			}

			// Wheel: the cursor stays and goes out of view, and any move brings it back.
			m.Scroll(10)
			if m.Cursor() != rows[0].ID {
				t.Errorf("scroll moved the cursor")
			}
			if m.Place() != treetable.CursorAbove {
				t.Errorf("place: %d", m.Place())
			}
			m.MoveCursor(1)
			check("back")
			m.Scroll(1000)
			last := m.Geometry()[len(m.Geometry())-1]
			if last.ID != rows[29].ID {
				t.Errorf("scroll to the end shows %q last", last.ID)
			}
			m.Scroll(-1000)
			if m.Geometry()[0].ID != rows[0].ID {
				t.Errorf("scroll to the top")
			}
			m.CursorEnd()
			m.Scroll(-1000)
			if m.Place() != treetable.CursorBelow {
				t.Errorf("place below: %d", m.Place())
			}
		})
	}
}

func TestWheelStepByTier(t *testing.T) {
	if got := treetable.New(mono, theme.LayoutFor(120, 30)).WheelStep(); got != 3 {
		t.Errorf("wide: %d", got)
	}
	if got := treetable.New(mono, theme.LayoutFor(48, 30)).WheelStep(); got != 1 {
		t.Errorf("narrow: %d", got)
	}
}

func TestScrollbar(t *testing.T) {
	m := treetable.New(mono, theme.LayoutFor(80, 30))
	m.SetSize(80, 11) // 10 body lines
	rows := manyRows(40)
	m.SetRows(rows)
	lines := strings.Split(ansi.Strip(m.Render()), "\n")
	if !strings.HasSuffix(lines[1], "┃") || !strings.HasSuffix(lines[10], "│") {
		t.Errorf("thumb at the top, track at the bottom:\n%s", strings.Join(lines, "\n"))
	}
	if strings.HasSuffix(lines[0], "┃") || strings.HasSuffix(lines[0], "│") {
		t.Errorf("no scrollbar on the title line")
	}
	got := m.HitTest(79, 1)
	if got.Zone != treetable.ZoneScrollbar || got.Fraction != 0 {
		t.Errorf("top of the bar: %+v", got)
	}
	got = m.HitTest(79, 10)
	if got.Zone != treetable.ZoneScrollbar || got.Fraction != 1 {
		t.Errorf("bottom of the bar: %+v", got)
	}
	m.ScrollTo(got.Fraction)
	if g := m.Geometry(); g[len(g)-1].ID != rows[39].ID {
		t.Errorf("scrollbar bottom must show the last row")
	}
	lines = strings.Split(ansi.Strip(m.Render()), "\n")
	if !strings.HasSuffix(lines[10], "┃") || !strings.HasSuffix(lines[1], "│") {
		t.Errorf("thumb at the bottom:\n%s", strings.Join(lines, "\n"))
	}
	if m.HitTest(40, 5).Zone != treetable.ZoneBody {
		t.Errorf("the rest of the row is body")
	}

	// Everything fits: no bar, and the last column is an ordinary part of the row.
	m.SetRows(rows[:5])
	if m.HitTest(79, 1).Zone != treetable.ZoneBody {
		t.Errorf("no scrollbar zone when it all fits: %+v", m.HitTest(79, 1))
	}
	if out := ansi.Strip(m.Render()); strings.Contains(out, "┃") || strings.Contains(out, "│") {
		t.Errorf("no scrollbar when it all fits:\n%s", out)
	}
}

func TestStableCursorAcrossUpdates(t *testing.T) {
	m := treetable.New(mono, theme.LayoutFor(80, 30))
	m.SetSize(80, 8)
	rows := manyRows(20)
	m.SetRows(rows)
	m.SetCursor(rows[10].ID)
	top := m.Geometry()[0].ID

	// New values, same ids: nothing moves.
	next := append([]treetable.Row{}, rows...)
	next[10].CPU = treetable.Value("9.9%")
	m.SetRows(next)
	if m.Cursor() != rows[10].ID || m.Geometry()[0].ID != top {
		t.Errorf("a cell update moved the view")
	}
	if ids := treetable.ChangedIDs(rows, next); len(ids) != 1 || ids[0] != rows[10].ID {
		t.Errorf("changed: %v", ids)
	}

	// A row above goes: the cursor and the top row stay on their ids.
	m.SetRows(append(append([]treetable.Row{}, rows[:2]...), rows[3:]...))
	if m.Cursor() != rows[10].ID || m.Geometry()[0].ID != top {
		t.Errorf("a row removed above moved the view: %q %q", m.Cursor(), m.Geometry()[0].ID)
	}

	// The cursor row goes: the cursor takes the row in its place.
	m.SetRows(rows)
	m.SetCursor(rows[10].ID)
	m.SetRows(append(append([]treetable.Row{}, rows[:10]...), rows[11:]...))
	if m.Cursor() != rows[11].ID {
		t.Errorf("cursor after its row went: %q", m.Cursor())
	}
	m.SetCursor(rows[19].ID)
	m.SetRows(rows[:5])
	if m.Cursor() != rows[4].ID {
		t.Errorf("cursor past the end: %q", m.Cursor())
	}
	m.SetRows(nil)
	if m.Cursor() != "" || m.Place() != treetable.CursorNone {
		t.Errorf("no rows, no cursor")
	}
	m.MoveCursor(1)
	m.MovePage(1)
	m.CursorEnd()
	m.CursorHome()
	m.Scroll(1)
	if got := m.HitTest(10, 2); got.Zone != treetable.ZoneNone {
		t.Errorf("no rows: %+v", got)
	}
}

func TestCursorSkipsWhatTakesNoCursor(t *testing.T) {
	rows := []treetable.Row{
		{ID: "a", Kind: treetable.KindWorktree, Name: "a"},
		{ID: "s", Kind: treetable.KindSeparator},
		{ID: "b", Kind: treetable.KindGroup, Name: "b"},
	}
	m := treetable.New(mono, theme.LayoutFor(80, 30))
	m.SetSize(80, 10)
	m.SetRows(rows)
	m.MoveCursor(1)
	if m.Cursor() != "b" {
		t.Errorf("cursor: %q", m.Cursor())
	}
	m.MoveCursor(-1)
	if m.Cursor() != "a" {
		t.Errorf("cursor: %q", m.Cursor())
	}
	if m.SetCursor("s") || m.SetCursor("nope") {
		t.Errorf("SetCursor must refuse")
	}
	if m.HitTest(10, 2).Zone != treetable.ZoneNone {
		t.Errorf("a separator is not a target")
	}
}

func TestFloor(t *testing.T) {
	s := loaded(tree.ScopeRepo, treetest.Repo())
	m := treetable.New(mono, theme.LayoutFor(39, 24))
	m.SetSize(39, 20)
	m.SetRows(s.Visible())
	lines := strings.Split(ansi.Strip(m.Render()), "\n")
	if len(lines) != 20 || !strings.Contains(lines[0], "wtree needs 40 columns") {
		t.Errorf("floor:\n%s", strings.Join(lines, "\n"))
	}
	if got := m.HitTest(5, 5); got.Zone != treetable.ZoneNone {
		t.Errorf("no targets below the floor: %+v", got)
	}
	for i, line := range lines {
		if treetable.Width(line) != 39 {
			t.Errorf("line %d width %d", i, treetable.Width(line))
		}
	}
	tiny := treetable.New(mono, theme.LayoutFor(10, 3))
	tiny.SetSize(10, 3)
	if got := strings.Split(ansi.Strip(tiny.Render()), "\n"); len(got) != 3 || treetable.Width(got[0]) != 10 {
		t.Errorf("tiny: %q", got)
	}
}

func TestEmptyText(t *testing.T) {
	m := treetable.New(mono, theme.LayoutFor(80, 24))
	m.SetSize(80, 20)
	m.SetEmptyText("loading worktrees…")
	lines := strings.Split(ansi.Strip(m.Render()), "\n")
	if len(lines) != 20 || strings.TrimSpace(lines[1]) != "loading worktrees…" {
		t.Errorf("empty:\n%s", strings.Join(lines, "\n"))
	}
}

// cellsOf turns a line into one string per terminal cell, so a test can look at a
// column whatever wide characters came before it. The second cell of a wide character
// is "".
func cellsOf(line string) []string {
	var out []string
	g := displaywidth.StringGraphemes(line)
	for g.Next() {
		s := g.Value()
		out = append(out, s)
		for i := 1; i < treetable.Width(s); i++ {
			out = append(out, "")
		}
	}
	return out
}

func hostileRows() []treetable.Row {
	long := strings.Repeat("very-long-name-", 8)
	names := []string{
		"plain", long, "日本語のワークツリーの名前がとても長い場合のテスト", "🚀rocket🚀launch🚀party🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀", "👨‍👩‍👧‍👦family👨‍👩‍👧‍👦", "ééé",
		"🇩🇪flag🇩🇪", "a", "",
	}
	var rows []treetable.Row
	for i, n := range names {
		cpu := []treetable.Cell{treetable.Value("12.0%"), treetable.Value("4.1%"), treetable.Value("1632%"), treetable.Loading(), treetable.Failed(), treetable.NA(), treetable.Stale("12.0%"), treetable.Stale("4.1%"), treetable.Value("0.0%")}[i]
		disk := []treetable.Cell{treetable.Value("1.2G"), treetable.Stale("1.2G"), treetable.Value("900B"), treetable.Loading(), treetable.Failed(), treetable.NA(), treetable.Value("12G"), treetable.Value("999M"), treetable.Value("1.0T")}[i]
		rows = append(rows,
			treetable.Row{
				ID: fmt.Sprintf("worktree:%d", i), Kind: treetable.KindWorktree, Name: n, Branch: n, Expandable: true, Markable: true, Marked: i%2 == 0,
				Count: "12/12", Run: treetable.RunUp, CPU: cpu, Mem: treetable.Value("612M"), Disk: disk, Age: treetable.Value("12d"), Tab: "tab 4",
				Git: treetable.Git{State: treetable.CellValue, Ahead: 123, Behind: 456, Changed: 7890, Locked: true},
			},
			treetable.Row{
				ID: fmt.Sprintf("stack:%d", i), Kind: treetable.KindStack, Depth: 1, Name: n, Word: "partial", Count: "10/20", Run: treetable.RunPartial,
				Reason: n, Expandable: true, CPU: cpu, Mem: treetable.Stale("612M"), Disk: disk, Age: treetable.Value("3d"),
			},
			treetable.Row{
				ID: fmt.Sprintf("container:%d", i), Kind: treetable.KindContainer, Depth: 2, Name: n, Word: "restart", Run: treetable.RunPartial, Health: "unhealthy",
				CPU: cpu, Mem: treetable.Value("1M"), Disk: treetable.NA(), Age: treetable.Value("1h"),
				Ports: []string{":8080", ":5555", ":12345", ":23456", ":34567", ":45678"},
			},
		)
	}
	return rows
}

func TestColumnAlignmentHoldsWithAnyData(t *testing.T) {
	sizes := []struct{ w, h, tableW int }{
		{120, 40, 83}, {100, 40, 63}, {100, 40, 100}, {200, 40, 200}, {99, 40, 99}, {80, 24, 80}, {60, 20, 60}, {93, 14, 93}, {120, 14, 120},
	}
	for _, sz := range sizes {
		for _, th := range []theme.Theme{mono, theme.New(theme.Options{Profile: theme.TrueColor}), theme.New(theme.Options{Profile: theme.Mono, ASCII: true})} {
			m := treetable.New(th, theme.LayoutFor(sz.w, sz.h))
			m.SetSize(sz.tableW, 60)
			m.SetTitlePrefix(" x")
			m.SetRows(hostileRows())
			lines := strings.Split(ansi.Strip(m.Render()), "\n")
			for i, line := range lines {
				if got := treetable.Width(line); got != sz.tableW {
					t.Errorf("%dx%d/%d line %d: width %d: %q", sz.w, sz.h, sz.tableW, i, got, line)
				}
			}
			titles := map[string]treetable.ColumnGeom{}
			for _, g := range m.Titles() {
				titles[g.Title] = g
			}
			// The right edge of each number column holds the last cell of the value on
			// every row, and the cell after it is free.
			for _, col := range []string{"CPU", "MEM", "DISK"} {
				g, ok := titles[col]
				if !ok {
					continue
				}
				for i, line := range lines[1:] {
					if strings.TrimSpace(line) == "" {
						continue
					}
					cs := cellsOf(line)
					if len(cs) != sz.tableW {
						t.Fatalf("%dx%d line %d has %d cells", sz.w, sz.tableW, i, len(cs))
					}
					// A cell that does not apply is blank, so a blank edge means a blank cell.
					if strings.TrimSpace(cs[g.X1-1]) == "" && strings.TrimSpace(strings.Join(cs[g.X0:g.X1], "")) != "" {
						t.Errorf("%d/%d %s line %d: nothing at the right edge (%d): %q", sz.w, sz.tableW, col, i, g.X1-1, line)
					}
					if g.X1 >= sz.tableW {
						t.Errorf("%d/%d %s: the last column is not free for the scrollbar", sz.w, sz.tableW, col)
					} else if strings.TrimSpace(cs[g.X1]) != "" {
						t.Errorf("%d/%d %s line %d: column overruns: %q", sz.w, sz.tableW, col, i, line)
					}
				}
			}
		}
	}
}

func TestNarrowWithAnyData(t *testing.T) {
	for _, w := range []int{40, 41, 44, 48, 52, 59} {
		for _, th := range []theme.Theme{mono, theme.New(theme.Options{Profile: theme.TrueColor}), theme.New(theme.Options{Profile: theme.Mono, ASCII: true})} {
			m := treetable.New(th, theme.LayoutFor(w, 80))
			m.SetSize(w, 70)
			m.SetRows(hostileRows())
			for i, line := range strings.Split(ansi.Strip(m.Render()), "\n") {
				if got := treetable.Width(line); got != w {
					t.Errorf("%d line %d: width %d: %q", w, i, got, line)
				}
			}
		}
	}
}

func TestNarrowAt40Columns(t *testing.T) {
	m := tableFor(t, 40, 40, 40, 35, 0, tree.ScopeRepo, treetest.Repo())
	out := ansi.Strip(m.Render())
	for _, want := range []string{"partial 1/2 ◐", "feat-login-api", "1/1 up ●", "exited 0/2 ○", "volumes ▭"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q at 40 columns:\n%s", want, out)
		}
	}
	// A branch that does not fit in what is left goes, and the numbers stay.
	rows := []treetable.Row{{
		ID: "w", Kind: treetable.KindWorktree, Name: "x", Branch: "feature/a-very-long-branch-name-here",
		CPU: treetable.Value("12.0%"), Mem: treetable.Value("1.4G"), Disk: treetable.Value("3.1G"),
		Git: treetable.Git{State: treetable.CellValue, Ahead: 123, Behind: 456, Changed: 789},
	}}
	m.SetRows(rows)
	line2 := strings.Split(ansi.Strip(m.Render()), "\n")[1]
	if strings.Contains(line2, "branch") || !strings.Contains(line2, "12.0%  1.4G  3.1G") || !strings.Contains(line2, "±789") {
		t.Errorf("line 2 at 40: %q", line2)
	}
}

func TestMarkDrawsInTheGutter(t *testing.T) {
	s := loaded(tree.ScopeRepo, treetest.Repo())
	id := "worktree:/home/dev/dev/foundry/main"
	if !s.ToggleMark(id) {
		t.Fatal("mark refused")
	}
	m := treetable.New(mono, theme.LayoutFor(80, 24))
	m.SetSize(80, 20)
	m.SetRows(s.Visible())
	for _, line := range strings.Split(ansi.Strip(m.Render()), "\n") {
		if strings.Contains(line, "⌂ main") && !strings.HasPrefix(line, "■ ⌂ main") {
			t.Errorf("mark must be in cell 0: %q", line)
		}
	}
	// On narrow the mark is on the second line, where a tap toggles it.
	n := treetable.New(mono, theme.LayoutFor(48, 40))
	n.SetSize(48, 36)
	n.SetRows(s.Visible())
	lines := strings.Split(ansi.Strip(n.Render()), "\n")
	for i, line := range lines {
		if strings.Contains(line, "⌂ main") {
			if strings.Contains(line, "■") || !strings.HasPrefix(lines[i+1], " ■ ") {
				t.Errorf("mark on narrow: %q %q", line, lines[i+1])
			}
		}
	}
}

// A row with nothing to show has an empty second line, and no dashes.
func TestNarrowLeavesOutWhatDoesNotApply(t *testing.T) {
	m := treetable.New(mono, theme.LayoutFor(48, 40))
	m.SetSize(48, 36)
	m.SetRows([]treetable.Row{
		{ID: "w", Kind: treetable.KindWorktree, Name: "plain", CPU: treetable.NA(), Mem: treetable.NA(), Disk: treetable.NA(), Git: treetable.Git{State: treetable.CellNA}},
		{ID: "l", Kind: treetable.KindWorktree, Name: "later", CPU: treetable.Loading(), Mem: treetable.Loading(), Disk: treetable.Failed()},
	})
	lines := strings.Split(ansi.Strip(m.Render()), "\n")
	if strings.Contains(lines[0], "-") || strings.TrimSpace(lines[1]) != "" {
		t.Errorf("not applicable draws nothing: %q %q", lines[0], lines[1])
	}
	if !strings.Contains(lines[3], "·  ·  ?") {
		t.Errorf("a value to come and a failed one keep their place: %q", lines[3])
	}
}

// With room to spare the columns stay together: the name does not push the branch away.
func TestNameIsCappedAtWhatItNeeds(t *testing.T) {
	m := treetable.New(mono, theme.LayoutFor(200, 40))
	m.SetSize(200, 30)
	m.SetRows([]treetable.Row{{ID: "w", Kind: treetable.KindWorktree, Name: "short-name", Branch: "feat/x"}})
	for _, c := range m.Titles() {
		if c.Title == "BRANCH" && c.X0 > 4+len("short-name")+4 {
			t.Errorf("BRANCH at %d", c.X0)
		}
	}
}

// A branch is cut at its end, and a detached head keeps its commit.
func TestBranchIsCutOnTheRight(t *testing.T) {
	m := treetable.New(mono, theme.LayoutFor(60, 20))
	m.SetSize(60, 15)
	m.SetRows([]treetable.Row{
		{ID: "w", Kind: treetable.KindWorktree, Name: "name-that-takes-room", Branch: "feature/login-with-a-long-name"},
		{ID: "d", Kind: treetable.KindWorktree, Name: "detached", Branch: "(detached) 9ac1e2"},
	})
	out := ansi.Strip(m.Render())
	if !strings.Contains(out, "feature/") || strings.Contains(out, "…long-name") {
		t.Errorf("cut at the end:\n%s", out)
	}
	if !strings.Contains(out, "9ac1e2") {
		t.Errorf("the commit stays:\n%s", out)
	}
}

func TestFourCellStates(t *testing.T) {
	r := func(id string, c treetable.Cell) treetable.Row {
		return treetable.Row{ID: id, Kind: treetable.KindStack, Name: id, Word: "up", Count: "1/1", Run: treetable.RunUp, CPU: c, Mem: c, Disk: c, Age: treetable.NA()}
	}
	for _, tc := range []struct {
		th   theme.Theme
		want []string
	}{
		{mono, []string{"1.2G", "", "·", "?", "~1.2G"}},
		{theme.New(theme.Options{Profile: theme.Mono, ASCII: true}), []string{"1.2G", "", ".", "?", "~1.2G"}},
	} {
		m := treetable.New(tc.th, theme.LayoutFor(120, 30))
		m.SetSize(120, 8)
		m.SetRows([]treetable.Row{r("a", treetable.Value("1.2G")), r("b", treetable.NA()), r("c", treetable.Loading()), r("d", treetable.Failed()), r("e", treetable.Stale("1.2G"))})
		lines := strings.Split(ansi.Strip(m.Render()), "\n")
		edge := m.Titles()[len(m.Titles())-2].X1 // DISK
		for i, want := range tc.want {
			cs := cellsOf(lines[i+1])
			got := strings.TrimSpace(strings.Join(cs[edge-len(want):edge], ""))
			if got != want {
				t.Errorf("row %d: got %q, want %q: %q", i, got, want, lines[i+1])
			}
		}
		// The digits stay in place: the tilde hangs into the gap.
		a, e := cellsOf(lines[1]), cellsOf(lines[5])
		if strings.Join(a[edge-4:edge], "") != strings.Join(e[edge-4:edge], "") {
			t.Errorf("stale digits moved: %q %q", lines[1], lines[5])
		}
	}
}
