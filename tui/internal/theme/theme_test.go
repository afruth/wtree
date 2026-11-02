package theme

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestDetect(t *testing.T) {
	utf := map[string]string{"LANG": "en_US.UTF-8"}
	with := func(kv ...string) map[string]string {
		out := map[string]string{"LANG": utf["LANG"]}
		for i := 0; i < len(kv); i += 2 {
			out[kv[i]] = kv[i+1]
		}
		return out
	}
	cases := []struct {
		name  string
		env   map[string]string
		want  Profile
		ascii bool
		light bool
	}{
		{"plain", with("TERM", "xterm"), ANSI16, false, false},
		{"256", with("TERM", "xterm-256color"), ANSI256, false, false},
		{"truecolor", with("TERM", "xterm-256color", "COLORTERM", "truecolor"), TrueColor, false, false},
		{"24bit", with("COLORTERM", "24bit"), TrueColor, false, false},
		{"no color wins", with("TERM", "xterm-256color", "COLORTERM", "truecolor", "NO_COLOR", "1"), Mono, false, false},
		{"dumb", with("TERM", "dumb"), Mono, false, false},
		{"light", with("WTREE_THEME", "light"), ANSI16, false, true},
		{"ascii env", with("WTREE_ASCII", "1"), ANSI16, true, false},
		{"latin1", map[string]string{"LANG": "de_DE.ISO-8859-1"}, ANSI16, true, false},
		{"no locale", map[string]string{}, ANSI16, true, false},
		{"lc_all wins", map[string]string{"LC_ALL": "C", "LANG": "en_US.UTF-8"}, ANSI16, true, false},
		{"force unicode", map[string]string{"LC_ALL": "C", "WTREE_ASCII": "0"}, ANSI16, false, false},
	}
	for _, c := range cases {
		o := Detect(envOf(c.env))
		if o.Profile != c.want || o.ASCII != c.ascii || o.Light != c.light {
			t.Errorf("%s: got %+v", c.name, o)
		}
	}
}

func TestPaletteTable(t *testing.T) {
	// Spot values from the table of ux.md 6.1, one per profile and background.
	hex := func(o Options, tone Tone) string {
		c := New(o).Color(tone)
		if c == nil {
			return ""
		}
		r, g, b, _ := c.RGBA()
		return strings.ToUpper(string([]byte{hexd(r >> 12), hexd(r >> 8), hexd(g >> 12), hexd(g >> 8), hexd(b >> 12), hexd(b >> 8)}))
	}
	if got := hex(Options{Profile: TrueColor}, ToneAccent); got != "4FB8A8" {
		t.Errorf("accent dark = %s", got)
	}
	if got := hex(Options{Profile: TrueColor, Light: true}, ToneLoss); got != "7B3B96" {
		t.Errorf("loss light = %s", got)
	}
	if New(Options{Profile: ANSI16}).Color(ToneFg) != nil {
		t.Errorf("fg in 16 colours must be the default of the terminal")
	}
	for _, tone := range []Tone{ToneFg, ToneDim, ToneAccent, ToneOk, ToneWarn, ToneDanger, ToneLoss} {
		if New(Options{Profile: Mono}).Color(tone) != nil {
			t.Errorf("mono must have no colour, tone %d", tone)
		}
		if New(Options{Profile: ANSI256}).Color(tone) == nil {
			t.Errorf("256 must have a colour, tone %d", tone)
		}
	}
}

func hexd(v uint32) byte { return "0123456789abcdef"[v&0xf] }

func TestMonoHasNoColorCodes(t *testing.T) {
	th := New(Options{Profile: Mono})
	for _, tone := range []Tone{ToneFg, ToneDim, ToneAccent, ToneOk, ToneWarn, ToneDanger, ToneLoss} {
		for _, out := range []string{th.Style(tone).Render("x"), th.Bold(tone).Render("x"), th.Button(tone).Render("x")} {
			if strings.Contains(out, "38;") || strings.Contains(out, "48;") || strings.Contains(out, "[3") {
				t.Errorf("colour code in mono: %q", out)
			}
		}
	}
	if !strings.Contains(th.Cursor().Render("x"), "7") {
		t.Errorf("cursor must be reverse in mono: %q", th.Cursor().Render("x"))
	}
}

func TestAsciiGlyphsArePureAscii(t *testing.T) {
	v := ASCII
	for _, g := range []string{v.Current, v.Main, v.Expanded, v.Collapsed, v.Running, v.Partial, v.Exited, v.Down, v.Volumes, v.Cleanup, v.Mark, v.Merged, v.Locked, v.Ahead, v.Behind, v.Changed, v.Loading, v.Unknown, v.Stale, v.Done, v.Failed, v.InProgress, v.BarFull, v.BarEmpty, v.ScopeOpen, v.ScopeClose, v.Ellipsis, v.Dot, v.Rule, v.ScrollTrack, v.ScrollThumb} {
		if g == "" {
			t.Errorf("empty glyph")
		}
		for _, r := range g {
			if r > 126 {
				t.Errorf("not ascii: %q", g)
			}
		}
	}
}

func TestLayoutFor(t *testing.T) {
	cases := []struct {
		w, h  int
		tier  Tier
		short bool
		small bool
		panel bool
	}{
		{120, 35, TierWide, false, false, true},
		{100, 30, TierWide, false, false, true},
		{99, 30, TierMedium, false, false, false},
		{80, 24, TierMedium, false, false, false},
		{60, 20, TierMedium, false, false, false},
		{59, 20, TierNarrow, false, false, false},
		{48, 40, TierNarrow, false, false, false},
		{93, 14, TierMedium, true, false, false},
		{120, 16, TierWide, true, false, false},
		{120, 17, TierWide, false, false, true},
		{40, 24, TierNarrow, false, false, false},
		{39, 24, TierNarrow, false, true, false},
	}
	for _, c := range cases {
		l := LayoutFor(c.w, c.h)
		if l.Tier != c.tier || l.Short != c.short || l.TooSmall != c.small || l.PanelAllowed() != c.panel {
			t.Errorf("%dx%d: %+v panel=%v", c.w, c.h, l, l.PanelAllowed())
		}
	}
	if got := LayoutFor(120, 35).TableWidth(true); got != 83 {
		t.Errorf("table width with panel = %d, the mock-up has 83", got)
	}
	if got := LayoutFor(120, 14).TableWidth(true); got != 120 {
		t.Errorf("short forces the panel off, got %d", got)
	}
}

// A dim button is a grey that stays grey when a multiplexer rounds it to 256 colours, and a
// button that does nothing keeps a block.
func TestNeutralButtonsAreGrey(t *testing.T) {
	for _, light := range []bool{false, true} {
		th := New(Options{Profile: TrueColor, Light: light})
		for _, s := range []lipgloss.Style{th.Button(ToneDim), th.ButtonOff()} {
			r, g, b, _ := s.GetBackground().RGBA()
			if r != g || g != b {
				t.Errorf("light %v: background %d %d %d is not grey", light, r, g, b)
			}
		}
	}
	if out := New(Options{Profile: Mono}).ButtonOff().Render("x"); !strings.Contains(out, "7") {
		t.Errorf("no block in mono: %q", out)
	}
}
