package treetable

import (
	"strings"
	"testing"
)

func TestWidth(t *testing.T) {
	cases := []struct {
		s string
		w int
	}{
		{"abc", 3},
		{"日本語", 6},
		{"é", 1},        // precomposed
		{"é", 1},       // combining
		{"🚀", 2},        // emoji
		{"👨‍👩‍👧‍👦", 2},  // a ZWJ family is one grapheme
		{"🇩🇪", 2},       // a flag is two runes and one grapheme
		{"⌂▸▾●◐○▭■", 8}, // the glyph set is one cell each
	}
	for _, c := range cases {
		if got := Width(c.s); got != c.w {
			t.Errorf("Width(%q) = %d, want %d", c.s, got, c.w)
		}
	}
}

func TestSanitize(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain", "plain"},
		{"é日本🚀", "é日本🚀"},
		{"a\x1b[31mred\x1b[0mb", "aredb"},
		{"a\x1b]0;title\x07b", "ab"},
		{"a\x1b]8;;http://x\x1b\\link\x1b]8;;\x1b\\b", "alinkb"},
		{"a\x1bPq#0\x1b\\b", "ab"},
		{"a\x1b(Bb", "ab"},
		{"a\u009b31mb", "ab"},
		{"a\u009d0;x\u009cb", "ab"},
		{"tab\there\nnl\r", "tab here nl "},
		{"bell\x07del\x7f", "belldel"},
		{"evil‮gnp.exe", "evilgnp.exe"},
		{"\xff\xfe", "��"},
		{"cut\x1b[", "cut"},
		{"cut\x1b", "cut"},
		{"csi\x1b[1\x07x", "csix"},
	}
	for _, c := range cases {
		if got := Sanitize(c.in); got != c.want {
			t.Errorf("Sanitize(%q) = %q, want %q", c.in, got, c.want)
		}
		if strings.ContainsAny(Sanitize(c.in), "\x1b\x07\n\t") {
			t.Errorf("Sanitize(%q) kept a control", c.in)
		}
	}
}

func TestTruncate(t *testing.T) {
	const e = "…"
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"fits", TruncateStart("feat-login", 10, e), "feat-login"},
		{"start", TruncateStart("foundry-feat-login-api", 10, e), "…login-api"},
		{"end", TruncateEnd("merged into main", 10, e), "merged in…"},
		{"start wide", TruncateStart("日本語の名前", 7, e), "…の名前"},
		{"start wide no half", TruncateStart("日本語の名前", 6, e), "…名前"},
		{"end wide no half", TruncateEnd("日本語の名前", 6, e), "日本…"},
		{"end emoji", TruncateEnd("a🚀🚀🚀", 4, e), "a🚀…"},
		{"zwj stays whole", TruncateStart("abc👨‍👩‍👧‍👦y", 4, e), "…👨‍👩‍👧‍👦y"},
		{"ascii ellipsis", TruncateStart("foundry-api", 6, ".."), "..-api"},
		{"too small", TruncateStart("abcdef", 1, ".."), "."},
		{"zero", TruncateEnd("abc", 0, e), ""},
		{"path whole", TruncatePath("~/dev/wt/hotfix-1", 17, e), "~/dev/wt/hotfix-1"},
		{"path segments", TruncatePath("~/dev/worktrees/wt/hotfix-1", 14, e), "…/wt/hotfix-1"},
		{"path last only", TruncatePath("~/dev/worktrees/wt/hotfix-1", 10, e), "…/hotfix-1"},
		{"path long leaf", TruncatePath("~/dev/a-very-long-leaf-name", 8, e), "…af-name"},
		{"path no slash", TruncatePath("abcdefghij", 5, e), "…ghij"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, c.got, c.want)
		}
	}
}

func TestTruncatedTextNeverExceedsWidth(t *testing.T) {
	inputs := []string{"abcdefghijkl", "日本語の名前です", "a🚀b🚀c🚀", "👨‍👩‍👧‍👦👨‍👩‍👧‍👦👨‍👩‍👧‍👦", "ééééé", "~/a/b/日本語/🚀"}
	for _, s := range inputs {
		for w := 0; w <= 14; w++ {
			for _, e := range []string{"…", ".."} {
				for name, got := range map[string]string{
					"start": TruncateStart(s, w, e), "end": TruncateEnd(s, w, e), "path": TruncatePath(s, w, e), "clip": clip(s, w),
				} {
					if Width(got) > w {
						t.Errorf("%s(%q, %d, %q) = %q has width %d", name, s, w, e, got, Width(got))
					}
				}
			}
		}
	}
}
