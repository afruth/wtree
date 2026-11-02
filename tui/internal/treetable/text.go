package treetable

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/clipperhouse/displaywidth"
)

// Width is the width of s in terminal cells. Graphemes count as a unit, so a wide
// character or an emoji sequence counts as what the terminal draws. It is the same
// measure that lipgloss and the Bubble Tea renderer use.
func Width(s string) int { return ansi.StringWidth(s) }

// Sanitize makes text from the core safe to draw on one line. The contract warns
// that any string can hold escape sequences, for example from a lock reason or a
// commit subject. It removes whole escape sequences (CSI, OSC, DCS and the others,
// in their 7-bit and 8-bit forms), C0 and C1 controls, DEL, and the bidirectional
// overrides, which could make a name read as another name. Tabs and line breaks
// become one space, and bytes that are not UTF-8 become U+FFFD.
func Sanitize(s string) string {
	clean := true
	for i := 0; i < len(s); i++ {
		if b := s[i]; b < 0x20 || b >= 0x7f {
			clean = false
			break
		}
	}
	if clean {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size <= 1:
			b.WriteRune(utf8.RuneError)
			i += size
		case r == 0x1b:
			i += escapeLen(s[i:])
		case r == 0x9b: // 8-bit CSI
			i += size + csiBody(s[i+size:])
		case r == 0x90, r == 0x98, r == 0x9d, r == 0x9e, r == 0x9f: // 8-bit DCS, SOS, OSC, PM, APC
			i += size + stringBody(s[i+size:])
		case r == '\t', r == '\n', r == '\r', r == 0x85, r == 0x2028, r == 0x2029:
			b.WriteByte(' ')
			i += size
		case isHiddenControl(r):
			i += size
		default:
			b.WriteRune(r)
			i += size
		}
	}
	return b.String()
}

func isHiddenControl(r rune) bool {
	switch {
	case unicode.IsControl(r): // C0, DEL, C1
		return true
	case r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069, r == 0x200e, r == 0x200f, r == 0x061c:
		return true // bidirectional overrides and marks
	}
	return false
}

// escapeLen is the length of the sequence at the start of s, which starts with ESC.
func escapeLen(s string) int {
	if len(s) < 2 {
		return len(s)
	}
	switch s[1] {
	case '[':
		return 2 + csiBody(s[2:])
	case ']', 'P', 'X', '^', '_':
		return 2 + stringBody(s[2:])
	}
	if s[1] >= 0x20 && s[1] <= 0x2f { // nF: intermediates, then a final byte
		i := 2
		for i < len(s) && s[i] >= 0x20 && s[i] <= 0x2f {
			i++
		}
		if i < len(s) {
			i++
		}
		return i
	}
	return 2
}

func csiBody(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x40 && s[i] <= 0x7e {
			return i + 1
		}
		if s[i] < 0x20 || s[i] > 0x7e { // broken: drop what came, keep the rest
			return i
		}
	}
	return len(s)
}

func stringBody(s string) int {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case 0x07:
			return i + 1
		case 0x1b:
			if i+1 < len(s) && s[i+1] == '\\' {
				return i + 2
			}
			return i
		case 0xc2:
			if i+1 < len(s) && s[i+1] == 0x9c { // 8-bit ST
				return i + 2
			}
		}
	}
	return len(s)
}

type cluster struct {
	text  string
	width int
}

func clusters(s string) []cluster {
	var out []cluster
	g := displaywidth.StringGraphemes(s)
	for g.Next() {
		v := g.Value()
		out = append(out, cluster{v, Width(v)})
	}
	return out
}

// TruncateEnd keeps the start of s in at most w cells: "merged into m…". It never
// cuts a wide character or an emoji sequence in half.
func TruncateEnd(s string, w int, ellipsis string) string {
	if w <= 0 {
		return ""
	}
	if Width(s) <= w {
		return s
	}
	room := w - Width(ellipsis)
	if room < 0 {
		return strings.Repeat(".", w)
	}
	var b strings.Builder
	used := 0
	for _, c := range clusters(s) {
		if used+c.width > room {
			break
		}
		b.WriteString(c.text)
		used += c.width
	}
	return b.String() + ellipsis
}

// TruncateStart keeps the end of s in at most w cells: "…eat-login-api". This is the
// rule for names, whose end tells two rows apart.
func TruncateStart(s string, w int, ellipsis string) string {
	if w <= 0 {
		return ""
	}
	if Width(s) <= w {
		return s
	}
	room := w - Width(ellipsis)
	if room < 0 {
		return strings.Repeat(".", w)
	}
	parts := clusters(s)
	used, i := 0, len(parts)
	for i > 0 && used+parts[i-1].width <= room {
		i--
		used += parts[i].width
	}
	var b strings.Builder
	b.WriteString(ellipsis)
	for _, c := range parts[i:] {
		b.WriteString(c.text)
	}
	return b.String()
}

// TruncatePath keeps the meaningful end of a path: whole segments when they fit,
// "…/wt/hotfix-1", and the end of the last segment when they do not.
func TruncatePath(s string, w int, ellipsis string) string {
	if w <= 0 {
		return ""
	}
	if Width(s) <= w {
		return s
	}
	room := w - Width(ellipsis)
	segments := strings.Split(strings.TrimRight(s, "/"), "/")
	tail := ""
	for i := len(segments) - 1; i > 0; i-- {
		next := "/" + segments[i] + tail
		if Width(next) > room {
			break
		}
		tail = next
	}
	if tail != "" {
		return ellipsis + tail
	}
	return TruncateStart(s, w, ellipsis)
}

// clip cuts s to w cells with no ellipsis.
func clip(s string, w int) string {
	if Width(s) <= w {
		return s
	}
	var b strings.Builder
	used := 0
	for _, c := range clusters(s) {
		if used+c.width > w {
			break
		}
		b.WriteString(c.text)
		used += c.width
	}
	return b.String()
}
