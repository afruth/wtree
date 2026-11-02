package treetable

import "github.com/afruth/wtree/tui/internal/theme"

type colID int

const (
	colName colID = iota
	colBranch
	colGit
	colStack
	colCPU
	colMem
	colDisk
	colAge
)

// column is one column of the frame. x and w are the cells of its content. A number
// is right-aligned in them, and a stale value takes one cell of the gap on its left
// for its tilde, so the digits stay in line.
type column struct {
	id    colID
	title string
	x, w  int
	right bool
}

// titleSpan is the tappable span of the title, at least 5 cells.
func (c column) titleSpan() (int, int) {
	w := max(c.w, 5)
	if c.right {
		return c.x + c.w - w, c.x + c.w
	}
	return c.x, c.x + w
}

// The first cells of a row. On wide and medium, cells 0 and 1 are the gutter of the mark, and
// cells 2 and 3 hold the marker or the chevron and its space, so each tap has its own target.
// Narrow has no cells to spare, and uses the two lines of a row: see HitTest.
const (
	leadCells   = 4
	narrowCells = 3
)

type fixedCol struct {
	id    colID
	title string
	gap   int
	w     int
	right bool
}

// columnsFor computes the frame. All tiers come from the mock-ups of ux.md 2.5 at
// their stated width. The name and the branch take what the fixed columns leave, so
// the columns line up whatever the data is: long data is cut and moves nothing.
func columnsFor(l theme.Layout, width int, rows []Row, reasons bool, titlePrefix int) []column {
	var fixed []fixedCol
	var nameBase, branchBase, pad int
	if reasons {
		return reasonColumns(l, width, rows)
	}
	switch {
	case l.Tier == theme.TierWide:
		nameBase, branchBase, pad = 24, 18, 1
		fixed = []fixedCol{
			{colGit, "GIT", 0, 7, false},
			{colStack, "STACK", 1, 6, false},
			{colCPU, "CPU", 2, 5, true},
			{colMem, "MEM", 2, 4, true},
			{colDisk, "DISK", 2, 4, true},
			{colAge, "AGE", 1, 3, false},
		}
		if l.Short {
			fixed = append(fixed[:3:3], fixed[4:]...)
		}
	case l.Short:
		nameBase, branchBase, pad = 25, 19, 1
		fixed = []fixedCol{
			{colStack, "STACK", 0, 14, false},
			{colCPU, "CPU", 0, 5, true},
			{colDisk, "DISK", 3, 4, true},
		}
	default:
		nameBase, branchBase, pad = 26, 18, 1
		fixed = []fixedCol{
			{colStack, "STACK", 0, 11, false},
			{colCPU, "CPU", 1, 5, true},
			{colMem, "MEM", 2, 4, true},
			{colDisk, "DISK", 3, 4, true},
		}
	}

	used := leadCells + pad
	for _, f := range fixed {
		used += f.gap + f.w
	}
	nameSlot, branchSlot := nameBranchSlots(width-used, nameBase, branchBase, false, rows)
	if l.Short {
		// The header shares the line of the titles: the first title starts after it, if there
		// is room, because a cut through its numbers says something wrong.
		short := titlePrefix + 2 - leadCells - nameSlot - branchSlot
		branchSlot += max(min(short, width-used-nameSlot-branchSlot), 0)
	}

	cols := []column{
		{id: colName, title: "NAME", x: leadCells, w: nameSlot - 1},
		{id: colBranch, title: "BRANCH", x: leadCells + nameSlot, w: branchSlot - 1},
	}
	x := leadCells + nameSlot + branchSlot
	for _, f := range fixed {
		x += f.gap
		cols = append(cols, column{id: f.id, title: f.title, x: x, w: f.w, right: f.right})
		x += f.w
	}
	return cols
}

// reasonColumns is the frame of the cleanup view. CPU and memory say nothing there, and the
// reason is why the row is on the screen, so it gets their width and all that is left.
func reasonColumns(l theme.Layout, width int, rows []Row) []column {
	fixed := []fixedCol{
		{colGit, "GIT", 0, 7, false},
		{colStack, "STACK", 1, 6, false},
		{colDisk, "DISK", 2, 4, true},
		{colAge, "AGE", 1, 3, false},
	}
	if l.Tier != theme.TierWide {
		fixed = []fixedCol{
			{colStack, "STACK", 0, 11, false},
			{colDisk, "DISK", 1, 4, true},
		}
	}
	used := leadCells + 1
	for _, f := range fixed {
		used += f.gap + f.w
	}
	nameSlot, branchSlot := nameBranchSlots(width-used, 20, 24, true, rows)
	cols := []column{
		{id: colName, title: "NAME", x: leadCells, w: nameSlot - 1},
		{id: colBranch, title: "WHY", x: leadCells + nameSlot, w: branchSlot - 1},
	}
	x := leadCells + nameSlot + branchSlot
	for _, f := range fixed {
		x += f.gap
		cols = append(cols, column{id: f.id, title: f.title, x: x, w: f.w, right: f.right})
		x += f.w
	}
	return cols
}

// nameBranchSlots shares the elastic width between the name and the branch. Each slot holds
// its gap. Each asks for what its longest value needs, so that a wide screen does not push
// the branch to its far edge. A row with no branch can use both, and is counted for that.
// With too little room they shrink towards a share of it. With fill, the branch column takes
// all that is left: it holds reasons, which are long.
func nameBranchSlots(avail, nameBase, branchBase int, fill bool, rows []Row) (int, int) {
	const nameMin, branchMin, branchCap = 12, 10, 40
	nameWant, branchWant, wholeWant := 0, 0, 0
	for _, r := range rows {
		if r.Kind == KindSeparator || r.Kind == KindGroup {
			continue
		}
		if r.Branch == "" && r.Reason == "" {
			wholeWant = max(wholeWant, nameCells(r)+1)
			if r.Kind != KindWorktree {
				continue
			}
		}
		nameWant = max(nameWant, nameCells(r)+2)
		branchWant = max(branchWant, Width(r.Branch)+1, Width(r.Reason)+1)
	}
	name := max(nameWant, nameMin)
	branch := min(max(branchWant, branchMin), branchCap)
	if name+branch > avail {
		name = max(min(name, max(avail*nameBase/(nameBase+branchBase), avail-branch)), nameMin)
		branch = max(avail-name, branchMin)
		return name, branch
	}
	if fill {
		return name, avail - name
	}
	if name+branch < wholeWant {
		branch += min(wholeWant-name-branch, avail-name-branch)
	}
	return name, branch
}

// nameCells is the width that the name part of a row asks for.
func nameCells(r Row) int {
	w := indentOf(r.Depth) + Width(r.Name)
	if r.Prefix != "" {
		w += Width(r.Prefix) + 1
	}
	if r.Kind == KindWorktree {
		w += 2 // the folded git glyph
	}
	if r.Tab != "" {
		w += Width(r.Tab) + 1
	}
	return w
}

// indentOf is the indent of a name from the name column, from the mock-ups: a stack
// sits 1 cell in, and a container 3.
func indentOf(depth int) int {
	switch {
	case depth <= 0:
		return 0
	case depth == 1:
		return 1
	}
	return 3 + (depth-2)*2
}
