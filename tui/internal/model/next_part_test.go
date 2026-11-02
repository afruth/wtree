package model

import (
	"testing"

	"github.com/afruth/wtree/tui/internal/core"
)

// The second part of a batch of mixed marks is not lost to keys and taps that come while its
// plan is read, and the results of the first part say that it comes.
func TestTheNextPartIsNotLostWhileItsPlanIsRead(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.m.tree.ToggleMark(wtAlpha)
	h.m.tree.ToggleMark(stGone)
	h.key("d")
	h.key("y")
	h.expect("next: 1 worktree · part 2 of 2")
	h.expect("enter next")
	h.rapid("enter", "j", "x", "space", "enter")
	x, y := h.findLine(" beta")
	h.tap(x+3, y)
	h.pump()
	if h.m.CurrentMode() != ModeConfirm || h.m.confirm.Plan.Action != core.ActionDeleteWorktrees {
		t.Fatalf("part 2 was lost: mode %d\n%s", h.m.CurrentMode(), h.screen())
	}
	h.expect("(2/2)")
	if n := len(h.calls("apply")); n != 1 {
		t.Fatalf("a key while the plan was read answered its sheet: %d applies", n)
	}
}

// Cancelling the next part while its plan is read says so in words, not only "cancelled".
func TestCancellingTheNextPartSaysWhatDidNotRun(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.m.tree.ToggleMark(wtAlpha)
	h.m.tree.ToggleMark(stGone)
	h.key("d")
	h.key("y")
	h.rapid("enter", "backspace")
	h.pump()
	if h.m.CurrentMode() != ModeList {
		t.Fatalf("mode %d\n%s", h.m.CurrentMode(), h.screen())
	}
	h.expect("part 2 of 2 cancelled: 1 worktree not deleted")
}

// Keys that came while the plan was read: the sheet holds its confirm a full second, and says why.
func TestKeysWhileThePlanIsReadHoldTheSheetLonger(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.cursorTo(wtAlpha)
	h.rapid("d", "/", "p")
	h.pump()
	if h.m.CurrentMode() != ModeConfirm {
		t.Fatalf("mode %d\n%s", h.m.CurrentMode(), h.screen())
	}
	h.expect("were ignored")
	h.letGo() // past the usual hold, still inside the longer one
	h.rapid("y")
	h.pump()
	if n := len(h.calls("apply")); n != 0 {
		t.Fatalf("a y soon after the sheet came confirmed: %d applies", n)
	}
}

// The scope control is the key g and the two scopes: a tap on g switches, a tap on a scope picks it.
func TestATapOnTheScopeKeySwitchesTheScope(t *testing.T) {
	h := newHarness(t, 120, 35)
	h.fake.On("list:global", core.FakeScript{Fixture: "list-stream.ndjson"})
	h.start()
	_, zones := h.m.header()
	var toggle, repo *struct{ x, y int }
	for _, z := range zones {
		switch z.Action {
		case "scope:toggle":
			toggle = &struct{ x, y int }{z.X0, z.Y0}
		case "scope:repo":
			repo = &struct{ x, y int }{z.X0 + 1, z.Y0}
		}
	}
	if toggle == nil || repo == nil {
		t.Fatalf("the header has no scope control: %+v", zones)
	}
	h.expect(" g ")
	h.tap(toggle.x, toggle.y)
	if !h.m.machine() {
		t.Fatalf("a tap on g did not switch to the machine\n%s", h.screen())
	}
	h.tap(repo.x, repo.y)
	if h.m.machine() {
		t.Fatalf("a tap on repo did not pick the repo\n%s", h.screen())
	}
}
