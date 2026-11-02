package model

import (
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/afruth/wtree/tui/internal/core"
)

type streamKind int

const (
	kindList streamKind = iota
	kindStats
	kindApply
	kindOpen
	kindNew
	kindDryRun
)

// streamMsg is what a stream gave since the last one. The events are decoded values.
type streamMsg struct {
	kind   streamKind
	token  int
	stream *core.Stream
	events []any
	done   bool
	result core.Result
	err    error // it did not start
}

type planMsg struct {
	seq   int
	req   core.PlanRequest
	plan  core.Plan
	err   error
	level bool // a level change inside the sheet
}

// SignalMsg is a signal that says that the program must end: HUP, TERM, PIPE, INT.
type SignalMsg struct{}

type (
	statsTickMsg    struct{}
	settleMsg       struct{ gen int }
	redrawMsg       struct{}
	statusExpireMsg struct{ gen int }
	pressExpireMsg  struct{ gen int }
	spinMsg         struct{}
	flashClearMsg   struct{ gen int }
	newFlashMsg     struct{}
	logSavedMsg     struct{ path string }
	panicMsg        struct{}
)

// readBatch waits for the next events of a stream. A burst of lines comes as one message,
// so that a list of a hundred rows draws once and not a hundred times. It stops when the
// program is over: the stream itself goes on being read by its own goroutine.
func readBatch(kind streamKind, token int, s *core.Stream, uiDone <-chan struct{}) tea.Cmd {
	return func() tea.Msg {
		msg := streamMsg{kind: kind, token: token, stream: s}
		finish := func() tea.Msg {
			msg.done = true
			msg.result = s.Wait()
			return msg
		}
		select {
		case ev, ok := <-s.Events():
			if !ok {
				return finish()
			}
			msg.events = append(msg.events, ev.Value)
		case <-uiDone:
			return nil
		}
		window := time.NewTimer(8 * time.Millisecond)
		defer window.Stop()
		for len(msg.events) < 256 {
			select {
			case ev, ok := <-s.Events():
				if !ok {
					return finish()
				}
				msg.events = append(msg.events, ev.Value)
			case <-window.C:
				return msg
			case <-uiDone:
				return nil
			}
		}
		return msg
	}
}

// startStream starts a command. Its first message comes at once, with the stream and no
// events: a command can take its time before its first line, and the model must be able
// to stop it, and know that it runs, from the start.
func startStream(kind streamKind, token int, open func() (*core.Stream, error)) tea.Cmd {
	return func() tea.Msg {
		s, err := open()
		if err != nil {
			return streamMsg{kind: kind, token: token, done: true, err: err}
		}
		return streamMsg{kind: kind, token: token, stream: s}
	}
}

// startWriter is startStream for a command that changes state. The box lets the last words
// of the program find it, also when its first message never reached the model.
func startWriter(box *atomic.Pointer[core.Stream], kind streamKind, open func() (*core.Stream, error)) tea.Cmd {
	return startStream(kind, 0, func() (*core.Stream, error) {
		s, err := open()
		if err == nil {
			box.Store(s)
		}
		return s, err
	})
}
