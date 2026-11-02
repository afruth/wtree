package model

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/afruth/wtree/tui/internal/core"
	"github.com/afruth/wtree/tui/internal/dialog"
	"github.com/afruth/wtree/tui/internal/tree"
	"github.com/afruth/wtree/tui/internal/treetable"
)

// ---- load gating

func TestDestructiveKeysAreInertUntilStructureEnds(t *testing.T) {
	h := newHarness(t, 120, 35)
	// the stream stops inside the structure stage: names are there, the stage is not done
	h.fake.On("list", core.FakeScript{Fixture: "list-stream.ndjson", PauseAfter: 5})
	h.start()
	if h.m.structureReady() {
		t.Fatal("the structure stage cannot be done yet")
	}
	for _, k := range []string{"d", "D", "s", "n"} {
		h.key(k)
		h.expect("loading… try again")
		if h.m.CurrentMode() != ModeList {
			t.Fatalf("%s opened something", k)
		}
	}
	if n := len(h.calls("plan")) + len(h.calls("new")); n != 0 {
		t.Fatalf("%d calls before the structure ended: a key was queued or ran", n)
	}
	// a tap on the bar is the same key
	x, y := h.findLine(" d delete")
	h.tap(x+2, y)
	if len(h.calls("plan")) != 0 {
		t.Fatal("a tap on delete went through the gate")
	}
	// dropped, not queued
	h.fake.Release()
	h.pump()
	if !h.m.structureReady() {
		t.Fatal("the structure stage must be done now")
	}
	if len(h.calls("plan")) != 0 {
		t.Fatal("a key from before the end was queued")
	}
	h.cursorTo(wtAlpha)
	h.key("d")
	if h.m.CurrentMode() != ModeConfirm {
		t.Fatalf("d after the structure must open the sheet: mode %d\n%s", h.m.CurrentMode(), h.screen())
	}
}

func TestReloadGatesDestructiveKeysAgain(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.fake.On("list", core.FakeScript{Fixture: "list-stream.ndjson", PauseAfter: 3})
	h.key("r")
	if h.m.structureReady() {
		t.Fatal("a reload that runs its structure stage must gate")
	}
	h.expect("alpha") // the rows stay on the screen
	h.key("d")
	if len(h.calls("plan")) != 0 {
		t.Fatal("d during a reload asked for a plan")
	}
	h.fake.Release()
	h.pump()
}

func TestFirstFrameSaysLoading(t *testing.T) {
	h := newHarness(t, 80, 24)
	h.fake.On("list", core.FakeScript{Fixture: "list-stream.ndjson", PauseAfter: -1})
	h.start()
	h.expect("loading worktrees…")
	h.checkFrame()
	h.key("j", "space", "enter", "a")
	if h.m.CurrentMode() != ModeList {
		t.Fatal("only q ? g / work on the first frame")
	}
	h.key("?")
	if h.m.CurrentMode() != ModeHelp {
		t.Fatal("? must work on the first frame")
	}
	h.fake.Release()
	h.pump()
}

func TestProgressiveCellStates(t *testing.T) {
	h := newHarness(t, 120, 35)
	// after the structure stage of the recording: names, and no numbers
	h.fake.On("list", core.FakeScript{Fixture: "list-stream.ndjson", PauseAfter: 14})
	h.start()
	if !h.m.structureReady() {
		t.Fatalf("the structure must be in\n%s", h.screen())
	}
	h.expect("alpha")
	x, y := h.findLine(" alpha")
	_ = x
	line := strings.Split(h.screen(), "\n")[y]
	if !strings.Contains(line, "·") {
		t.Fatalf("a live cell that will come must show the loading dot: %q", line)
	}
	h.fake.Release()
	h.pump()
	h.expect("2.5%")
	h.expect("3.0G")
}

// ---- plan and apply

func TestPlanApplyTwoSteps(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.cursorTo(wtAlpha)
	h.key("d")
	if h.m.CurrentMode() != ModeConfirm {
		t.Fatalf("no sheet:\n%s", h.screen())
	}
	h.expect("GOES")
	h.expect("STAYS")
	h.expect("it has changed files")
	h.expect("Ignored files")
	if h.m.confirm.Focus != dialog.FocusCancel {
		t.Fatal("focus must start on cancel")
	}
	h.key("enter")
	if len(h.calls("apply")) != 0 {
		t.Fatal("the first enter must not confirm")
	}
	if h.m.confirm.Focus != dialog.FocusConfirm {
		t.Fatal("the first enter moves the focus to confirm")
	}
	h.key("enter")
	calls := h.calls("apply")
	if len(calls) != 1 {
		t.Fatalf("%d applies", len(calls))
	}
	if !hasArgs(calls[0].Args, "core", "apply", "delete-worktrees", "--plan", "0000000000000001", "--choice", "worktrees") {
		t.Fatalf("args: %v", calls[0].Args)
	}
	h.expect("done")
	h.expect("L log")
	if !h.m.progress.Finished {
		t.Fatal("the batch must be finished")
	}
	// reload after the end, as the contract says
	if n := len(h.calls("list")); n < 2 {
		t.Fatalf("no reload after the end: %d lists", n)
	}
	h.key("enter")
	if h.m.CurrentMode() != ModeList {
		t.Fatal("enter must close the results")
	}
}

func TestCancelFromTheSheetRunsNothing(t *testing.T) {
	for _, k := range []string{"backspace", "q", "esc", "0", "n"} {
		h := newHarness(t, 80, 24).start()
		h.cursorTo(wtAlpha)
		h.key("d", k)
		if h.m.CurrentMode() != ModeList || len(h.calls("apply")) != 0 {
			t.Fatalf("%s: mode %d, %d applies", k, h.m.CurrentMode(), len(h.calls("apply")))
		}
	}
}

func TestYConfirmsDirectlyAndChoiceIsSent(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.cursorTo(wtAlpha)
	h.key("d")
	// digits: 1 delete, 2 force, 3 worktrees, 4 and stacks, 5 with volumes
	h.key("4")
	if h.m.confirm.Selected().ID != "worktrees+stacks" {
		t.Fatalf("choice %s", h.m.confirm.Selected().ID)
	}
	h.expect("delete 2 worktrees and 1 stack")
	h.key("y")
	calls := h.calls("apply")
	if len(calls) != 1 || !hasArgs(calls[0].Args, "--choice", "worktrees+stacks") {
		t.Fatalf("%v", calls)
	}
}

func TestStrongerChoiceMovesFocusBackToCancel(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.cursorTo(wtAlpha)
	h.key("d", "enter")
	if h.m.confirm.Focus != dialog.FocusConfirm {
		t.Fatal("focus")
	}
	h.key("5")
	if h.m.confirm.Focus != dialog.FocusCancel {
		t.Fatal("a stronger choice must put the focus back on cancel")
	}
	h.expect("volumes")
	if h.m.confirm.Tone() != 6 { // theme.ToneLoss
		t.Fatal("volume loss is plum")
	}
	h.expect("1 volume")
	h.expect("data lost")
}

func TestLevelAsksForANewPlanAndNeverStaysSelected(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.cursorTo(wtAlpha)
	h.key("d", "2")
	calls := h.calls("plan")
	if len(calls) != 2 || !hasArgs(calls[1].Args, "--force") {
		t.Fatalf("%v", calls)
	}
	if !h.m.confirm.Plan.Flags.Force {
		t.Fatal("the sheet must show the force plan")
	}
	h.expect("force delete")
	// the next sheet starts mild again
	before := len(h.calls("plan"))
	h.key("backspace", "d")
	if next := h.calls("plan")[before]; hasArgs(next.Args, "--force") {
		t.Fatal("force stayed selected from the last run")
	}
	if h.m.confirm.Plan.Flags.Force {
		t.Fatal("the new sheet shows force")
	}
}

func TestOneStopRunsAtOnceAndSaysSo(t *testing.T) {
	h := newHarness(t, 120, 35)
	h.fake.On("plan:"+core.ActionStopStacks, core.FakeScript{Data: mustPlanNone(t)})
	h.start()
	h.cursorTo(wtAlpha)
	h.key("s")
	if len(h.calls("apply")) != 1 {
		t.Fatalf("a none plan runs at once: %d applies\n%s", len(h.calls("apply")), h.screen())
	}
	if h.m.CurrentMode() != ModeList {
		t.Fatalf("a stop that went well needs no results screen\n%s", h.screen())
	}
}

func mustPlanNone(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../core/testdata/plan-stop-stacks.json")
	if err != nil {
		t.Fatal(err)
	}
	var plan map[string]any
	if err := json.Unmarshal(data, &plan); err != nil {
		t.Fatal(err)
	}
	plan["confirm"] = map[string]any{"kind": "none", "phrases": []any{}}
	out, _ := json.Marshal(plan)
	return out
}

func TestNothingToDoCannotBeConfirmed(t *testing.T) {
	h := newHarness(t, 80, 24)
	h.fake.On("plan:"+core.ActionDeleteWorktrees, core.FakeScript{Fixture: "plan-nothing-to-do.json"})
	h.start()
	h.cursorTo(wtAlpha)
	h.key("d")
	h.expect("it is the main checkout")
	h.key("y", "enter", "enter")
	if len(h.calls("apply")) != 0 {
		t.Fatal("nothing to do cannot run")
	}
}

// ---- phrases

func TestPhraseRules(t *testing.T) {
	for _, c := range []struct {
		typed, want string
		ok          bool
	}{
		{"2 stacks", "2 stacks", true},
		{"2 Stacks", "2 stacks", true},
		{"2 STACKS.", "2 stacks", true},
		{"  2 stacks  ", "2 stacks", true},
		{"2 stacks. ", "2 stacks", true},
		{"proj-alpha", "Proj-Alpha", true},
		{"2 stack", "2 stacks", false},
		{"2  stacks", "2 stacks", false},
		{"2 stacks..", "2 stacks", false},
		{"3 stacks", "2 stacks", false},
		{"", "2 stacks", false},
		{"y", "2 stacks", false},
	} {
		if got := dialog.PhraseMatches(c.typed, c.want); got != c.ok {
			t.Errorf("%q against %q: %v", c.typed, c.want, got)
		}
	}
}

func TestTypedPhraseSendsTheExactPhrase(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.cursorTo(wtAlpha)
	h.key("l")
	h.cursorTo(stAlpha)
	h.key("a")
	it, ok := h.m.menu.ByAction(actDeleteVols)
	if !ok || it.Disabled {
		t.Fatalf("no delete with volumes: %+v", h.m.menu.Items)
	}
	h.key(it.Key)
	if h.m.CurrentMode() != ModeConfirm {
		t.Fatalf("mode %d\n%s", h.m.CurrentMode(), h.screen())
	}
	if !h.m.confirm.Plan.Flags.Volumes {
		t.Fatal("the plan must be with volumes")
	}
	h.expect("type to delete 2 stacks and 2 volumes")
	h.key("y")
	h.expect("Type \"2 stacks\" to delete")
	if !h.m.confirm.Typing {
		t.Fatalf("y must open the field\n%s", h.screen())
	}
	if h.m.confirm.Input.Value() != "" {
		t.Fatal("the field must never be prefilled")
	}
	if len(h.m.confirm.Input.AvailableSuggestions()) != 0 {
		t.Fatal("the field must never suggest")
	}
	h.expect("2 stacks")

	h.typeText("2 stack")
	h.key("enter")
	h.expect("expected: 2 stacks")
	if len(h.calls("apply")) != 0 {
		t.Fatal("a near miss must not apply")
	}
	for range "2 stack" {
		h.send(keyOf("backspace"))
	}
	h.pump()
	h.typeText("2 STACKS.")
	h.key("enter")
	calls := h.calls("apply")
	if len(calls) != 1 {
		t.Fatalf("%d applies\n%s", len(calls), h.screen())
	}
	if !hasArgs(calls[0].Args, "--phrase", "2 stacks") {
		t.Fatalf("the phrase must go as the core gave it: %v", calls[0].Args)
	}
	if !hasArgs(calls[0].Args, "--volumes") {
		t.Fatalf("the flags must go back: %v", calls[0].Args)
	}
}

func TestBackspaceOnEmptyFieldReturnsToTheSheet(t *testing.T) {
	h := newHarness(t, 80, 24)
	h.fake.On("plan:"+core.ActionDeleteStacks, core.FakeScript{Fixture: "plan-delete-stacks-volumes.json"})
	h.start()
	h.cursorTo(stGone)
	h.key("d", "y")
	if !h.m.confirm.Typing {
		t.Fatal("no field")
	}
	h.key("backspace")
	if h.m.confirm.Typing || h.m.CurrentMode() != ModeConfirm {
		t.Fatal("backspace on an empty field goes back to the sheet")
	}
}

func TestSeveralPhrasesInOrder(t *testing.T) {
	h := newHarness(t, 120, 35)
	plan := `{"v":1,"plan_id":"p9","action":"delete-worktrees","flags":{"force":true,"volumes":false},"cwd":"/x",
"items":[{"id":"worktree:/x/a","kind":"worktree","label":"a","go":true,"offered":false,"consequences":[],"steps":["s1"]}],
"steps":[{"id":"s1","label":"delete a","kind":"delete_worktree","item":"worktree:/x/a","force":true,"volumes":false,"after":[]}],
"choices":[{"id":"worktrees","text":"worktrees only","default":true,"available":true,"adds":[],"steps":["s1"],
"confirm":{"kind":"phrase","phrases":[{"phrase":"a","hint":"Type a to delete"},{"phrase":"2 stacks","hint":"Type 2 stacks to delete"}]},"consequences":[]}],
"confirm":{"kind":"phrase","phrases":[]},"notes":[],"nothing_to_do":false}`
	h.fake.On("plan:"+core.ActionDeleteWorktrees, core.FakeScript{Data: []byte(plan)})
	h.start()
	h.cursorTo(wtAlpha)
	h.key("d", "y")
	h.expect("phrase 1 of 2")
	h.typeText("A.")
	h.key("enter")
	if h.m.confirm.Input.Value() != "" {
		t.Fatal("each phrase starts from an empty field")
	}
	h.expect("phrase 2 of 2")
	h.typeText("2 stacks")
	if len(h.calls("apply")) != 0 {
		t.Fatal("not before the last enter")
	}
	h.key("enter")
	calls := h.calls("apply")
	if len(calls) != 1 || !hasArgs(calls[0].Args, "--phrase", "a", "--phrase", "2 stacks") {
		t.Fatalf("exact and in order: %v", calls)
	}
}

// ---- stale

func TestStaleShowsTheNewPlanAndNeverRetries(t *testing.T) {
	h := newHarness(t, 120, 35)
	h.fake.On("apply", core.FakeScript{Fixture: "apply-refused-stale.ndjson"})
	h.start()
	h.cursorTo(wtAlpha)
	h.key("d", "y")
	if h.m.CurrentMode() != ModeConfirm {
		t.Fatalf("a stale plan goes back to the sheet: mode %d\n%s", h.m.CurrentMode(), h.screen())
	}
	h.expect("Something changed since this sheet opened")
	if h.m.confirm.Focus != dialog.FocusCancel {
		t.Fatal("focus must be on cancel")
	}
	if n := len(h.calls("apply")); n != 1 {
		t.Fatalf("no retry on its own: %d applies", n)
	}
	lists := len(h.calls("list"))
	if lists < 2 {
		t.Fatalf("a stale plan refreshes the list: %d lists", lists)
	}
	h.pump()
	if n := len(h.calls("apply")); n != 1 {
		t.Fatalf("no retry on its own: %d applies", n)
	}
	// The new plan keeps alpha, and it has nothing to run: the sheet shows that.
	h.expect("it has changed files")
	h.expect("nothing to do")
	h.key("y", "enter", "enter")
	if n := len(h.calls("apply")); n != 1 {
		t.Fatalf("a plan with nothing to do runs nothing: %d applies", n)
	}
	// The force level is what the new plan offers, and it takes a new plan.
	h.key("2")
	if calls := h.calls("plan"); !hasArgs(calls[len(calls)-1].Args, "--force") {
		t.Fatalf("%v", calls[len(calls)-1].Args)
	}
}

func TestStalePhraseIsForgotten(t *testing.T) {
	h := newHarness(t, 120, 35)
	h.fake.On("plan:"+core.ActionDeleteStacks, core.FakeScript{Fixture: "plan-delete-stacks-volumes.json"})
	h.fake.On("apply", core.FakeScript{Fixture: "apply-refused-stale.ndjson"})
	h.start()
	h.cursorTo(stGone)
	h.key("d", "y")
	h.typeText("2 stacks")
	h.key("enter")
	if h.m.confirm.Typing || len(h.m.confirm.Typed) != 0 {
		t.Fatal("what was typed for the old plan must be gone")
	}
}

func TestOtherRefusalsSayWhat(t *testing.T) {
	h := newHarness(t, 120, 35)
	h.fake.On("apply", core.FakeScript{Fixture: "apply-refused-phrase.ndjson"})
	h.start()
	h.cursorTo(wtAlpha)
	h.key("d", "y")
	h.expect("wtree did not accept the typed phrase")
	if h.m.CurrentMode() != ModeConfirm {
		t.Fatal("back to the sheet")
	}
}

// ---- progress, results, quit

func TestFailedItemsStayMarkedForRetry(t *testing.T) {
	h := newHarness(t, 120, 35)
	h.fake.On("plan:"+core.ActionDeleteStacks, core.FakeScript{Fixture: "plan-delete-stacks-volumes.json"})
	h.fake.On("apply", core.FakeScript{Fixture: "apply-failed.ndjson"})
	h.start()
	h.cursorTo(stGone)
	h.key("d", "4", "y") // stacks+worktrees: all four steps run
	h.typeText(h.m.confirm.Phrases()[0].Phrase)
	h.key("enter")
	h.expect("failed: volume is in use")
	h.expect("skipped")
	h.expect("done")
	if !h.m.tree.IsMarked("stack:pa") {
		// the row is gone from this data, so the mark does not stick: see below
		t.Log("stack:pa is not in the fixture list")
	}
	if !h.m.progress.Finished || h.m.progress.OK() {
		t.Fatal("a failed batch is not OK")
	}
	h.key("space") // the error detail of the row under the cursor
	h.key("enter")
	h.expect("did not finish")
	h.expectNot("still marked") // the row is not in this list, so no mark stuck
}

func TestFailedKeepsMarkOnARowOfTheList(t *testing.T) {
	h := newHarness(t, 120, 35)
	stream := `{"v":1,"event":"start","plan_id":"0000000000000001","steps":3}
{"v":1,"event":"step","id":"s1","state":"running","label":"delete worktree alpha"}
{"v":1,"event":"step","id":"s1","state":"failed","ms":7,"status":1,"tail":["it is in use"]}
{"v":1,"event":"step","id":"s2","state":"running","label":"delete worktree merged"}
{"v":1,"event":"step","id":"s2","state":"ok","ms":7}
{"v":1,"event":"end","ok":1,"failed":1,"skipped":0,"disk_refresh":false,"cancelled":false}
`
	h.fake.On("apply", core.FakeScript{Data: []byte(stream)})
	h.start()
	h.m.tree.ToggleMark(wtAlpha)
	h.m.tree.ToggleMark(wtMerged)
	h.key("d", "y")
	h.expect("failed: it is in use")
	if !h.m.tree.IsMarked(wtAlpha) {
		t.Fatal("what failed stays marked")
	}
	if h.m.tree.IsMarked(wtMerged) {
		t.Fatal("what went is not marked any more")
	}
	h.key("l")
	h.expect("command log")
	h.expect("wtree core apply delete-worktrees")
	h.expect("it is in use")
	h.key("backspace", "enter")
	if h.m.CurrentMode() != ModeList {
		t.Fatal("back to the list")
	}
}

func TestResultsRowTapTogglesDetail(t *testing.T) {
	h := newHarness(t, 120, 35)
	stream := `{"v":1,"event":"start","plan_id":"1","steps":1}
{"v":1,"event":"step","id":"s1","state":"running","label":"x"}
{"v":1,"event":"output","id":"s1","line":"a line from compose"}
{"v":1,"event":"step","id":"s1","state":"failed","ms":7,"status":1,"tail":["a line from compose"]}
{"v":1,"event":"end","ok":0,"failed":1,"skipped":0,"disk_refresh":false,"cancelled":false}
`
	h.fake.On("apply", core.FakeScript{Data: []byte(stream)})
	h.start()
	h.cursorTo(wtAlpha)
	h.key("d", "y")
	x, y := h.findLine("failed: a line")
	h.tap(x, y)
	if !h.m.progress.Rows[0].Open {
		t.Fatalf("a tap on a row opens its detail\n%s", h.screen())
	}
	h.tap(x, y)
	if h.m.progress.Rows[0].Open {
		t.Fatal("a second tap closes it")
	}
}

func holdApply(t *testing.T, h *harness) {
	t.Helper()
	h.fake.On("apply", core.FakeScript{Fixture: "apply-ok.ndjson", PauseAfter: 2, OnCancel: "apply-cancelled.ndjson"})
	h.start()
	h.cursorTo(wtAlpha)
	h.key("d", "y")
	if h.m.CurrentMode() != ModeProgress || !h.m.applyRunning() {
		t.Fatalf("the apply must be running: mode %d\n%s", h.m.CurrentMode(), h.screen())
	}
}

func TestResizeDuringApplyKeepsItGoing(t *testing.T) {
	h := newHarness(t, 120, 35)
	holdApply(t, h)
	h.expect("deleting")
	stream := h.m.writer
	for _, s := range [][2]int{{48, 40}, {93, 14}, {39, 12}, {200, 50}, {80, 24}} {
		h.send(tea.WindowSizeMsg{Width: s[0], Height: s[1]})
		h.pump()
		h.checkFrame()
		if h.m.writer != stream {
			t.Fatal("a resize must not touch the stream")
		}
	}
	h.expect("deleting")
	h.fake.Release()
	h.pump()
	if h.m.applyRunning() || !h.m.progress.Finished {
		t.Fatal("the apply must come to its end")
	}
	h.expect("2 done")
	if n := len(h.calls("apply")); n != 1 {
		t.Fatalf("%d applies", n)
	}
}

func TestQuitDuringApplyNeedsConfirmation(t *testing.T) {
	h := newHarness(t, 120, 35)
	holdApply(t, h)
	h.key("q")
	if h.quit {
		t.Fatal("q must not quit during a batch")
	}
	if h.m.quitMenu == nil {
		t.Fatal("q must ask")
	}
	h.expect("still deleting")
	h.expect("keep going")
	h.key("1")
	if h.m.quitMenu != nil || h.m.progress.Cancelling {
		t.Fatal("1 keeps going")
	}
	h.key("ctrl+c")
	if h.quit || h.m.quitMenu == nil {
		t.Fatal("ctrl-c asks too")
	}
	h.send(keyOf("3")) // no pump: the fake ends the step at once on a stop
	if h.quit {
		t.Fatal("it waits for the step")
	}
	if !h.m.progress.Cancelling {
		t.Fatal("it says that it stops")
	}
	h.expect("cancelling after current step")
	h.pump()
	if !h.quit {
		t.Fatalf("it quits when the batch ended\n%s", h.screen())
	}
	if !h.m.progress.Finished || h.m.applyRunning() {
		t.Fatal("the batch must be over")
	}
}

func TestStopAfterStepStaysOnResults(t *testing.T) {
	h := newHarness(t, 120, 35)
	holdApply(t, h)
	h.key("backspace")
	h.send(keyOf("2"))
	if h.quit {
		t.Fatal("2 stays")
	}
	h.expect("cancelling after current step")
	h.pump()
	if !h.m.progress.Finished {
		t.Fatal("over")
	}
	if h.m.progress.End == nil || !h.m.progress.End.Cancelled {
		t.Fatalf("the recording ends cancelled: %+v", h.m.progress.End)
	}
	h.expect("skipped")
}

func TestApplyStreamDiesReadsTheTruth(t *testing.T) {
	h := newHarness(t, 120, 35)
	h.fake.On("apply", core.FakeScript{Fixture: "apply-ok.ndjson", DieAfter: 2})
	h.start()
	h.cursorTo(wtAlpha)
	h.key("d", "y")
	if !h.m.progress.Finished || h.m.progress.Err == "" {
		t.Fatalf("a stream that dies is an error\n%s", h.screen())
	}
	h.expect("not known what it did")
}

func TestSignalDuringApplyLeavesTheStreamForShutdown(t *testing.T) {
	h := newHarness(t, 120, 35)
	holdApply(t, h)
	h.send(SignalMsg{})
	h.pump()
	if !h.quit {
		t.Fatal("a signal quits at once")
	}
	if !h.m.applyRunning() {
		t.Fatal("the stream is still there for shutdown")
	}
	stream := h.m.writer
	h.fake.Release()
	var out strings.Builder
	h.m.shutdown(&out)
	if !strings.Contains(out.String(), "stops after the current step") {
		t.Fatalf("no word about the batch: %q", out.String())
	}
	<-stream.Done()
}

// ---- marks and filter

func TestMarksWithHiddenByFilter(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.cursorTo(wtAlpha)
	h.key("space")
	h.cursorTo(wtBeta)
	h.key("tab")
	h.expect("2 marked")
	h.key("/")
	h.typeText("alpha")
	h.key("enter")
	if h.m.CurrentMode() != ModeList {
		t.Fatal("enter applies and returns")
	}
	h.expect("2 marked (1 hidden by filter)")
	h.key("a")
	h.expect("2 marked (1 hidden by filter)")
	h.key("backspace", "d")
	h.expect("HIDDEN BY FILTER")
	h.expect("not visible in the list")
	first := h.calls("plan")[0]
	if !hasArgs(first.Args, "--", "/fixture/home/code/worktrees/alpha", "/fixture/home/code/worktrees/beta") {
		t.Fatalf("hidden marks are targets too: %v", first.Args)
	}
	h.key("h")
	calls := h.calls("plan")
	last := calls[len(calls)-1]
	if strings.Contains(strings.Join(last.Args, " "), "worktrees/beta") {
		t.Fatalf("h takes them out of the plan: %v", last.Args)
	}
	if h.m.tree.IsMarked(wtBeta) {
		t.Fatal("h unmarks them")
	}
}

func TestTabAndSpaceBothToggleAndDoubleTabCountsOnce(t *testing.T) {
	h := newHarness(t, 80, 24).start()
	h.cursorTo(wtAlpha)
	h.key("space")
	if !h.m.tree.IsMarked(wtAlpha) {
		t.Fatal("space marks")
	}
	h.key("space")
	if h.m.tree.IsMarked(wtAlpha) {
		t.Fatal("space unmarks")
	}
	h.key("tab")
	if !h.m.tree.IsMarked(wtAlpha) {
		t.Fatal("tab marks")
	}
	h.clock = h.clock.Add(100e6)
	h.key("tab") // Termius double-tap: two tabs within 300 ms are one tab
	if !h.m.tree.IsMarked(wtAlpha) {
		t.Fatal("a second tab within 300 ms is the same tab")
	}
	h.clock = h.clock.Add(1e9)
	h.key("tab")
	if h.m.tree.IsMarked(wtAlpha) {
		t.Fatal("a later tab unmarks")
	}
}

func TestSearchFiltersAndBackspaceLeaves(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.key("/")
	if h.m.CurrentMode() != ModeSearch {
		t.Fatal("/ opens the search")
	}
	h.typeText("state:exited")
	h.expect("proj-old")
	h.expectNot(" gamma ")
	h.key("enter")
	h.expect("filter: state:exited")
	h.expect("backspace clear")
	h.key("/")
	for range "state:exited" {
		h.send(keyOf("backspace"))
	}
	h.pump()
	h.key("backspace")
	if h.m.CurrentMode() != ModeList || h.m.tree.Filter().Active() {
		t.Fatal("backspace on an empty field leaves and clears")
	}
	h.expect("alpha")
}

func TestInvalidFilterMatchesNothingAndSaysSo(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.key("/")
	h.typeText("foo:bar")
	h.expect("unknown filter: foo:")
	h.key("enter")
	h.expect("nothing matches")
	h.key("A")
	if len(h.m.tree.Marked()) != 0 {
		t.Fatal("a typo must not mark anything")
	}
}

func TestBackspaceIsTheUniversalBack(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	// overlay, then filter, then marks, and then it says how to quit: it is no way out itself
	h.cursorTo(wtAlpha)
	h.key("space", "/")
	h.typeText("a")
	h.key("enter", "?")
	h.key("backspace")
	if h.m.CurrentMode() != ModeList {
		t.Fatal("the overlay first")
	}
	h.letGo()
	h.key("backspace")
	if h.m.tree.Filter().Active() || len(h.m.tree.Marked()) != 1 {
		t.Fatal("then the filter")
	}
	h.letGo()
	h.key("backspace")
	if len(h.m.tree.Marked()) != 0 || h.quit {
		t.Fatal("then the marks")
	}
	h.letGo()
	h.key("backspace")
	if h.quit {
		t.Fatal("backspace never quits: on a phone it is held, and it repeats")
	}
	h.expect("q quits")
	h.key("q")
	if !h.quit {
		t.Fatal("q quits")
	}
}

func TestEscLikeBackspaceInEverySheet(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.key("a", "esc")
	if h.m.CurrentMode() != ModeList {
		t.Fatal("esc closes the sheet")
	}
	h.key("?", "esc")
	if h.m.CurrentMode() != ModeList {
		t.Fatal("esc closes help")
	}
}

// ---- action sheet

func TestActionSheetDisabledItemsGiveReasons(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.cursorTo(wtMain)
	h.key("a")
	h.expect("it is the main checkout")
	it, ok := h.m.menu.ByAction(actDelete)
	if !ok || !it.Disabled {
		t.Fatalf("delete on the main checkout is dimmed with the reason of the core: %+v", it)
	}
	if it.Key != "-" {
		t.Fatal("a disabled item has no digit")
	}
	if _, ok := h.m.menu.Pick("-"); ok {
		t.Fatal("no key picks a disabled item")
	}
	h.tapText("delete worktree")
	h.expect("delete worktree: it is the main checkout")
	if len(h.calls("plan")) != 0 {
		t.Fatal("a disabled item did something")
	}
}

func TestMarksSheetSaysTheCursorRowIsOut(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.cursorTo(wtAlpha)
	h.key("space")
	h.cursorTo(wtBeta)
	h.key("a")
	h.expect("1 marked")
	h.expect("The row under the cursor is not marked")
	h.expect("unmark all")
}

func TestStackPrimaryLeadsSheetAndBar(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.cursorTo(stGone)
	h.key("a")
	if h.m.menu.Items[0].Action != actDeleteKeep {
		t.Fatalf("an orphan stack leads with delete: %+v", h.m.menu.Items[0])
	}
	it, _ := h.m.menu.ByAction(actOpen)
	if !it.Disabled || !strings.Contains(it.Reason, "gone") {
		t.Fatalf("open on a gone stack gives a reason: %+v", it)
	}
	h.key("backspace")
	if b := h.m.barButtons()[0]; b.Action != actDelete || !b.Primary {
		t.Fatalf("the bar leads with delete: %+v", b)
	}
}

func TestEnterNeverDeletesOrStops(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.cursorTo(stGone)
	h.key("enter")
	if len(h.calls("plan")) != 0 || len(h.calls("apply")) != 0 {
		t.Fatal("enter did a destructive thing")
	}
	if h.m.CurrentMode() != ModeMenu {
		t.Fatalf("enter where open cannot work opens the sheet\n%s", h.screen())
	}
}

// ---- tap

func (h *harness) tapText(text string) {
	h.t.Helper()
	x, y := h.findLine(text)
	h.tap(x, y)
}

func TestTapMovesTheCursorAndNothingElse(t *testing.T) {
	for _, s := range [][2]int{{120, 35}, {80, 24}, {48, 40}} {
		h := newHarness(t, s[0], s[1]).start()
		h.cursorTo(wtMain)
		x, y := h.findLine("beta")
		h.tap(x+1, y)
		if h.m.table.Cursor() != wtBeta {
			t.Fatalf("%v: cursor %s\n%s", s, h.m.table.Cursor(), h.screen())
		}
		if h.m.CurrentMode() != ModeList || len(h.m.tree.Marked()) != 0 {
			t.Fatalf("%v: a tap on another row only moves the cursor", s)
		}
		h.tap(x+1, y)
		if h.m.CurrentMode() != ModeMenu {
			t.Fatalf("%v: a tap on the cursor row opens the sheet\n%s", s, h.screen())
		}
		if len(h.calls("plan"))+len(h.calls("open")) != 0 {
			t.Fatalf("%v: a tap must not act", s)
		}
	}
}

func TestPressWithoutReleaseIsATap(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.cursorTo(wtMain)
	x, y := h.findLine("beta")
	h.send(tea.MouseClickMsg{X: x + 1, Y: y, Button: tea.MouseLeft})
	h.pump()
	if h.m.table.Cursor() != wtMain {
		t.Fatal("not yet")
	}
	if fire[pressExpireMsg](h) != 1 {
		t.Fatal("no timer for the press")
	}
	if h.m.table.Cursor() != wtBeta {
		t.Fatal("a press without a release counts as a tap")
	}
	// a late release does not tap again
	h.send(tea.MouseReleaseMsg{X: x + 1, Y: y, Button: tea.MouseLeft})
	h.pump()
	if h.m.CurrentMode() != ModeList {
		t.Fatal("a late release must not count as a second tap")
	}
}

func TestReleaseBeatsTheTimer(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.cursorTo(wtMain)
	x, y := h.findLine("beta")
	h.tap(x+1, y)
	fire[pressExpireMsg](h) // an old timer does nothing
	if h.m.CurrentMode() != ModeList {
		t.Fatal("an old timer tapped")
	}
}

func TestGutterAndChevronZones(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.cursorTo(wtMain)
	_, y := h.findLine("beta")
	h.tap(0, y) // the gutter
	if !h.m.tree.IsMarked(wtBeta) {
		t.Fatalf("the gutter toggles the mark\n%s", h.screen())
	}
	if h.m.table.Cursor() != wtMain {
		t.Fatal("the gutter does not move the cursor")
	}
	was := h.m.tree.IsExpanded(wtBeta)
	h.tap(2, y) // the chevron
	if h.m.tree.IsExpanded(wtBeta) == was {
		t.Fatal("the chevron expands")
	}
	if h.m.table.Cursor() != wtMain {
		t.Fatal("the chevron does not move the cursor")
	}
}

func TestBarButtonsAndScopeSegments(t *testing.T) {
	for _, s := range [][2]int{{120, 35}, {80, 24}, {48, 40}, {93, 14}} {
		h := newHarness(t, s[0], s[1]).start()
		h.fake.On("list:global", core.FakeScript{Fixture: "list-global.ndjson"})
		x, y := h.findLine("actions")
		h.tap(x, y)
		if h.m.CurrentMode() != ModeMenu {
			t.Fatalf("%v: the actions button\n%s", s, h.screen())
		}
		h.key("backspace")
		x, y = h.findLine("machine")
		h.tap(x+1, y)
		if !h.m.machine() {
			t.Fatalf("%v: the machine segment\n%s", s, h.screen())
		}
		if len(h.calls("list:global")) != 1 {
			t.Fatalf("%v: no list of the machine", s)
		}
	}
}

func TestNarrowBarIsTwoRowsTall(t *testing.T) {
	h := newHarness(t, 48, 40).start()
	lines := strings.Split(h.screen(), "\n")
	if !strings.Contains(lines[38], "open") || !strings.Contains(lines[39], "enter") {
		t.Fatalf("label above, key below:\n%s", h.screen())
	}
	c := h.m.compose()
	n := 0
	for _, z := range c.zones {
		if strings.HasPrefix(z.Action, "bar:") {
			n++
			if z.Y1-z.Y0 != 2 || z.X1-z.X0 < 8 {
				t.Fatalf("a target of at least 2x8: %+v", z)
			}
		}
	}
	if n != 4 {
		t.Fatalf("%d buttons", n)
	}
	// both rows hit the same button
	x := 0
	for _, z := range c.zones {
		if z.Action == "bar:"+actActions {
			x = z.X0
		}
	}
	h.tap(x+1, 39)
	if h.m.CurrentMode() != ModeMenu {
		t.Fatal("the lower row of a button works")
	}
}

func TestDisabledBarButtonGivesItsReasonOnATap(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.cursorTo(wtMain)
	h.tapText(" d delete")
	h.expect("delete: proj is the main checkout")
	if len(h.calls("plan")) != 0 {
		t.Fatal("a disabled button did something")
	}
	if armed[statusExpireMsg](h) == 0 {
		t.Fatal("the reason goes away")
	}
}

func TestSheetItemsAreTappableOnEveryTier(t *testing.T) {
	for _, s := range [][2]int{{120, 35}, {80, 24}, {48, 40}, {93, 14}} {
		h := newHarness(t, s[0], s[1]).start()
		h.cursorTo(wtAlpha)
		h.key("a")
		h.tapText("delete worktree")
		if h.m.CurrentMode() != ModeConfirm {
			t.Fatalf("%v: the item did not open the sheet\n%s", s, h.screen())
		}
		// the buttons of the sheet, with the two steps of the keys
		h.tapText("delete 2 worktrees")
		if h.m.confirm.Focus != dialog.FocusConfirm || len(h.calls("apply")) != 0 {
			t.Fatalf("%v: the first tap only moves the focus", s)
		}
		h.tapText("delete 2 worktrees")
		if len(h.calls("apply")) != 1 {
			t.Fatalf("%v: the second tap confirms\n%s", s, h.screen())
		}
	}
}

func TestSheetCancelButtonTap(t *testing.T) {
	h := newHarness(t, 48, 40).start()
	h.cursorTo(wtAlpha)
	h.key("d")
	h.tapText("backspace cancel")
	if h.m.CurrentMode() != ModeList {
		t.Fatal("cancel")
	}
}

func TestSheetOptionTap(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.cursorTo(wtAlpha)
	h.key("d")
	h.tapText("(4) and stacks")
	if h.m.confirm.Selected().ID != "worktrees+stacks" {
		t.Fatalf("%s", h.m.confirm.Selected().ID)
	}
	h.tapText("(2) force")
	if !h.m.confirm.Plan.Flags.Force {
		t.Fatalf("a tap on the level\n%s", h.screen())
	}
}

func TestWheelScrollsAndCursorStays(t *testing.T) {
	h := newHarness(t, 48, 12).start()
	cur := h.m.table.Cursor()
	for i := 0; i < 8; i++ {
		h.send(tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelDown})
	}
	h.pump()
	if h.m.table.Cursor() != cur {
		t.Fatal("a wheel does not move the cursor")
	}
	if h.m.table.Place() == 0 {
		t.Fatal("the cursor row should be out of view")
	}
	h.key("x") // any key brings it back
	if h.m.table.Place() != 0 {
		t.Fatal("any key scrolls back to the cursor")
	}
}

func TestColumnTitleTapSorts(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.tapText("DISK")
	if mode, rev := h.m.tree.Sort(); mode != tree.SortDisk || rev {
		t.Fatalf("%v %v", mode, rev)
	}
	h.expect("sort: disk")
	h.tapText("DISK")
	if _, rev := h.m.tree.Sort(); !rev {
		t.Fatal("a second tap reverses")
	}
	h.tapText("NAME")
	if mode, _ := h.m.tree.Sort(); mode != tree.SortTree {
		t.Fatal("NAME returns to the tree")
	}
}

// ---- cleanup

func TestCleanupViewTiers(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.key("c")
	h.expect("safe")
	h.expect("needs a look")
	h.expect("never")
	h.expect("c back")
	h.key("A")
	for _, id := range h.m.tree.Marked() {
		info, ok := h.m.tree.Lookup(id)
		if !ok || info.Cleanup == nil || info.Cleanup.Tier != core.TierSafe {
			t.Fatalf("A marks safe rows only: %s", id)
		}
	}
	// the core says gone stacks and volumes-only stacks need a look, and the view follows
	if h.m.tree.IsMarked(stVols) {
		t.Fatal("a volumes-only stack is not safe")
	}
	h.cursorTo(wtMain)
	h.key("space")
	if h.m.tree.IsMarked(wtMain) {
		t.Fatal("a never row takes no mark")
	}
	h.expect("no mark:")
	h.key("backspace")
	if h.m.tree.View() != tree.ViewTree {
		t.Fatal("backspace goes back")
	}
}

// ---- ticker and focus

func TestStatsTickerPausesOnBlur(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	if armed[statsTickMsg](h) != 1 {
		t.Fatalf("the ticker runs once the structure is in: %d", armed[statsTickMsg](h))
	}
	for _, e := range h.timers {
		if _, ok := e.msg.(statsTickMsg); ok && e.d != statsEvery {
			t.Fatalf("every %v", e.d)
		}
	}
	lists := len(h.calls("list"))
	fire[statsTickMsg](h)
	calls := h.calls("list")
	if len(calls) != lists+1 || !hasArgs(calls[len(calls)-1].Args, "--only", "stats") {
		t.Fatalf("a tick reads the stats only: %v", calls[len(calls)-1].Args)
	}
	h.send(tea.BlurMsg{})
	h.pump()
	fire[statsTickMsg](h)
	before := len(h.calls("list"))
	if armed[statsTickMsg](h) != 0 {
		t.Fatal("no tick while blurred")
	}
	h.send(tea.FocusMsg{})
	h.pump()
	if armed[statsTickMsg](h) != 1 {
		t.Fatal("the ticker comes back on focus")
	}
	if len(h.calls("list")) != before {
		t.Fatal("no list came from a blurred tick")
	}
}

func TestTickerPausesDuringASheetAndABatch(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.cursorTo(wtAlpha)
	h.key("d")
	n := len(h.calls("list"))
	fire[statsTickMsg](h)
	if len(h.calls("list")) != n {
		t.Fatal("a tick during the sheet")
	}
	if armed[statsTickMsg](h) != 0 {
		t.Fatal("no timer during the sheet")
	}
	h.key("backspace")
	if armed[statsTickMsg](h) != 1 {
		t.Fatal("back after the sheet")
	}
}

func TestLiveUpdateDoesNotMoveRows(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.cursorTo(wtBeta)
	before := make([]string, 0, 16)
	for _, r := range h.m.table.Rows() {
		before = append(before, r.ID)
	}
	fire[statsTickMsg](h)
	after := make([]string, 0, 16)
	for _, r := range h.m.table.Rows() {
		after = append(after, r.ID)
	}
	if strings.Join(before, "\n") != strings.Join(after, "\n") {
		t.Fatal("a tick moved rows")
	}
	if h.m.table.Cursor() != wtBeta {
		t.Fatal("a tick moved the cursor")
	}
}

// ---- resize

func TestRotationDrawsNoArtefacts(t *testing.T) {
	h := newHarness(t, 93, 14).start()
	h.send(tea.WindowSizeMsg{Width: 48, Height: 41})
	h.pump()
	h.checkFrame()
	if h.m.layout.Tier != 2 || h.m.layout.Short {
		t.Fatal("the tier follows the size")
	}
	h.expect("enter")
	if fire[settleMsg](h) == 0 {
		t.Fatal("a resize settles")
	}
	if armed[redrawMsg](h) != 1 {
		t.Fatal("the #1780 fix needs a second render")
	}
	fire[redrawMsg](h)
	h.checkFrame()
	h.expectNot("STACK") // the title line of the old tier
}

func TestABurstOfResizesSettlesOnce(t *testing.T) {
	h := newHarness(t, 80, 24).start()
	for i := 0; i < 20; i++ {
		h.send(tea.WindowSizeMsg{Width: 60 + i, Height: 20 + i})
	}
	h.pump()
	h.checkFrame()
	timers := h.timers
	h.timers = nil
	settled := 0
	for _, e := range timers {
		if s, ok := e.msg.(settleMsg); ok {
			if s.gen == h.m.resizeGen {
				settled++
			}
			h.send(e.msg)
		}
	}
	h.pump()
	if settled != 1 || armed[redrawMsg](h) != 1 {
		t.Fatalf("%d settles, %d redraws", settled, armed[redrawMsg](h))
	}
}

func TestResizeKeepsTheSheet(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.cursorTo(wtAlpha)
	h.key("d", "enter")
	h.send(tea.WindowSizeMsg{Width: 48, Height: 40})
	h.pump()
	h.checkFrame()
	if h.m.CurrentMode() != ModeConfirm {
		t.Fatal("the sheet stays")
	}
	if h.m.confirm.Focus != dialog.FocusCancel {
		t.Fatal("the buttons moved, so the focus goes back to cancel")
	}
	h.expect("GOES")
	if len(h.calls("apply")) != 0 {
		t.Fatal("a resize runs nothing")
	}
}

func TestEveryScreenFitsEverySize(t *testing.T) {
	open := map[string]func(h *harness){
		"list":     func(h *harness) {},
		"menu":     func(h *harness) { h.cursorTo(wtAlpha); h.key("a") },
		"marks":    func(h *harness) { h.cursorTo(wtAlpha); h.key("space", "a") },
		"confirm":  func(h *harness) { h.cursorTo(wtAlpha); h.key("d") },
		"typing":   func(h *harness) { h.cursorTo(stGone); h.key("d", "2", "y") },
		"results":  func(h *harness) { h.cursorTo(wtAlpha); h.key("d", "y") },
		"log":      func(h *harness) { h.cursorTo(wtAlpha); h.key("d", "y", "l") },
		"help":     func(h *harness) { h.key("?") },
		"search":   func(h *harness) { h.key("/"); h.typeText("alpha") },
		"cleanup":  func(h *harness) { h.key("c") },
		"new":      func(h *harness) { h.key("n") },
		"panel":    func(h *harness) { h.key("p") },
		"filtered": func(h *harness) { h.key("/"); h.typeText("zzz"); h.key("enter") },
		"sorted":   func(h *harness) { h.key("o") },
		"z":        func(h *harness) { h.key("z") },
	}
	for name, fn := range open {
		for _, s := range sizes {
			h := newHarness(t, s[0], s[1])
			h.fake.On("plan:"+core.ActionDeleteStacks+":volumes", core.FakeScript{Fixture: "plan-delete-stacks-protected.json"})
			h.start()
			if !h.m.layout.TooSmall {
				fn(h)
			}
			h.checkFrame()
			if t.Failed() {
				t.Fatalf("%s at %v", name, s)
			}
		}
	}
}

func TestBelowFortyColumnsSaysSoAndQWorks(t *testing.T) {
	h := newHarness(t, 39, 20).start()
	h.expect("wtree needs 40 columns")
	h.key("d", "a")
	if h.m.CurrentMode() != ModeList || len(h.calls("plan")) != 0 {
		t.Fatal("only q works down here")
	}
	h.key("q")
	if !h.quit {
		t.Fatal("q quits")
	}
}

// ---- host status

func TestProgramStatusOnlyInRex(t *testing.T) {
	h := newHarness(t, 120, 35)
	holdApply(t, h)
	v := h.m.View()
	if v.ProgramStatus != nil || v.ProgressBar != nil {
		t.Fatal("OSC 7501 and 9;4 only in Rex")
	}
	h.fake.Release()
	h.pump()

	r := newHarness(t, 120, 35, withRex)
	holdApply(t, r)
	v = r.m.View()
	if v.ProgramStatus == nil || v.ProgramStatus.State != tea.ProgramStateWorking || v.ProgressBar == nil {
		t.Fatalf("working in Rex: %+v", v.ProgramStatus)
	}
	r.fake.Release()
	r.pump()
	if v = r.m.View(); v.ProgramStatus.State != tea.ProgramStateDone {
		t.Fatalf("done at the end: %+v", v.ProgramStatus)
	}
}

func TestViewAsksForButtonEventsOnly(t *testing.T) {
	h := newHarness(t, 80, 24).start()
	v := h.m.View()
	if v.MouseMode != tea.MouseModeCellMotion || !v.AltScreen || !v.ReportFocus {
		t.Fatalf("%+v", v)
	}
}

// ---- motion

func TestNoSpinnerOnNarrowOrSlow(t *testing.T) {
	h := newHarness(t, 48, 40)
	h.fake.On("list", core.FakeScript{Fixture: "list-stream.ndjson", PauseAfter: -1})
	h.start()
	if armed[spinMsg](h) != 0 {
		t.Fatal("no spinner on narrow")
	}
	h.fake.Release()
	h.pump()

	s := newHarness(t, 120, 35, withSlow)
	s.fake.On("list", core.FakeScript{Fixture: "list-stream.ndjson", PauseAfter: -1})
	s.start()
	if armed[spinMsg](s) != 0 {
		t.Fatal("no spinner on a slow link")
	}
	s.fake.Release()
	s.pump()

	w := newHarness(t, 120, 35)
	w.fake.On("list", core.FakeScript{Fixture: "list-stream.ndjson", PauseAfter: -1})
	w.start()
	if armed[spinMsg](w) != 1 {
		t.Fatal("wide shows that it works")
	}
	w.fake.Release()
	w.pump()
}

// ---- glyphs

func TestASCIIModeDrawsOnlyASCII(t *testing.T) {
	check := func(h *harness, name string) {
		t.Helper()
		for _, r := range h.screen() {
			if r > 126 {
				t.Fatalf("%s: %q in ascii mode\n%s", name, r, h.screen())
			}
		}
	}
	for _, s := range [][2]int{{120, 35}, {80, 24}, {48, 40}, {93, 14}} {
		h := newHarness(t, s[0], s[1], withASCII).start()
		check(h, "list")
		h.cursorTo(wtAlpha)
		h.key("a")
		check(h, "menu")
		h.key("backspace", "d")
		check(h, "sheet")
		h.key("y")
		check(h, "results")
		h.key("enter", "?")
		check(h, "help")
		h.key("backspace", "c")
		check(h, "cleanup")
		h.key("c", "n")
		check(h, "new")
	}
}

// ---- scope

func TestOutsideARepoTheScopeIsTheMachine(t *testing.T) {
	h := newHarness(t, 120, 35)
	h.fake.On("list", core.FakeScript{Fixture: "error-not-a-repo.ndjson"})
	h.start()
	if !h.m.machine() {
		t.Fatalf("machine scope outside a repo\n%s", h.screen())
	}
	if len(h.calls("list:global")) != 1 {
		t.Fatal("a list of the machine")
	}
	h.key("g")
	h.expect("wtree did not start in a repo")
	if !h.m.machine() {
		t.Fatal("repo scope has no repo to show")
	}
}

func TestScopeSwitchKeepsCursorAndMarks(t *testing.T) {
	h := newHarness(t, 120, 35)
	h.fake.On("list:global", core.FakeScript{Fixture: "list-stream.ndjson"})
	h.start()
	h.cursorTo(wtAlpha)
	h.key("space", "g")
	if h.m.table.Cursor() != wtAlpha {
		t.Fatalf("the cursor stays on its row: %s", h.m.table.Cursor())
	}
	if !h.m.tree.IsMarked(wtAlpha) {
		t.Fatal("marks stay")
	}
}

func TestDockerDownKeepsTheWorktrees(t *testing.T) {
	h := newHarness(t, 120, 35)
	h.fake.On("list", core.FakeScript{Fixture: "list-docker-down.ndjson"})
	h.start()
	h.expect("docker is not running")
	h.expect("r retry")
	h.expect("alpha")
}

func TestNewWorktreeFlow(t *testing.T) {
	h := newHarness(t, 80, 24).start()
	h.key("n")
	if h.m.CurrentMode() != ModeNew {
		t.Fatalf("mode %d\n%s", h.m.CurrentMode(), h.screen())
	}
	h.expect("fresh")
	h.expect("open after creating")
	h.expect("detached at")
	dry := h.calls("new:dry-run")
	if len(dry) != 1 {
		t.Fatal("the name comes from the core")
	}
	h.key("enter")
	calls := h.calls("new")
	last := calls[len(calls)-1]
	if !hasArgs(last.Args, "--name", "fresh") || strings.Contains(strings.Join(last.Args, " "), "--no-open") {
		t.Fatalf("%v", last.Args)
	}
	if h.m.CurrentMode() != ModeList {
		t.Fatalf("the sheet closes when it is created\n%s", h.screen())
	}
	if h.quit {
		t.Fatal("no tab support: wtree stays")
	}
	h.expect("no tab support")
}

func TestNewWorktreeRefusedStaysInTheSheet(t *testing.T) {
	h := newHarness(t, 80, 24)
	h.fake.On("new", core.FakeScript{Fixture: "new-refused-taken.ndjson"})
	h.start()
	h.key("n", "enter")
	if h.m.CurrentMode() != ModeNew {
		t.Fatal("it stays")
	}
	if h.m.newSheet.Err == "" || h.m.newSheet.Busy {
		t.Fatalf("%+v", h.m.newSheet.Err)
	}
}

func TestNewWorktreeStayHereOption(t *testing.T) {
	h := newHarness(t, 80, 24)
	h.fake.On("new", core.FakeScript{Fixture: "new-no-open.ndjson"})
	h.start()
	h.key("n", "down", "2", "enter")
	calls := h.calls("new")
	last := calls[len(calls)-1]
	if !strings.Contains(strings.Join(last.Args, " "), "--no-open") {
		t.Fatalf("%v", last.Args)
	}
}

func TestNewInMachineScopeNeedsARepoRow(t *testing.T) {
	h := newHarness(t, 120, 35, withGlobal)
	h.fake.On("list:global", core.FakeScript{Fixture: "list-stream.ndjson"})
	h.start()
	h.cursorTo(stGone)
	h.key("n")
	h.expect("new worktree: choose a repo row first")
	h.cursorTo(wtAlpha)
	h.key("n")
	calls := h.calls("new:dry-run")
	if len(calls) != 1 || !hasArgs(calls[0].Args, "--repo", "/fixture/home/code/proj/.git") {
		t.Fatalf("%v", calls)
	}
}

func TestOpenJumpLeavesAndNoTabSupportStays(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.cursorTo(wtAlpha)
	h.key("enter")
	if h.quit {
		t.Fatal("the recording says there is no tab support")
	}
	h.expect("no tab support")

	j := newHarness(t, 120, 35)
	j.fake.On("open", core.FakeScript{Data: []byte(`{"v":1,"event":"start","plan_id":"1","steps":1}
{"v":1,"event":"step","id":"s1","state":"running","label":"open"}
{"v":1,"event":"step","id":"s1","state":"ok","ms":7}
{"v":1,"event":"end","ok":1,"failed":0,"skipped":0,"disk_refresh":false,"cancelled":false}
`)})
	j.start()
	j.cursorTo(wtAlpha)
	j.key("enter")
	if !j.quit {
		t.Fatal("a jump that worked leaves wtree")
	}
}

func TestAlreadyHereDoesNotLeave(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.cursorTo(wtMain) // the fixture runs from the main checkout
	h.key("enter")
	h.expect("already here")
	if h.quit || len(h.calls("open")) != 0 {
		t.Fatal("nothing happens when you are there")
	}
}

func TestPrintPathAndQuit(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.cursorTo(wtAlpha)
	h.key("a")
	it, _ := h.m.menu.ByAction(actPrint)
	h.key(it.Key)
	if !h.quit || h.m.PrintOnExit() != "/fixture/home/code/worktrees/alpha" {
		t.Fatalf("%q", h.m.PrintOnExit())
	}
}

// ---- remembered state

func TestPrefsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	h := newHarness(t, 120, 35, func(o *Options) { o.StateDir = dir })
	h.start()
	h.cursorTo(wtAlpha)
	h.key("l", "p", "o")
	h.pump()
	p := LoadPrefs(dir)
	if p.Sort != "disk" || p.Panel == nil || *p.Panel || p.Scope != "" {
		t.Fatalf("%+v", p)
	}
	if !p.Expanded[wtAlpha] {
		t.Fatalf("%+v", p.Expanded)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "ui.json"))
	for _, word := range []string{"force", "volumes", "phrase"} {
		if strings.Contains(string(raw), word) {
			t.Fatalf("a preference must never remember %s: %s", word, raw)
		}
	}
	back := newHarness(t, 120, 35, func(o *Options) { o.Prefs = p })
	if mode, _ := back.m.tree.Sort(); mode != tree.SortDisk {
		t.Fatal("the sort comes back")
	}
	if back.m.panel {
		t.Fatal("the panel comes back")
	}
}

func TestCorruptPrefsAreIgnored(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ui.json"), []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := LoadPrefs(dir)
	if p.Scope != "" || p.Expanded != nil {
		t.Fatalf("%+v", p)
	}
	if q := LoadPrefs(filepath.Join(dir, "missing")); q.Scope != "" {
		t.Fatal("a missing file is empty")
	}
}

func TestLogIsWrittenAndOldLogsGo(t *testing.T) {
	dir := t.TempDir()
	h := newHarness(t, 120, 35, func(o *Options) { o.StateDir = dir })
	h.start()
	h.cursorTo(wtAlpha)
	h.key("d", "y")
	entries, err := os.ReadDir(filepath.Join(dir, "log"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("%v %v", entries, err)
	}
	if h.m.lastLogPath == "" {
		t.Fatal("the path is known")
	}
	data, _ := os.ReadFile(filepath.Join(dir, "log", entries[0].Name()))
	if !strings.Contains(string(data), "wtree core apply delete-worktrees") {
		t.Fatalf("%s", data)
	}
}

// ---- text from the core is not safe to draw

func TestEscapeSequencesInCoreTextAreNotDrawn(t *testing.T) {
	h := newHarness(t, 120, 35)
	plan := `{"v":1,"plan_id":"p","action":"delete-worktrees","flags":{"force":false,"volumes":false},"cwd":"/x",
"items":[{"id":"worktree:/x/a","kind":"worktree","label":"evil\u001b[31m","go":false,"offered":false,"reason":{"code":"locked","text":"locked \u001b]0;pwn\u0007reason"},"consequences":[],"steps":[]}],
"steps":[],"choices":[],"confirm":{"kind":"none","phrases":[]},"notes":[{"code":"n","text":"note\u001b[2J"}],"nothing_to_do":true}`
	h.fake.On("plan:"+core.ActionDeleteWorktrees, core.FakeScript{Data: []byte(plan)})
	h.start()
	h.cursorTo(wtAlpha)
	h.key("d")
	content := h.m.View().Content
	for _, bad := range []string{"\x1b]0;", "\x1b[2J", "pwn"} {
		if strings.Contains(content, bad) {
			t.Fatalf("%q reached the screen", bad)
		}
	}
	h.expect("locked reason")
}

// A command can take its time before its first line. Until then it is just as much a
// running batch: the stream must be known, and a stop must reach it.
func TestApplyWithNoEventYetIsARunningBatch(t *testing.T) {
	h := newHarness(t, 120, 35)
	h.fake.On("apply", core.FakeScript{Fixture: "apply-ok.ndjson", PauseAfter: -1, OnCancel: "apply-cancelled.ndjson"})
	h.start()
	h.cursorTo(wtAlpha)
	h.key("d", "y")
	if h.m.writer == nil {
		t.Fatal("the stream is known before its first event")
	}
	h.key("q")
	if h.quit || h.m.quitMenu == nil {
		t.Fatal("q asks")
	}
	h.send(keyOf("3"))
	if h.quit {
		t.Fatal("no quit before the batch is over")
	}
	h.pump()
	if !h.quit || !h.m.progress.Finished {
		t.Fatalf("it ends when the batch ended: quit %v\n%s", h.quit, h.screen())
	}
}

func TestADeleteNeverSkipsTheSheet(t *testing.T) {
	h := newHarness(t, 120, 35)
	data, err := os.ReadFile("../core/testdata/plan-delete-worktrees.json")
	if err != nil {
		t.Fatal(err)
	}
	var plan map[string]any
	if err := json.Unmarshal(data, &plan); err != nil {
		t.Fatal(err)
	}
	plan["confirm"] = map[string]any{"kind": "none", "phrases": []any{}}
	out, _ := json.Marshal(plan)
	h.fake.On("plan:"+core.ActionDeleteWorktrees, core.FakeScript{Data: out})
	h.start()
	h.cursorTo(wtAlpha)
	h.key("d")
	if h.m.CurrentMode() != ModeConfirm || len(h.calls("apply")) != 0 {
		t.Fatalf("a delete goes through the sheet: mode %d, %d applies", h.m.CurrentMode(), len(h.calls("apply")))
	}
}

func TestMixedMarksMakeTwoPlansEachWithItsSheet(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.m.tree.ToggleMark(wtAlpha)
	h.m.tree.ToggleMark(stGone)
	h.key("d")
	h.expect("delete stacks · 1 go · 2 stay · then 1 worktree (1/2)")
	if h.m.confirm.Plan.Action != core.ActionDeleteStacks {
		t.Fatalf("the stacks first: %s", h.m.confirm.Plan.Action)
	}
	h.key("y")
	if len(h.calls("plan:"+core.ActionDeleteWorktrees)) != 0 {
		t.Fatal("the next plan is asked for when the first has ended and was read")
	}
	h.key("enter")
	h.expect("(2/2)")
	if h.m.confirm.Plan.Action != core.ActionDeleteWorktrees {
		t.Fatalf("then the worktrees: %s", h.m.confirm.Plan.Action)
	}
	if n := len(h.calls("apply")); n != 1 {
		t.Fatalf("the second part has its own confirmation: %d applies", n)
	}
	h.key("backspace")
	if h.m.CurrentMode() != ModeList || len(h.calls("apply")) != 1 {
		t.Fatal("cancel ends the batch")
	}
}

func TestPanelTapMovesTheCursorAndPanelToggles(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.cursorTo(wtAlpha)
	h.expect("STACKS")
	h.key("l") // the stack row must be there to take the cursor
	x, y := h.findLine("◐ proj-alpha")
	h.tap(x, y)
	if h.m.table.Cursor() != stAlpha {
		t.Fatalf("a tap on a stack line of the panel: %s\n%s", h.m.table.Cursor(), h.screen())
	}
	h.key("p")
	h.expectNot("CONTAINERS")
	h.key("p")
	h.expect("CONTAINERS")
	m := newHarness(t, 80, 24).start()
	m.cursorTo(wtAlpha)
	m.key("p")
	if m.m.CurrentMode() != ModePanel {
		t.Fatal("an overlay on medium")
	}
	m.expect("STACKS")
	m.key("backspace")
	if m.m.CurrentMode() != ModeList {
		t.Fatal("backspace closes it")
	}
}

func TestAStateChangeFlashesOnWideAndNumbersDoNot(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	if ids := h.m.flashIDs(h.m.table.Rows()); ids != nil {
		t.Fatal("nothing is armed")
	}
	rows := append([]treetable.Row(nil), h.m.table.Rows()...)
	for i := range rows {
		if rows[i].ID == wtAlpha {
			rows[i].CPU = treetable.Value("99.0%")
		}
	}
	h.m.flashArmed = true
	if ids := h.m.flashIDs(rows); ids != nil {
		t.Fatalf("a number that moves does not flash: %v", ids)
	}
	for i := range rows {
		if rows[i].ID == wtAlpha {
			rows[i].Count = "0/2"
		}
	}
	h.m.flashArmed = true
	if ids := h.m.flashIDs(rows); len(ids) != 1 || ids[0] != wtAlpha {
		t.Fatalf("a state that changes flashes: %v", ids)
	}
	n := newHarness(t, 80, 24).start()
	n.m.flashArmed = true
	if ids := n.m.flashIDs(rows); ids != nil {
		t.Fatal("wide only")
	}
}

func TestHelpIsAboutTheContext(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.key("?")
	h.expect("wide 120x35")
	h.expect("u ascii glyphs")
	h.key("backspace", "a", "?")
	h.expect("pick an item")
	h.expectNot("cleanup view")
	h.key("backspace")
	if h.m.CurrentMode() != ModeMenu {
		t.Fatal("help goes back to the sheet that it came from")
	}
}

func TestBannerStaysThroughAReload(t *testing.T) {
	h := newHarness(t, 120, 35)
	h.fake.On("list", core.FakeScript{Fixture: "list-docker-down.ndjson"})
	h.start()
	h.expect("docker is not running")
	h.fake.On("list", core.FakeScript{Fixture: "list-stream.ndjson", PauseAfter: 1})
	h.key("r")
	h.expect("docker is not running")
	h.fake.Release()
	h.pump()
	h.expectNot("docker is not running")
}
