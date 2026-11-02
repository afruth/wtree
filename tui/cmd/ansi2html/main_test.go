package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func page1(input, theme string, cols int) string {
	var b strings.Builder
	p := palettes[theme]
	render(&b, parse(input, p, cols), page{cols: cols, fontSize: 14, lineHeight: 18, title: "<t>", pal: p})
	return b.String()
}

func TestSGRForms(t *testing.T) {
	p := palettes["dark"]
	for _, c := range []struct {
		params string
		check  func(style) bool
	}{
		{"1", func(s style) bool { return s.bold }},
		{"7", func(s style) bool { return s.reversed }},
		{"38;2;79;184;168", func(s style) bool { return s.fg == "#4fb8a8" }},
		{"38:2::79:184:168", func(s style) bool { return s.fg == "#4fb8a8" }},
		{"48;2;1;2;3", func(s style) bool { return s.bg == "#010203" }},
		{"38;5;196", func(s style) bool { return s.fg == "#ff0000" }},
		{"38;5;240", func(s style) bool { return s.fg == "#585858" }},
		{"31", func(s style) bool { return s.fg == p.ansi[1] }},
		{"91", func(s style) bool { return s.fg == p.ansi[9] }},
		{"1;38;2;1;1;1;7", func(s style) bool { return s.bold && s.reversed && s.fg == "#010101" }},
	} {
		if got := applySGR(style{}, c.params, p); !c.check(got) {
			t.Errorf("%s: %+v", c.params, got)
		}
	}
	if got := applySGR(style{bold: true, fg: "#000"}, "0", p); got != (style{}) {
		t.Errorf("0 resets: %+v", got)
	}
	if got := applySGR(style{bold: true}, "", p); got != (style{}) {
		t.Errorf("an empty SGR resets: %+v", got)
	}
}

func TestReverseUsesTheDefaultsOfThePage(t *testing.T) {
	p := palettes["light"]
	css := style{reversed: true}.css(p)
	if !strings.Contains(css, "color:"+p.bg) || !strings.Contains(css, "background:"+p.fg) {
		t.Fatal(css)
	}
}

func TestStyleCarriesOverLines(t *testing.T) {
	lines := parse("\x1b[1mone\ntwo\x1b[0m three", palettes["dark"], 0)
	if len(lines) != 2 || !lines[0][0].style.bold || !lines[1][0].style.bold || lines[1][1].style.bold {
		t.Fatalf("%+v", lines)
	}
}

func TestTabGoesToTheNextStop(t *testing.T) {
	lines := parse("ab\tc", palettes["dark"], 80)
	if got := lines[0][0].text; got != "ab      c" {
		t.Fatalf("%q", got)
	}
	// at a width of 12 the second tab stops in the last column, and not at the stop of 16
	lines = parse("ab\t\t|", palettes["dark"], 12)
	if got := lines[0][0].text; got != "ab         |" {
		t.Fatalf("%q", got)
	}
}

func TestPageHoldsEachLineAndEscapesText(t *testing.T) {
	out := page1("<b>&\n\x1b[31m▸ x\x1b[0m", "dark", 10)
	if !strings.Contains(out, "&lt;b&gt;&amp;") {
		t.Fatal("not escaped: " + out)
	}
	if strings.Count(out, `<div class="l">`) != 2 {
		t.Fatal("two lines")
	}
	// a glyph outside ASCII has a box of its width, so a fallback font cannot move the grid
	if !strings.Contains(out, `<i style="width:1ch">▸</i>`) {
		t.Fatal(out)
	}
	if strings.Contains(out, "\x1b") {
		t.Fatal("an escape is in the page")
	}
	if ansi.StringWidth("▸") != 1 {
		t.Fatal("width")
	}
}
