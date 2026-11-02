package model

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/afruth/wtree/tui/internal/core"
)

// listWithBetaCurrent is a list whose current worktree comes third in scan order.
func listWithBetaCurrent(t *testing.T) []byte {
	t.Helper()
	data, err := fs.ReadFile(core.Fixtures(), "list-stream.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		switch {
		case strings.Contains(line, `"event":"worktree"`) && strings.Contains(line, `"current":true`):
			lines[i] = strings.Replace(line, `"current":true`, `"current":false`, 1)
		case strings.Contains(line, `"path":"/fixture/home/code/worktrees/beta"`) && strings.Contains(line, `"event":"worktree"`):
			lines[i] = strings.Replace(line, `"current":false`, `"current":true`, 1)
		}
	}
	return []byte(strings.Join(lines, "\n"))
}

func TestFirstCursorIsTheCurrentWorktreeWhateverCameFirst(t *testing.T) {
	h := newHarness(t, 120, 35)
	// the stream stops after the first worktree, so that row is drawn before the others come
	h.fake.On("list", core.FakeScript{Data: listWithBetaCurrent(t), PauseAfter: 3})
	h.start()
	h.fake.Release()
	h.pump()
	if got := h.m.table.Cursor(); got != wtBeta {
		t.Fatalf("the cursor starts on the current worktree, not on the first row that arrived: %s\n%s", got, h.screen())
	}
}

func TestAMoveDuringTheFirstLoadStays(t *testing.T) {
	h := newHarness(t, 120, 35)
	h.fake.On("list", core.FakeScript{Data: listWithBetaCurrent(t), PauseAfter: 5})
	h.start()
	h.key("j")
	moved := h.m.table.Cursor()
	h.fake.Release()
	h.pump()
	if got := h.m.table.Cursor(); got != moved {
		t.Fatalf("the load took the cursor from %s to %s", moved, got)
	}
}

func TestAReloadLeavesTheCursorAlone(t *testing.T) {
	h := newHarness(t, 120, 35).start()
	h.cursorTo(wtAlpha)
	h.key("r")
	h.pump()
	if got := h.m.table.Cursor(); got != wtAlpha {
		t.Fatalf("a reload moved the cursor to %s", got)
	}
}
