package core

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
)

// maxLineBytes bounds one line of a stream, so a broken script cannot fill the
// memory. The longest real line is a refused event that holds a whole plan.
const maxLineBytes = 16 << 20

// maxLineText bounds the text of a line that is too long.
const maxLineText = 4 << 10

var errLineTooLong = errors.New("core: the line is too long")

// Event is one line of a stream.
type Event struct {
	// Gen is the generation of a list: see Client.List. It is 0 on the other streams.
	Gen uint64
	// Value is a value of a type that DecodeEvent returns, or a BadLine.
	Value any
}

// BadLine is a line of a stream that is not an event of this contract. The stream
// goes on after it. Show its text in the status area: it is not safe to draw until
// you sanitize it, and Err.Error() already is.
type BadLine struct {
	Text      string
	Truncated bool // the output stopped in the middle of this line
	Err       *ProtocolError
}

// Result is how a stream ended.
type Result struct {
	// Exit is the exit code, or -1 when a signal ended the command.
	Exit int
	// Signal is the name of the signal that ended the command, or "".
	Signal string
	// Err is nil for a stream that ended as the contract says, also when a step
	// failed: look at End for that. Otherwise it is a *RefusedError, a *CoreError,
	// a *ChildDiedError, an *ExitError, ErrSuperseded, or the error of the context.
	Err error
	// End is the last event of a stream, when it came. End.Cancelled says that a
	// TERM stopped an apply before its next step.
	End *EndEvent
	// Created is the worktree that `core new` made, or the name it picked with
	// --dry-run. It can be set when Err is not nil: the worktree exists then.
	Created *NewWorktreeEvent
	// Stopped says that Stop, a context or a newer list ended the stream before its
	// last event. If it ran steps, the events that came say which, so reload.
	Stopped bool
	// Stderr is the end of what the command wrote there.
	Stderr string
}

// Stream is a running command that writes events.
//
// Read Events to the end: the channel closes when the command is over, and Wait
// does not block after that. The stream reads the output of the command at all
// times, so a slow UI never holds up a step.
//
// A list and a dry run are reads: on Stop or on a cancelled context, the command and
// all its children end, and the events that were not read yet are dropped. An apply,
// an open and a new change state: on Stop or on a cancelled context, the process
// alone gets one TERM. The step that runs goes on, and the events keep coming until
// its `end`. Nothing here ever kills a command that changes state.
type Stream struct {
	gen      uint64
	cancel   context.CancelCauseFunc
	readOnly bool
	pid      int // the process and its group, for the tests. 0 on a fake

	events chan Event

	mu     sync.Mutex
	wake   *sync.Cond
	queue  []Event
	ended  bool
	dropCh chan struct{}
	drop   bool

	done   chan struct{}
	result Result
}

func newStream(ctx context.Context, cancel context.CancelCauseFunc, gen uint64, readOnly bool) *Stream {
	s := &Stream{
		gen:      gen,
		cancel:   cancel,
		readOnly: readOnly,
		events:   make(chan Event),
		dropCh:   make(chan struct{}),
		done:     make(chan struct{}),
	}
	s.wake = sync.NewCond(&s.mu)
	if readOnly {
		context.AfterFunc(ctx, s.dropEvents)
	}
	go s.pump()
	return s
}

// Gen is the generation of a list. A model drops an event with an older Gen than
// that of the last list that it started without Only.
func (s *Stream) Gen() uint64 { return s.gen }

// Events gives the events in the order of the lines.
func (s *Stream) Events() <-chan Event { return s.events }

// Stop asks the command to stop, as the comment of Stream says. It is safe to call
// more than once and after the end.
func (s *Stream) Stop() { s.cancel(context.Canceled) }

// Close says that nobody reads the events any more. A read stops as with Stop. A
// command that changes state gets no signal: it goes on, and its events are dropped.
func (s *Stream) Close() {
	s.dropEvents()
	if s.readOnly {
		s.Stop()
	}
}

// Done closes when the command is over.
func (s *Stream) Done() <-chan struct{} { return s.done }

// Wait blocks until the command is over, and gives its Result.
func (s *Stream) Wait() Result {
	<-s.done
	return s.result
}

func (s *Stream) push(value any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.drop {
		return
	}
	s.queue = append(s.queue, Event{Gen: s.gen, Value: value})
	s.wake.Signal()
}

// finish comes after the last push. The result is set before the channel closes.
func (s *Stream) finish(result Result) {
	s.result = result
	close(s.done)
	s.mu.Lock()
	s.ended = true
	s.wake.Broadcast()
	s.mu.Unlock()
}

func (s *Stream) dropEvents() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.drop {
		return
	}
	s.drop = true
	s.queue = nil
	close(s.dropCh)
	s.wake.Broadcast()
}

// pump moves the queue to the channel, and it ends when the queue is empty after
// the end. It holds the context until then, because a read drops what is queued
// when its context ends.
func (s *Stream) pump() {
	defer s.cancel(nil)
	defer close(s.events)
	for {
		s.mu.Lock()
		for len(s.queue) == 0 && !s.ended {
			s.wake.Wait()
		}
		if len(s.queue) == 0 {
			s.mu.Unlock()
			return
		}
		event := s.queue[0]
		s.queue = s.queue[1:]
		s.mu.Unlock()
		select {
		case s.events <- event:
		case <-s.dropCh:
		}
	}
}

// ---- reading lines

// readLines calls line for each line of r, also the last one if it has no newline.
// A line above maxLineBytes comes cut, with tooLong. It returns at the end of r, on
// any read error, and when a deadline on the pipe has passed.
func readLines(r io.Reader, line func(text []byte, terminated, tooLong bool)) {
	reader := bufio.NewReaderSize(r, 64<<10)
	var long []byte
	tooLong := false
	// add keeps a line while it is within the limit, and only its start after that
	add := func(chunk []byte) {
		if tooLong {
			return
		}
		long = append(long, chunk...)
		if len(long) > maxLineBytes {
			long = bytes.Clone(long[:maxLineText])
			tooLong = true
		}
	}
	for {
		chunk, err := reader.ReadSlice('\n')
		add(chunk)
		switch {
		case err == nil:
			line(long, true, tooLong)
			long, tooLong = nil, false
		case errors.Is(err, bufio.ErrBufferFull):
		default:
			if len(long) > 0 {
				line(long, false, tooLong)
			}
			return
		}
	}
}

// parseLine gives the value of an event, a BadLine, or nil for an empty line.
func parseLine(text []byte, terminated, tooLong bool) any {
	text = bytes.TrimRight(text, "\r\n")
	if tooLong {
		return badLine(text, !terminated, errLineTooLong)
	}
	if len(bytes.TrimSpace(text)) == 0 {
		return nil
	}
	value, err := DecodeEvent(text)
	if err != nil {
		return badLine(text, !terminated, err)
	}
	return value
}

func badLine(text []byte, truncated bool, err error) BadLine {
	raw := string(text)
	return BadLine{Text: raw, Truncated: truncated, Err: &ProtocolError{Text: raw, Truncated: truncated, Err: err}}
}

// ---- how a stream ended

// exitState is what is known about a command that is over.
type exitState struct {
	exit   int
	signal string
	stderr string
}

// tracker follows the events of a stream, to say at the end if the stream is whole.
type tracker struct {
	end     *EndEvent
	refused *RefusedEvent
	coreErr *ErrorEvent
	created *NewWorktreeEvent
	started bool
	partial string
}

func (t *tracker) observe(value any) {
	switch ev := value.(type) {
	case EndEvent:
		t.end = &ev
	case RefusedEvent:
		t.refused = &ev
	case ErrorEvent:
		t.coreErr = &ev
	case NewWorktreeEvent:
		t.created = &ev
	case StartEvent:
		t.started = true
	case BadLine:
		if ev.Truncated {
			t.partial = ev.Text
		}
	}
}

// result classifies the end of a stream. A stream that came to its last event is
// whole, also when somebody asked it to stop at the same moment. cause is the
// cause of its context, or nil.
func (t *tracker) result(state exitState, cause error) Result {
	r := Result{Exit: state.exit, Signal: state.signal, Stderr: state.stderr, End: t.end, Created: t.created}
	switch {
	case t.refused != nil:
		r.Err = &RefusedError{Code: t.refused.Code, Message: t.refused.Message, Plan: t.refused.Plan}
	case t.coreErr != nil:
		r.Err = &CoreError{Code: t.coreErr.Code, Message: t.coreErr.Message, Exit: state.exit}
	case t.end != nil:
		// exit 1 is the answer for a failed step and for a cancel, and for nothing else
		if state.exit != 0 && !((t.end.Failed > 0 || t.end.Cancelled) && state.exit == ExitFailure) {
			r.Err = &ExitError{Exit: state.exit, Stderr: state.stderr}
		}
	case t.created != nil && !t.started && state.exit == ExitOK:
		// core new --no-open and --dry-run end with this one line
	case cause != nil:
		r.Stopped = true
		r.Err = cause
	default:
		r.Err = &ChildDiedError{Exit: state.exit, Signal: state.signal, Stderr: state.stderr, Partial: t.partial}
	}
	return r
}
