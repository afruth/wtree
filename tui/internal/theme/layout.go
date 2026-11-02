package theme

// Tier is the responsive layout, chosen from the width.
type Tier int

// The three tiers of ux.md 2.4.
const (
	TierWide   Tier = iota // >= 100 columns
	TierMedium             // 60 to 99 columns
	TierNarrow             // < 60 columns: two-line rows
)

// String names the tier for the help overlay.
func (t Tier) String() string {
	switch t {
	case TierMedium:
		return "medium"
	case TierNarrow:
		return "narrow"
	}
	return "wide"
}

// Breakpoints and fixed sizes of the layout.
const (
	WideMin     = 100
	MediumMin   = 60
	ShortMax    = 16  // the short rule applies at this height and below
	MinWidth    = 40  // below this the screen only says that wtree needs more
	PanelWidth  = 37  // the divider and 36 columns of the detail panel
	PanelAuto   = 110 // the panel is on by default from here: below it starves the table
	WheelWide   = 3   // rows for one wheel step on wide and medium
	WheelNarrow = 1   // logical rows for one wheel step on narrow
)

// TooSmallText is what the screen shows below MinWidth.
const TooSmallText = "wtree needs 40 columns"

// Layout is the tier and the short rule for one terminal size.
type Layout struct {
	Width, Height int
	Tier          Tier
	Short         bool
	TooSmall      bool
}

// LayoutFor picks the layout. The caller does it again on every resize.
func LayoutFor(width, height int) Layout {
	l := Layout{Width: width, Height: height, Short: height <= ShortMax, TooSmall: width < MinWidth}
	switch {
	case width >= WideMin:
		l.Tier = TierWide
	case width >= MediumMin:
		l.Tier = TierMedium
	default:
		l.Tier = TierNarrow
	}
	return l
}

// PanelAllowed says whether the detail panel can sit beside the table. Medium and
// narrow open it as an overlay, and the short rule forces it off.
func (l Layout) PanelAllowed() bool { return l.Tier == TierWide && !l.Short }

// TableWidth is the width that is left for the table with the panel on or off.
func (l Layout) TableWidth(panel bool) int {
	if panel && l.PanelAllowed() {
		return l.Width - PanelWidth
	}
	return l.Width
}
