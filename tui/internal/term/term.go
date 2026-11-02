// Package term knows about the terminal that wtree-tui runs in: what the environment
// says about it, and how to give it back in a usable state on every way out.
package term

import (
	"io"
	"os"
	"strings"
	"sync"

	"github.com/charmbracelet/x/ansi"
	xterm "github.com/charmbracelet/x/term"
)

// Caps is what the environment says about the terminal.
type Caps struct {
	// Rex says that the host is Rex. Only then does the program send OSC 7501 and 9;4.
	Rex bool
	// Slow says that the link is slow or the terminal is dumb: no motion at all.
	Slow bool
	// SSH says that the program runs over ssh. DEC 2026 is then asked for explicitly,
	// because Bubble Tea does not ask over ssh.
	SSH bool
}

// Detect reads the environment through env, normally os.Getenv.
func Detect(env func(string) string, rexFlag bool) Caps {
	termName := strings.ToLower(env("TERM"))
	return Caps{
		Rex: rexFlag || strings.EqualFold(env("TERM_PROGRAM"), "rex") || env("REX_SESSION") != "" || env("WTREE_STATUS") == "1",
		Slow: termName == "dumb" || strings.Contains(termName, "mosh") || env("MOSH_CONNECTION") != "" ||
			env("WTREE_SLOW") == "1",
		SSH: env("SSH_TTY") != "" || env("SSH_CONNECTION") != "",
	}
}

// ModesOff turns off what the program can have turned on, except the alternate screen
// and the kitty stack. It is safe at any time.
const ModesOff = "\x1b[?1006l\x1b[?1003l\x1b[?1002l\x1b[?1000l\x1b[?2004l\x1b[?1004l\x1b[?2026l\x1b[?25h"

// Emergency is the whole reset: it also leaves the alternate screen and pops the kitty
// keyboard flags. It is for a way out where Bubble Tea may not have cleaned up.
const Emergency = "\x1b[?1006l\x1b[?1003l\x1b[?1002l\x1b[?1000l\x1b[?2004l\x1b[?1004l\x1b[?2026l\x1b[<u\x1b[0m\x1b[?1049l\x1b[?25h"

// Guard gives the terminal back. It keeps the terminal state from before the program
// took it over, and it restores it at most once.
type Guard struct {
	out   io.Writer
	rex   bool
	tty   *os.File
	state *xterm.State
	once  sync.Once
}

// NewGuard remembers the state of tty, which can be nil or not a terminal. Make it
// before the program starts.
func NewGuard(out io.Writer, tty *os.File, rex bool) *Guard {
	g := &Guard{out: out, rex: rex}
	if tty != nil && xterm.IsTerminal(tty.Fd()) {
		if state, err := xterm.GetState(tty.Fd()); err == nil {
			g.tty, g.state = tty, state
		}
	}
	return g
}

// Restore writes the reset and puts the terminal state back. With clean false, it is the
// emergency reset. A write error is ignored, because the terminal can already be gone.
func (g *Guard) Restore(clean bool) {
	g.once.Do(func() {
		var seq strings.Builder
		if g.rex {
			seq.WriteString(ansi.ClearProgramStatus)
			seq.WriteString(ansi.ResetProgressBar)
		}
		if clean {
			seq.WriteString(ModesOff)
		} else {
			seq.WriteString(Emergency)
		}
		_, _ = io.WriteString(g.out, seq.String())
		if g.state != nil {
			_ = xterm.Restore(g.tty.Fd(), g.state)
		}
	})
}
