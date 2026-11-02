// Package model is the Bubble Tea application of wtree: one screen, one tree-table, and
// the sheets on top of it. Update does no I/O: everything that touches the world is a
// tea.Cmd, and a Cmd only returns a message. View draws from the state. There is no
// global state, and all policy is in the plans of the core: this package shows a plan and
// sends a choice back.
package model

import (
	"context"
	"sync/atomic"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/afruth/wtree/tui/internal/core"
	"github.com/afruth/wtree/tui/internal/dialog"
	"github.com/afruth/wtree/tui/internal/term"
	"github.com/afruth/wtree/tui/internal/theme"
	"github.com/afruth/wtree/tui/internal/tree"
	"github.com/afruth/wtree/tui/internal/treetable"
)

// Mode says what has the keys.
type Mode int

// The modes. All but ModeList and ModeSearch draw an overlay.
const (
	ModeList Mode = iota
	ModeSearch
	ModeMenu
	ModeConfirm
	ModeNew
	ModeProgress
	ModeLog
	ModeHelp
	ModePanel
)

// Times of the interface.
const (
	statsEvery    = 5 * time.Second
	settleAfter   = 150 * time.Millisecond
	redrawAfter   = 50 * time.Millisecond
	pressTimeout  = 400 * time.Millisecond
	doubleTab     = 300 * time.Millisecond
	heldKey       = 300 * time.Millisecond // how long a repeat cannot fall through to what is under
	flashFor      = 300 * time.Millisecond
	statusShort   = 3 * time.Second
	spinEvery     = 500 * time.Millisecond
	sortStill     = 5 * time.Second
	diskEveryTick = 12 // one tick in this many also reads the disk cache
)

// Options configure a Model.
type Options struct {
	Client   core.Client
	Cwd      string
	Home     string
	StateDir string // $XDG_STATE_HOME/wtree: ui.json and log/ are here. Empty: nothing is kept
	Global   bool   // -g forces machine scope
	Theme    theme.Options
	// Stacks is -d: this run opens on the flat list of stacks. It is for this run only.
	Stacks bool
	// ThemeForced says that --theme or WTREE_THEME chose light or dark, so the answer of
	// the terminal background query does not count.
	ThemeForced bool
	ASCIIForced bool
	Caps        term.Caps
	Prefs       Prefs
	Now         func() time.Time
	// HoldNow is the clock of the moment in which a sheet holds still. Default: Now. A demo that
	// shows a fixed time needs a clock that runs for this one.
	HoldNow func() time.Time
	// Timer makes a tea.Cmd that gives msg after d. Tests replace it. Default: tea.Tick.
	Timer func(d time.Duration, msg tea.Msg) tea.Cmd
	// PanicAfter makes Update panic after this time, for the test of the terminal restore.
	PanicAfter time.Duration
}

// Model is the application. Use it through a pointer: Update changes it in place.
type Model struct {
	o     Options
	th    theme.Theme
	light bool

	client core.Client
	ctx    context.Context
	cancel context.CancelFunc
	uiDone chan struct{} // closed when the program is over: readers stop

	w, h     int
	layout   theme.Layout
	lastGeom geom
	tree     *tree.State
	table    treetable.Model
	mode     Mode
	panel    bool // the panel beside the table on wide, once the user has chosen
	panelSet bool // the user chose: until then the width decides

	now   func() time.Time
	nowAt time.Time
	timer func(time.Duration, tea.Msg) tea.Cmd

	// load
	token        int
	loading      bool
	loadStarted  time.Time
	statsRunning bool
	ticks        int
	tickArmed    bool
	blurred      bool
	noRepo       bool
	repoName     string
	repoNames    map[string]string
	warnings     map[string]string
	warnSeen     map[string]bool
	listErr      string
	diskAt       time.Time
	diskAgeBase  int64
	diskKnown    bool
	diskScan     bool
	wantCursor   string
	autoCursor   string // where the first load last put the cursor
	cursorPlaced bool   // the first load no longer moves the cursor
	cleanupFrom  string // the row that the cleanup view came from
	flashArmed   bool
	flashGen     int
	everLoaded   bool

	// input
	search      textinput.Model
	seekMatch   bool
	lastTab     time.Time
	heldName    string    // the key that just closed something
	heldUntil   time.Time // its repeats do nothing until this time
	sheetHold   time.Time // what confirms a sheet waits until this time: see armSheet
	inTap       bool      // this action comes from a tap, and a tap never runs a thing directly
	lastMove    time.Time
	press       *pressState
	pressGen    int
	dragY       int
	panelScroll int

	// status line
	status     string
	statusTone theme.Tone
	statusGen  int
	statusMark bool // the status line says that rows are still marked

	// resize
	resizeGen int
	settling  bool

	// spinner
	spin      int
	spinArmed bool

	// sheets
	menu        dialog.Menu
	menuReturn  Mode
	confirm     dialog.Confirm
	planSeq     int
	planPending bool
	planCancel  context.CancelFunc // stops the core plan that runs
	planSheet   bool               // this plan gets a sheet, also when it could run at once
	queue       []core.PlanRequest
	queueTotal  int
	queuePart   int
	newSheet    dialog.NewWorktree
	newPending  bool
	help        dialog.Help
	helpReturn  Mode
	info        dialog.Info
	logView     dialog.LogView
	logReturn   Mode

	// batch
	progress    dialog.Progress
	quitMenu    *dialog.Menu
	writer      *core.Stream
	writerBox   *atomic.Pointer[core.Stream]
	writerKind  streamKind
	writerOn    bool // a writer was started, also before its stream reached the model
	applyReq    core.ApplyRequest
	autoClose   bool
	quitAfter   bool
	opening     string
	openLines   []string
	openOK      bool
	newRequest  core.NewRequest
	lastLog     []string
	lastLogPath string
	logUnsaved  bool

	// exit
	quitting   bool
	printOnEnd string
	savedPrefs string
	scopeSet   bool // the user changed the scope: -g and the fallback outside a repo are for one run
	sortSet    bool // the user changed the sort: -d is for one run
	cleanStale bool // the sizes came after the totals of the cleanup view: read them again
}

// New makes a Model. It does nothing until Init.
func New(o Options) *Model {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.HoldNow == nil {
		o.HoldNow = o.Now
	}
	if o.Timer == nil {
		o.Timer = func(d time.Duration, msg tea.Msg) tea.Cmd {
			return tea.Tick(d, func(time.Time) tea.Msg { return msg })
		}
	}
	scope := tree.ScopeRepo
	if o.Global || o.Prefs.Scope == "machine" {
		scope = tree.ScopeMachine
	}
	opts := o.Theme
	if !o.ASCIIForced && o.Prefs.ASCII != nil {
		opts.ASCII = *o.Prefs.ASCII
	}
	if !o.ThemeForced {
		switch o.Prefs.Theme {
		case "light":
			opts.Light = true
		case "dark":
			opts.Light = false
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	th := theme.New(opts)
	st := tree.New(tree.Options{Scope: scope, Home: o.Home, Now: o.Now})
	st.RestoreExpanded(o.Prefs.Expanded)
	sortPref := o.Prefs.Sort
	if sortPref == "" && o.Stacks {
		sortPref = "name"
	}
	switch sortPref {
	case "disk":
		st.SetSort(tree.SortDisk, false)
	case "cpu":
		st.SetSort(tree.SortCPU, false)
	case "name":
		st.SetSort(tree.SortName, false)
	}
	panel, panelSet := false, false
	if o.Prefs.Panel != nil {
		panel, panelSet = *o.Prefs.Panel, true
	}
	layout := theme.LayoutFor(80, 24)
	m := &Model{
		o: o, th: th, client: o.Client, ctx: ctx, cancel: cancel, uiDone: make(chan struct{}),
		tree: st, table: treetable.New(th, layout), layout: layout, panel: panel, panelSet: panelSet,
		now: o.Now, nowAt: o.Now(), timer: o.Timer,
		repoNames: map[string]string{}, warnings: map[string]string{}, warnSeen: map[string]bool{},
		search:    dialog.NewInput(th, "/ "),
		writerBox: &atomic.Pointer[core.Stream]{},
	}
	m.savedPrefs = m.prefsJSON()
	m.sync()
	return m
}

// Init starts the first list, and asks what the terminal is.
func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.startFullList()}
	if !m.o.ThemeForced && m.o.Prefs.Theme == "" {
		cmds = append(cmds, tea.RequestBackgroundColor)
	}
	if m.o.Caps.SSH {
		// Bubble Tea does not ask for DEC 2026 over ssh. Asking does no harm, and a
		// frame that is never half-drawn is worth the most on a phone.
		cmds = append(cmds, tea.Raw("\x1b[?2026$p"))
	}
	if m.o.PanicAfter > 0 {
		cmds = append(cmds, m.timer(m.o.PanicAfter, panicMsg{}))
	}
	cmds = append(cmds, m.keepSpinning())
	return tea.Batch(cmds...)
}

// Tier and friends, for main and tests.

// Layout is the current layout.
func (m *Model) Layout() theme.Layout { return m.layout }

// CurrentMode is what has the keys.
func (m *Model) CurrentMode() Mode { return m.mode }

// PrintOnExit is what `print path and quit` asks to print after the terminal is back.
func (m *Model) PrintOnExit() string { return m.printOnEnd }

// structureReady says that the keys that act on rows work.
func (m *Model) structureReady() bool { return m.tree.StructureDone() }

// Machine says that the scope is the machine.
func (m *Model) machine() bool { return m.tree.Scope() == tree.ScopeMachine }

func (m *Model) busy() bool { return m.loading || (m.mode == ModeProgress && !m.progress.Finished) }

// applyRunning says that a batch runs, also before its stream has reached the model. An open
// or a new is no batch: its window of 20 s holds nobody back.
func (m *Model) applyRunning() bool {
	return (m.writer != nil && m.writerKind == kindApply) || (m.mode == ModeProgress && !m.progress.Finished)
}

// armSheet starts the moment in which nothing confirms a sheet that just came or changed. A key
// that repeats, a buffered key and a second send all land in it, and each one starts it again, so
// a held key cannot answer a sheet that nobody has read. A person who has read it is never held.
func (m *Model) armSheet() { m.sheetHold = m.o.HoldNow().Add(heldKey) }

// sheetHeld says that this confirm comes too soon. A key gets an answer on the sheet. A tap gets
// none: the answer would add a row, and the buttons must stay under the thumb.
func (m *Model) sheetHeld() bool {
	if !m.o.HoldNow().Before(m.sheetHold) {
		return false
	}
	m.armSheet()
	if !m.inTap {
		m.confirm.Said = "read the sheet, then confirm"
	}
	return true
}

// planWaiting says that a key asked for a plan or a name, and its sheet has not come.
func (m *Model) planWaiting() bool {
	return m.mode == ModeList && (m.planPending || m.newPending)
}

// cancelPlan forgets the plan that is on its way, and the rest of the batch: its sheet will not
// open. A sheet may only open on what the user still asks for.
func (m *Model) cancelPlan() {
	if m.planCancel != nil {
		m.planCancel()
		m.planCancel = nil
	}
	m.planSeq++
	m.planPending, m.newPending, m.planSheet = false, false, false
	m.queue, m.queueTotal, m.queuePart = nil, 0, 0
}
