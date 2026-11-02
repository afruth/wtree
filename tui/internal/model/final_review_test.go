package model

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/afruth/wtree/tui/internal/core"
	"github.com/afruth/wtree/tui/internal/dialog"
	"github.com/afruth/wtree/tui/internal/tree"
	"github.com/afruth/wtree/tui/internal/treetable"
	"github.com/charmbracelet/x/ansi"
)

// fixture reads a recording of the core.
func fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("../core/testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// listWithout is the recorded list, without the lines that hold one of these words.
func listWithout(t *testing.T, words ...string) []byte {
	t.Helper()
	var out []string
	for _, line := range strings.Split(fixture(t, "list-stream.ndjson"), "\n") {
		drop := false
		for _, w := range words {
			drop = drop || strings.Contains(line, w)
		}
		if !drop {
			out = append(out, line)
		}
	}
	return []byte(strings.Join(out, "\n"))
}

// stackPlan is a plan to delete these stacks. A phrase makes it ask for that.
func stackPlan(phrase string, volumes bool, projects ...string) []byte {
	var items, steps, ids []string
	for i, p := range projects {
		id := fmt.Sprintf("s%d", i+1)
		items = append(items, fmt.Sprintf(`{"id":"stack:%s","kind":"stack","label":%q,"go":true,"offered":false,"consequences":[{"code":"containers_removed","text":"Its containers and networks are deleted.","severe":false}],"steps":[%q]}`, p, p, id))
		steps = append(steps, fmt.Sprintf(`{"id":%q,"label":"delete stack %s","kind":"stack_down","item":"stack:%s","force":false,"volumes":%v,"after":[]}`, id, p, p, volumes))
		ids = append(ids, fmt.Sprintf("%q", id))
	}
	confirm := `{"kind":"key","phrases":[]}`
	if phrase != "" {
		quoted, _ := json.Marshal(phrase)
		hint, _ := json.Marshal(`Type "` + phrase + `" to delete`)
		confirm = fmt.Sprintf(`{"kind":"phrase","phrases":[{"phrase":%s,"hint":%s}]}`, quoted, hint)
	}
	return []byte(fmt.Sprintf(`{"v":1,"plan_id":"p","action":"delete-stacks","flags":{"force":false,"volumes":%v},"cwd":"/x","items":[%s],"steps":[%s],"choices":[{"id":"stacks","text":"stacks only","default":true,"available":true,"adds":[],"steps":[%s],"confirm":%s,"consequences":[]}],"confirm":%s,"notes":[],"nothing_to_do":false}`,
		volumes, strings.Join(items, ","), strings.Join(steps, ","), strings.Join(ids, ","), confirm, confirm))
}

// onStack opens the worktree of the stack row, and puts the cursor on that row.
func (h *harness) onStack(id string) {
	h.t.Helper()
	h.m.tree.SetAllExpanded(true)
	h.send(redrawMsg{})
	h.cursorTo(id)
	h.send(redrawMsg{})
}

// ---- a sheet is answered by someone who has read it

// A held y crosses a refusal: the sheet comes back with a new plan, and the repeats do not confirm it.
func TestHeldYDoesNotConfirmTheNewPlanOfARefusal(t *testing.T) {
	h := newHarness(t, 120, 35)
	plan := stackPlan("", false, "proj-alpha")
	h.fake.On("plan:"+core.ActionDeleteStacks, core.FakeScript{Data: plan})
	h.fake.On("apply", core.FakeScript{Data: []byte(`{"v":1,"event":"refused","code":"plan_stale","message":"m","plan":` + string(plan) + "}\n")})
	h.start()
	h.m.tree.ToggleMark(stAlpha)
	h.key("d")
	h.key("y")
	if len(h.calls("apply")) != 1 || h.m.CurrentMode() != ModeConfirm {
		t.Fatalf("%d applies, mode %d", len(h.calls("apply")), h.m.CurrentMode())
	}
	h.expect("Something changed")
	for i := 0; i < 60; i++ {
		h.rapid("y")
		h.pump()
	}
	if n := len(h.calls("apply")); n != 1 {
		t.Fatalf("the repeats of a key confirmed a plan that nobody read: %d applies", n)
	}
	h.expect("read the sheet, then confirm")
	h.letGo()
	h.rapid("y")
	h.pump()
	if n := len(h.calls("apply")); n != 2 {
		t.Fatalf("a y after the key was let go is an answer: %d applies", n)
	}
}

func TestEnterEnterIsNotTheTwoSteps(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.cursorTo(wtAlpha)
	h.key("d")
	h.m.sheetHold = h.m.nowAt // read
	h.rapid("enter", "enter")
	h.pump()
	if len(h.calls("apply")) != 0 {
		t.Fatal("one enter that repeats went through both steps")
	}
	if h.m.confirm.Focus != dialog.FocusConfirm {
		t.Fatal("the first enter moves the focus")
	}
	h.letGo()
	h.rapid("enter")
	h.pump()
	if len(h.calls("apply")) != 1 {
		t.Fatal("a second press confirms")
	}
}

func TestTwoTapsInARowDoNotConfirm(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.cursorTo(wtAlpha)
	h.key("d")
	x, y := h.findLine(" y ")
	tap := func() {
		h.send(tea.MouseClickMsg{X: x + 2, Y: y, Button: tea.MouseLeft})
		h.send(tea.MouseReleaseMsg{X: x + 2, Y: y, Button: tea.MouseLeft})
		h.pump()
	}
	h.letGo()
	tap()
	if h.m.confirm.Focus != dialog.FocusConfirm {
		t.Fatalf("a tap moves the focus\n%s", h.screen())
	}
	tap()
	if len(h.calls("apply")) != 0 {
		t.Fatal("a double tap confirmed")
	}
	h.letGo()
	tap()
	if len(h.calls("apply")) != 1 {
		t.Fatal("a tap after the sheet held still confirms")
	}
}

// A sheet that comes by itself does not take what was typed for something else.
func TestLateSheetDoesNotTakeWhatIsTyped(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.cursorTo(wtAlpha)
	h.rapid("d", "/", "p", "r", "o")
	h.pump()
	h.rapid("y")
	h.pump()
	if h.m.CurrentMode() != ModeSearch || h.m.search.Value() != "proy" {
		t.Fatalf("mode %d, search %q\n%s", h.m.CurrentMode(), h.m.search.Value(), h.screen())
	}
	if len(h.calls("apply")) != 0 {
		t.Fatal("a typed letter ran a delete")
	}
}

func TestBackingOutCancelsThePlan(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.key("A")
	h.rapid("d", "backspace")
	h.pump()
	if h.m.CurrentMode() != ModeList {
		t.Fatalf("a sheet opened after backspace\n%s", h.screen())
	}
	h.expect("cancelled")
	if len(h.m.tree.Marked()) == 0 {
		t.Fatal("the backspace that cancels does only that")
	}
	h.letGo()
	h.rapid("d", "u", "/", "y", "y")
	h.pump()
	if h.m.CurrentMode() != ModeSearch || len(h.calls("apply")) != 0 {
		t.Fatalf("mode %d, %d applies", h.m.CurrentMode(), len(h.calls("apply")))
	}
}

func TestATapCancelsThePlan(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.cursorTo(wtAlpha)
	h.rapid("d")
	x, y := h.findLine(" beta")
	h.tap(x+3, y)
	if h.m.CurrentMode() != ModeList {
		t.Fatal("a sheet came after the user went on")
	}
}

func TestAPlanThatArrivesAloneIsHeldForAMoment(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.cursorTo(wtAlpha)
	h.rapid("d")
	h.pump()
	h.rapid("y")
	h.pump()
	if len(h.calls("apply")) != 0 {
		t.Fatal("a key on its way answered a sheet that had just come")
	}
}

func TestAPendingPlanSaysSo(t *testing.T) {
	for _, s := range [][2]int{{120, 35}, {48, 40}, {93, 14}} {
		h := newHarness(t, s[0], s[1]).start()
		h.cursorTo(wtAlpha)
		h.rapid("d")
		h.expect("backspace cancels")
		h.rapid("d", "d", "s", "n")
		h.pump()
		if n := len(h.calls("plan")); n != 1 {
			t.Fatalf("%d plans", n)
		}
		if h.m.CurrentMode() != ModeConfirm {
			t.Fatal("the repeats of d keep the plan")
		}
	}
}

func TestPhraseWaitsForTheNewPlan(t *testing.T) {
	h := newHarness(t, 120, 35)
	h.fake.On("plan:"+core.ActionDeleteStacks, core.FakeScript{Data: stackPlan("2 stacks", true, "proj-alpha", "proj-old")})
	h.start()
	h.m.tree.ToggleMark(stAlpha)
	h.m.tree.ToggleMark(stOld)
	h.key("/")
	h.typeText("proj-old")
	h.key("enter")
	h.key("d", "y")
	h.typeText("2 stacks")
	h.expect("HIDDEN BY FILTER")
	x, y := h.findLine("HIDDEN BY FILTER")
	// the plan of the unmark is on its way when enter lands
	h.send(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	h.send(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
	h.rapid("enter")
	if len(h.calls("apply")) != 0 {
		t.Fatal("enter applied the old plan with its old targets")
	}
	h.expect("wtree is reading the new plan")
	h.pump()
	if h.m.tree.IsMarked(stAlpha) {
		t.Fatal("the mark goes")
	}
	if !hasArgs(h.calls("plan")[1].Args, "--", "proj-old") {
		t.Fatalf("%v", h.calls("plan")[1].Args)
	}
}

// A plan on marks that fails to come must not leave the old plan to confirm.
func TestFailedPlanOfAnUnmarkClosesTheSheet(t *testing.T) {
	h := newHarness(t, 120, 35)
	h.fake.On("plan:"+core.ActionDeleteStacks, core.FakeScript{Data: stackPlan("", false, "proj-alpha", "proj-old")})
	h.start()
	h.m.tree.ToggleMark(stAlpha)
	h.m.tree.ToggleMark(stOld)
	h.key("/")
	h.typeText("proj-old")
	h.key("enter", "d")
	h.fake.On("plan:"+core.ActionDeleteStacks, core.FakeScript{Data: []byte("not json")})
	h.key("h")
	if h.m.CurrentMode() != ModeList {
		t.Fatalf("the old plan stayed on the screen\n%s", h.screen())
	}
}

func TestResizeTakesFocusOffConfirmAndHoldsTaps(t *testing.T) {
	h := newHarness(t, 40, 44).start()
	h.cursorTo(wtAlpha)
	h.key("d", "tab")
	if h.m.confirm.Focus != dialog.FocusConfirm {
		t.Fatal("tab moves the focus")
	}
	cx, cy := h.findLine("backspace cancel")
	h.send(tea.WindowSizeMsg{Width: 40, Height: 42})
	if h.m.confirm.Focus != dialog.FocusCancel {
		t.Fatal("the focus stayed on confirm through a resize")
	}
	h.tap(cx+3, cy)
	if len(h.calls("apply")) != 0 || h.m.CurrentMode() != ModeConfirm {
		t.Fatalf("a tap landed while the layout moved: mode %d", h.m.CurrentMode())
	}
	fire[settleMsg](h)
	x, y := h.findLine(" y ")
	h.tap(x+2, y)
	if len(h.calls("apply")) != 0 {
		t.Fatal("after a resize one tap is the first of two")
	}
}

func TestResizeForgetsAPress(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.cursorTo(wtAlpha)
	x, y := h.findLine(" alpha")
	h.send(tea.MouseClickMsg{X: x + 3, Y: y, Button: tea.MouseLeft})
	h.send(tea.WindowSizeMsg{Width: 120, Height: 33})
	fire[settleMsg](h)
	h.send(tea.MouseReleaseMsg{X: x + 3, Y: y, Button: tea.MouseLeft})
	if h.m.CurrentMode() != ModeList {
		t.Fatal("a press from before the resize counted")
	}
}

// ---- what the sheet says

func TestStopOfMarksAlwaysHasASheet(t *testing.T) {
	h := newHarness(t, 120, 35)
	h.fake.On("plan:"+core.ActionStopStacks, core.FakeScript{Data: mustPlanNone(t)})
	h.start()
	h.m.tree.ToggleMark(wtAlpha)
	h.cursorTo(wtBeta)
	h.key("/")
	h.typeText("beta")
	h.key("enter")
	h.expect("hidden by filter")
	h.key("s")
	if h.m.CurrentMode() != ModeConfirm {
		t.Fatalf("a stop of a mark ran with no sheet\n%s", h.screen())
	}
	if len(h.calls("apply")) != 0 {
		t.Fatal("it ran")
	}
	h.expect("HIDDEN BY FILTER")
	h.expect("The row under the cursor is not marked")
	// h takes it out: no target is left, and nothing runs
	h.key("h")
	if h.m.tree.IsMarked(wtAlpha) {
		t.Fatal("the mark on the worktree row stands for its stack, and it goes")
	}
}

func TestStopOfAMarkAfterAFailureHasASheet(t *testing.T) {
	h := newHarness(t, 120, 35)
	h.fake.On("plan:"+core.ActionStopStacks, core.FakeScript{Data: mustPlanNone(t)})
	h.start()
	h.m.tree.ToggleMark(stAlpha)
	h.cursorTo(wtBeta)
	h.key("s")
	if h.m.CurrentMode() != ModeConfirm || len(h.calls("apply")) != 0 {
		t.Fatalf("mode %d, %d applies", h.m.CurrentMode(), len(h.calls("apply")))
	}
}

func TestTheSheetFollowsThePlanAndNotTheList(t *testing.T) {
	// the list says that only volumes are left, and the plan knows better now
	h := newHarness(t, 120, 35)
	h.fake.On("plan:"+core.ActionDeleteStacks, core.FakeScript{Data: stackPlan("", false, "proj-vols")})
	h.start()
	h.cursorTo(stVols)
	h.key("d")
	h.expect("y  delete 1 stack")
	goes, _, _ := h.m.confirm.Sections()
	if len(goes) != 1 || goes[0].ID != stVols {
		t.Fatalf("what apply deletes is under GOES: %+v", goes)
	}
	if can, _ := h.m.confirm.CanConfirm(); !can {
		t.Fatal("this delete does something")
	}

	// the list says that it has containers, and the plan says that they went
	h = newHarness(t, 120, 35)
	plan := strings.Replace(string(stackPlan("", false, "proj-alpha")), `"offered":false,`, `"offered":false,"only_volumes":true,`, 1)
	h.fake.On("plan:"+core.ActionDeleteStacks, core.FakeScript{Data: []byte(plan)})
	h.start()
	h.m.tree.ToggleMark(stAlpha)
	h.key("d")
	h.expect("nothing goes at this level")
}

func TestMachineScopeNamesTheRepoOnTheSheet(t *testing.T) {
	h := newHarness(t, 120, 35, withGlobal).start()
	h.cursorTo(wtAlpha)
	h.key("d")
	h.expect("proj/alpha")
	// and in repo scope the sheet says what the list says
	h = newHarness(t, 120, 35).start()
	h.cursorTo(wtAlpha)
	h.key("d")
	h.expectNot("proj/alpha")
}

func TestLongPhraseIsWholeOnTheSheet(t *testing.T) {
	name := strings.Repeat("abcdefghij", 10) + "tail1"
	h := newHarness(t, 48, 40)
	h.fake.On("plan:"+core.ActionDeleteStacks, core.FakeScript{Data: stackPlan(name, true, "proj-alpha")})
	h.start()
	h.m.tree.ToggleMark(stAlpha)
	h.key("d", "y")
	var joined strings.Builder
	on := false
	for _, line := range strings.Split(h.screen(), "\n") {
		line = strings.Trim(line, " │")
		switch {
		case strings.HasPrefix(line, "Type "):
			on = true
		case strings.HasPrefix(line, ">"):
			on = false
		case on:
			joined.WriteString(line)
		}
	}
	if joined.String() != name {
		t.Fatalf("the phrase on the sheet is %q\n%s", joined.String(), h.screen())
	}
	if strings.Count(h.screen(), "tail1") != 1 {
		t.Fatalf("a long phrase is said once\n%s", h.screen())
	}
	h.typeText(strings.ToUpper(name) + ".")
	h.key("enter")
	calls := h.calls("apply")
	if len(calls) != 1 || !hasArgs(calls[0].Args, "--phrase", name) {
		t.Fatalf("%v", calls)
	}
}

// ---- two writers, and the way out

func pausedOpen(h *harness) {
	h.fake.On("open", core.FakeScript{Fixture: "open.ndjson", PauseAfter: -1})
}

func TestAnApplyWaitsForAnOpenThatRuns(t *testing.T) {
	h := newHarness(t, 120, 35)
	pausedOpen(h)
	h.start()
	h.cursorTo(wtBeta)
	h.key("enter")
	h.expect("opening beta")
	h.cursorTo(wtAlpha)
	h.key("d", "y")
	if len(h.calls("apply")) != 0 || h.m.CurrentMode() != ModeConfirm {
		t.Fatalf("an apply started on an open: %d, mode %d", len(h.calls("apply")), h.m.CurrentMode())
	}
	h.expect("wtree is busy")
	h.fake.Release()
	h.pump()
	if h.m.quitting || h.m.CurrentMode() != ModeConfirm {
		t.Fatal("the end of the open dropped the sheet")
	}
	h.key("y")
	if len(h.calls("apply")) != 1 {
		t.Fatal("after the open it runs")
	}
}

func TestAStopByKeyWaitsForAnOpenThatRuns(t *testing.T) {
	h := newHarness(t, 120, 35)
	pausedOpen(h)
	h.fake.On("plan:"+core.ActionStopStacks, core.FakeScript{Data: mustPlanNone(t)})
	h.start()
	h.cursorTo(wtBeta)
	h.key("enter")
	h.cursorTo(wtAlpha)
	h.key("s")
	if len(h.calls("apply")) != 0 {
		t.Fatal("a stop started on an open")
	}
	h.expect("wtree is busy")
	h.fake.Release()
	h.pump()
}

func TestQuitWorksWhileAnOpenRuns(t *testing.T) {
	for _, k := range []string{"q", "ctrl+c"} {
		h := newHarness(t, 120, 35)
		pausedOpen(h)
		h.start()
		h.cursorTo(wtBeta)
		h.key("enter")
		h.key(k)
		if !h.m.quitting {
			t.Fatalf("%s did nothing while an open ran", k)
		}
		h.fake.Release()
		h.pump()
	}
}

func TestQuitDuringABatchStillAsks(t *testing.T) {
	h := newHarness(t, 120, 35)
	h.fake.On("apply", core.FakeScript{Fixture: "apply-ok.ndjson", PauseAfter: -1})
	h.start()
	h.cursorTo(wtAlpha)
	h.key("d", "y", "q")
	if h.m.quitting || h.m.quitMenu == nil {
		t.Fatal("a batch is never left without a word")
	}
	h.fake.Release()
	h.pump()
}

// ---- taps

func TestATapOnStopHasASheet(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.fake.On("plan:"+core.ActionStopStacks, core.FakeScript{Data: mustPlanNone(t)})
	h.start()
	h.onStack(stAlpha)
	x, y := h.findLine(" s stop")
	h.tap(x+2, y)
	if h.m.CurrentMode() != ModeConfirm || len(h.calls("apply")) != 0 {
		t.Fatalf("a tap stopped a stack: mode %d, %d applies", h.m.CurrentMode(), len(h.calls("apply")))
	}
}

func TestATapOnStopInTheActionSheetHasASheet(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.fake.On("plan:"+core.ActionStopStacks, core.FakeScript{Data: mustPlanNone(t)})
	h.start()
	h.onStack(stAlpha)
	h.key("a")
	x, y := h.findLine("1 stop")
	h.tap(x+2, y)
	if h.m.CurrentMode() != ModeConfirm || len(h.calls("apply")) != 0 {
		t.Fatalf("mode %d, %d applies", h.m.CurrentMode(), len(h.calls("apply")))
	}
}

func TestSpaceInTheActionSheetDoesNothing(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.fake.On("plan:"+core.ActionStopStacks, core.FakeScript{Data: mustPlanNone(t)})
	h.start()
	h.onStack(stAlpha)
	h.key("a", "space")
	if h.m.CurrentMode() != ModeMenu || len(h.calls("plan")) != 0 || h.m.tree.IsMarked(stAlpha) {
		t.Fatalf("mode %d, %d plans", h.m.CurrentMode(), len(h.calls("plan")))
	}
}

// scrollAway leaves the cursor above the view of a list that has more rows than the screen.
func scrollAway(t *testing.T, h *harness) {
	t.Helper()
	h.cursorTo(wtMain)
	h.m.tree.SetAllExpanded(true)
	h.send(redrawMsg{})
	for i := 0; i < 40 && h.m.table.Place() == treetable.CursorVisible; i++ {
		h.send(tea.MouseWheelMsg{X: 5, Y: 3, Button: tea.MouseWheelDown})
	}
	if h.m.table.Place() == treetable.CursorVisible {
		t.Fatalf("the list does not scroll here\n%s", h.screen())
	}
}

func TestABarTapBringsTheCursorBack(t *testing.T) {
	h := newHarness(t, 93, 8).start()
	scrollAway(t, h)
	x, y := h.findLine(" a actions")
	h.tap(x+2, y)
	if h.m.table.Place() != treetable.CursorVisible {
		t.Fatal("a tap on the bar acted on a row off the screen")
	}
}

func TestTheShortFooterSaysWhereTheCursorIs(t *testing.T) {
	h := newHarness(t, 120, 8).start()
	scrollAway(t, h)
	h.expect("(above)")
}

func TestAGutterTapKeepsTheView(t *testing.T) {
	h := newHarness(t, 93, 9).start()
	scrollAway(t, h)
	var row treetable.RowGeom
	for _, g := range h.m.table.Geometry() {
		if g.ID != "" && strings.HasPrefix(g.ID, "stack:") {
			row = g
			break
		}
	}
	if row.ID == "" {
		t.Fatalf("no stack row on the screen\n%s", h.screen())
	}
	before := h.screen()
	h.tap(0, row.Top)
	if !h.m.tree.IsMarked(row.ID) {
		t.Fatalf("the tap did not mark %s", row.ID)
	}
	if h.m.table.Place() == treetable.CursorVisible {
		t.Fatalf("the view jumped back to the cursor\nbefore:\n%s\nafter:\n%s", before, h.screen())
	}
}

func TestAPanelTapOpensWhatTheRowIsUnder(t *testing.T) {
	h := newHarness(t, 120, 30).start()
	h.cursorTo(wtBeta)
	h.send(redrawMsg{})
	x, y := h.findLine("proj-old")
	h.tap(x+1, y)
	if h.m.table.Cursor() != stOld {
		t.Fatalf("cursor %s\n%s", h.m.table.Cursor(), h.screen())
	}
	x, y = h.findLine("web")
	if x < h.m.layout.TableWidth(true) {
		t.Fatalf("the panel line of the container is not where it should be\n%s", h.screen())
	}
	h.tap(x+1, y)
	if !strings.HasPrefix(h.m.table.Cursor(), "container:") {
		t.Fatalf("cursor %s\n%s", h.m.table.Cursor(), h.screen())
	}
}

// ---- after a batch

func stopStream(failed string) string {
	return `{"v":1,"event":"start","plan_id":"0000000000000001","steps":2}
{"v":1,"event":"step","id":"s1","state":"running","label":"stop stack proj-alpha"}
{"v":1,"event":"step","id":"s1","state":"ok","ms":7}
{"v":1,"event":"step","id":"s2","state":"running","label":"stop stack proj-old"}
{"v":1,"event":"step","id":"s2","state":"failed","ms":7,"status":1,"tail":["` + failed + `"]}
{"v":1,"event":"end","ok":1,"failed":1,"skipped":0,"disk_refresh":false,"cancelled":false}
`
}

func TestAFailedStopRetriesWithS(t *testing.T) {
	h := newHarness(t, 120, 35)
	h.fake.On("apply", core.FakeScript{Data: []byte(stopStream("no"))})
	h.start()
	h.m.tree.ToggleMark(stAlpha)
	h.m.tree.ToggleMark(stOld)
	h.key("s")
	h.key("y")
	h.expect("what did not finish stays marked: s retries")
	h.expectNot("d retries")
	h.key("enter")
	h.expect("s retries")
	h.expectNot("d retries")
	if !h.m.tree.IsMarked(stOld) || h.m.tree.IsMarked(stAlpha) {
		t.Fatalf("%v", h.m.tree.Marked())
	}
	// the sentence goes with the marks
	h.key("u")
	h.expectNot("still marked")
}

func TestStopFromMarkedWorktreesKeepsTheMarksTrue(t *testing.T) {
	h := newHarness(t, 120, 35)
	// each stack of these two rows is in the plan: the first stops, and the second does not
	h.fake.On("apply", core.FakeScript{Data: []byte(stopStream("no"))})
	h.start()
	h.m.tree.ToggleMark(wtAlpha)
	h.m.tree.ToggleMark(wtBeta)
	plan := mustPlan(t)
	h.m.progress = dialog.NewProgress(*plan, &plan.Choices[0], "x")
	for _, line := range strings.Split(strings.TrimSpace(stopStream("no")), "\n") {
		ev, err := core.DecodeEvent([]byte(line))
		if err != nil {
			t.Fatal(err)
		}
		h.m.progress.Apply(ev)
	}
	h.m.progress.Finished = true
	h.m.onApplyFinished()
	if h.m.tree.IsMarked(wtAlpha) {
		t.Fatal("its stack stopped, so the mark goes")
	}
	if !h.m.tree.IsMarked(wtBeta) {
		t.Fatal("its stack did not stop, so the mark stays")
	}
	if h.m.tree.IsMarked(stOld) {
		t.Fatal("the mark of its worktree stands for it: no new mark, and no count that grows")
	}
}

func mustPlan(t *testing.T) *core.Plan {
	t.Helper()
	var plan core.Plan
	if err := json.Unmarshal([]byte(fixture(t, "plan-stop-stacks.json")), &plan); err != nil {
		t.Fatal(err)
	}
	return &plan
}

// ---- prefs

func TestFlagsOfOneRunStayOutOfThePrefs(t *testing.T) {
	dir := t.TempDir()
	h := newHarness(t, 120, 35, withGlobal, func(o *Options) { o.StateDir = dir; o.Stacks = true }).start()
	if mode, _ := h.m.tree.Sort(); mode != tree.SortName {
		t.Fatal("-d starts on the stacks")
	}
	h.key("p")
	h.pump()
	p := LoadPrefs(dir)
	if p.Scope != "" || p.Sort != "" || p.Panel == nil {
		t.Fatalf("%+v", p)
	}
	h.m.shutdown(os.Stderr)
	if p := LoadPrefs(dir); p.Scope != "" || p.Sort != "" {
		t.Fatalf("on the way out: %+v", p)
	}
}

func TestAChoiceOfTheUserIsKept(t *testing.T) {
	dir := t.TempDir()
	h := newHarness(t, 120, 35, func(o *Options) { o.StateDir = dir }).start()
	h.key("g")
	h.key("o")
	h.pump()
	if p := LoadPrefs(dir); p.Scope != "machine" || p.Sort == "" {
		t.Fatalf("%+v", p)
	}
}

func TestTheFallbackOutsideARepoStaysOutOfThePrefs(t *testing.T) {
	dir := t.TempDir()
	h := newHarness(t, 120, 35, func(o *Options) { o.StateDir = dir })
	h.fake.On("list", core.FakeScript{Fixture: "error-not-a-repo.ndjson"})
	h.start()
	if !h.m.machine() {
		t.Fatal("outside a repo the scope is the machine")
	}
	h.key("p")
	h.pump()
	if p := LoadPrefs(dir); p.Scope != "" {
		t.Fatalf("%+v", p)
	}
}

// ---- empty states and banners

func TestAFlatListWithNoStacksSaysSo(t *testing.T) {
	for _, s := range [][2]int{{48, 24}, {120, 24}} {
		h := newHarness(t, s[0], s[1], func(o *Options) { o.Prefs.Sort = "disk" })
		h.fake.On("list", core.FakeScript{Data: listWithout(t, `"event":"stack`, `"stack":`, `"kind":"stack"`)})
		h.start()
		h.expect("no stacks here")
		h.expect("o shows the tree")
		h.key("d")
		h.expect("no stacks here")
		if len(h.calls("plan")) != 0 {
			t.Fatal("no row, no plan")
		}
	}
}

func warningList(t *testing.T, code, message string) []byte {
	t.Helper()
	line := fmt.Sprintf(`{"v":1,"event":"warning","code":%q,"message":%q}`, code, message)
	return []byte(line + "\n" + fixture(t, "list-stream.ndjson"))
}

func TestDockerMissingHasABanner(t *testing.T) {
	for _, s := range [][2]int{{48, 24}, {120, 30}, {93, 14}} {
		h := newHarness(t, s[0], s[1])
		h.fake.On("list", core.FakeScript{Data: warningList(t, core.WarnDockerMissing, "Docker is not on PATH.")})
		h.start()
		h.expect("no docker on PATH")
		h.checkFrame()
	}
}

func TestDockerTimeoutHasNoLiveAgeAndAWholeBanner(t *testing.T) {
	const text = "Docker did not answer within 5 seconds. Reload the list to try again."
	h := newHarness(t, 48, 24)
	h.fake.On("list", core.FakeScript{Data: warningList(t, core.WarnDockerTimeout, text)})
	h.start()
	h.expectNot("live ")
	h.expect("Docker did not answer · r retry")
	h.expectNot("…")
	wide := newHarness(t, 120, 30)
	wide.fake.On("list", core.FakeScript{Data: warningList(t, core.WarnDockerTimeout, text)})
	wide.start()
	wide.expect(text)
	wide.expectNot("live ")
}

func TestOneStackIsNotStacks(t *testing.T) {
	h := newHarness(t, 120, 30)
	h.fake.On("list", core.FakeScript{Data: listWithout(t, `"project":"proj-old"`, `"project":"proj-gone"`, `"project":"proj-vols"`)})
	h.start()
	h.expect("1 stack ")
	h.expectNot("1 stacks")
}

// ---- cleanup

func coldList(t *testing.T) []byte {
	t.Helper()
	return []byte(strings.Replace(fixture(t, "list-first-scan.ndjson"),
		`"age_s":null,"refreshing":false}`, `"age_s":null,"refreshing":true}`, -1))
}

func TestColdCleanupIsNotNothingAndComesRight(t *testing.T) {
	h := newHarness(t, 120, 35)
	h.fake.On("list", core.FakeScript{Data: coldList(t)})
	h.fake.On("list:only", core.FakeScript{Data: []byte(`{"v":1,"event":"stage","stage":"disk"}
{"v":1,"event":"stack_disk","project":"proj-alpha","bytes":3000000000,"age_s":1,"refreshing":false}
{"v":1,"event":"disk_totals","images":1,"containers":1,"volumes":1,"build":1,"age_s":1,"refreshing":false}
{"v":1,"event":"stage","stage":"disk","done":true,"ms":7}
{"v":1,"event":"end","ms":7}
`)})
	h.start()
	h.key("c")
	h.expectNot("nothing to clean ·")
	// the scan ends, and the next tick reads the totals again
	if fire[statsTickMsg](h) == 0 {
		t.Fatal("a tick is armed")
	}
	if !h.m.cleanStale {
		t.Fatal("the sizes came after the totals")
	}
	h.fake.On("list:only", core.FakeScript{Data: []byte(`{"v":1,"event":"stage","stage":"cleanup"}
{"v":1,"event":"cleanup","id":"worktree:/fixture/home/code/worktrees/alpha","kind":"worktree","label":"alpha","tier":"needs_look","reasons":[{"code":"merged_running","text":"merged into main, and a stack still runs"}],"bytes":3000000000,"stacks":["proj-alpha"]}
{"v":1,"event":"cleanup_totals","safe":0,"needs_look":3000000000,"never":0,"unknown":false}
{"v":1,"event":"stage","stage":"cleanup","done":true,"ms":7}
{"v":1,"event":"end","ms":7}
`)})
	fire[statsTickMsg](h)
	calls := h.calls("list:only")
	if !hasArgs(calls[len(calls)-1].Args, "--only", "git,stats,disk,cleanup") {
		t.Fatalf("%v", calls[len(calls)-1].Args)
	}
	h.expect("needs a look 3.0G")
	if h.m.cleanStale {
		t.Fatal("once is enough")
	}
}

func TestReallyEmptyCleanupSaysNothing(t *testing.T) {
	h := newHarness(t, 120, 35)
	h.fake.On("list", core.FakeScript{Data: listWithout(t, `"event":"cleanup","id":"worktree:/fixture/home/code/worktrees`, `"event":"cleanup","id":"stack`, `"event":"cleanup_totals"`)})
	h.start()
	h.m.tree.Apply(core.CleanupTotals{})
	h.key("c")
	h.expect("nothing to clean · 0 B reclaimable")
}

func TestBackspaceInCleanupClearsTheFilterFirst(t *testing.T) {
	h := newHarness(t, 48, 40).start()
	h.key("c", "/")
	h.typeText("zzz")
	h.key("enter")
	h.expect("backspace clear")
	h.key("backspace")
	if h.m.tree.Filter().Active() || h.m.tree.View() != tree.ViewCleanup {
		t.Fatal("the text says that it clears the filter")
	}
	h.letGo()
	h.key("backspace")
	if h.m.tree.View() != tree.ViewTree {
		t.Fatal("then it leaves the view")
	}
}

// ---- marks

func TestARowThatCanNeverGoTakesNoMark(t *testing.T) {
	h := newHarness(t, 48, 40).start()
	h.cursorTo(wtMain)
	h.key("space")
	if h.m.tree.IsMarked(wtMain) {
		t.Fatal("the main checkout took a mark")
	}
	h.expect("no mark: it is the main checkout")
	h.m.status = ""
	_, y := h.findLine("proj")
	h.tap(0, y)
	if h.m.tree.IsMarked(wtMain) {
		t.Fatal("a tap marked the main checkout")
	}
	h.expect("no mark: it is the main checkout")
	h.key("A")
	for _, id := range h.m.tree.Marked() {
		if id == wtMain {
			t.Fatal("A marked the main checkout")
		}
	}
	if m := h.m.tree.MarkCounts().Total; m == 0 {
		t.Fatal("A marks the rest")
	}
}

func TestAMarkFromBeforeTheCleanupStageGoes(t *testing.T) {
	h := newHarness(t, 120, 35)
	h.fake.On("list", core.FakeScript{Fixture: "list-stream.ndjson", PauseAfter: 14})
	h.start()
	h.m.tree.ToggleMark(wtMain)
	if !h.m.tree.IsMarked(wtMain) {
		t.Fatal("the core has not said it yet")
	}
	h.fake.Release()
	h.pump()
	if h.m.tree.IsMarked(wtMain) {
		t.Fatal("the core said that this row can never go")
	}
}

// ---- text that fits

func TestNarrowBarNeverCutsACount(t *testing.T) {
	h := newHarness(t, 40, 24).start()
	lines, _ := h.m.narrowBar([]barButton{
		{Key: "d", Label: "delete 30", Short: "del 30", Action: actDelete},
		{Key: "s", Label: "stop 30", Action: actStop},
		{Key: "A", Label: "mark safe", Short: "mk safe", Action: actMarkAll},
		{Key: "a", Label: "actions", Action: actActions},
	}, 20)
	top := ansi.Strip(lines[0])
	for _, want := range []string{"del 30", "stop 30", "mk safe", "actions"} {
		if !strings.Contains(top, want) {
			t.Fatalf("%q in %q", want, top)
		}
	}
	if strings.Contains(top, "delete 3") {
		t.Fatalf("a count that is cut says another number: %q", top)
	}
	lines, _ = h.m.narrowBar([]barButton{{Key: "x", Label: "something very long"}, {Key: "a", Label: "b"}, {Key: "c", Label: "d"}, {Key: "e", Label: "f"}}, 20)
	if !strings.Contains(ansi.Strip(lines[0]), "…") {
		t.Fatalf("a cut has its mark: %q", lines[0])
	}
}

func TestEmptyRepoHintKeepsItsActions(t *testing.T) {
	h := newHarness(t, 48, 24)
	h.fake.On("list", core.FakeScript{Data: listWithout(t, `"path":"/fixture/home/code/worktrees/`, `"id":"worktree:/fixture/home/code/worktrees`)})
	h.start()
	h.expect("n new · g machine")
	h.expectNot("· g\n")
	w := newHarness(t, 120, 30)
	w.fake.On("list", core.FakeScript{Data: listWithout(t, `"path":"/fixture/home/code/worktrees/`, `"id":"worktree:/fixture/home/code/worktrees`)})
	w.start()
	w.expect("n new worktree · g machine scope")
}

func TestShortHeaderGivesWayWhole(t *testing.T) {
	cut := regexp.MustCompile(`\d+ w( |$)|\d+ s( |$)|\d+ $`)
	for _, s := range [][2]int{{60, 16}, {64, 16}, {70, 14}, {75, 16}, {93, 14}} {
		for _, opt := range []harnessOption{withGlobal, func(*Options) {}} {
			h := newHarness(t, s[0], s[1], opt).start()
			head := strings.Split(h.screen(), "\n")[0]
			before, _, _ := strings.Cut(head, "STACK")
			if cut.MatchString(strings.TrimRight(before, " ")) {
				t.Fatalf("%v: a number is cut in %q", s, head)
			}
			h.checkFrame()
		}
	}
}

func TestHelpSaysWhyAKeyDoesNothingNow(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.cursorTo(wtMain)
	h.key("?")
	h.expect("d         it is the main checkout")
	h.expect("nothing is running")
}

func TestPrintItemRefusesAPathWithAnEscape(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	dirty := "/x/esc\x1b]0;PWNED\x07name"
	if it := h.m.printItem(&core.Worktree{Path: dirty}); !it.Disabled {
		t.Fatal("the raw path would reach the terminal")
	}
	if it := h.m.printItem(&core.Worktree{Path: "/x/clean"}); it.Disabled || it.Right != "" {
		t.Fatalf("%+v", it)
	}
}

// h acts on what the sheet shows. A hidden mark of the next part of a batch is listed there.
func TestHOnlyActsOnWhatTheSheetShows(t *testing.T) {
	h := newHarness(t, 120, 35)
	h.fake.On("plan:"+core.ActionDeleteStacks, core.FakeScript{Data: stackPlan("", false, "proj-alpha")})
	h.start()
	h.m.tree.ToggleMark(wtMerged)
	h.m.tree.ToggleMark(stAlpha)
	h.key("/")
	h.typeText("proj-alpha")
	h.key("enter")
	h.expect("hidden by filter")
	h.key("d")
	h.expectNot("HIDDEN BY FILTER")
	plans := len(h.calls("plan"))
	h.key("h")
	if !h.m.tree.IsMarked(wtMerged) || len(h.calls("plan")) != plans {
		t.Fatalf("h unmarked what no heading shows: %v, %d plans", h.m.tree.Marked(), len(h.calls("plan")))
	}
	h.key("y")
	h.key("enter")
	if h.m.CurrentMode() != ModeConfirm {
		t.Fatalf("the next part has its sheet\n%s", h.screen())
	}
	h.expect("HIDDEN BY FILTER")
	if _, _, hidden := h.m.confirm.Sections(); len(hidden) != 1 || hidden[0].ID != wtMerged {
		t.Fatalf("%+v", hidden)
	}
	h.key("h")
	if h.m.tree.IsMarked(wtMerged) {
		t.Fatal("on its own sheet h takes it out")
	}
}
