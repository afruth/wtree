package term

import (
	"bytes"
	"strings"
	"testing"
)

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestDetectRex(t *testing.T) {
	for _, c := range []struct {
		env  map[string]string
		flag bool
		want bool
	}{
		{nil, false, false},
		{map[string]string{"TERM_PROGRAM": "iTerm.app"}, false, false},
		{map[string]string{"TERM_PROGRAM": "rex"}, false, true},
		{map[string]string{"REX_SESSION": "s1"}, false, true},
		{map[string]string{"WTREE_STATUS": "1"}, false, true},
		{nil, true, true},
	} {
		if got := Detect(envOf(c.env), c.flag).Rex; got != c.want {
			t.Errorf("env %v flag %v: rex %v", c.env, c.flag, got)
		}
	}
}

func TestDetectSlow(t *testing.T) {
	if Detect(envOf(map[string]string{"TERM": "xterm-256color"}), false).Slow {
		t.Error("a plain xterm is not slow")
	}
	for _, env := range []map[string]string{{"TERM": "dumb"}, {"TERM": "mosh-256color"}, {"WTREE_SLOW": "1"}, {"MOSH_CONNECTION": "x"}} {
		if !Detect(envOf(env), false).Slow {
			t.Errorf("%v must be slow", env)
		}
	}
}

func TestGuardEmergencyResetsEverythingOnce(t *testing.T) {
	var out bytes.Buffer
	g := NewGuard(&out, nil, false)
	g.Restore(false)
	g.Restore(false)
	got := out.String()
	if got != Emergency {
		t.Fatalf("got %q", got)
	}
	for _, seq := range []string{"\x1b[?1006l", "\x1b[?1002l", "\x1b[?1000l", "\x1b[?2004l", "\x1b[?1049l", "\x1b[<u", "\x1b[?25h"} {
		if !strings.Contains(got, seq) {
			t.Errorf("no %q in the reset", seq)
		}
	}
}

func TestGuardCleanKeepsTheKittyStack(t *testing.T) {
	var out bytes.Buffer
	NewGuard(&out, nil, false).Restore(true)
	if strings.Contains(out.String(), "\x1b[<u") || strings.Contains(out.String(), "\x1b[?1049l") {
		t.Fatalf("a clean exit must leave what Bubble Tea restored: %q", out.String())
	}
}

func TestGuardClearsTheRexStatusOnlyInRex(t *testing.T) {
	var plain, rex bytes.Buffer
	NewGuard(&plain, nil, false).Restore(false)
	NewGuard(&rex, nil, true).Restore(false)
	if strings.Contains(plain.String(), "]7501") || strings.Contains(plain.String(), "]9;4") {
		t.Errorf("OSC outside Rex: %q", plain.String())
	}
	if !strings.Contains(rex.String(), "]7501") || !strings.Contains(rex.String(), "]9;4") {
		t.Errorf("no OSC in Rex: %q", rex.String())
	}
}
