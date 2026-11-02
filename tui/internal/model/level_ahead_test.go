package model

import (
	"testing"
	"time"
)

// sheetOnly handles messages until the sheet is open, and no further: the plan of the other
// level, which the sheet asks for when it opens, stays on its way.
func (h *harness) sheetOnly() {
	h.t.Helper()
	deadline := time.After(2 * time.Second)
	for h.m.CurrentMode() != ModeConfirm {
		select {
		case msg := <-h.msgs:
			h.handle(msg)
		case <-deadline:
			h.t.Fatalf("no sheet:\n%s", h.screen())
		}
	}
	h.m.sheetHold = time.Time{}
}

func TestTheOtherLevelIsReadWhileTheSheetIsRead(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.cursorTo(wtAlpha)
	h.key("d")
	if n := len(h.calls("plan")); n != 2 || !hasArgs(h.calls("plan")[1].Args, "--force") {
		t.Fatalf("the force plan is read with the sheet open: %v", h.calls("plan"))
	}
	h.key("2")
	if !h.m.confirm.Plan.Flags.Force || h.m.confirm.Busy {
		t.Fatal("a level read ahead is on the sheet at once")
	}
	h.expect("force delete")
	h.key("1")
	if h.m.confirm.Plan.Flags.Force || h.m.confirm.Busy {
		t.Fatal("the plan of the level before is kept, and the way back is at once too")
	}
	if n := len(h.calls("plan")); n != 2 {
		t.Fatalf("a switch with both plans read asks for none: %d plans", n)
	}
}

func TestALevelOnItsWayShowsAtOnceAndSaysSo(t *testing.T) {
	for _, s := range [][2]int{{120, 35}, {48, 40}} {
		h := newHarness(t, s[0], s[1]).start()
		h.cursorTo(wtAlpha)
		h.rapid("d")
		h.sheetOnly()
		h.rapid("2")
		if !h.m.confirm.Busy || h.m.confirm.BusyLevel != 1 {
			t.Fatal("the pick waits for its plan")
		}
		h.expect("(2) force …")
		h.expect("Reading the force plan")
		h.rapid("2", "2")
		if n := len(h.calls("plan")); n != 2 {
			t.Fatalf("a second press starts the plan again: %d plans", n)
		}
		h.rapid("enter")
		if len(h.calls("apply")) != 0 {
			t.Fatal("enter applied while the plan of the pick was on its way")
		}
		h.pump()
		if !h.m.confirm.Plan.Flags.Force || h.m.confirm.Busy {
			t.Fatalf("the plan of the pick comes on the sheet:\n%s", h.screen())
		}
		h.expectNot("Reading the force plan")
	}
}

func TestTheLevelOnTheSheetTakesAPickBack(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.cursorTo(wtAlpha)
	h.rapid("d")
	h.sheetOnly()
	h.rapid("2", "1")
	if h.m.confirm.Busy {
		t.Fatal("1 goes back to delete while force is read")
	}
	h.expectNot("Reading the force plan")
	h.pump()
	if h.m.confirm.Plan.Flags.Force {
		t.Fatal("the force plan came after the user went back, and took the sheet")
	}
}

func TestNothingIsReadAheadDuringAnApply(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.cursorTo(wtAlpha)
	h.rapid("d")
	h.sheetOnly()
	if h.m.ahead == nil {
		t.Fatal("the sheet reads the other level")
	}
	h.m.mode = ModeList
	h.send(redrawMsg{})
	if h.m.ahead != nil {
		t.Fatal("the plan read ahead stays when its sheet is gone")
	}
}
