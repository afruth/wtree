package model

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/afruth/wtree/tui/internal/term"
)

// RunConfig says how to run the program. Stdout and Stdin nil mean the terminal.
type RunConfig struct {
	Output   *os.File
	NoInput  bool // for a test with no terminal
	Stderr   io.Writer
	KillWait time.Duration // how long a signal waits for a clean quit. Default 2 s
}

// Exit codes of the program.
const (
	ExitOK    = 0
	ExitFatal = 2
)

// Run runs the program to its end, and gives the terminal back on every way out: a quit,
// a panic in any goroutine of the program, and HUP, TERM, PIPE and INT. A batch that still
// runs is told to stop after its current step, and the program waits for that with the
// terminal already restored, so no work is left without a word. A step has no time limit, so
// while that wait lasts another signal ends the program: it says what it leaves, and the step goes
// on alone with nothing queued after it. It returns the exit code.
func Run(o Options, cfg RunConfig) (code int) {
	out := cfg.Output
	if out == nil {
		out = os.Stdout
	}
	if cfg.Stderr == nil {
		cfg.Stderr = os.Stderr
	}
	if cfg.KillWait <= 0 {
		cfg.KillWait = 2 * time.Second
	}
	var tty *os.File
	if !cfg.NoInput {
		tty = os.Stdin
	}
	guard := term.NewGuard(out, tty, o.Caps.Rex)
	m := New(o)

	options := []tea.ProgramOption{tea.WithoutSignalHandler(), tea.WithOutput(out)}
	if cfg.NoInput {
		options = append(options, tea.WithInput(nil))
	}
	p := tea.NewProgram(m, options...)

	var signalled atomic.Int32
	var waiting atomic.Bool // the terminal is back, and the program only waits for a step
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGPIPE, syscall.SIGINT, syscall.SIGQUIT)
	stopSignals := make(chan struct{})
	go func() {
		for {
			select {
			case s := <-sigs:
				n, _ := s.(syscall.Signal)
				if waiting.Load() {
					if n == syscall.SIGPIPE {
						continue // the output is gone, and that is no answer to the wait
					}
					fmt.Fprintln(cfg.Stderr, "wtree: left without waiting. The step that runs goes on alone, and nothing starts after it. Read the list to see what is left.")
					_, _ = io.WriteString(out, term.ModesOff)
					os.Exit(128 + int(n))
				}
				if signalled.CompareAndSwap(0, int32(n)) {
					go p.Send(SignalMsg{})
					// a program that does not answer, as when the terminal is gone, still ends
					time.AfterFunc(cfg.KillWait, p.Kill)
				}
			case <-stopSignals:
				return
			}
		}
	}()

	finish := func(clean bool) {
		defer close(stopSignals)
		close(m.uiDone)
		guard.Restore(clean)
		waiting.Store(true)
		m.shutdown(cfg.Stderr)
		if m.printOnEnd != "" {
			fmt.Fprintln(out, m.printOnEnd)
		}
		// Nothing comes after this. A client that lost a sequence gets it back here.
		_, _ = io.WriteString(out, term.ModesOff)
	}
	defer func() {
		if r := recover(); r != nil {
			finish(false)
			fmt.Fprintf(cfg.Stderr, "wtree: %v\n", r)
			code = ExitFatal
		}
	}()

	_, err := p.Run()
	n := signalled.Load()
	finish(err == nil && n == 0)
	switch {
	case n != 0:
		return 128 + int(n)
	case err != nil:
		fmt.Fprintf(cfg.Stderr, "wtree: %v\n", err)
		return ExitFatal
	}
	return ExitOK
}

// shutdown is the last thing: the terminal is back. A batch that still runs stops after
// its step, and this waits for it and says how it ended.
func (m *Model) shutdown(stderr io.Writer) {
	m.cancel()
	if data := m.prefsJSON(); m.o.StateDir != "" && data != m.savedPrefs {
		_ = writePrefs(m.o.StateDir, data)
		m.savedPrefs = data
	}
	if m.logUnsaved {
		if path := writeLog(m.o.StateDir, m.now(), m.lastLog); path != "" {
			fmt.Fprintf(stderr, "wtree: stopped. Log: %s\n", path)
		}
	}
	writer := m.writer
	if writer == nil {
		writer = m.writerBox.Load()
	}
	if writer == nil {
		return
	}
	if m.writerKind != kindApply {
		fmt.Fprintln(stderr, "wtree: stopping what still runs. ctrl-c leaves without waiting.")
		writer.Stop()
		writer.Wait()
		m.clearWriter()
		return
	}
	fmt.Fprintln(stderr, "wtree: a batch is running: it stops after the current step. ctrl-c leaves without waiting.")
	writer.Stop()
	result := writer.Wait()
	m.clearWriter()
	m.progress.Finish(result)
	m.lastLog = m.progress.Log
	path := writeLog(m.o.StateDir, m.now(), m.lastLog)
	if result.End != nil {
		fmt.Fprintf(stderr, "wtree: %d ok, %d failed, %d skipped.", result.End.OK, result.End.Failed, result.End.Skipped)
	} else {
		fmt.Fprint(stderr, "wtree: it ended before its last event. Read the list to see what is left.")
	}
	if path != "" {
		fmt.Fprintf(stderr, " Log: %s", path)
	}
	fmt.Fprintln(stderr)
}
