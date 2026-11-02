package model

import (
	"strings"
	"testing"

	"github.com/afruth/wtree/tui/internal/core"
	"github.com/afruth/wtree/tui/internal/dialog"
	"github.com/afruth/wtree/tui/internal/theme"
	"github.com/charmbracelet/x/ansi"
)

// A phone repeats a held backspace. Held through a sheet it stops at the list, and it never quits.
func TestHeldBackspaceDoesNotFallThroughOrQuit(t *testing.T) {
	h := newHarness(t, 48, 40).start()
	h.m.newSheet, _ = dialog.NewWorktreeSheet(h.m.th, "", "proj", "/fixture/home/code/worktrees/fresh", "ab", "abc")
	h.m.mode = ModeNew
	h.m.tree.ToggleMark(wtAlpha)
	h.key("backspace", "backspace") // the name is gone
	h.key("backspace")              // the sheet is gone
	if h.m.CurrentMode() != ModeList {
		t.Fatalf("mode %d", h.m.CurrentMode())
	}
	h.key("backspace", "backspace", "backspace")
	if h.quit || len(h.m.tree.Marked()) != 1 {
		t.Fatalf("a held key went on to what was under the sheet: quit %v, marks %d", h.quit, len(h.m.tree.Marked()))
	}
	// another key is not the held key, and it works at once
	h.key("j")
	h.letGo()
	h.key("backspace")
	if len(h.m.tree.Marked()) != 0 {
		t.Fatal("after the key is up, backspace is a back again")
	}
	h.letGo()
	h.key("backspace")
	h.expect("q quits")
	if h.quit {
		t.Fatal("backspace does not quit")
	}
	h.key("esc")
	if !h.quit {
		t.Fatal("esc quits")
	}
}

// A held enter or q that closes a sheet does not open a tab or quit under it.
func TestAKeyThatClosesASheetCannotRepeatIntoTheList(t *testing.T) {
	h := newHarness(t, 80, 24).start()
	h.key("?", "q", "q")
	if h.quit {
		t.Fatal("q closed the help, and its repeat must not quit")
	}
	h.letGo()
	h.key("q")
	if !h.quit {
		t.Fatal("a new q quits")
	}
}

// A digit that picked in one sheet can pick in the next one at once: it is a choice, and to drop
// it would run another choice than the one on the screen when y comes.
func TestSameDigitInTheNextSheetIsNotDropped(t *testing.T) {
	h := newHarness(t, 120, 35)
	h.fake.On("plan:"+core.ActionDeleteWorktrees, core.FakeScript{Fixture: "plan-delete-worktrees.json"})
	h.start()
	h.cursorTo(wtAlpha)
	h.key("a", "3")
	if h.m.CurrentMode() != ModeConfirm {
		t.Fatalf("mode %d", h.m.CurrentMode())
	}
	o, ok := h.m.confirm.OptionByDigit("3")
	if !ok || o.IsLevel {
		t.Fatalf("%+v", o)
	}
	h.key("3")
	if h.m.confirm.Choice != o.Index {
		t.Fatalf("choice %d, want %d", h.m.confirm.Choice, o.Index)
	}
}

// volumePlan is a delete with volumes of one stack, with this many volumes.
func volumePlan(count int) core.Plan {
	bytes := int64(500_000_000)
	id := "stack:proj-gamma"
	return core.Plan{
		Action: core.ActionDeleteStacks, Flags: core.Flags{Volumes: true},
		Items: []core.Item{{ID: id, Kind: "stack", Label: "proj-gamma", Go: true, Consequences: []core.Consequence{{Code: "containers_removed", Item: id, Text: "Its containers and networks are deleted."}}}},
		Steps: []core.Step{{ID: "s1", Item: id}},
		Choices: []core.Choice{{ID: "stacks", Default: true, Available: true, Steps: []string{"s1"},
			Consequences: []core.Consequence{{Code: "volumes_removed", Item: id, Text: "volumes", Severe: true, Count: &count, Bytes: &bytes}}}},
	}
}

func sheetText(c dialog.Confirm, th theme.Theme, l theme.Layout) string {
	return ansi.Strip(strings.Join(c.Render(th, l).Lines, "\n"))
}

// Plum means that data goes. With no volumes the plan runs the same, and it says so in warn.
func TestNoVolumesIsNoLoss(t *testing.T) {
	th := theme.New(theme.Options{Profile: theme.TrueColor})
	none := dialog.NewConfirm(core.PlanRequest{}, volumePlan(0), nil, nil)
	if none.Tone() != theme.ToneWarn {
		t.Fatalf("tone %v", none.Tone())
	}
	text := sheetText(none, th, theme.LayoutFor(48, 23))
	for _, want := range []string{"containers, networks (500M)", "deletes 1 stack (500M)", "y delete 1 stack"} {
		if !strings.Contains(text, want) {
			t.Errorf("%q is not on the sheet:\n%s", want, text)
		}
	}
	if strings.Contains(text, "lost") || strings.Contains(text, "0 volumes") {
		t.Errorf("nothing is lost:\n%s", text)
	}
	one := dialog.NewConfirm(core.PlanRequest{}, volumePlan(1), nil, nil)
	if one.Tone() != theme.ToneLoss {
		t.Fatalf("with a volume it is a loss: %v", one.Tone())
	}
	if text := sheetText(one, th, theme.LayoutFor(48, 23)); !strings.Contains(text, "1 stack + 1 volume (500M) lost") && !strings.Contains(text, "· data lost") {
		t.Errorf("the loss is not said:\n%s", text)
	}
	// a count that the plan does not give is never read as no loss
	unknown := volumePlan(1)
	unknown.Choices[0].Consequences[0].Count = nil
	if c := dialog.NewConfirm(core.PlanRequest{}, unknown, nil, nil); c.Tone() != theme.ToneLoss {
		t.Fatalf("an unknown count: %v", c.Tone())
	}
}

// On a phone the summary says the same in fewer words, rather than wrap.
func TestSummaryFitsOnAPhone(t *testing.T) {
	plan := volumePlan(2)
	c := dialog.NewConfirm(core.PlanRequest{}, plan, nil, nil)
	if got := c.LossSummary("·", 80); got != "deletes 1 stack and 2 volumes (500M) · data lost" {
		t.Fatalf("%q", got)
	}
	if got := c.LossSummary("·", 44); got != "1 stack + 2 volumes (500M) lost" {
		t.Fatalf("%q", got)
	}
}

// While a phrase is typed the loss comes first, under its heading, and STAYS comes after it.
func TestPhraseStepHasGoesFirst(t *testing.T) {
	for _, s := range [][2]int{{48, 40}, {48, 23}, {93, 14}, {120, 40}} {
		h := newHarness(t, s[0], s[1])
		h.fake.On("plan:"+core.ActionDeleteStacks, core.FakeScript{Fixture: "plan-delete-stacks-volumes.json"})
		h.start()
		h.cursorTo(stGone)
		h.key("d", "y")
		h.expect("Type ")
		f := h.m.overlay()
		text := ansi.Strip(strings.Join(f.Lines, "\n"))
		goes, loss, stays := strings.Index(text, "GOES"), strings.Index(text, "data lost"), strings.Index(text, "STAYS")
		if goes < 0 || loss < goes || stays < loss {
			t.Fatalf("%v: GOES %d, loss %d, STAYS %d:\n%s", s, goes, loss, stays, text)
		}
		// no blank rows where the groups of digits were
		lines := strings.Split(text, "\n")
		if strings.Contains(lines[1], "level") || !strings.Contains(lines[1], "GOES") {
			t.Fatalf("%v: %q", s, lines[1])
		}
	}
}

// A stack of which only volumes are left: enter moves to the level that does something, and it
// only moves. That level has its own confirm, and the focus is on cancel.
func TestLevelHintMovesToTheLevelAndRunsNothing(t *testing.T) {
	h := newHarness(t, 48, 40)
	h.fake.On("plan:"+core.ActionDeleteStacks, core.FakeScript{Data: lonelyPlan()})
	h.start()
	h.cursorTo(stVols)
	h.key("d")
	h.expect("▸ 2 choose with volumes")
	h.expectNot("▸ backspace cancel")
	if h.m.confirm.Selected() == nil || !h.m.confirm.Selected().Default || h.m.confirm.Req.Volumes {
		t.Fatal("the level that loses data is not chosen for the user")
	}
	h.fake.On("plan:"+core.ActionDeleteStacks, core.FakeScript{Fixture: "plan-delete-stacks-volumes.json"})
	h.key("enter")
	if !h.m.confirm.Req.Volumes {
		t.Fatal("enter moves to the level with volumes")
	}
	if h.m.confirm.Focus != dialog.FocusCancel || len(h.calls("apply")) != 0 {
		t.Fatalf("focus %d, %d applies", h.m.confirm.Focus, len(h.calls("apply")))
	}
	h.expect("y type to delete")
	h.letGo()
	h.key("enter", "enter")
	if len(h.calls("apply")) != 0 {
		t.Fatal("the phrase still applies")
	}
	h.expect("Type ")
}

// The two buttons share a row where they fit, and a tap on each does what it says.
func TestButtonsShareARowOnAPhone(t *testing.T) {
	h := newHarness(t, 48, 23).start()
	h.cursorTo(wtAlpha)
	h.key("d")
	h.expect("backspace cancel")
	_, y := h.findLine("y delete 2 worktrees")
	line := strings.Split(h.screen(), "\n")[y]
	if !strings.Contains(line, "backspace cancel") {
		t.Fatalf("not on one row: %q", line)
	}
	h.tapText("y delete")
	if h.m.confirm.Focus != dialog.FocusConfirm || len(h.calls("apply")) != 0 {
		t.Fatal("the first tap moves the focus, and nothing more")
	}
	h.tapText("backspace cancel")
	if h.m.CurrentMode() != ModeList {
		t.Fatal("cancel")
	}
}

// A disabled force stays on the row of its group on a phone, and a tap gives its reason there.
func TestDisabledForceStaysOnTheLevelRow(t *testing.T) {
	plan := core.Plan{Action: core.ActionDeleteWorktrees,
		Items:   []core.Item{{ID: "worktree:/a", Kind: "worktree", Label: "a", Go: true}},
		Steps:   []core.Step{{ID: "s1", Item: "worktree:/a"}},
		Choices: []core.Choice{{ID: "worktrees", Default: true, Available: true, Steps: []string{"s1"}}}}
	th := theme.New(theme.Options{Profile: theme.TrueColor})
	c := dialog.NewConfirm(core.PlanRequest{}, plan, nil, nil)
	f := c.Render(th, theme.LayoutFor(48, 23))
	text := ansi.Strip(strings.Join(f.Lines, "\n"))
	if !strings.Contains(text, "(1) delete  (–) force") || strings.Contains(text, "nothing here needs") {
		t.Fatalf("%s", text)
	}
	if got := c.DisabledReason("reason:level:1"); got != "nothing here needs force" {
		t.Fatalf("%q", got)
	}
	wide := ansi.Strip(strings.Join(c.Render(th, theme.LayoutFor(120, 40)).Lines, "\n"))
	if !strings.Contains(wide, "(–) force: nothing here needs force") {
		t.Fatalf("%s", wide)
	}
}

// The header has no token that nobody can read.
func TestHeaderHasOnlyWhatItCanSay(t *testing.T) {
	h := newHarness(t, 48, 23).start()
	first := strings.Split(h.screen(), "\n")[0]
	if !strings.Contains(first, "! ~1K") {
		t.Fatalf("%q", first)
	}
	wide := newHarness(t, 60, 23).start()
	if first := strings.Split(wide.screen(), "\n")[0]; !strings.Contains(first, "cleanup ~1K") {
		t.Fatalf("%q", first)
	}
	down := newHarness(t, 48, 23)
	down.fake.On("list", core.FakeScript{Fixture: "list-docker-down.ndjson"})
	down.start()
	if first := strings.Split(down.screen(), "\n")[0]; strings.Contains(first, "?") || strings.Contains(first, "~0B") {
		t.Fatalf("%q", first)
	}
	for _, s := range [][2]int{{48, 23}, {40, 23}, {120, 35}} {
		c := newHarness(t, s[0], s[1]).start()
		c.key("c")
		first := strings.Split(c.screen(), "\n")[0]
		if !strings.Contains(first, "needs a look") || strings.Contains(first, "0B") {
			t.Fatalf("%v: %q", s, first)
		}
		if !strings.Contains(first, "c back") {
			t.Fatalf("%v: %q", s, first)
		}
	}
}

// The age in the cleanup header goes whole where it does not fit.
func TestCleanupHeaderDoesNotCutTheAge(t *testing.T) {
	h := newHarness(t, 60, 20).start()
	h.key("c")
	first := strings.Split(h.screen(), "\n")[0]
	if strings.Contains(first, " 1m") && !strings.Contains(first, "disk 1m") {
		t.Fatalf("%q", first)
	}
}

// Not applicable is blank on every tier. A value to come and a failed one keep their glyph.
func TestNoDashesInTheList(t *testing.T) {
	for _, s := range [][2]int{{120, 35}, {80, 24}, {48, 40}, {93, 14}} {
		h := newHarness(t, s[0], s[1]).start()
		for i, line := range strings.Split(h.screen(), "\n") {
			for _, field := range strings.Fields(line) {
				if field == "-" {
					t.Fatalf("%v line %d: %q", s, i, line)
				}
			}
		}
	}
}

// Each item of the action sheet says something on its second row, and cancel has one row.
func TestActionSheetIsNotHollowOnAPhone(t *testing.T) {
	h := newHarness(t, 48, 40).start()
	h.cursorTo(wtAlpha)
	h.key("a")
	f := h.m.overlay()
	lines := strings.Split(ansi.Strip(strings.Join(f.Lines, "\n")), "\n")
	for i, line := range lines[1 : len(lines)-1] {
		if strings.Trim(line, "│ ") == "" {
			t.Fatalf("an empty row %d:\n%s", i, strings.Join(lines, "\n"))
		}
	}
	h.expect("0 cancel")
	h.expect("~/code/worktrees/alpha")
	h.expect("stacks stay (3.0G)")
	h.expect("0 marked")
}

// On medium and short the sheet fits its content, in the middle, with a sub-text by its label.
func TestActionSheetFitsItsContent(t *testing.T) {
	for _, s := range [][2]int{{93, 14}, {80, 24}, {120, 40}} {
		h := newHarness(t, s[0], s[1]).start()
		h.cursorTo(wtAlpha)
		h.key("a")
		f := h.m.overlay()
		if f.W > 60 {
			t.Fatalf("%v: %d cells", s, f.W)
		}
		mid := (h.m.layout.Width - f.W) / 2
		if h.m.panelOnScreen() {
			mid = (h.m.layout.TableWidth(true) - f.W) / 2
		}
		if f.X != mid {
			t.Fatalf("%v: at %d, not %d", s, f.X, mid)
		}
	}
}

// A row of the cleanup view says why in a few words, and the panel has the sentence.
func TestVolumesOnlyReasonFits(t *testing.T) {
	h := newHarness(t, 120, 40).start()
	h.key("c")
	h.expect("volumes only · origin unknown")
	h.cursorTo(stVols)
	h.expect("cannot tell where they came from")
}

// A bad name has one message, in the field. Enter sends nothing, and it does not add a second.
func TestBadNameHasOneMessageAndEnterSendsNothing(t *testing.T) {
	h := newHarness(t, 48, 40).start()
	h.m.newSheet, _ = dialog.NewWorktreeSheet(h.m.th, "", "proj", "/fixture/home/code/worktrees/fresh", "fresh", "abc")
	h.m.mode = ModeNew
	h.key("/")
	h.expect("name invalid")
	before := len(h.calls("new"))
	h.key("enter")
	if !h.m.newSheet.Flash || len(h.calls("new")) != before || h.m.newSheet.Err != "" {
		t.Fatalf("flash %v, err %q", h.m.newSheet.Flash, h.m.newSheet.Err)
	}
	if n := strings.Count(h.screen(), "only letters"); n != 1 {
		t.Fatalf("%d messages:\n%s", n, h.screen())
	}
	fire[newFlashMsg](h)
	if h.m.newSheet.Flash {
		t.Fatal("the flash ends")
	}
}

// Only what failed is red, and a batch that runs is plain until something fails.
func TestTitlesColourOnlyWhatFailed(t *testing.T) {
	th := theme.New(theme.Options{Profile: theme.TrueColor})
	red := th.Bold(theme.ToneDanger).Render("1 failed")
	p := dialog.Progress{Rows: []dialog.ProgressRow{{StepID: "a", State: dialog.StepDone}, {StepID: "b", State: dialog.StepFailed}}, Finished: true}
	title := p.Render(th, theme.LayoutFor(120, 40)).Lines[0]
	if !strings.Contains(title, red) || !strings.Contains(title, th.Bold(theme.ToneOk).Render("1 done")) {
		t.Fatalf("%q", title)
	}
	run := dialog.Progress{Rows: []dialog.ProgressRow{{StepID: "a", State: dialog.StepDone}, {StepID: "b", State: dialog.StepRunning}}}
	if title := run.Render(th, theme.LayoutFor(120, 40)).Lines[0]; !strings.Contains(title, th.Bold(theme.ToneFg).Render("deleting 2 of 2")) {
		t.Fatalf("%q", title)
	}
	run.Rows[0].State = dialog.StepFailed
	if title := run.Render(th, theme.LayoutFor(120, 40)).Lines[0]; !strings.Contains(title, th.Bold(theme.ToneDanger).Render("deleting 2 of 2")) {
		t.Fatalf("%q", title)
	}
}

// The panel is on by default from 110 columns. A choice is a choice at any width, and it is kept.
func TestPanelDefaultFollowsTheWidthUntilTheUserChooses(t *testing.T) {
	if newHarness(t, 109, 30).start().m.panelOnScreen() {
		t.Fatal("109 columns: it starves the table")
	}
	h := newHarness(t, 110, 30).start()
	if !h.m.panelOnScreen() {
		t.Fatal("110 columns: on")
	}
	if h.m.prefs().Panel != nil {
		t.Fatal("no choice to keep yet")
	}
	n := newHarness(t, 100, 30).start()
	n.key("p")
	if !n.m.panelOnScreen() || n.m.prefs().Panel == nil || !*n.m.prefs().Panel {
		t.Fatal("p opens it at 100 columns, and that is kept")
	}
}

// What the tier hides is in the help, and not on every frame.
func TestHiddenColumnsAreInTheHelpOnly(t *testing.T) {
	h := newHarness(t, 80, 24).start()
	h.expectNot("hidden: ")
	h.key("?")
	h.expect("hides AGE")
}

// A flash is news about a list that holds still: no scope switch, reload or first fill.
func TestNoFlashOnAScopeSwitch(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	if h.m.flashGen != 0 {
		t.Fatalf("the first fill flashed: %d", h.m.flashGen)
	}
	h.key("g")
	if h.m.flashGen != 0 {
		t.Fatalf("the scope switch flashed: %d", h.m.flashGen)
	}
	h.key("g", "r")
	if h.m.flashGen != 0 {
		t.Fatalf("a reload flashed: %d", h.m.flashGen)
	}
}

// The selected "also" is accent on both themes: fg reverse is a black block on a light one.
func TestAlsoSelectionIsAccent(t *testing.T) {
	th := theme.New(theme.Options{Profile: theme.TrueColor, Light: true})
	h := newHarness(t, 80, 24)
	h.fake.On("plan:"+core.ActionDeleteWorktrees, core.FakeScript{Fixture: "plan-delete-worktrees.json"})
	h.start()
	h.cursorTo(wtAlpha)
	h.key("d")
	text := strings.Join(h.m.confirm.Render(th, h.m.layout).Lines, "\n")
	if !strings.Contains(text, th.Button(theme.ToneAccent).Bold(true).Render("(3) worktrees only")) {
		t.Fatalf("%q", text)
	}
}

// A sheet where no level helps has only the way back.
func TestCannotDeleteHasOnlyCancel(t *testing.T) {
	h := newHarness(t, 80, 24)
	h.fake.On("plan:"+core.ActionDeleteWorktrees, core.FakeScript{Fixture: "plan-nothing-to-do.json"})
	h.start()
	h.cursorTo(wtAlpha)
	h.key("d")
	h.expect("▸ backspace cancel")
	h.expectNot("- nothing to do")
}

// The answer to a key is on the sheet, which covers the status line on a phone.
func TestSheetAnswersOnItself(t *testing.T) {
	h := newHarness(t, 48, 40)
	h.fake.On("plan:"+core.ActionDeleteWorktrees, core.FakeScript{Fixture: "plan-nothing-to-do.json"})
	h.start()
	h.cursorTo(wtAlpha)
	h.key("d", "y")
	h.expect("nothing to do")
	f := h.m.overlay()
	if !strings.Contains(ansi.Strip(strings.Join(f.Lines, "\n")), "nothing to do") {
		t.Fatal("not on the sheet")
	}
	h.key("k")
	if h.m.confirm.Said != "" {
		t.Fatal("the next key takes it away")
	}
}
