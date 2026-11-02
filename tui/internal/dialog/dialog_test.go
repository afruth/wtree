package dialog

import (
	"strings"
	"testing"

	"github.com/afruth/wtree/tui/internal/core"
	"github.com/afruth/wtree/tui/internal/theme"
)

func TestWrap(t *testing.T) {
	got := Wrap("one two three four", 9)
	if strings.Join(got, "|") != "one two|three|four" {
		t.Fatalf("%q", got)
	}
	if got := Wrap("abcdefghij", 4); strings.Join(got, "|") != "abcd|efgh|ij" {
		t.Fatalf("%q", got)
	}
	if Wrap("x", 0) != nil {
		t.Fatal("no width, no lines")
	}
}

func TestMenuNumbersEnabledItemsAndEndsInCancel(t *testing.T) {
	m := NewMenu("t", "", []Item{
		{Label: "a", Action: "a", Disabled: true, Reason: "no"},
		{Label: "b", Action: "b"},
	})
	if m.Items[0].Key != "-" || m.Items[1].Key != "1" {
		t.Fatalf("%+v", m.Items)
	}
	if last := m.Items[len(m.Items)-1]; last.Key != "0" || last.Action != CancelAction {
		t.Fatalf("%+v", last)
	}
	if m.Current().Action != "b" {
		t.Fatal("the cursor starts on the first enabled item")
	}
}

func TestFrameHitIsInScreenCoordinates(t *testing.T) {
	th := theme.New(theme.Options{Profile: theme.Mono})
	m := NewMenu("t", "", []Item{{Label: "a", Action: "a"}})
	f := m.Render(th, theme.LayoutFor(120, 35))
	action, inside := f.Hit(f.X+1, f.Y+1)
	if action != "item:a" || !inside {
		t.Fatalf("%q %v", action, inside)
	}
	if _, inside := f.Hit(0, 0); inside {
		t.Fatal("outside the frame")
	}
}

func TestOfferedItemStaysUntilAChoiceTakesIt(t *testing.T) {
	plan := core.Plan{
		PlanID: "p", Action: core.ActionDeleteWorktrees,
		Items: []core.Item{
			{ID: "worktree:/a", Kind: "worktree", Label: "a", Go: true, Steps: []string{"s1"}},
			{ID: "stack:s", Kind: "stack", Label: "s", Go: true, Offered: true, Steps: []string{"s2"}},
		},
		Steps: []core.Step{{ID: "s1", Item: "worktree:/a"}, {ID: "s2", Item: "stack:s"}},
		Choices: []core.Choice{
			{ID: "w", Text: "worktrees only", Default: true, Available: true, Steps: []string{"s1"}},
			{ID: "ws", Text: "and stacks", Available: true, Adds: []string{"stack:s"}, Steps: []string{"s1", "s2"}},
		},
	}
	c := NewConfirm(core.PlanRequest{}, plan, nil, map[string]int{"stack:s": 2})
	goes, stays, _ := c.Sections()
	if len(goes) != 1 || len(stays) != 1 || !strings.Contains(stays[0].Text, "and stacks") {
		t.Fatalf("%+v %+v", goes, stays)
	}
	c.Choice = 1
	goes, stays, _ = c.Sections()
	if len(goes) != 2 {
		t.Fatalf("%+v", goes)
	}
	if len(stays) != 1 || stays[0].Text != "its 2 volumes stay" {
		t.Fatalf("the volumes that a delete leaves are said: %+v", stays)
	}
	if c.ConfirmLabel() != "delete 1 worktree and 1 stack" {
		t.Fatal(c.ConfirmLabel())
	}
}

func TestProgressEndsEveryRow(t *testing.T) {
	plan := core.Plan{
		Action: core.ActionDeleteStacks,
		Items:  []core.Item{{ID: "stack:a", Kind: "stack", Label: "a", Go: true}, {ID: "stack:b", Kind: "stack", Label: "b", Go: true}},
		Steps:  []core.Step{{ID: "s1", Item: "stack:a"}, {ID: "s2", Item: "stack:b", After: []string{"s1"}}},
	}
	ch := core.Choice{Steps: []string{"s1", "s2"}}
	p := NewProgress(plan, &ch, "wtree core apply")
	if p.Rows[1].State != StepWaiting {
		t.Fatalf("%+v", p.Rows[1])
	}
	p.Apply(core.StepEvent{ID: "s1", State: core.StepRunning})
	p.Apply(core.OutputEvent{ID: "s1", Line: "stopping \x1b[31mcontainers"})
	if p.Rows[0].Detail != "stopping containers" {
		t.Fatalf("output is sanitized and shown in place: %q", p.Rows[0].Detail)
	}
	p.Apply(core.StepEvent{ID: "s1", State: core.StepFailed, Tail: []string{"", "volume is in use"}, Status: 1})
	p.Apply(core.StepEvent{ID: "s2", State: core.StepSkipped, Because: "s1"})
	p.Finish(core.Result{})
	if p.Rows[0].Detail != "failed: volume is in use" || p.Rows[1].Detail != "skipped: a failed" {
		t.Fatalf("%q %q", p.Rows[0].Detail, p.Rows[1].Detail)
	}
	if got := p.Unfinished(); len(got) != 2 {
		t.Fatalf("%v", got)
	}
	if p.OK() {
		t.Fatal("not OK")
	}
}
