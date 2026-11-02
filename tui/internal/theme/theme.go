// Package theme holds the "tide" palette, the glyph sets and the layout tiers of
// the wtree TUI. It only computes: detection takes an environment lookup and
// touches no terminal.
package theme

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// Profile is how many colours the terminal can show.
type Profile int

// Profiles, from least to most colour.
const (
	Mono Profile = iota // NO_COLOR or a dumb terminal: bold, faint and reverse only
	ANSI16
	ANSI256
	TrueColor
)

// String names the profile for the help overlay.
func (p Profile) String() string {
	switch p {
	case ANSI16:
		return "16 colours"
	case ANSI256:
		return "256 colours"
	case TrueColor:
		return "truecolor"
	}
	return "monochrome"
}

// Tone is a semantic colour token. Nothing else in the UI picks a colour.
type Tone int

// The tokens of the palette. ToneLoss means data goes, and nothing else uses it.
const (
	ToneFg Tone = iota
	ToneDim
	ToneAccent
	ToneOk
	ToneWarn
	ToneDanger
	ToneLoss
)

type swatch struct {
	trueDark, trueLight string
	x256Dark, x256Light string
	ansi16              string // "" is the default colour of the terminal
}

var palette = map[Tone]swatch{
	ToneFg:     {"#D8D4C8", "#2B2A26", "252", "236", ""},
	ToneDim:    {"#7A776E", "#8A867C", "243", "245", "8"},
	ToneAccent: {"#4FB8A8", "#0F7F72", "73", "30", "6"},
	ToneOk:     {"#9ACD5B", "#3E7D1F", "107", "64", "2"},
	ToneWarn:   {"#E0B04D", "#9A6A00", "179", "136", "3"},
	ToneDanger: {"#E26D5A", "#B5341C", "173", "160", "1"},
	ToneLoss:   {"#B678C4", "#7B3B96", "139", "97", "5"},
}

// Options choose a theme. Detect fills them from the environment.
type Options struct {
	Profile Profile
	Light   bool
	ASCII   bool
}

// Theme is the resolved palette and glyph set. It is a plain value.
type Theme struct {
	Options
	G Glyphs
}

// New builds a theme.
func New(o Options) Theme {
	t := Theme{Options: o, G: Unicode}
	if o.ASCII {
		t.G = ASCII
	}
	return t
}

// Detect reads the environment through env, normally os.Getenv.
//
//   - NO_COLOR with any non-empty value, or TERM=dumb, gives Mono.
//   - COLORTERM=truecolor or 24bit gives TrueColor; a TERM with 256color gives ANSI256.
//   - WTREE_THEME=light sets Light. Otherwise it is dark, until the model has the answer
//     of the terminal background query, and builds a new theme with Options.Light.
//   - WTREE_ASCII=1 forces ASCII glyphs, WTREE_ASCII=0 forces Unicode, and otherwise a
//     locale that is not UTF-8 gives ASCII.
func Detect(env func(string) string) Options {
	var o Options
	term := strings.ToLower(env("TERM"))
	colorterm := strings.ToLower(env("COLORTERM"))
	switch {
	case env("NO_COLOR") != "", term == "dumb":
		o.Profile = Mono
	case colorterm == "truecolor", colorterm == "24bit":
		o.Profile = TrueColor
	case strings.Contains(term, "256color"), colorterm != "":
		o.Profile = ANSI256
	default:
		o.Profile = ANSI16
	}
	o.Light = strings.EqualFold(env("WTREE_THEME"), "light")
	switch env("WTREE_ASCII") {
	case "1":
		o.ASCII = true
	case "0":
		o.ASCII = false
	default:
		o.ASCII = !localeIsUTF8(env)
	}
	return o
}

func localeIsUTF8(env func(string) string) bool {
	for _, name := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if v := env(name); v != "" {
			v = strings.ToLower(v)
			return strings.Contains(v, "utf-8") || strings.Contains(v, "utf8")
		}
	}
	return false
}

// Color returns the colour of a token, or nil when the token has none here: the
// default foreground in 16 colours, and everything in Mono.
func (t Theme) Color(tone Tone) color.Color {
	s := palette[tone]
	var v string
	switch t.Profile {
	case TrueColor:
		v = s.trueDark
		if t.Light {
			v = s.trueLight
		}
	case ANSI256:
		v = s.x256Dark
		if t.Light {
			v = s.x256Light
		}
	case ANSI16:
		v = s.ansi16
	}
	if v == "" {
		return nil
	}
	return lipgloss.Color(v)
}

// Style is the foreground style of a token. In Mono, ToneDim is faint and the other
// tokens carry no attribute at all, so the glyphs have to carry the meaning.
func (t Theme) Style(tone Tone) lipgloss.Style {
	s := lipgloss.NewStyle()
	if c := t.Color(tone); c != nil {
		s = s.Foreground(c)
	}
	if t.Profile == Mono && tone == ToneDim {
		s = s.Faint(true)
	}
	return s
}

// Bold is the style of a token in bold: row names, column titles, totals.
func (t Theme) Bold(tone Tone) lipgloss.Style { return t.Style(tone).Bold(true) }

// Cursor is the cursor bar: reverse video in accent, so it works in 16 colours and
// in monochrome.
func (t Theme) Cursor() lipgloss.Style { return t.Style(ToneAccent).Faint(false).Reverse(true) }

// A button in dim is a neutral grey block, so that only warn, danger and loss carry a warm
// colour. These two shades belong to the buttons: nothing else may use them. The truecolor
// values sit on the grey ramp of the 256 colours, because tmux and mosh can round a colour to
// that palette, and a warm grey then comes out as khaki.
var (
	neutralBlock = map[bool]swatch{
		false: {"#585858", "#C6C6C6", "240", "251", ""}, // a dim button
		true:  {"#303030", "#E4E4E4", "236", "254", ""}, // a button that does nothing now
	}
)

// offText is the text of a button that does nothing now: as neutral as its block, because the
// warm dim reads as olive there.
func (t Theme) offText() color.Color {
	switch {
	case t.Profile == TrueColor && t.Light:
		return lipgloss.Color("#6E6E6E")
	case t.Profile == TrueColor:
		return lipgloss.Color("#8A8A8A")
	case t.Light:
		return lipgloss.Color("242")
	}
	return lipgloss.Color("245")
}

func (t Theme) block(off bool) (lipgloss.Style, bool) {
	s := neutralBlock[off]
	var bg string
	switch t.Profile {
	case TrueColor:
		bg = s.trueDark
		if t.Light {
			bg = s.trueLight
		}
	case ANSI256:
		bg = s.x256Dark
		if t.Light {
			bg = s.x256Light
		}
	default:
		return lipgloss.NewStyle(), false
	}
	if off {
		return lipgloss.NewStyle().Foreground(t.offText()).Background(lipgloss.Color(bg)), true
	}
	return t.Style(ToneFg).Background(lipgloss.Color(bg)), true
}

// Button is a block: a neutral grey for dim, or reverse video in tone for the primary button
// (accent) and a confirm button (the colour of its level).
func (t Theme) Button(tone Tone) lipgloss.Style {
	if t.Profile == Mono {
		return lipgloss.NewStyle().Reverse(true)
	}
	if tone == ToneDim {
		if s, ok := t.block(false); ok {
			return s
		}
	}
	return t.Style(tone).Reverse(true)
}

// ButtonOff is a button that does nothing now. It keeps its block, so that the target stays
// and a row of buttons stays uniform.
func (t Theme) ButtonOff() lipgloss.Style {
	if s, ok := t.block(true); ok {
		return s
	}
	if t.Profile == Mono {
		return lipgloss.NewStyle().Reverse(true).Faint(true)
	}
	return t.Style(ToneDim).Reverse(true).Faint(true)
}
