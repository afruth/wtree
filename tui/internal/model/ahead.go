package model

import (
	"context"
	"reflect"

	tea "charm.land/bubbletea/v2"

	"github.com/afruth/wtree/tui/internal/core"
	"github.com/afruth/wtree/tui/internal/dialog"
	"github.com/afruth/wtree/tui/internal/treetable"
)

// aheadPlan is the plan of the other level of the sheet: delete or force, keep or with volumes.
// It is read while the user reads the sheet, so that a switch of the level does not wait for
// the core: a plan of many worktrees can take seconds. A plan only reads. Apply checks it again
// and refuses a plan that went stale, so a plan read ahead is as safe as one read on the key.
type aheadPlan struct {
	seq    int
	req    core.PlanRequest
	plan   core.Plan
	err    error
	done   bool
	cancel context.CancelFunc
}

type aheadMsg struct {
	seq  int
	plan core.Plan
	err  error
}

// keepAhead keeps the plan of the other level on its way while a sheet with levels is open,
// and drops it when the sheet goes: nothing reads in the background of an apply.
func (m *Model) keepAhead() tea.Cmd {
	if m.mode != ModeConfirm {
		m.dropAhead()
		return nil
	}
	c := &m.confirm
	if c.Busy && c.BusyLevel < 0 {
		return nil // a plan for other targets is on its way: what is ahead waits for it
	}
	other := 1 - c.Level()
	if c.Busy {
		other = c.BusyLevel
	}
	if !m.levelOffered(other) {
		m.dropAhead()
		return nil
	}
	req, _ := c.LevelRequest(other)
	if m.ahead != nil && reflect.DeepEqual(m.ahead.req, req) {
		return nil
	}
	return m.startAhead(req)
}

// levelOffered says that the sheet offers this level, and that it is not the one it shows.
func (m *Model) levelOffered(level int) bool {
	c := &m.confirm
	if _, ok := c.LevelRequest(level); !ok || (level == c.Level() && !c.Busy) {
		return false
	}
	for _, o := range c.Options() {
		if o.IsLevel && o.Index == level {
			return !o.Disabled
		}
	}
	return false
}

func (m *Model) startAhead(req core.PlanRequest) tea.Cmd {
	m.dropAhead()
	m.aheadSeq++
	ctx, cancel := context.WithCancel(m.ctx)
	m.ahead = &aheadPlan{seq: m.aheadSeq, req: req, cancel: cancel}
	seq, client := m.aheadSeq, m.client
	return func() tea.Msg {
		defer cancel()
		plan, err := client.Plan(ctx, req)
		return aheadMsg{seq: seq, plan: plan, err: err}
	}
}

func (m *Model) dropAhead() {
	if m.ahead != nil && m.ahead.cancel != nil {
		m.ahead.cancel()
	}
	m.ahead = nil
}

func (m *Model) onAhead(msg aheadMsg) tea.Cmd {
	a := m.ahead
	if a == nil || msg.seq != a.seq {
		return nil
	}
	a.done, a.plan, a.err, a.cancel = true, msg.plan, msg.err, nil
	c := &m.confirm
	if m.mode != ModeConfirm || !c.Busy || c.BusyLevel < 0 {
		return nil // it waits for a pick
	}
	if req, _ := c.LevelRequest(c.BusyLevel); !reflect.DeepEqual(req, a.req) {
		return nil
	}
	if a.err != nil {
		c.Busy, c.BusyLevel = false, -1
		c.Notice = treetable.Sanitize(a.err.Error())
		return nil
	}
	m.takeAhead()
	return nil
}

// pickLevel moves the sheet to another level. The pick shows at once. The plan of that level
// is often there already. If not, the sheet says that it reads it, and the level of the plan
// on the sheet takes the pick back.
func (m *Model) pickLevel(o dialog.Option) tea.Cmd {
	c := &m.confirm
	switch {
	case o.Index == c.Level():
		if c.Busy && c.BusyLevel >= 0 {
			c.Busy, c.BusyLevel = false, -1 // back: the plan on the sheet is the one it shows
		}
		return nil
	case c.Busy && c.BusyLevel == o.Index:
		return nil // it is on its way; a second press does not start it again
	case c.Busy:
		c.Said = "wtree is reading the new plan"
		return nil
	}
	req, _ := c.LevelRequest(o.Index)
	var cmd tea.Cmd
	if m.ahead == nil || !reflect.DeepEqual(m.ahead.req, req) || (m.ahead.done && m.ahead.err != nil) {
		cmd = m.startAhead(req)
	}
	if m.ahead.done {
		m.takeAhead()
		return nil
	}
	c.Busy, c.BusyLevel = true, o.Index
	c.Notice = ""
	return cmd
}

// takeAhead puts the plan of the other level on the sheet, and keeps the plan that was on it:
// a switch back is at once too.
func (m *Model) takeAhead() {
	c := &m.confirm
	a := m.ahead
	old := &aheadPlan{seq: a.seq, req: c.Req, plan: c.Plan, done: true}
	c.Req = a.req
	c.SetPlan(a.plan)
	c.Lonely = m.lonelyOf(a.plan)
	c.Notice = ""
	m.ahead = old
	m.armSheet()
}
