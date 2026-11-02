package treetable_test

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/afruth/wtree/tui/internal/theme"
	"github.com/afruth/wtree/tui/internal/tree"
	"github.com/afruth/wtree/tui/internal/tree/treetest"
	"github.com/afruth/wtree/tui/internal/treetable"
	"github.com/charmbracelet/x/ansi"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func loaded(scope tree.Scope, set treetest.Set) *tree.State {
	s := tree.New(tree.Options{Scope: scope, Home: treetest.Home, Now: func() time.Time { return treetest.Now }})
	for _, ev := range set.Events() {
		s.Apply(ev)
	}
	for _, id := range set.Expanded {
		s.SetExpanded(id, true)
	}
	return s
}

// scene is one mock-up: the table region and the lines around it, which belong to the
// model and which the test takes from the mock-up itself.
type scene struct {
	name         string
	w, h         int
	tableW       int
	top, height  int // the lines of the mock-up that the table draws
	scope        tree.Scope
	set          func() treetest.Set
	compareWords bool // compare words and not cells, see below
	// differs lists the lines of the mock-up that the render does not match, by line
	// number, each with its reason. A line that starts to differ fails the test, and a
	// line in this list that stops differing fails it too.
	differs map[int]string
}

const rollupDiffers = "the mock-up says 2/3, but its own rows add up to 1/3 containers"

var scenes = []scene{
	{
		name: "120x35", w: 120, h: 35, tableW: 83, top: 1, height: 32, scope: tree.ScopeMachine, set: treetest.Machine,
		differs: map[int]string{2: rollupDiffers},
	},
	{
		// Medium in the mock-up has its numbers left-aligned and containers at cell 5.
		// ux.md 6.2 asks for right-aligned numbers, the other three mock-ups have them,
		// and the other three have containers at cell 6. So this compares words.
		name: "80x24", w: 80, h: 24, tableW: 80, top: 1, height: 21, scope: tree.ScopeRepo, set: treetest.Repo, compareWords: true,
		differs: map[int]string{2: rollupDiffers},
	},
	{
		name: "48x40", w: 48, h: 40, tableW: 48, top: 1, height: 36, scope: tree.ScopeRepo, set: treetest.Repo,
		differs: map[int]string{1: rollupDiffers},
	},
	{
		name: "93x14", w: 93, h: 14, tableW: 93, top: 0, height: 13, scope: tree.ScopeRepo, set: treetest.Repo,
		differs: map[int]string{1: rollupDiffers},
	},
}

func readMock(t *testing.T, name string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "mock_"+name+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
}

// compose draws the table into the mock-up, so a golden file is a whole frame.
func compose(t *testing.T, sc scene, mock []string, th theme.Theme) ([]string, treetable.Model) {
	t.Helper()
	s := loaded(sc.scope, sc.set())
	layout := theme.LayoutFor(sc.w, sc.h)
	m := treetable.New(th, layout)
	m.SetOrigin(0, sc.top)
	m.SetSize(sc.tableW, sc.height)
	if layout.Short {
		m.SetTitlePrefix(cells(mock[0], 0, 45))
	}
	m.SetRows(s.Visible())
	// Off the rows, so that the cursor bar does not hide in the text comparison.
	lines := strings.Split(ansi.Strip(m.Render()), "\n")
	if len(lines) != sc.height {
		t.Fatalf("%s: %d lines, want %d", sc.name, len(lines), sc.height)
	}
	frame := append([]string{}, mock...)
	for i, line := range lines {
		if got := treetable.Width(line); got != sc.tableW {
			t.Errorf("%s line %d: width %d, want %d: %q", sc.name, i, got, sc.tableW, line)
		}
		frame[sc.top+i] = line + cells(mock[sc.top+i], sc.tableW, sc.w)
	}
	return frame, m
}

// cells cuts a line of the mock-up by rune, which here are all one cell.
func cells(line string, from, to int) string {
	rs := []rune(line)
	return string(rs[from:to])
}

func TestGoldenFramesAgainstMockups(t *testing.T) {
	th := theme.New(theme.Options{Profile: theme.Mono})
	for _, sc := range scenes {
		t.Run(sc.name, func(t *testing.T) {
			mock := readMock(t, sc.name)
			frame, _ := compose(t, sc, mock, th)
			text := strings.Join(frame, "\n") + "\n"

			path := filepath.Join("testdata", "frame_"+sc.name+".golden")
			if *update {
				if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (run with -update)", err)
			}
			if string(want) != text {
				t.Errorf("frame differs from %s\ngot:\n%s", path, text)
			}

			for i := range frame {
				same := frame[i] == mock[i]
				if sc.compareWords {
					same = strings.Join(strings.Fields(frame[i]), " ") == strings.Join(strings.Fields(mock[i]), " ")
				}
				_, allowed := sc.differs[i]
				switch {
				case !same && !allowed:
					t.Errorf("line %d differs from the mock-up\n got %q\nmock %q", i, frame[i], mock[i])
				case same && allowed:
					t.Errorf("line %d now matches the mock-up: take it off the list", i)
				}
			}
		})
	}
}

func TestStyledRenderKeepsTheCells(t *testing.T) {
	for _, o := range []theme.Options{
		{Profile: theme.TrueColor},
		{Profile: theme.TrueColor, Light: true},
		{Profile: theme.ANSI256},
		{Profile: theme.ANSI16},
		{Profile: theme.Mono},
		{Profile: theme.TrueColor, ASCII: true},
	} {
		for _, sc := range scenes {
			mock := readMock(t, sc.name)
			frame, m := compose(t, sc, mock, theme.New(o))
			_ = frame
			for i, line := range strings.Split(m.Render(), "\n") {
				if got := treetable.Width(line); got != sc.tableW {
					t.Errorf("%+v %s line %d: width %d", o, sc.name, i, got)
				}
			}
		}
	}
}

func TestNoColorHasNoColorCodes(t *testing.T) {
	sc := scenes[0]
	_, m := compose(t, sc, readMock(t, sc.name), theme.New(theme.Options{Profile: theme.Mono}))
	out := m.Render()
	if strings.Contains(out, "38;") || strings.Contains(out, "48;") || strings.Contains(out, "\x1b[3") {
		t.Errorf("colour in a NO_COLOR render: %q", out)
	}
	if !strings.Contains(out, "\x1b[7m") && !strings.Contains(out, ";7m") {
		t.Errorf("the cursor bar must be reverse video: %q", out)
	}
}

func TestAsciiGlyphsOnly(t *testing.T) {
	sc := scenes[0]
	_, m := compose(t, sc, readMock(t, sc.name), theme.New(theme.Options{Profile: theme.Mono, ASCII: true}))
	for _, r := range ansi.Strip(m.Render()) {
		if r > 126 {
			t.Fatalf("not ascii: %q", r)
		}
	}
}
