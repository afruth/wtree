package treetable

import (
	"reflect"

	"github.com/afruth/wtree/tui/internal/theme"
)

// Kind says what a Row is.
type Kind int

// Row kinds. A separator carries no action: the cursor skips it.
const (
	KindWorktree Kind = iota
	KindStack
	KindContainer
	KindGroup     // the cleanup group, or a heading of the cleanup view
	KindSeparator // one blank line, dropped by the short rule
)

// CellState is one of the four cell states of ux.md, or a blank.
type CellState int

// The cell states. A value of CellBlank draws nothing.
const (
	CellBlank   CellState = iota
	CellValue             // 612M
	CellNA                // not applicable: it draws nothing, and on narrow takes no room
	CellLoading           // · a value will come
	CellFailed            // ? it could not be read
	CellStale             // ~1.2G old, dim, and the tilde survives monochrome
)

// Cell is one value in a column. Text has no glyphs, so it works in both glyph sets.
type Cell struct {
	Text  string
	State CellState
	Tone  theme.Tone
}

// Constructors for the cell states.
func Value(text string) Cell { return Cell{Text: text, State: CellValue} }
func NA() Cell               { return Cell{State: CellNA} }
func Loading() Cell          { return Cell{State: CellLoading} }
func Failed() Cell           { return Cell{State: CellFailed} }
func Stale(text string) Cell { return Cell{Text: text, State: CellStale} }

// WithTone returns the cell in another tone.
func (c Cell) WithTone(t theme.Tone) Cell { c.Tone = t; return c }

// Marker is the identity glyph in the second cell of a row. It wins over the chevron.
type Marker int

// Row markers.
const (
	MarkerNone Marker = iota
	MarkerCurrent
	MarkerMain
	MarkerCleanup
	MarkerSafe // the safe heading of the cleanup view
	MarkerLook // the heading of what needs a look
)

// Run is the state of what runs in a row. It picks the glyph and the tone.
type Run int

// Run states.
const (
	RunNone Run = iota
	RunUp
	RunPartial
	RunExited
	RunDown
	RunVolumes
)

// Git is the git state of a worktree row.
type Git struct {
	State                  CellState // CellBlank when the row has no git state at all
	Ahead, Behind, Changed int
	Merged                 bool
	Locked                 bool
}

// Row is one row of the table. tree.State.Visible builds them, and it formats the
// numbers, so that only the glyphs depend on the theme here. All text must already
// be sanitized.
type Row struct {
	ID   string
	Kind Kind

	Depth      int
	Expandable bool
	Expanded   bool
	Marker     Marker
	Marked     bool
	Markable   bool
	Dim        bool // context for a filter, or a row that cannot be acted on

	Prefix   string // a dim prefix before the name, in a flat sorted view
	Name     string
	NameNote string // dim text after the name: "nothing to clean"
	Tab      string // dim hint after the name: "tab 4"

	Branch     string
	Reason     string // shown in place of the branch: "worktree gone"
	ReasonTone theme.Tone
	Git        Git

	Word   string // the state of a stack or container: "partial", "running"
	Count  string // "1/2"; on a worktree row the roll-up
	Run    Run
	Health string // on a container: healthy, unhealthy, starting

	CPU, Mem, Disk, Age Cell
	Ports               []string // ":8080", for the second line on narrow
}

// selectable says whether the cursor can land on the row.
func (r Row) selectable() bool { return r.Kind != KindSeparator }

// height is the number of lines of the row in a tier.
func (r Row) height(t theme.Tier) int {
	if t == theme.TierNarrow && (r.Kind == KindWorktree || r.Kind == KindStack || r.Kind == KindContainer) {
		return 2
	}
	return 1
}

// ChangedIDs lists the rows of next whose content differs from the same row in prev.
// A UI flashes them on the wide tier, through Model.SetFlash. A new row does not count.
func ChangedIDs(prev, next []Row) []string {
	old := make(map[string]Row, len(prev))
	for _, r := range prev {
		old[r.ID] = r
	}
	var out []string
	for _, r := range next {
		if o, ok := old[r.ID]; ok && !reflect.DeepEqual(o, r) {
			out = append(out, r.ID)
		}
	}
	return out
}
