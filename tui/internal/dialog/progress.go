package dialog

import (
	"fmt"
	"strings"
	"time"

	"github.com/afruth/wtree/tui/internal/core"
	"github.com/afruth/wtree/tui/internal/theme"
	"github.com/afruth/wtree/tui/internal/treetable"
)

// StepState is what a row of the progress screen shows.
type StepState int

// The states of a row.
const (
	StepQueued StepState = iota
	StepWaiting
	StepRunning
	StepDone
	StepFailed
	StepSkipped
	StepKept
)

// ProgressRow is one item of the progress and results screen.
type ProgressRow struct {
	StepID string
	ItemID string
	Label  string
	Kind   string
	State  StepState
	Detail string   // what the state cell says
	Lines  []string // all output of the step, for the error detail
	Open   bool     // the detail is open
	MS     int64
	After  []string
}

// Progress is the batch screen. While it runs it updates in place; when it ends the same
// table is the results screen.
type Progress struct {
	Action  string
	Command string
	Rows    []ProgressRow
	Cursor  int
	Scroll  int

	Moved      bool // the user chose a row: the view stops following the running step
	Started    bool
	Finished   bool
	Cancelling bool
	End        *core.EndEvent
	Err        string // the stream did not end as the contract says
	Log        []string
}

// NewProgress prepares the rows from the choice of the plan: the steps it runs, then the
// items that the plan keeps with their reason.
func NewProgress(plan core.Plan, choice *core.Choice, command string) Progress {
	p := Progress{Action: plan.Action, Command: treetable.Sanitize(command)}
	p.Log = append(p.Log, "$ "+p.Command)
	items := make(map[string]core.Item, len(plan.Items))
	for _, it := range plan.Items {
		items[it.ID] = it
	}
	steps := make(map[string]core.Step, len(plan.Steps))
	for _, s := range plan.Steps {
		steps[s.ID] = s
	}
	running := map[string]bool{}
	if choice != nil {
		for _, id := range choice.Steps {
			s := steps[id]
			running[id] = true
			it := items[s.Item]
			label := treetable.Sanitize(it.Label)
			if label == "" {
				label = treetable.Sanitize(s.Label)
			}
			row := ProgressRow{StepID: id, ItemID: s.Item, Label: label, Kind: kindWord(it), After: s.After}
			row.State, row.Detail = StepQueued, "queued"
			p.Rows = append(p.Rows, row)
		}
	}
	for i := range p.Rows {
		for _, dep := range p.Rows[i].After {
			if running[dep] {
				p.Rows[i].State = StepWaiting
				p.Rows[i].Detail = waitingText(p.Rows[i].Kind)
				break
			}
		}
	}
	for _, it := range plan.Items {
		if it.Go {
			continue
		}
		text := "kept"
		if it.Reason != nil {
			text = "kept: " + treetable.Sanitize(it.Reason.Text)
		}
		p.Rows = append(p.Rows, ProgressRow{ItemID: it.ID, Label: treetable.Sanitize(it.Label), Kind: kindWord(it), State: StepKept, Detail: text})
	}
	return p
}

func waitingText(kind string) string {
	if kind == "worktree" {
		return "waiting for its stacks"
	}
	return "waiting for its worktree"
}

func (p *Progress) row(id string) *ProgressRow {
	for i := range p.Rows {
		if p.Rows[i].StepID == id {
			return &p.Rows[i]
		}
	}
	return nil
}

// Apply takes one event of the stream of an apply.
func (p *Progress) Apply(value any) {
	switch ev := value.(type) {
	case core.StartEvent:
		p.Started = true
	case core.StepEvent:
		r := p.row(ev.ID)
		label := ev.ID
		if r != nil {
			label = r.Label
		}
		switch ev.State {
		case core.StepRunning:
			p.Log = append(p.Log, "--- "+label)
			if r != nil {
				r.State, r.Detail = StepRunning, "working"+"…"
			}
		case core.StepOK:
			p.Log = append(p.Log, fmt.Sprintf("    ok in %s", seconds(ev.MS)))
			if r != nil {
				r.State, r.MS, r.Detail = StepDone, ev.MS, doneText(ev.MS)
			}
		case core.StepFailed:
			first := ""
			for _, line := range ev.Tail {
				if clean := strings.TrimSpace(treetable.Sanitize(line)); clean != "" {
					first = clean
					break
				}
			}
			if r == nil || len(r.Lines) == 0 {
				for _, line := range ev.Tail {
					p.Log = append(p.Log, "    "+treetable.Sanitize(line))
				}
			}
			p.Log = append(p.Log, fmt.Sprintf("    failed in %s (exit %d)", seconds(ev.MS), ev.Status))
			if r != nil {
				r.State, r.MS = StepFailed, ev.MS
				r.Detail = "failed"
				if first != "" {
					r.Detail = "failed: " + first
				}
				if len(r.Lines) == 0 {
					for _, line := range ev.Tail {
						r.Lines = append(r.Lines, treetable.Sanitize(line))
					}
				}
			}
		case core.StepSkipped:
			text := "skipped: cancelled"
			if ev.Because != "" && ev.Because != "cancelled" {
				dep := ev.Because
				if d := p.row(ev.Because); d != nil {
					dep = d.Label
				}
				text = "skipped: " + dep + " failed"
			}
			p.Log = append(p.Log, "--- "+label+": "+text)
			if r != nil {
				r.State, r.Detail = StepSkipped, text
			}
		}
	case core.OutputEvent:
		line := treetable.Sanitize(ev.Line)
		p.Log = append(p.Log, "    "+line)
		if r := p.row(ev.ID); r != nil {
			r.Lines = append(r.Lines, line)
			if r.State == StepRunning && strings.TrimSpace(line) != "" {
				r.Detail = line
			}
		}
	case core.EndEvent:
		end := ev
		p.End = &end
		p.Log = append(p.Log, fmt.Sprintf("end: %d ok, %d failed, %d skipped", ev.OK, ev.Failed, ev.Skipped))
	}
}

// Finish says that the stream is over.
func (p *Progress) Finish(result core.Result) {
	p.Finished = true
	if result.Err != nil {
		p.Err = treetable.Sanitize(result.Err.Error())
		p.Log = append(p.Log, "error: "+p.Err)
		for i := range p.Rows {
			switch p.Rows[i].State {
			case StepRunning:
				p.Rows[i].State, p.Rows[i].Detail = StepFailed, "failed: not known what it did, read the list"
			case StepQueued, StepWaiting:
				p.Rows[i].State, p.Rows[i].Detail = StepSkipped, "skipped: wtree stopped first"
			}
		}
	}
	if result.End != nil && p.End == nil {
		p.End = result.End
	}
	if p.End != nil && p.End.Cancelled {
		for i := range p.Rows {
			switch p.Rows[i].State {
			case StepQueued, StepWaiting:
				p.Rows[i].State, p.Rows[i].Detail = StepSkipped, "skipped: cancelled"
			}
		}
	}
	for i := range p.Rows {
		switch p.Rows[i].State {
		case StepQueued, StepWaiting:
			p.Rows[i].State, p.Rows[i].Detail = StepSkipped, "skipped: it did not run"
		}
	}
	if text := strings.TrimSpace(result.Stderr); text != "" {
		for _, line := range strings.Split(text, "\n") {
			p.Log = append(p.Log, "stderr: "+treetable.Sanitize(line))
		}
	}
}

// doneText leaves out a time that says nothing.
func doneText(ms int64) string {
	if ms < 100 {
		return "done"
	}
	return "done " + seconds(ms)
}

func seconds(ms int64) string {
	return fmt.Sprintf("%.1fs", time.Duration(ms*int64(time.Millisecond)).Seconds())
}

// Counts are what happened so far.
func (p Progress) Counts() (done, failed, skipped, kept, total int) {
	for _, r := range p.Rows {
		if r.StepID != "" {
			total++
		}
		switch r.State {
		case StepDone:
			done++
		case StepFailed:
			failed++
		case StepSkipped:
			skipped++
		case StepKept:
			kept++
		}
	}
	return done, failed, skipped, kept, total
}

// Fraction is how far the batch is, from 0 to 1.
func (p Progress) Fraction() float64 {
	done, failed, skipped, _, total := p.Counts()
	if total == 0 {
		return 1
	}
	return float64(done+failed+skipped) / float64(total)
}

// Verb is the running word of the action.
func (p Progress) Verb() string {
	switch p.Action {
	case core.ActionStopStacks:
		return "stopping"
	case core.ActionOpen:
		return "opening"
	}
	return "deleting"
}

// StatusText is the one line for the status of the host.
func (p Progress) StatusText() string {
	done, failed, skipped, _, total := p.Counts()
	return fmt.Sprintf("%s %d of %d", p.Verb(), min(done+failed+skipped+1, total), total)
}

// Succeeded lists the items whose steps all went well.
func (p Progress) Succeeded() []string {
	bad := map[string]bool{}
	for _, r := range p.Rows {
		if r.StepID != "" && r.State != StepDone {
			bad[r.ItemID] = true
		}
	}
	var out []string
	for _, r := range p.Rows {
		if r.StepID != "" && r.State == StepDone && !bad[r.ItemID] {
			out = append(out, r.ItemID)
		}
	}
	return out
}

// Unfinished lists the items that failed or did not run, for a retry.
func (p Progress) Unfinished() []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range p.Rows {
		if (r.State == StepFailed || r.State == StepSkipped) && !seen[r.ItemID] {
			seen[r.ItemID] = true
			out = append(out, r.ItemID)
		}
	}
	return out
}

// RetryKey is the key that does this batch again. A stop that failed is a stop again, and never
// a step up to a delete. What an open leaves has no marks to retry.
func (p Progress) RetryKey() string {
	switch p.Action {
	case core.ActionStopStacks:
		return "s"
	case core.ActionOpen:
		return ""
	}
	return "d"
}

// OK says that the batch ended with nothing failed.
func (p Progress) OK() bool {
	_, failed, skipped, _, _ := p.Counts()
	return p.Finished && p.Err == "" && failed == 0 && skipped == 0
}

// Move moves the selection.
func (p *Progress) Move(delta int) {
	p.Moved = true
	p.Cursor = min(max(p.Cursor+delta, 0), max(len(p.Rows)-1, 0))
}

// Toggle opens or closes the error detail of the selected row.
func (p *Progress) Toggle() {
	if p.Cursor >= 0 && p.Cursor < len(p.Rows) {
		p.Rows[p.Cursor].Open = !p.Rows[p.Cursor].Open
	}
}

// ToggleID opens or closes the error detail of a row by its index, from a tap.
func (p *Progress) ToggleID(i int) {
	if i >= 0 && i < len(p.Rows) {
		p.Cursor = i
		p.Toggle()
	}
}

// title has a colour for each count, because one failure does not make the rest fail. While the
// batch runs it is plain, and it turns red only once an item failed.
func (p Progress) title(th theme.Theme) string {
	done, failed, skipped, kept, total := p.Counts()
	if !p.Finished {
		text := fmt.Sprintf("%s %d of %d", p.Verb(), min(done+failed+skipped+1, total), total)
		if p.Cancelling {
			text += " " + th.G.Dot + " cancelling after current step"
		}
		tone := theme.ToneFg
		if failed > 0 {
			tone = theme.ToneDanger
		}
		return th.Bold(tone).Render(text)
	}
	doneTone := theme.ToneOk
	if done == 0 {
		doneTone = theme.ToneDim
	}
	parts := []string{th.Bold(doneTone).Render(fmt.Sprintf("%d done", done))}
	if kept > 0 {
		parts = append(parts, th.Bold(theme.ToneFg).Render(fmt.Sprintf("%d kept", kept)))
	}
	if failed > 0 {
		parts = append(parts, th.Bold(theme.ToneDanger).Render(fmt.Sprintf("%d failed", failed)))
	}
	if skipped > 0 {
		parts = append(parts, th.Bold(theme.ToneWarn).Render(fmt.Sprintf("%d skipped", skipped)))
	}
	return strings.Join(parts, th.Style(theme.ToneDim).Render(" "+th.G.Dot+" "))
}

func (p Progress) bar(th theme.Theme, w int) string {
	full := int(p.Fraction()*float64(w) + 0.5)
	tone := theme.ToneAccent
	_, failed, _, _, _ := p.Counts()
	if failed > 0 {
		tone = theme.ToneDanger
	}
	return th.Style(tone).Render(strings.Repeat(th.G.BarFull, full)) + th.Style(theme.ToneDim).Render(strings.Repeat(th.G.BarEmpty, w-full))
}

// Render draws the screen. It is full width on narrow and on a short screen, and a large
// overlay on the others, so the list stays visible behind it on wide.
func (p Progress) Render(th theme.Theme, l theme.Layout) Frame {
	const want = 82 // it stops short of the panel at 120 columns, which stays beside it
	inner := shapeFor(l, want).w - 4
	narrow := l.Tier == theme.TierNarrow
	ell := th.G.Ellipsis

	var body []Row
	if !p.Finished { // at the end the title is the summary
		body = append(body, textRow(p.bar(th, min(max(inner/3, 10), inner))))
	}
	focus := -1
	follow := p.Cursor
	if !p.Finished && !p.Moved {
		// a long batch on a phone: the step that runs stays in view
		for i, r := range p.Rows {
			if r.State == StepRunning {
				follow = i
				break
			}
		}
	}
	for i, r := range p.Rows {
		if i == follow {
			focus = len(body)
		}
		body = append(body, p.rowRows(th, i, r, inner, narrow, ell)...)
	}
	if p.Err != "" {
		for _, line := range Wrap(p.Err, inner) {
			body = append(body, textRow(th.Style(theme.ToneDanger).Render(line)))
		}
	}

	foot := []Row{textRow("")}
	if p.Finished {
		_, failed, skipped, _, _ := p.Counts()
		if retry := p.RetryKey(); failed+skipped > 0 && retry != "" {
			foot = append(foot, textRow(th.Style(theme.ToneDim).Render(treetable.TruncateEnd("what did not finish stays marked: "+retry+" retries", inner, ell))))
		}
		if p.End != nil && p.End.DiskRefresh {
			foot = append(foot, textRow(th.Style(theme.ToneDim).Render(treetable.TruncateEnd("disk sizes update in the background", inner, ell))))
		}
		a, b := button(th, "L log", theme.ToneDim, false), button(th, "enter back", theme.ToneAccent, true)
		ax, bx := 0, Width(a)+2
		foot = append(foot, Row{Lines: []string{a + "  " + b}, Spans: []Span{{ax, ax + Width(a), "log"}, {bx, bx + Width(b), "close"}}})
	} else {
		label := "backspace stop"
		if p.Cancelling {
			label = "stopping after this step"
		}
		a, b := button(th, "L log", theme.ToneDim, false), button(th, label, theme.ToneWarn, false)
		ax, bx := 0, Width(a)+2
		foot = append(foot, Row{Lines: []string{a + "  " + b}, Spans: []Span{{ax, ax + Width(a), "log"}, {bx, bx + Width(b), "stop"}}})
	}
	f, _ := assembleSheet(th, l, shapeFor(l, want), sheetParts{styled: p.title(th), body: body, foot: foot, scroll: p.Scroll, focus: focus})
	return f
}

func (p Progress) glyph(th theme.Theme, r ProgressRow) (string, theme.Tone) {
	g := th.G
	switch r.State {
	case StepRunning:
		return g.InProgress, theme.ToneAccent
	case StepDone:
		return g.Done, theme.ToneOk
	case StepFailed:
		return g.Failed, theme.ToneDanger
	case StepSkipped:
		return g.Down, theme.ToneWarn
	case StepKept:
		return g.Kept, theme.ToneDim
	}
	return g.Loading, theme.ToneDim
}

func (p Progress) rowRows(th theme.Theme, i int, r ProgressRow, inner int, narrow bool, ell string) []Row {
	glyph, tone := p.glyph(th, r)
	action := fmt.Sprintf("row:%d", i)
	selected := i == p.Cursor && (p.Finished || p.Moved)
	detailTone := theme.ToneDim
	switch r.State {
	case StepFailed:
		detailTone = theme.ToneDanger
	case StepSkipped:
		detailTone = theme.ToneWarn
	case StepDone:
		detailTone = theme.ToneOk
	case StepRunning:
		detailTone = theme.ToneAccent
	}
	lead := fitTo(glyph, 2) + " "
	var lines []string
	if narrow {
		name := treetable.TruncateStart(r.Label, inner-3-Width(r.Kind)-1, ell)
		first := th.Style(tone).Render(lead) + th.Bold(theme.ToneFg).Render(name) + " " + th.Style(theme.ToneDim).Render(r.Kind)
		if selected {
			first = th.Cursor().Render(fitTo(lead+name+" "+r.Kind, inner))
		}
		lines = []string{first}
		// A failure is two lines before it is cut, and a tap on the row shows all of it.
		detail := Wrap(r.Detail, inner-3)
		if len(detail) > 2 {
			detail = []string{detail[0], treetable.TruncateEnd(strings.Join(detail[1:], " "), inner-3, ell)}
		}
		for _, d := range detail {
			if selected {
				lines = append(lines, th.Cursor().Render(fitTo("   "+d, inner)))
			} else {
				lines = append(lines, "   "+th.Style(detailTone).Render(d))
			}
		}
	} else {
		nameW := min(max(inner/3, 14), 26)
		kindW := 9
		detailW := max(inner-3-nameW-1-kindW-1, 8)
		name := treetable.TruncateStart(r.Label, nameW, ell)
		detail := treetable.TruncateEnd(r.Detail, detailW, ell)
		var more string
		if r.State == StepFailed && Width(r.Detail) > detailW {
			// A failure is two lines before it is cut, as on narrow.
			parts := Wrap(r.Detail, detailW)
			detail = parts[0]
			more = treetable.TruncateEnd(strings.Join(parts[1:], " "), detailW, ell)
		}
		indent := strings.Repeat(" ", 3+nameW+1+kindW+1)
		if selected {
			lines = []string{th.Cursor().Render(fitTo(lead+fitTo(name, nameW)+" "+fitTo(r.Kind, kindW)+" "+detail, inner))}
			if more != "" {
				lines = append(lines, th.Cursor().Render(fitTo(indent+more, inner)))
			}
		} else {
			lines = []string{th.Style(tone).Render(lead) + fitTo(th.Bold(theme.ToneFg).Render(name), nameW) + " " + fitTo(th.Style(theme.ToneDim).Render(r.Kind), kindW) + " " + th.Style(detailTone).Render(detail)}
			if more != "" {
				lines = append(lines, indent+th.Style(detailTone).Render(more))
			}
		}
	}
	if r.Open {
		shown := r.Lines
		if len(shown) == 0 {
			shown = []string{"no output"}
		}
		for _, line := range shown {
			for _, wrapped := range Wrap(line, inner-4) {
				lines = append(lines, "    "+th.Style(theme.ToneDim).Render(wrapped))
			}
		}
	}
	return []Row{{Lines: lines, Action: action}}
}

// LogView is the command log of the last batch.
type LogView struct {
	Title  string
	Path   string
	Lines  []string
	Scroll int
}

// Render draws the log, with the end of it in view unless the user scrolled.
func (v LogView) Render(th theme.Theme, l theme.Layout) Frame {
	const want = 92
	inner := shapeFor(l, want).w - 4
	var body []Row
	for _, line := range v.Lines {
		indent := len(line) - len(strings.TrimLeft(line, " "))
		pad := strings.Repeat(" ", indent)
		wrapped := Wrap(line, max(inner-indent, 8))
		if len(wrapped) == 0 {
			body = append(body, textRow(""))
		}
		for _, w := range wrapped {
			tone := theme.ToneDim
			if strings.HasPrefix(line, "$") || strings.HasPrefix(line, "---") {
				tone = theme.ToneFg
			}
			body = append(body, textRow(th.Style(tone).Render(pad+w)))
		}
	}
	var foot []Row
	if v.Path != "" {
		foot = append(foot, textRow(th.Style(theme.ToneDim).Render(treetable.TruncateStart(v.Path, inner, th.G.Ellipsis))))
	}
	b := button(th, "backspace back", theme.ToneAccent, true)
	foot = append(foot, Row{Lines: []string{b}, Spans: []Span{{0, Width(b), "close"}}})
	f, _ := assemble(th, l, want, v.Title, theme.ToneFg, body, foot, v.Scroll, -1)
	return f
}
