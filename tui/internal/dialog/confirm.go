package dialog

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/afruth/wtree/tui/internal/core"
	"github.com/afruth/wtree/tui/internal/theme"
	"github.com/afruth/wtree/tui/internal/tree"
	"github.com/afruth/wtree/tui/internal/treetable"
	"github.com/charmbracelet/x/ansi"
)

// PhraseMatches is the only leniency of the typed confirmation, so that a phone keyboard
// does not get in the way: it trims whitespace, and it ignores case and one trailing full
// stop. The core compares exactly, so the caller sends want and never what was typed.
func PhraseMatches(typed, want string) bool {
	return strings.EqualFold(normalizePhrase(typed), normalizePhrase(want))
}

func normalizePhrase(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, ".")
	return strings.TrimSpace(s)
}

// Focus is the button that has the focus in a sheet.
type Focus int

// The buttons. Focus starts on cancel, so a buffered enter cannot confirm.
const (
	FocusCancel Focus = iota
	FocusConfirm
)

// Option is one radio item of the sheet: a level, which asks for a new plan with other
// flags, or a choice of the plan.
type Option struct {
	Digit    string
	IsLevel  bool
	Index    int
	Label    string
	Selected bool
	Disabled bool
	Reason   string
	Tone     theme.Tone
}

// Confirm is the consequences sheet of one plan. The plan is what it shows: it adds no
// check, and it never computes a phrase.
type Confirm struct {
	Req     core.PlanRequest
	Plan    core.Plan
	Part    int // 1 of Parts, when the marks need more than one plan
	Parts   int
	Hidden  map[string]bool   // ids of marked rows that the filter hides
	Volumes map[string]int    // the volumes of stacks, by item id, as the list knows them
	Lonely  map[string]*int64 // stacks with only volumes left, by item id: their size, or nil
	Next    string            // what the next part of the batch is: "1 worktree"
	Note    string            // the first line of the sheet: the cursor row is not included
	Repos   map[string]string // in machine scope, the name of a repo by its common directory
	Choice  int

	Focus  Focus
	Scroll int
	Notice string // why this sheet came back: the plan went stale
	Said   string // the answer to the last key or tap, above the buttons: a sheet covers the status line
	Busy   bool   // a new plan is on its way

	Typing bool
	Typed  []string // the exact phrases so far
	Input  textinput.Model
	Wrong  string
}

// NewConfirm opens a sheet on a plan with the default choice and the focus on cancel.
func NewConfirm(req core.PlanRequest, plan core.Plan, hidden map[string]bool, volumes map[string]int) Confirm {
	c := Confirm{Req: req, Hidden: hidden, Volumes: volumes, Part: 1, Parts: 1}
	c.SetPlan(plan)
	return c
}

// SetPlan shows a new plan. The mildest choice is selected again, a phrase typed for the
// old plan is gone, and the focus goes back to cancel.
func (c *Confirm) SetPlan(plan core.Plan) {
	c.Plan = plan
	c.Choice = 0
	for i, ch := range plan.Choices {
		if ch.Default {
			c.Choice = i
			break
		}
	}
	c.Focus = FocusCancel
	c.Typing = false
	c.Typed = nil
	c.Wrong = ""
	c.Busy = false
	c.Scroll = 0
}

// Selected is the choice that is selected, or nil on a plan with no choices.
func (c Confirm) Selected() *core.Choice {
	if c.Choice >= 0 && c.Choice < len(c.Plan.Choices) {
		return &c.Plan.Choices[c.Choice]
	}
	return nil
}

func (c Confirm) stepItems() map[string]bool {
	out := map[string]bool{}
	ch := c.Selected()
	if ch == nil {
		return out
	}
	byID := make(map[string]core.Step, len(c.Plan.Steps))
	for _, s := range c.Plan.Steps {
		byID[s.ID] = s
	}
	for _, id := range ch.Steps {
		out[byID[id].Item] = true
	}
	return out
}

// idle says that the step of this item changes nothing at this choice: only volumes are left
// of the stack, and they stay. The plan says that: the list can be old.
func (c Confirm) idle(id string, lost map[string]bool) bool {
	if lost[id] {
		return false
	}
	for i := range c.Plan.Items {
		if c.Plan.Items[i].ID == id {
			return c.Plan.Items[i].OnlyVolumes
		}
	}
	return false
}

// itemLabel names an item. In machine scope a worktree has its repo before its name, as in the
// list: two repos can each have a worktree of this name, and this sheet is where it counts.
func (c Confirm) itemLabel(it core.Item) string {
	label := treetable.Sanitize(it.Label)
	if it.Kind == "worktree" && c.Repos != nil {
		if repo := treetable.Sanitize(c.Repos[it.Common]); repo != "" && repo != label {
			return repo + "/" + label
		}
	}
	return label
}

func (c Confirm) lostItems() map[string]bool {
	out := map[string]bool{}
	if ch := c.Selected(); ch != nil {
		for _, cons := range ch.Consequences {
			if cons.Code == "volumes_removed" {
				out[cons.Item] = true
			}
		}
	}
	return out
}

// allIdle says that this choice has steps, and that none of them would change a thing.
func (c Confirm) allIdle() bool {
	lost := c.lostItems()
	items := c.stepItems()
	for id := range items {
		if !c.idle(id, lost) {
			return false
		}
	}
	return len(items) > 0
}

// noVolumes says that the plan read the volumes of this stack, and that there are none: a level
// "with volumes" takes no data from it. A count that the plan does not give counts as a loss.
func noVolumes(cons core.Consequence) bool {
	return cons.Code == "volumes_removed" && cons.Count != nil && *cons.Count == 0
}

func (c Confirm) volumeLoss() (stacks, volumes int, bytes int64, known bool) {
	ch := c.Selected()
	if ch == nil {
		return 0, 0, 0, false
	}
	known = true
	for _, cons := range ch.Consequences {
		if cons.Code != "volumes_removed" || noVolumes(cons) {
			continue
		}
		stacks++
		if cons.Count != nil {
			volumes += *cons.Count
		}
		if cons.Bytes != nil {
			bytes += *cons.Bytes
		} else {
			known = false
		}
	}
	return stacks, volumes, bytes, known
}

// Tone is the colour of the level that the sheet is on: warn for a delete, danger for a
// force, loss when data goes, and accent for what can be undone. Loss means that data goes: the
// plan says how many volumes go, and with none it is a plain delete.
func (c Confirm) Tone() theme.Tone {
	if stacks, _, _, _ := c.volumeLoss(); stacks > 0 {
		return theme.ToneLoss
	}
	switch c.Plan.Action {
	case core.ActionStopStacks, core.ActionOpen:
		return theme.ToneAccent
	}
	if c.Plan.Flags.Force {
		return theme.ToneDanger
	}
	return theme.ToneWarn
}

// Plural writes a count with its noun: "1 stack", "2 stacks", "2 directories".
func Plural(n int, one string) string { return plural(n, one) }

func plural(n int, one string) string {
	switch {
	case n == 1:
		return "1 " + one
	case strings.HasSuffix(one, "y"):
		return strconv.Itoa(n) + " " + strings.TrimSuffix(one, "y") + "ies"
	}
	return strconv.Itoa(n) + " " + one + "s"
}

func (c Confirm) counts() (worktrees, stacks, dirs int) {
	lost := c.lostItems()
	for id := range c.stepItems() {
		if c.idle(id, lost) {
			continue
		}
		switch {
		case strings.HasPrefix(id, "worktree:"):
			worktrees++
		case strings.HasPrefix(id, "stack:"):
			stacks++
		default:
			dirs++
		}
	}
	return worktrees, stacks, dirs
}

// ConfirmLabel says what the confirm button does, in the words of what the choice runs.
func (c Confirm) ConfirmLabel() string {
	if c.Selected() == nil || c.Plan.NothingToDo || c.allIdle() {
		return "nothing to do"
	}
	worktrees, stacks, dirs := c.counts()
	var parts []string
	if worktrees > 0 {
		parts = append(parts, plural(worktrees, "worktree"))
	}
	if stacks > 0 {
		parts = append(parts, plural(stacks, "stack"))
	}
	verb := "delete"
	if c.Plan.Flags.Force && worktrees > 0 {
		verb = "force delete"
	}
	switch c.Plan.Action {
	case core.ActionStopStacks:
		verb = "stop"
	case core.ActionOpen:
		return "open " + plural(dirs, "directory")
	}
	if _, volumes, _, _ := c.volumeLoss(); volumes > 0 {
		parts = append(parts, plural(volumes, "volume"))
	}
	if len(parts) > 1 {
		return verb + " " + strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
	}
	return verb + " " + strings.Join(parts, "")
}

// noLevelHelps says that no level of this sheet can change the outcome: nothing goes, and the
// plan says that force would not take any of it either.
func (c Confirm) noLevelHelps() bool {
	if !c.Plan.NothingToDo || c.Plan.Action != core.ActionDeleteWorktrees {
		return false
	}
	if c.Plan.Flags.Force {
		return false // there is a milder level to go back to
	}
	for _, it := range c.Plan.Items {
		if it.ForceOK {
			return false
		}
	}
	return true
}

// Title is the sentence on the frame of the sheet: the request, and how many of it go and
// stay, so that it cannot disagree with the confirm button. Its dots are "·": Render puts the
// dot of the glyph set in.
func (c Confirm) Title() string { return c.titleFit(1 << 20) }

func (c Confirm) titleFit(room int) string {
	targets := 0
	first := ""
	for _, it := range c.Plan.Items {
		if it.Offered {
			continue
		}
		targets++
		if first == "" {
			first = c.itemLabel(it)
		}
	}
	// The numbers are the rows of the sheet, on every path, so the title cannot disagree with them.
	going, staying := c.sectionCounts()
	verb, noun := "delete", "worktree"
	switch c.Plan.Action {
	case core.ActionStopStacks:
		verb, noun = "stop", "stack"
	case core.ActionDeleteStacks:
		noun = "stack"
	case core.ActionOpen:
		verb, noun = "open", "directory"
	}
	title := verb + " " + plural(targets, noun)
	switch {
	case c.noLevelHelps() && targets == 1:
		title = "cannot delete " + first
	case c.noLevelHelps():
		title = "cannot delete " + plural(targets, noun)
	case (going != targets || staying > 0) && targets > 0:
		plain := noun + "s"
		if noun == "directory" {
			plain = "directories"
		}
		title = fmt.Sprintf("%s %s · %d go · %d stay", verb, plain, going, staying)
	}
	if c.Parts <= 1 {
		return title
	}
	part := fmt.Sprintf(" (%d/%d)", c.Part, c.Parts)
	if c.Next != "" && Width(title+part)+len(" · then ")+Width(c.Next) <= room {
		return title + " · then " + c.Next + part
	}
	return title + part
}

// sectionCounts counts the rows of the sheet: what goes, each item once, and what stays.
func (c Confirm) sectionCounts() (going, staying int) {
	goes, stays, hidden := c.Sections()
	seen := map[string]bool{}
	count := func(line Line) {
		if !seen[line.ID] {
			seen[line.ID] = true
			going++
		}
	}
	for _, line := range goes {
		count(line)
	}
	staying = len(stays)
	for _, line := range hidden {
		if line.Going {
			count(line)
		} else {
			staying++
		}
	}
	return going, staying
}

// LevelHint is the level that the sheet offers in place of a confirm button that has nothing to
// do: only volumes are left, and this level keeps them. It moves to that level and nothing
// else: that level is a plan of its own, with its own colour, button and phrase.
func (c Confirm) LevelHint() (Option, bool) {
	if c.Busy || !c.allIdle() {
		return Option{}, false
	}
	for _, o := range c.Options() {
		if o.IsLevel && !o.Disabled && !o.Selected {
			return o, true
		}
	}
	return Option{}, false
}

// CanConfirm says whether the plan has anything to run, and if not, why.
func (c Confirm) CanConfirm() (bool, string) {
	if c.Busy {
		return false, "wtree is reading the new plan"
	}
	if c.noLevelHelps() {
		return false, "nothing to do"
	}
	if c.Plan.NothingToDo || c.Selected() == nil || c.allIdle() {
		return false, "nothing to do at this level"
	}
	if !c.Selected().Available {
		return false, treetable.Sanitize(c.Selected().Reason)
	}
	return true, ""
}

// Phrases are what this choice asks to type, or none.
func (c Confirm) Phrases() []core.Phrase {
	if ch := c.Selected(); ch != nil && ch.Confirm.Kind == core.ConfirmPhrase {
		return ch.Confirm.Phrases
	}
	return nil
}

// Options lists the levels, then the choices. The digits are in that order.
func (c Confirm) Options() []Option {
	var out []Option
	n := 1
	digit := func() string {
		d := strconv.Itoa(n)
		n++
		return d
	}
	add := func(label string, on bool, force bool, tone theme.Tone, index int, disabled bool, reason string) {
		d := "-"
		if !disabled {
			d = digit()
		}
		out = append(out, Option{Digit: d, IsLevel: true, Index: index, Label: label, Selected: on == force, Disabled: disabled, Reason: reason, Tone: tone})
	}
	switch c.Plan.Action {
	case core.ActionDeleteWorktrees:
		anyForce := c.Plan.Flags.Force
		for _, it := range c.Plan.Items {
			if it.ForceOK {
				anyForce = true
			}
		}
		if c.noLevelHelps() {
			break // no choice to make: the reason comes first
		}
		add("delete", c.Plan.Flags.Force, false, theme.ToneWarn, 0, false, "")
		add("force", c.Plan.Flags.Force, true, theme.ToneDanger, 1, !anyForce, c.forceReason(anyForce))
	case core.ActionDeleteStacks:
		add("keep volumes", c.Plan.Flags.Volumes, false, theme.ToneWarn, 0, false, "")
		withVolumes := theme.ToneLoss
		if stacks, _, _, _ := c.volumeLoss(); c.Plan.Flags.Volumes && stacks == 0 {
			withVolumes = theme.ToneWarn // this plan read them, and no data goes
		}
		add("with volumes", c.Plan.Flags.Volumes, true, withVolumes, 1, false, "")
	}
	if len(c.Plan.Choices) > 1 {
		for i, ch := range c.Plan.Choices {
			d := "-"
			if ch.Available {
				d = digit()
			}
			tone := theme.ToneFg
			for _, cons := range ch.Consequences {
				if cons.Severe && !noVolumes(cons) {
					tone = theme.ToneLoss
				}
			}
			out = append(out, Option{Digit: d, Index: i, Label: treetable.Sanitize(ch.Text), Selected: i == c.Choice, Disabled: !ch.Available, Reason: treetable.Sanitize(ch.Reason), Tone: tone})
		}
	}
	return out
}

// forceReason says why the force level is not there, in what the plan says: every target goes
// without it, or force cannot take what stays.
func (c Confirm) forceReason(anyForce bool) string {
	if anyForce {
		return ""
	}
	for _, it := range c.Plan.Items {
		if !it.Go {
			return "force would not change this"
		}
	}
	return "nothing here needs force"
}

// OptionByDigit finds the option of a digit.
func (c Confirm) OptionByDigit(d string) (Option, bool) {
	for _, o := range c.Options() {
		if o.Digit == d {
			return o, true
		}
	}
	return Option{}, false
}

// Line is one affected item in a section of the sheet.
type Line struct {
	ID     string
	Label  string
	Kind   string
	Text   string
	Severe bool
	Going  bool // a line of HIDDEN BY FILTER: this item goes
}

func kindWord(it core.Item) string {
	if it.Kind == "dir" {
		return "directory"
	}
	return it.Kind
}

// consequenceText is what an item costs in few words. A code that it does not know keeps the
// sentence of the plan.
func consequenceText(it core.Item, notes map[string]bool) string {
	var parts []string
	for _, cons := range it.Consequences {
		switch {
		case cons.Code == "containers_removed":
			parts = append(parts, "containers, networks")
		case !notes[cons.Text] && cons.Text != "":
			parts = append(parts, treetable.Sanitize(cons.Text))
		}
	}
	return strings.Join(parts, " ")
}

func sizeOf(bytes *int64) string {
	if bytes == nil {
		return ""
	}
	return tree.FormatDisk(*bytes)
}

// volumeKind is the kind cell of a stack whose volumes go: "stack · 1 volume (1.5G)".
func volumeKind(cons core.Consequence) string {
	kind := "stack · " + plural(*cons.Count, "volume")
	if size := sizeOf(cons.Bytes); size != "" {
		kind += " (" + size + ")"
	}
	return kind
}

// noVolumesText is what goes of a stack with no volumes. The size is of the project: its
// containers, as the contract says.
func noVolumesText(cons core.Consequence, text string) string {
	const words = "containers, networks"
	if !strings.Contains(text, words) {
		text = strings.TrimSpace(words + " " + text)
	}
	if size := sizeOf(cons.Bytes); size != "" && *cons.Bytes > 0 {
		text = strings.Replace(text, words, words+" ("+size+")", 1)
	}
	return text
}

func stayingVolumes(n int) string {
	if n == 1 {
		return "its volume stays"
	}
	return "its " + plural(n, "volume") + " stay"
}

// Sections sorts the items of the plan under what goes, what stays and what is hidden by
// the filter, for the choice that is selected. It follows the plan and nothing else. A stack
// whose volumes go is one row, with the words in the colour of loss; a volume consequence
// that this cannot put in numbers keeps the sentence of the plan.
func (c Confirm) Sections() (goes, stays, hidden []Line) {
	notes := map[string]bool{}
	for _, n := range c.Plan.Notes {
		notes[n.Text] = true
	}
	runs := c.stepItems()
	lost := c.lostItems()
	loss := map[string]core.Consequence{}
	none := map[string]core.Consequence{}
	var extras []core.Consequence
	if ch := c.Selected(); ch != nil {
		for _, cons := range ch.Consequences {
			if noVolumes(cons) {
				none[cons.Item] = cons
			} else if cons.Code == "volumes_removed" && cons.Count != nil && runs[cons.Item] {
				loss[cons.Item] = cons
			} else {
				extras = append(extras, cons)
			}
		}
	}
	offeredBy := func(id string) string {
		for _, ch := range c.Plan.Choices {
			for _, add := range ch.Adds {
				if add == id && ch.Available {
					return treetable.Sanitize(ch.Text)
				}
			}
		}
		return ""
	}
	for _, it := range c.Plan.Items {
		label := c.itemLabel(it)
		line := Line{ID: it.ID, Label: label, Kind: kindWord(it)}
		switch {
		case it.Go && runs[it.ID] && c.idle(it.ID, lost):
			line.Text = "only volumes are left"
			if n := c.Volumes[it.ID]; n > 0 {
				line.Text = "only volumes are left, " + stayingVolumes(n)
			}
			stays = append(stays, line)
		case it.Go && runs[it.ID]:
			line.Text = consequenceText(it, notes)
			if note := treetable.Sanitize(it.Note); note != "" && !strings.Contains(line.Text, note) {
				line.Text = strings.TrimSpace(line.Text + " " + note)
			}
			if cons, ok := loss[it.ID]; ok {
				line.Kind, line.Text, line.Severe = volumeKind(cons), "data lost", true
			}
			if cons, ok := none[it.ID]; ok {
				line.Text = noVolumesText(cons, line.Text)
			}
			if c.Hidden[it.ID] {
				line.Going = true
				line.Text = "marked, not visible in the list; it goes"
				if line.Severe {
					line.Text += ", data lost"
				}
				hidden = append(hidden, line)
			} else {
				goes = append(goes, line)
			}
		case it.Go:
			line.Text = "stays"
			if by := offeredBy(it.ID); by != "" {
				line.Text = `choose "` + by + `"`
			}
			stays = append(stays, line)
		default:
			if it.Reason != nil {
				line.Text = treetable.Sanitize(it.Reason.Text)
			}
			if c.Hidden[it.ID] {
				line.Text = "marked, not visible in the list; it stays: " + line.Text
				hidden = append(hidden, line)
			} else {
				stays = append(stays, line)
			}
		}
	}
	for _, cons := range extras {
		goes = append(goes, Line{ID: cons.Item, Label: labelOfItem(c.Plan, cons.Item), Kind: "volumes", Text: treetable.Sanitize(cons.Text), Severe: cons.Severe})
	}
	// The volumes that a delete leaves are in STAYS, so nobody has to wonder.
	if c.Plan.Action != core.ActionOpen && c.Selected() != nil {
		for _, it := range c.Plan.Items {
			if it.Kind == "stack" && it.Go && runs[it.ID] && !lost[it.ID] && !c.idle(it.ID, lost) {
				if n := c.Volumes[it.ID]; n > 0 {
					stays = append(stays, Line{ID: it.ID, Label: treetable.Sanitize(it.Label), Kind: "stack", Text: stayingVolumes(n)})
				}
			}
		}
	}
	return goes, stays, hidden
}

// IdleText says why this level does nothing, or "".
func (c Confirm) IdleText(dot string) string {
	if !c.allIdle() {
		return ""
	}
	var total int64
	known := true
	for id := range c.stepItems() {
		if size := c.Lonely[id]; size != nil {
			total += *size
		} else {
			known = false
		}
	}
	text := "nothing goes at this level " + dot + " only volumes are left"
	if known {
		text += " (" + tree.FormatDisk(total) + ")"
	}
	return text // the button in place of confirm says where to go from here
}

// LossSummary is the one line above the field and the buttons when a choice is about volumes,
// or "": what it deletes, and how much of it the plan could put a size on. With room for less
// than the sentence it says the same in fewer words. It says "lost" only when data goes.
func (c Confirm) LossSummary(dot string, room int) string {
	stacks, _, bytes, known := c.volumeLoss()
	ch := c.Selected()
	if ch == nil || !c.aboutVolumes(ch) || c.ConfirmLabel() == "nothing to do" {
		return ""
	}
	if stacks == 0 {
		bytes, known = 0, true
		for _, cons := range ch.Consequences {
			if noVolumes(cons) && cons.Bytes != nil {
				bytes += *cons.Bytes
			}
		}
	}
	size := ""
	switch {
	case known && bytes > 0:
		size = " (" + tree.FormatDisk(bytes) + ")"
	case bytes > 0:
		size = " (at least " + tree.FormatDisk(bytes) + ")"
	}
	long := strings.Replace(c.ConfirmLabel(), "delete", "deletes", 1) + size
	short := strings.ReplaceAll(strings.ReplaceAll(strings.TrimPrefix(strings.TrimPrefix(c.ConfirmLabel(), "force "), "delete "), ", ", " + "), " and ", " + ") + size
	if stacks > 0 {
		long += " " + dot + " data lost"
		short += " lost"
	}
	if Width(long) <= room {
		return long
	}
	return short
}

func (c Confirm) aboutVolumes(ch *core.Choice) bool {
	for _, cons := range ch.Consequences {
		if cons.Code == "volumes_removed" {
			return true
		}
	}
	return false
}

func labelOfItem(p core.Plan, id string) string {
	for _, it := range p.Items {
		if it.ID == id {
			return treetable.Sanitize(it.Label)
		}
	}
	return treetable.Sanitize(id)
}

// StartTyping opens the field for the first phrase.
func (c *Confirm) StartTyping(th theme.Theme) tea.Cmd {
	c.Typing = true
	c.Typed = nil
	c.Wrong = ""
	c.Scroll = 0
	return c.newInput(th)
}

func (c *Confirm) newInput(th theme.Theme) tea.Cmd {
	c.Input = NewInput(th, "> ")
	return c.Input.Focus()
}

// Submit checks what was typed against the phrase that it stands for. done says that all
// phrases are in. Typed then holds the exact phrases of the core, never the typed text.
func (c *Confirm) Submit(th theme.Theme) (done bool, cmd tea.Cmd) {
	phrases := c.Phrases()
	i := len(c.Typed)
	if i >= len(phrases) {
		return true, nil
	}
	if !PhraseMatches(c.Input.Value(), phrases[i].Phrase) {
		c.Wrong = "expected: " + treetable.Sanitize(phrases[i].Phrase)
		return false, nil
	}
	c.Typed = append(c.Typed, phrases[i].Phrase)
	c.Wrong = ""
	if len(c.Typed) == len(phrases) {
		return true, nil
	}
	return false, c.newInput(th)
}

// Render draws the sheet.
func (c Confirm) Render(th theme.Theme, l theme.Layout) Frame {
	const want = 76
	s := shapeFor(l, want)
	inner := s.w - 4
	narrow := l.Tier == theme.TierNarrow
	tone := c.Tone()
	dot := th.G.Dot

	var body, head []Row
	// Up to 24 rows the headings carry the groups, and the rows go to what the sheet says.
	compact := l.Short || l.Height <= 24
	gap := func() {
		if !compact && (len(body) > 0 || len(head) > 0) {
			body = append(body, textRow(""))
		}
	}
	if c.Note != "" {
		for _, line := range Wrap(c.Note, inner) {
			body = append(body, textRow(th.Style(theme.ToneDim).Render(line)))
		}
	}
	if c.Notice != "" {
		for _, line := range Wrap(c.Notice, inner) {
			body = append(body, textRow(th.Bold(theme.ToneWarn).Render(line)))
		}
		gap()
	}
	if !c.Typing { // the digits go into the field now, and backspace goes back to them
		body = append(body, c.optionRows(th, inner, narrow)...)
	}
	if text := c.IdleText(dot); text != "" {
		gap()
		for _, line := range Wrap(text, inner) {
			body = append(body, textRow(th.Bold(theme.ToneWarn).Render(line)))
		}
	}

	// what is always in view: what is lost, and then the field or the buttons
	var under []Row
	if text := c.LossSummary(dot, inner); text != "" {
		summary := theme.ToneWarn
		if tone == theme.ToneLoss {
			summary = theme.ToneLoss
		}
		for _, line := range Wrap(text, inner) {
			under = append(under, textRow(th.Bold(summary).Render(line)))
		}
		if !c.Typing && !l.Short {
			under = append(under, textRow(""))
		}
	}
	if c.Typing {
		under = append(under, c.typingRows(th, inner)...)
	} else {
		if c.Said != "" {
			for _, line := range Wrap(c.Said, inner) {
				under = append(under, textRow(th.Style(theme.ToneDim).Render(line)))
			}
		}
		under = append(under, c.buttonRows(th, inner, tone, narrow, l.Height > 24)...)
	}

	goes, stays, hidden := c.Sections()
	var pinned []Row
	if c.Typing {
		// the borders, the heading, a blank, one row of the body, and what is under the lines
		room := l.Height - 4 - rowLines(under)
		if !l.Short {
			room--
		}
		goes, pinned = c.pinLoss(th, goes, inner, narrow, room)
	}
	columns := lineColumns(goes, stays, hidden, inner)
	section := func(title string, tone theme.Tone, lines []Line, action string) {
		if len(lines) == 0 {
			return
		}
		gap()
		heading := th.Bold(tone).Render(title)
		if action != "" {
			heading += th.Style(theme.ToneDim).Render("  h unmark them")
		}
		body = append(body, Row{Lines: []string{heading}, Action: action})
		for _, line := range lines {
			body = append(body, sectionRows(th, line, inner, narrow, columns)...)
		}
	}
	goesTone := tone
	if tone == theme.ToneAccent {
		goesTone = theme.ToneFg
	}
	if len(pinned) > 0 {
		// What goes comes first, and it stays there while the phrase is typed. The rest of it
		// goes on under the same heading, and scrolls.
		head = append([]Row{textRow(th.Bold(goesTone).Render("GOES"))}, pinned...)
		for _, line := range goes {
			body = append(body, sectionRows(th, line, inner, narrow, columns)...)
		}
	} else {
		section("GOES", goesTone, goes, "")
	}
	section("STAYS", theme.ToneOk, stays, "")
	section("HIDDEN BY FILTER", theme.ToneWarn, hidden, "hidden")

	// "The volumes stay." says what a line of STAYS says. Without that line it is the only word.
	stayLine := false
	for _, line := range stays {
		if strings.Contains(line.Text, "stay") {
			stayLine = true
		}
	}
	var notes []core.Note
	for _, n := range c.Plan.Notes {
		if n.Code == "volumes_stay" && stayLine {
			continue
		}
		notes = append(notes, n)
	}
	if len(notes) > 0 {
		gap()
		for _, n := range notes {
			for _, line := range Wrap(treetable.Sanitize(n.Text), inner) {
				body = append(body, textRow(th.Style(theme.ToneDim).Render(line)))
			}
		}
	}

	var foot []Row
	if !l.Short {
		foot = append(foot, textRow(""))
	}
	foot = append(foot, under...)
	f, _ := assembleSheet(th, l, s, sheetParts{title: strings.ReplaceAll(c.titleFit(max(s.w-6, 1)), "·", dot), tone: tone, head: head, body: body, foot: foot, scroll: c.Scroll, focus: -1})
	return f
}

// pinLoss takes the lines of volume loss out of what scrolls, and keeps them above the field,
// so that nobody types the phrase without them on the screen. Should they not all fit on this
// screen, the summary still has the totals, the rest scrolls, and a line says so.
func (c Confirm) pinLoss(th theme.Theme, goes []Line, inner int, narrow bool, room int) (rest []Line, pinned []Row) {
	var severe []Line
	for _, line := range goes {
		if line.Severe {
			severe = append(severe, line)
		} else {
			rest = append(rest, line)
		}
	}
	columns := lineColumns(severe, nil, nil, inner)
	var all [][]Row
	total := 0
	for _, line := range severe {
		rows := sectionRows(th, line, inner, narrow, columns)
		all = append(all, rows)
		total += rowLines(rows)
	}
	if total <= room {
		for _, rows := range all {
			pinned = append(pinned, rows...)
		}
		return rest, pinned
	}
	used := 1 // the line that says there is more
	for i, rows := range all {
		if used+rowLines(rows) > room {
			pinned = append(pinned, textRow(th.Style(theme.ToneLoss).Render(fmt.Sprintf("and %d more %s up down scroll", len(all)-i, th.G.Dot))))
			return append(severe[i:len(severe):len(severe)], rest...), pinned
		}
		pinned = append(pinned, rows...)
		used += rowLines(rows)
	}
	return rest, pinned
}

func rowLines(rows []Row) int {
	n := 0
	for _, r := range rows {
		n += len(r.Lines)
	}
	return n
}

func (c Confirm) optionRows(th theme.Theme, inner int, narrow bool) []Row {
	var rows []Row
	draw := func(o Option) string {
		mark := "(" + o.Digit + ")"
		if o.Disabled {
			mark = "(" + th.G.Kept + ")"
		}
		text := mark + " " + o.Label
		switch {
		case o.Disabled && o.IsLevel && narrow:
			return text // it stays on the row of its group, and a tap says why
		case o.Disabled:
			return strings.TrimRight(text+": "+o.Reason, ": ")
		case o.Selected && o.Tone == theme.ToneFg:
			return th.Button(theme.ToneAccent).Bold(true).Render(text)
		case o.Selected:
			return th.Button(o.Tone).Bold(true).Render(text)
		}
		return th.Style(o.Tone).Render(text)
	}
	// One group is one line of items side by side, and it wraps: a phone has no rows to spare.
	const indent = 6
	emit := func(heading string, opts []Option) {
		if len(opts) == 0 {
			return
		}
		line := th.Style(theme.ToneDim).Render(fitTo(heading, indent))
		x := indent
		var spans []Span
		flush := func() {
			rows = append(rows, Row{Lines: []string{line}, Spans: spans})
			line, x, spans = strings.Repeat(" ", indent), indent, nil
		}
		for _, o := range opts {
			text := draw(o)
			w := Width(text)
			if o.Disabled {
				parts := Wrap(text, inner-indent)
				if x > indent && x+w > inner {
					flush()
				}
				if len(parts) > 1 {
					if x > indent {
						flush()
					}
					for _, part := range parts {
						line += th.Style(theme.ToneDim).Render(part)
						rows = append(rows, Row{Lines: []string{line}, Action: optionAction(o)})
						line = strings.Repeat(" ", indent)
					}
					continue
				}
				text = th.Style(theme.ToneDim).Render(text)
			}
			if x > indent && x+w > inner {
				flush()
			}
			spans = append(spans, Span{x, x + w, optionAction(o)})
			line += text + "  "
			x += w + 2
		}
		if x > indent {
			flush()
		}
	}
	var levels, choices []Option
	for _, o := range c.Options() {
		if o.IsLevel {
			levels = append(levels, o)
		} else {
			choices = append(choices, o)
		}
	}
	emit("level", levels)
	emit("also", choices)
	return rows
}

func optionAction(o Option) string {
	if o.Disabled {
		kind := "choice"
		if o.IsLevel {
			kind = "level"
		}
		return "reason:" + kind + ":" + strconv.Itoa(o.Index)
	}
	return "option:" + o.Digit
}

// DisabledReason finds the reason of a disabled option from its zone action.
func (c Confirm) DisabledReason(action string) string {
	for _, o := range c.Options() {
		if o.Disabled && optionAction(o) == action {
			return o.Reason
		}
	}
	return ""
}

// sheetColumns are the widths of the name and the kind in a section, on wide and medium.
type sheetColumns struct{ name, kind int }

func lineColumns(goes, stays, hidden []Line, inner int) sheetColumns {
	cols := sheetColumns{name: 14, kind: 9}
	for _, lines := range [][]Line{goes, stays, hidden} {
		for _, line := range lines {
			cols.name = max(cols.name, Width(line.Label))
			cols.kind = max(cols.kind, Width(line.Kind))
		}
	}
	cols.kind = min(cols.kind, inner/3)
	cols.name = min(cols.name, max(inner/2, 14))
	return cols
}

func sectionRows(th theme.Theme, line Line, inner int, narrow bool, cols sheetColumns) []Row {
	ell := th.G.Ellipsis
	line.Kind = strings.ReplaceAll(line.Kind, "·", th.G.Dot)
	nameTone := theme.ToneFg
	textStyle := th.Style(theme.ToneDim)
	kindStyle := th.Style(theme.ToneDim)
	if line.Severe {
		nameTone = theme.ToneLoss
		textStyle = th.Style(theme.ToneLoss)
		kindStyle = th.Style(theme.ToneLoss)
	}
	name := th.Bold(nameTone)
	if narrow {
		// One row when it fits. A name is what the user reads before a delete, so it is not cut
		// for the rest: the rest goes on the next line.
		labelW := Width(line.Label)
		used := 2 + labelW + 2 + Width(line.Kind)
		if line.Text != "" {
			used += 2 + Width(line.Text)
		}
		if used <= inner {
			text := "  " + name.Render(line.Label) + "  " + kindStyle.Render(line.Kind)
			if line.Text != "" {
				text += "  " + textStyle.Render(line.Text)
			}
			return []Row{textRow(text)}
		}
		rows := []Row{textRow("  " + name.Render(treetable.TruncateStart(line.Label, inner-2, ell)))}
		rest := line.Kind
		if line.Text != "" {
			rest += "  " + line.Text
		}
		if Width(rest) <= inner-4 {
			second := "    " + kindStyle.Render(line.Kind)
			if line.Text != "" {
				second += "  " + textStyle.Render(line.Text)
			}
			return append(rows, textRow(second))
		}
		rows = append(rows, textRow("    "+kindStyle.Render(line.Kind)))
		for _, l := range Wrap(line.Text, inner-4) {
			rows = append(rows, textRow("    "+textStyle.Render(l)))
		}
		return rows
	}
	textW := max(inner-2-cols.name-1-cols.kind-2, 8)
	head := "  " + fitTo(name.Render(treetable.TruncateStart(line.Label, cols.name, ell)), cols.name) + " " + fitTo(kindStyle.Render(treetable.TruncateEnd(line.Kind, cols.kind, ell)), cols.kind) + "  "
	wrapped := Wrap(line.Text, textW)
	if len(wrapped) == 0 {
		return []Row{textRow(head)}
	}
	rows := []Row{textRow(head + textStyle.Render(wrapped[0]))}
	indent := strings.Repeat(" ", 2+cols.name+1+cols.kind+2)
	for _, l := range wrapped[1:] {
		rows = append(rows, textRow(indent+textStyle.Render(l)))
	}
	return rows
}

// buttonRows puts the two buttons on one row when they fit, at every width. Stacked on a phone
// with rows to spare, each is a target of two rows.
func (c Confirm) buttonRows(th theme.Theme, inner int, tone theme.Tone, narrow, tall bool) []Row {
	if c.noLevelHelps() {
		// no level helps, so there is nothing to confirm and nowhere to go: only the way back
		return []Row{{Lines: []string{button(th, "backspace cancel", theme.ToneDim, true)}, Action: "cancel"}}
	}
	can, reason := c.CanConfirm()
	hint, hasHint := c.LevelHint()
	cancelBtn := button(th, "backspace cancel", theme.ToneDim, c.Focus == FocusCancel && !hasHint)
	action := confirmAction(can)
	key, gap := "  ", "   "
	if narrow { // the cells that make the two fit on one row
		key, gap = " ", " "
	}
	var confirmBtn string
	switch {
	case hasHint:
		confirmBtn = button(th, hint.Digit+key+"choose "+hint.Label, theme.ToneAccent, true)
		action = optionAction(hint)
	case !can:
		confirmBtn = buttonOff(th, "- "+reason)
	case len(c.Phrases()) > 0:
		confirmBtn = button(th, "y"+key+"type to "+c.ConfirmLabel(), tone, c.Focus == FocusConfirm)
	default:
		confirmBtn = button(th, "y"+key+c.ConfirmLabel(), tone, c.Focus == FocusConfirm)
	}
	cw, kw := Width(cancelBtn), Width(confirmBtn)
	if cw+kw+len(gap) > inner {
		// a long label gets its own line
		if narrow && tall {
			return []Row{
				{Lines: []string{cancelBtn, ""}, Action: "cancel"},
				{Lines: []string{fitButton(confirmBtn, inner), ""}, Action: action},
			}
		}
		return []Row{
			{Lines: []string{cancelBtn}, Action: "cancel"},
			{Lines: []string{fitButton(confirmBtn, inner)}, Action: action},
		}
	}
	pad := max(inner-cw-kw-len(gap), 0)
	if narrow {
		pad = 0
	}
	line := strings.Repeat(" ", pad) + cancelBtn + gap + confirmBtn
	return []Row{{Lines: []string{line}, Spans: []Span{
		{pad, pad + cw, "cancel"},
		{pad + cw + len(gap), pad + cw + len(gap) + kw, action},
	}}}
}

func confirmAction(can bool) string {
	if can {
		return "confirm"
	}
	return "noop"
}

func fitButton(b string, inner int) string { return fitTo(b, inner) }

func (c Confirm) typingRows(th theme.Theme, inner int) []Row {
	phrases := c.Phrases()
	i := min(len(c.Typed), len(phrases)-1)
	if i < 0 {
		return nil
	}
	loss := th.Bold(theme.ToneLoss)
	if c.Tone() != theme.ToneLoss {
		loss = th.Bold(c.Tone())
	}
	var rows []Row
	// A long phrase wraps, all of it, in bold, and it is said once. A word of the phrase is never
	// cut off: what is on the line is what to type.
	phrase, hint := treetable.Sanitize(phrases[i].Phrase), treetable.Sanitize(phrases[i].Hint)
	if Width(phrase) > inner {
		hint = strings.Replace(hint, `"`+phrase+`"`, "this", 1)
	}
	for _, line := range Wrap(hint, inner) {
		rows = append(rows, textRow(th.Style(theme.ToneDim).Render(line)))
	}
	for _, line := range wrapPhrase(phrase, inner) {
		rows = append(rows, textRow(loss.Render(line)))
	}
	rows = append(rows, textRow(c.Input.View()))
	switch {
	case c.Wrong != "":
		rows = append(rows, textRow(th.Style(theme.ToneDanger).Render(c.Wrong)))
	case len(phrases) > 1:
		rows = append(rows, textRow(th.Style(theme.ToneDim).Render(fmt.Sprintf("phrase %d of %d", i+1, len(phrases)))))
	default:
		for _, line := range Wrap("enter confirms "+th.G.Dot+" empty backspace goes back", inner) {
			rows = append(rows, textRow(th.Style(theme.ToneDim).Render(line)))
		}
	}
	return rows
}

// wrapPhrase breaks a phrase by cells and keeps each of its characters, also its spaces: this is
// the text to type, and Wrap would close up a double space.
func wrapPhrase(phrase string, w int) []string {
	if w <= 0 || Width(phrase) <= w {
		return []string{phrase}
	}
	var out []string
	for Width(phrase) > w {
		out = append(out, ansi.Truncate(phrase, w, ""))
		phrase = ansi.Cut(phrase, Width(out[len(out)-1]), Width(phrase))
	}
	return append(out, phrase)
}

// NewInput makes a one-line field in the look of the theme, with no suggestions and a
// cursor that does not blink, so that a slow link does not redraw for it.
func NewInput(th theme.Theme, prompt string) textinput.Model {
	in := textinput.New()
	in.Prompt = prompt
	in.ShowSuggestions = false
	styles := textinput.DefaultStyles(!th.Light)
	text := lipgloss.NewStyle()
	if c := th.Color(theme.ToneFg); c != nil {
		text = text.Foreground(c)
	}
	styles.Focused.Text = text
	styles.Focused.Prompt = th.Style(theme.ToneAccent)
	styles.Blurred.Text = th.Style(theme.ToneDim)
	styles.Blurred.Prompt = th.Style(theme.ToneDim)
	styles.Cursor.Blink = false
	if c := th.Color(theme.ToneAccent); c != nil {
		styles.Cursor.Color = c
	}
	in.SetStyles(styles)
	return in
}
