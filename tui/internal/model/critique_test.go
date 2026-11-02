package model

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/afruth/wtree/tui/internal/core"
	"github.com/afruth/wtree/tui/internal/dialog"
	"github.com/charmbracelet/x/ansi"
)

// `d` on a row that the core says can never go ends in a word and not in a sheet, and it does
// not even ask for a plan. Every other route to it goes through the plan.
func TestDeleteOnANeverRowSaysWhyAndOpensNothing(t *testing.T) {
	h := newHarness(t, 80, 24).start()
	h.cursorTo(wtMain)
	h.key("d")
	h.expect("delete: proj is the main checkout")
	if h.m.CurrentMode() != ModeList || len(h.calls("plan")) != 0 {
		t.Fatalf("mode %d, %d plans", h.m.CurrentMode(), len(h.calls("plan")))
	}
}

// A plan can still say that nothing goes, and no level helps: the title says so.
func TestAPlanWhereNoLevelHelpsSaysCannot(t *testing.T) {
	h := newHarness(t, 80, 24)
	h.fake.On("plan:"+core.ActionDeleteWorktrees, core.FakeScript{Fixture: "plan-nothing-to-do.json"})
	h.start()
	h.cursorTo(wtAlpha)
	h.key("d")
	h.expect("cannot delete")
	h.expectNot("force")
	h.key("y", "enter", "enter")
	if len(h.calls("apply")) != 0 {
		t.Fatal("nothing to do cannot run")
	}
}

func lonelyPlan() []byte {
	return []byte(`{"v":1,"plan_id":"p","action":"delete-stacks","flags":{"force":false,"volumes":false},"cwd":"/x",
"items":[{"id":"stack:proj-vols","kind":"stack","label":"proj-vols","go":true,"offered":false,"only_volumes":true,"consequences":[{"code":"containers_removed","text":"Its containers and networks are deleted.","severe":false}],"steps":["s1"]}],
"steps":[{"id":"s1","label":"delete stack proj-vols","kind":"stack_down","item":"stack:proj-vols","force":false,"volumes":false,"after":[]}],
"choices":[{"id":"stacks","text":"stacks only","default":true,"available":true,"adds":[],"steps":["s1"],"confirm":{"kind":"key","phrases":[]},"consequences":[]}],
"confirm":{"kind":"key","phrases":[]},"notes":[{"code":"volumes_stay","text":"The volumes stay."}],"nothing_to_do":false}`)
}

// A stack of which only volumes are left: at this level the delete changes nothing, so the
// sheet says that, and where the level is that does something. It does not run a step.
func TestKeepVolumesOnAVolumesOnlyStackDoesNothing(t *testing.T) {
	h := newHarness(t, 120, 35)
	h.fake.On("plan:"+core.ActionDeleteStacks, core.FakeScript{Data: lonelyPlan()})
	h.start()
	h.cursorTo(stVols)
	h.key("d")
	h.expect("nothing goes at this level")
	h.expect("only volumes are left")
	h.expect("2  choose with volumes")
	h.expectNot("nothing to do at this level")
	h.expectNot("Its containers")
	h.key("y", "enter", "enter")
	if len(h.calls("apply")) != 0 {
		t.Fatal("a level with nothing to do cannot run")
	}
	if h.m.confirm.Selected().Default != true {
		t.Fatal("the level that loses data is never chosen for the user")
	}
}

// The sheet is the same at each width: one row for each group, and what goes in view.
func TestSheetGroupsAreOneRowAtEveryWidth(t *testing.T) {
	h := newHarness(t, 48, 23).start()
	h.cursorTo(wtAlpha)
	h.key("d")
	h.expect("level (1) delete  (2) force")
	h.expect("GOES")
	h.expect("merged")
	h.expect("STAYS")
	h.expect("delete worktrees · 2 go · 3 stay")
	h.checkFrame()
}

// While the phrase is asked, what is lost stays on the screen, on any screen.
func TestVolumeLinesStayInViewWhileThePhraseIsTyped(t *testing.T) {
	for _, s := range [][2]int{{48, 23}, {93, 14}, {41, 12}, {120, 35}, {80, 24}} {
		h := newHarness(t, s[0], s[1])
		h.fake.On("plan:"+core.ActionDeleteStacks, core.FakeScript{Fixture: "plan-delete-stacks-volumes.json"})
		h.start()
		h.cursorTo(stGone)
		h.key("d", "y")
		h.expect("Type ")
		h.expect("data lost")
		h.expect("GOES")
		h.expect("2 volumes")
		h.expect("pa")
		h.expect("pb")
		h.expect("1 volume")
		h.checkFrame()
		// keys scroll what else there is, and they still type nothing
		h.key("down", "up")
		if h.m.confirm.Input.Value() != "" {
			t.Fatalf("%v: %q", s, h.m.confirm.Input.Value())
		}
	}
}

// A long list of lost volumes cannot hide the field: the rest scrolls, and a line says so.
func TestManyVolumeLinesLeaveRoomForTheField(t *testing.T) {
	th := newHarness(t, 93, 14).m.th
	plan := core.Plan{Action: core.ActionDeleteStacks, Flags: core.Flags{Volumes: true}}
	choice := core.Choice{ID: "c", Default: true, Available: true, Confirm: core.Confirm{Kind: core.ConfirmPhrase, Phrases: []core.Phrase{{Phrase: "9 stacks", Hint: `Type "9 stacks" to delete`}}}}
	one := 1
	for _, name := range []string{"a1", "a2", "a3", "a4", "a5", "a6", "a7", "a8", "a9"} {
		id := "stack:" + name
		plan.Items = append(plan.Items, core.Item{ID: id, Kind: "stack", Label: name, Go: true})
		plan.Steps = append(plan.Steps, core.Step{ID: "s" + name, Item: id})
		choice.Steps = append(choice.Steps, "s"+name)
		choice.Consequences = append(choice.Consequences, core.Consequence{Code: "volumes_removed", Item: id, Text: name, Severe: true, Count: &one})
	}
	plan.Choices = []core.Choice{choice}
	c := dialog.NewConfirm(core.PlanRequest{}, plan, nil, nil)
	c.StartTyping(th)
	f := c.Render(th, newHarness(t, 93, 14).m.layout)
	text := ansi.Strip(strings.Join(f.Lines, "\n"))
	for _, want := range []string{"more", "deletes 9 stacks and 9 volumes", `Type "9 stacks" to delete`, "> "} {
		if !strings.Contains(text, want) {
			t.Errorf("%q is not on the sheet:\n%s", want, text)
		}
	}
	if len(f.Lines) > 14 {
		t.Errorf("%d lines at height 14", len(f.Lines))
	}
}

// Every button is a block, whichever one has the focus, and the focus takes no width.
func TestFocusIsAMarkerAndMovesNothing(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.cursorTo(wtAlpha)
	h.key("d")
	h.expect("▸ backspace cancel")
	_, y := h.findLine("backspace cancel")
	before := strings.Split(h.screen(), "\n")[y]
	h.key("enter")
	h.expect("▸ y  delete")
	h.expectNot("▸ backspace cancel")
	after := strings.Split(h.screen(), "\n")[y]
	if strings.Index(before, "y  delete") != strings.Index(after, "y  delete") {
		t.Fatalf("the buttons moved:\n%q\n%q", before, after)
	}
}

// The new worktree sheet says what backspace and the digits do while the field has the focus.
func TestNewSheetTellsTheTruth(t *testing.T) {
	h := newHarness(t, 48, 23)
	h.start()
	h.m.newSheet, _ = dialog.NewWorktreeSheet(h.m.th, "", "proj", "/fixture/home/code/worktrees/fresh", "fresh", "abc")
	h.m.mode = ModeNew
	h.expect("backspace clears, then cancels")
	h.expect("digits type into the name")
	h.expectNot("only letters")
	h.key("/")
	h.expect("only letters, digits")
	for range "fresh/" {
		h.key("backspace")
	}
	h.expect("backspace cancel")
	h.key("tab")
	h.expect("1 and 2 pick")
}

func TestNameProblemFollowsTheRuleOfTheCore(t *testing.T) {
	for name, bad := range map[string]bool{"": false, "a": false, "a.b-c_1": false, ".a": true, "-a": true, "a/b": true, "a b": true, "é": true} {
		if got := dialog.NameProblem(name) != ""; got != bad {
			t.Errorf("%q: %v", name, got)
		}
	}
}

// At 60 columns the bar still ends with actions: what falls off is in that sheet.
func TestBarAlwaysEndsWithActions(t *testing.T) {
	for _, s := range [][2]int{{60, 20}, {80, 24}, {93, 14}, {100, 30}, {120, 35}} {
		h := newHarness(t, s[0], s[1]).start()
		h.expect("a actions")
	}
	h := newHarness(t, 60, 20).start()
	h.expectNot("space mark")
}

// The header drops whole segments. It never ends in part of a number.
func TestHeaderNeverCutsANumber(t *testing.T) {
	for w := 60; w <= 130; w++ {
		h := newHarness(t, w, 24).start()
		first := strings.TrimRight(strings.Split(h.screen(), "\n")[0], " ")
		for _, cut := range []string{"cleanup ", "cleanup", "worktree", "live", "disk"} {
			if strings.HasSuffix(first, cut) {
				t.Fatalf("%d: %q", w, first)
			}
		}
		if strings.Contains(first, "cleanup") && !strings.Contains(first, "cleanup ~1K") {
			t.Fatalf("%d: %q", w, first)
		}
	}
}

// The first frame has words, and no glyphs that read as noise.
func TestFirstFrameHasWordsNotGlyphs(t *testing.T) {
	for _, s := range [][2]int{{48, 23}, {80, 24}, {120, 35}} {
		h := newHarness(t, s[0], s[1])
		h.fake.On("list", core.FakeScript{Fixture: "list-stream.ndjson", PauseAfter: -1})
		h.start()
		h.expect("loading…")
		h.expectNot("live ·")
		h.expectNot("disk ·")
		h.expectNot("· ·")
		h.fake.Release()
	}
}

// A phone has one header, one list and one bar until there is something to say.
func TestNarrowWastesNoRows(t *testing.T) {
	h := newHarness(t, 48, 23).start()
	lines := strings.Split(h.screen(), "\n")
	if h.m.geometry().statusY != -1 {
		t.Fatalf("a status line with nothing to say:\n%s", h.screen())
	}
	if strings.TrimSpace(lines[1]) == "" {
		t.Fatalf("a blank line under the header:\n%s", h.screen())
	}
	h.cursorTo(wtAlpha)
	h.key("space")
	h.expect("1 marked")
	g := h.m.geometry()
	if g.statusY != g.barY-1 {
		t.Fatalf("the status line sits on the bar: %+v", g)
	}
	h.checkFrame()
}

// A wheel that scrolls the cursor away brings the status line, and that must not scroll back.
func TestWheelOnNarrowLeavesTheCursorBehind(t *testing.T) {
	h := newHarness(t, 48, 18).start()
	h.cursorTo(wtMain)
	for i := 0; i < 8; i++ {
		h.send(tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelDown})
	}
	if strings.Contains(h.screen(), "@ proj") {
		t.Fatalf("the wheel did not leave the cursor behind:\n%s", h.screen())
	}
	h.expect("cursor: proj (above)")
	h.checkFrame()
}

// A row with no stacks has no chevron, and nothing opens under it.
func TestNoPlaceholderChildren(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.cursorTo(wtMain)
	h.key("l", "z")
	h.expectNot("no stacks")
	for _, r := range h.m.table.Rows() {
		if r.ID == wtMain && r.Expandable {
			t.Fatal("no children, no chevron")
		}
	}
}

// The sort is in the header, and only there.
func TestSortIsSaidOnce(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.key("o")
	if n := strings.Count(h.screen(), "sort: disk"); n != 1 {
		t.Fatalf("%d times:\n%s", n, h.screen())
	}
	h.key("o", "o", "o")
	h.expectNot("sort: tree")
	h.expectNot("sort:")
	n := newHarness(t, 48, 23).start()
	n.key("o")
	n.expect("sort: disk")
}

// The cleanup view starts on what it is about, and the never rows do not look like an action.
func TestCleanupStartsOnASafeRowAndDimsDeleteOnANeverRow(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	start := h.m.table.Cursor()
	h.key("c")
	if h.m.table.Cursor() != stGone {
		t.Fatalf("cursor on %s\n%s", h.m.table.Cursor(), h.screen())
	}
	h.expectNot("CPU")
	h.expect("WHY")
	h.cursorTo(wtMain)
	for _, b := range h.m.barButtons() {
		if b.Action == actDelete && (b.Reason == "" || b.Primary) {
			t.Fatalf("delete is lit on the main checkout: %+v", b)
		}
	}
	h.key("c")
	if h.m.table.Cursor() != start {
		t.Fatalf("back on %s, came from %s", h.m.table.Cursor(), start)
	}
}

// Nothing of the panel or of a row stays around a sheet.
func TestNoFragmentsAroundASheet(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.cursorTo(wtAlpha)
	h.key("a")
	frame := h.m.overlay()
	if frame.X+frame.W > h.m.layout.TableWidth(true) {
		t.Fatalf("the sheet crosses the divider: %d+%d", frame.X, frame.W)
	}
	tableW := h.m.layout.TableWidth(true)
	for i, line := range strings.Split(h.screen(), "\n") {
		if i < frame.Y || i >= frame.Y+frame.H {
			continue
		}
		// The panel stays behind the sheet, so that nothing jumps. A row of the list does not.
		left := ansi.Cut(line, 0, frame.X)
		right := ansi.Cut(line, frame.X+frame.W, tableW)
		if strings.TrimSpace(left+right) != "" {
			t.Fatalf("a fragment on line %d: %q", i, line)
		}
		if !strings.HasPrefix(ansi.Cut(line, tableW, tableW+1), "│") {
			t.Fatalf("the divider is gone on line %d: %q", i, line)
		}
	}
}

// Kept is no failure, and the results say what happened in their title.
func TestKeptIsNotDrawnAsFailed(t *testing.T) {
	h := newHarness(t, 120, 35)
	h.fake.On("plan:"+core.ActionDeleteWorktrees, core.FakeScript{Fixture: "plan-delete-worktrees.json"})
	h.start()
	h.cursorTo(wtAlpha)
	h.key("d", "y")
	h.expect("kept: ")
	h.expect("–")
	h.expectNot("▰")
	h.expectNot("done 0.0s")
}
