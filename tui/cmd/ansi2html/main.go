// Command ansi2html turns a capture of a terminal, as `tmux capture-pane -e -p` writes it,
// into one HTML page with a monospace grid. It draws what the capture holds, cell by cell:
// it adds nothing and it has no idea of what wtree is. tui/scripts/screens.sh uses it.
//
//	ansi2html -theme dark -cols 120 -rows 40 <capture.ansi >capture.html
//
// With -phone, the page is the size of a phone, and the font is as large as the width allows:
// that is the real size of the text on such a device. The part of the page under the grid
// stays empty, or says "keyboard" when -keyboard reserves it.
package main

import (
	"flag"
	"fmt"
	"html"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// palette is what the page takes as the default colours of the terminal, and its 16 colours.
type palette struct {
	bg, fg string
	ansi   [16]string
}

var palettes = map[string]palette{
	"dark": {
		bg: "#14161a", fg: "#d8d4c8",
		ansi: [16]string{"#1b1d22", "#e26d5a", "#9acd5b", "#e0b04d", "#5f9fd8", "#b678c4", "#4fb8a8", "#d8d4c8",
			"#7a776e", "#f0907e", "#b4e07a", "#f0c870", "#82b8ea", "#cc96d8", "#74d0c0", "#f4f0e4"},
	},
	"light": {
		bg: "#fbfaf6", fg: "#2b2a26",
		ansi: [16]string{"#2b2a26", "#b5341c", "#3e7d1f", "#9a6a00", "#2a62a8", "#7b3b96", "#0f7f72", "#e6e3da",
			"#8a867c", "#c9492f", "#4f9428", "#b58000", "#3a78c4", "#9150ae", "#169a8b", "#fbfaf6"},
	},
}

// style is the state of SGR. An empty colour is the default of the terminal.
type style struct {
	fg, bg              string
	bold, dim, reversed bool
	italic, underline   bool
}

// run is text with one style.
type run struct {
	style style
	text  string
}

// parse reads the capture into lines of runs. Only SGR matters: a capture has nothing else.
func parse(input string, p palette, cols int) [][]run {
	var lines [][]run
	var st style
	for _, raw := range strings.Split(strings.TrimRight(input, "\n"), "\n") {
		var line []run
		col := 0
		add := func(text string) {
			if text == "" {
				return
			}
			col += ansi.StringWidth(text)
			if n := len(line); n > 0 && line[n-1].style == st {
				line[n-1].text += text
				return
			}
			line = append(line, run{st, text})
		}
		for i := 0; i < len(raw); {
			switch {
			case strings.HasPrefix(raw[i:], "\x1b["):
				end := i + 2
				for end < len(raw) && (raw[end] < 0x40 || raw[end] > 0x7e) {
					end++
				}
				if end < len(raw) && raw[end] == 'm' {
					st = applySGR(st, raw[i+2:end], p)
				}
				i = end + 1
			case raw[i] == 0x1b:
				i++
			case raw[i] == '\t':
				// tmux gives back a tab that the program used. A terminal stops a tab at
				// its last column, also when the next stop is beyond it.
				stop := col + 8 - col%8
				if cols > 0 && stop > cols-1 {
					stop = max(cols-1, col)
				}
				add(strings.Repeat(" ", stop-col))
				i++
			default:
				end := i
				for end < len(raw) && raw[end] != 0x1b && raw[end] != '\t' {
					end++
				}
				add(raw[i:end])
				i = end
			}
		}
		lines = append(lines, line)
	}
	return lines
}

func applySGR(st style, params string, p palette) style {
	if params == "" {
		return style{}
	}
	// 38:2::r:g:b and 38;2;r;g;b are the same
	fields := strings.Split(strings.ReplaceAll(params, ":", ";"), ";")
	num := func(i int) int {
		if i >= len(fields) {
			return 0
		}
		n, _ := strconv.Atoi(fields[i])
		return n
	}
	for i := 0; i < len(fields); i++ {
		n := num(i)
		switch {
		case n == 0:
			st = style{}
		case n == 1:
			st.bold = true
		case n == 2:
			st.dim = true
		case n == 3:
			st.italic = true
		case n == 4:
			st.underline = true
		case n == 7:
			st.reversed = true
		case n == 22:
			st.bold, st.dim = false, false
		case n == 23:
			st.italic = false
		case n == 24:
			st.underline = false
		case n == 27:
			st.reversed = false
		case n >= 30 && n <= 37:
			st.fg = p.ansi[n-30]
		case n == 39:
			st.fg = ""
		case n >= 40 && n <= 47:
			st.bg = p.ansi[n-40]
		case n == 49:
			st.bg = ""
		case n >= 90 && n <= 97:
			st.fg = p.ansi[n-90+8]
		case n >= 100 && n <= 107:
			st.bg = p.ansi[n-100+8]
		case n == 38 || n == 48:
			color := ""
			switch num(i + 1) {
			case 5:
				color = color256(num(i+2), p)
				i += 2
			case 2:
				// a colon form can have an empty colour space: 38:2::r:g:b
				at := i + 2
				if at < len(fields) && fields[at] == "" {
					at++
				}
				color = fmt.Sprintf("#%02x%02x%02x", num(at), num(at+1), num(at+2))
				i = at + 2
			}
			if n == 38 {
				st.fg = color
			} else {
				st.bg = color
			}
		}
	}
	return st
}

func color256(n int, p palette) string {
	switch {
	case n < 16:
		return p.ansi[max(n, 0)]
	case n < 232:
		n -= 16
		level := func(v int) int {
			if v == 0 {
				return 0
			}
			return 55 + v*40
		}
		return fmt.Sprintf("#%02x%02x%02x", level(n/36), level(n/6%6), level(n%6))
	}
	gray := 8 + (min(n, 255)-232)*10
	return fmt.Sprintf("#%02x%02x%02x", gray, gray, gray)
}

func (s style) css(p palette) string {
	fg, bg := s.fg, s.bg
	if s.reversed {
		fg, bg = bg, fg
		if fg == "" {
			fg = p.bg
		}
		if bg == "" {
			bg = p.fg
		}
	}
	var out []string
	if fg != "" {
		out = append(out, "color:"+fg)
	}
	if bg != "" {
		out = append(out, "background:"+bg)
	}
	if s.bold {
		out = append(out, "font-weight:700")
	}
	if s.dim {
		out = append(out, "opacity:.6")
	}
	if s.italic {
		out = append(out, "font-style:italic")
	}
	if s.underline {
		out = append(out, "text-decoration:underline")
	}
	return strings.Join(out, ";")
}

// cells writes the text. A glyph outside of ASCII can come from a fallback font with another
// width, so it gets a box of exactly its cells, and the grid holds whatever the font has.
func cells(b *strings.Builder, text string) {
	var plain strings.Builder
	flush := func() {
		if plain.Len() > 0 {
			b.WriteString(html.EscapeString(plain.String()))
			plain.Reset()
		}
	}
	for _, r := range text {
		if r < 0x7f {
			plain.WriteRune(r)
			continue
		}
		flush()
		width := ansi.StringWidth(string(r))
		if width <= 0 {
			b.WriteString(html.EscapeString(string(r)))
			continue
		}
		fmt.Fprintf(b, `<i style="width:%dch">%s</i>`, width, html.EscapeString(string(r)))
	}
	flush()
}

type page struct {
	cols, rows     int
	fontSize       float64
	lineHeight     float64
	phoneW, phoneH int
	keyboard       int
	title          string
	pal            palette
}

const fontStack = `"JetBrainsMono Nerd Font Mono","JetBrains Mono NL","JetBrains Mono","DejaVu Sans Mono","Liberation Mono",monospace`

func render(w io.Writer, lines [][]run, pg page) {
	var b strings.Builder
	pad := 8
	bodyW, bodyH := "", ""
	if pg.phoneW > 0 {
		pad = 0
		bodyW = fmt.Sprintf("width:%dpx;", pg.phoneW)
		bodyH = fmt.Sprintf("height:%dpx;", pg.phoneH)
	}
	fmt.Fprintf(&b, `<!doctype html><html><head><meta charset="utf-8"><title>%s</title><style>
html,body{margin:0;padding:0;background:%s;color:%s;overflow:hidden}
body{%s%s}
#t{font-family:%s;font-size:%.3fpx;line-height:%.3fpx;padding:%dpx;white-space:pre;font-variant-ligatures:none;font-feature-settings:"liga" 0,"calt" 0}
.l{height:%.3fpx;min-width:%dch}
i{display:inline-block;font-style:normal;text-align:center;height:%.3fpx;vertical-align:top}
.k{position:absolute;left:0;right:0;bottom:0;font:13px sans-serif;display:flex;align-items:center;justify-content:center;border-top:1px dashed %s;opacity:.55}
</style></head><body><div id="t">`,
		html.EscapeString(pg.title), pg.pal.bg, pg.pal.fg, bodyW, bodyH, fontStack, pg.fontSize, pg.lineHeight, pad, pg.lineHeight, pg.cols, pg.lineHeight, pg.pal.fg)
	for i, line := range lines {
		if pg.rows > 0 && i >= pg.rows {
			break
		}
		b.WriteString(`<div class="l">`)
		for _, r := range line {
			css := r.style.css(pg.pal)
			if css != "" {
				fmt.Fprintf(&b, `<span style="%s">`, css)
			}
			cells(&b, r.text)
			if css != "" {
				b.WriteString("</span>")
			}
		}
		b.WriteString("</div>")
	}
	b.WriteString("</div>")
	if pg.keyboard > 0 {
		fmt.Fprintf(&b, `<div class="k" style="height:%dpx">the system keyboard covers this part</div>`, pg.keyboard)
	}
	b.WriteString("</body></html>\n")
	_, _ = io.WriteString(w, b.String())
}

func main() {
	themeName := flag.String("theme", "dark", "dark or light: the page takes this as the terminal background")
	cols := flag.Int("cols", 80, "columns of the capture")
	rows := flag.Int("rows", 0, "rows of the capture, 0: all")
	fontSize := flag.Float64("font-size", 14, "font size in px")
	title := flag.String("title", "capture", "the title of the page")
	phoneW := flag.Int("phone-width", 0, "a phone-sized page: its width in px")
	phoneH := flag.Int("phone-height", 0, "a phone-sized page: its height in px")
	keyboard := flag.Int("keyboard", 0, "the height in px that the system keyboard takes")
	flag.Parse()
	pal, ok := palettes[*themeName]
	if !ok {
		fmt.Fprintln(os.Stderr, "ansi2html: the theme is dark or light")
		os.Exit(2)
	}
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ansi2html:", err)
		os.Exit(1)
	}
	pg := page{cols: *cols, rows: *rows, fontSize: *fontSize, title: *title, pal: pal, phoneW: *phoneW, phoneH: *phoneH, keyboard: *keyboard}
	if pg.phoneW > 0 {
		// JetBrains Mono has an advance of 0.6 em: the font is as large as the columns allow
		// and a rounding of the browser must not push the last column off the page
		pg.fontSize = float64(pg.phoneW)/float64(pg.cols)/0.6 - 0.02
	}
	pg.lineHeight = float64(int(pg.fontSize*1.3 + 0.5))
	render(os.Stdout, parse(string(input), pal, *cols), pg)
}
