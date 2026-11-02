package theme

// Glyphs is one glyph set. Every value is one cell wide, except where the ASCII
// table of ux.md gives two characters.
type Glyphs struct {
	Current, Main                  string
	Expanded, Collapsed            string
	Running, Partial, Exited, Down string
	Volumes, Cleanup, Mark, Merged string
	Locked                         string
	Ahead, Behind, Changed         string
	Loading, Unknown, Stale        string
	Done, Failed, InProgress       string
	Kept                           string // an item that the plan keeps: no failure
	BarFull, BarEmpty              string
	ScopeOpen, ScopeClose          string
	Ellipsis                       string
	Dot                            string // the separator in "live 2s · disk 18m"
	Rule                           string // the divider before the detail panel
	ScrollTrack, ScrollThumb       string
}

// Unicode is the default glyph set. It has no emoji, because emoji widths break
// phone clients.
var Unicode = Glyphs{
	Current: "@", Main: "⌂",
	Expanded: "▾", Collapsed: "▸",
	Running: "●", Partial: "◐", Exited: "○", Down: "·",
	Volumes: "▭", Cleanup: "!", Mark: "■", Merged: "✓",
	Locked: "⚿",
	Ahead:  "↑", Behind: "↓", Changed: "±",
	Loading: "·", Unknown: "?", Stale: "~",
	Done: "✓", Failed: "×", InProgress: "▸", Kept: "–",
	BarFull: "▰", BarEmpty: "▱",
	ScopeOpen: "▌", ScopeClose: "▐",
	Ellipsis:    "…",
	Dot:         "·",
	Rule:        "│",
	ScrollTrack: "│", ScrollThumb: "┃",
}

// ASCII is the fallback for a locale that is not UTF-8, WTREE_ASCII=1 or --ascii.
var ASCII = Glyphs{
	Current: "@", Main: "M",
	Expanded: "-", Collapsed: "+",
	Running: "*", Partial: "%", Exited: "o", Down: ".",
	Volumes: "v", Cleanup: "!", Mark: "x", Merged: "m",
	Locked: "L",
	Ahead:  "+", Behind: "-", Changed: "~",
	Loading: ".", Unknown: "?", Stale: "~",
	Done: "ok", Failed: "X", InProgress: ">", Kept: "-",
	BarFull: "#", BarEmpty: "-",
	ScopeOpen: "[", ScopeClose: "]",
	Ellipsis:    "..",
	Dot:         ".",
	Rule:        "|",
	ScrollTrack: "|", ScrollThumb: "#",
}
