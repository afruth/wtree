package core

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// ErrSuperseded is the cause of a list that a newer list replaced. See Client.List.
var ErrSuperseded = errors.New("core: a newer list replaced this one")

// ContractError says that the script speaks another contract. Refuse to start.
type ContractError struct {
	Script string // the version of the script, when it told one
	Got    int
	Want   int
}

func (e *ContractError) Error() string {
	return fmt.Sprintf("wtree %s speaks contract %d, and this program knows contract %d", safeText(e.Script), e.Got, e.Want)
}

// StartError says that the command did not start: nothing ran.
type StartError struct {
	Path string
	Err  error
}

func (e *StartError) Error() string {
	return fmt.Sprintf("cannot start %s: %v", safeText(e.Path), e.Err)
}

func (e *StartError) Unwrap() error { return e.Err }

// CoreError is the error event: the last line of a command that failed.
type CoreError struct {
	Code    string // usage, not_a_repo or internal
	Message string
	Exit    int
}

func (e *CoreError) Error() string { return safeText(e.Message) }

// RefusedError is the refused event: apply or new did nothing. On plan_stale, Plan
// is the new plan. Show its consequences and ask again, and never retry on your own.
type RefusedError struct {
	Code    string
	Message string
	Plan    *Plan
}

func (e *RefusedError) Error() string { return safeText(e.Message) }

// ProtocolError says that an answer is not what the contract promises. Text is
// the raw output, which the contract tells a UI to show in the status area.
type ProtocolError struct {
	Text      string
	Truncated bool // the output stopped in the middle of a line
	Err       error
}

func (e *ProtocolError) Error() string {
	if e.Truncated {
		return "wtree stopped in the middle of a line: " + safeText(e.Text)
	}
	return "wtree wrote something that is not an event: " + safeText(e.Text)
}

func (e *ProtocolError) Unwrap() error { return e.Err }

// ChildDiedError says that a stream ended with no last event, and nobody asked it
// to stop. What a state-changing command did is not known, so reload the list.
type ChildDiedError struct {
	Exit   int    // -1 when a signal ended it
	Signal string // the name of the signal, or ""
	Stderr string
	// Partial is the text of a last line that did not end.
	Partial string
}

func (e *ChildDiedError) Error() string {
	how := fmt.Sprintf("with exit code %d", e.Exit)
	if e.Signal != "" {
		how = "on signal " + e.Signal
	}
	msg := "wtree ended " + how + " before its last event"
	if tail := lastLine(e.Stderr); tail != "" {
		msg += ": " + safeText(tail)
	}
	return msg
}

// ExitError says that a command failed and did not explain itself with an event.
type ExitError struct {
	Exit   int
	Stderr string
}

func (e *ExitError) Error() string {
	msg := fmt.Sprintf("wtree failed with exit code %d", e.Exit)
	if tail := lastLine(e.Stderr); tail != "" {
		msg += ": " + safeText(tail)
	}
	return msg
}

func lastLine(text string) string {
	text = strings.TrimSpace(text)
	if i := strings.LastIndexByte(text, '\n'); i >= 0 {
		return text[i+1:]
	}
	return text
}

// maxErrorText limits what an error message carries to a status area.
const maxErrorText = 512

// safeText makes output safe to put in an error message, which a UI draws as it
// is: valid UTF-8, no control characters, and not too long. The raw fields of the
// errors keep the original text.
func safeText(text string) string {
	text = strings.ToValidUTF8(text, "�")
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return '�'
		}
		return r
	}, text)
	if len(text) > maxErrorText {
		text = strings.ToValidUTF8(text[:maxErrorText], "") + "…"
	}
	return text
}
