package model

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/afruth/wtree/tui/internal/core"
	"github.com/afruth/wtree/tui/internal/term"
	"github.com/afruth/wtree/tui/internal/theme"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/teatest/v2"
)

// The helper process runs the whole program, on a pipe and with the fake. The tests below
// send it signals. It starts no real command.
func TestMain(m *testing.M) {
	if os.Getenv("WTREE_TUI_HELPER") == "1" {
		os.Exit(helperMain())
	}
	os.Exit(m.Run())
}

func helperMain() int {
	fake := core.NewFakeClient()
	var panicAfter time.Duration
	if v := os.Getenv("WTREE_TUI_PANIC"); v != "" {
		panicAfter, _ = time.ParseDuration(v)
	}
	return Run(Options{
		Client: fake, Cwd: "/fixture", Home: "/fixture", Theme: theme.Options{Profile: theme.ANSI256},
		ThemeForced: true, ASCIIForced: true, PanicAfter: panicAfter, Caps: term.Caps{Rex: os.Getenv("WTREE_TUI_REX") == "1"},
	}, RunConfig{NoInput: true, KillWait: 500 * time.Millisecond})
}

type helper struct {
	cmd  *exec.Cmd
	out  *bytes.Buffer
	done chan error
}

func startHelper(t *testing.T, env ...string) *helper {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^$")
	cmd.Env = append(os.Environ(), "WTREE_TUI_HELPER=1", "TERM=xterm-256color", "HOME=/nonexistent")
	cmd.Env = append(cmd.Env, env...)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout = w
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	w.Close()
	h := &helper{cmd: cmd, out: &bytes.Buffer{}, done: make(chan error, 1)}
	copied := make(chan struct{})
	go func() {
		_, _ = io.Copy(h.out, r)
		close(copied)
	}()
	go func() {
		err := cmd.Wait()
		<-copied
		h.done <- err
	}()
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	return h
}

func (h *helper) wait(t *testing.T) int {
	t.Helper()
	select {
	case <-h.done:
	case <-time.After(10 * time.Second):
		t.Fatal("the program did not end")
	}
	return h.cmd.ProcessState.ExitCode()
}

// assertRestored: the reset is the last thing written, whatever came before it.
func assertRestored(t *testing.T, out string) {
	t.Helper()
	if !strings.HasSuffix(out, term.ModesOff) {
		tail := out
		if len(tail) > 120 {
			tail = tail[len(tail)-120:]
		}
		t.Fatalf("the reset is not the last thing written: %q", tail)
	}
	for _, seq := range []string{"\x1b[?1006l", "\x1b[?1002l", "\x1b[?1000l", "\x1b[?2004l", "\x1b[?25h"} {
		if !strings.Contains(out, seq) {
			t.Errorf("no %q", seq)
		}
	}
	if strings.Contains(out, "\x1b[?1049h") && !strings.Contains(out, "\x1b[?1049l") {
		t.Error("the alternate screen stays on")
	}
}

func TestSignalsRestoreTheTerminal(t *testing.T) {
	if testing.Short() {
		t.Skip("starts processes")
	}
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGHUP, syscall.SIGPIPE, syscall.SIGINT} {
		t.Run(sig.String(), func(t *testing.T) {
			h := startHelper(t)
			time.Sleep(300 * time.Millisecond)
			if err := h.cmd.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			code := h.wait(t)
			if code != 128+int(sig) {
				t.Fatalf("exit %d", code)
			}
			assertRestored(t, h.out.String())
			if !strings.Contains(h.out.String(), "\x1b[?1049l") {
				t.Fatal("a signal leaves the alternate screen")
			}
		})
	}
}

func TestPanicRestoresTheTerminal(t *testing.T) {
	if testing.Short() {
		t.Skip("starts processes")
	}
	h := startHelper(t, "WTREE_TUI_PANIC=150ms")
	code := h.wait(t)
	if code != ExitFatal {
		t.Fatalf("exit %d", code)
	}
	assertRestored(t, h.out.String())
	if !strings.Contains(h.out.String(), term.Emergency) {
		t.Fatal("a panic gets the whole reset")
	}
}

func TestOSCOnlyInRexOnTheWire(t *testing.T) {
	if testing.Short() {
		t.Skip("starts processes")
	}
	plain := startHelper(t)
	time.Sleep(300 * time.Millisecond)
	_ = plain.cmd.Process.Signal(syscall.SIGTERM)
	plain.wait(t)
	for _, seq := range []string{"\x1b]7501;", "\x1b]9;4;"} {
		if strings.Contains(plain.out.String(), seq) {
			t.Fatalf("%q outside Rex", seq)
		}
	}
	rex := startHelper(t, "WTREE_TUI_REX=1")
	time.Sleep(300 * time.Millisecond)
	_ = rex.cmd.Process.Signal(syscall.SIGTERM)
	rex.wait(t)
	if !strings.Contains(rex.out.String(), "\x1b]7501;") {
		t.Fatal("no OSC 7501 in Rex")
	}
}

// ---- through the real decoder and renderer of Bubble Tea

func realModel(fake *core.FakeClient) *Model {
	return New(Options{Client: fake, Cwd: "/fixture", Home: "/fixture/home", Theme: theme.Options{Profile: theme.ANSI256}, ThemeForced: true})
}

func waitScreen(t *testing.T, tm *teatest.TestModel, text string) {
	t.Helper()
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return strings.Contains(ansi.Strip(string(b)), text)
	}, teatest.WithDuration(5*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}

// A phone client can send esc as CSI 27 u. This goes through the decoder of Bubble Tea,
// and the model closes an overlay on what comes out.
func TestCSI27uIsEscOnTheWire(t *testing.T) {
	in, out := io.Pipe()
	defer out.Close()
	keys := make(chan string, 16)
	m := realModel(core.NewFakeClient())
	p := tea.NewProgram(m, tea.WithInput(in), tea.WithOutput(io.Discard), tea.WithWindowSize(120, 35), tea.WithoutSignalHandler(),
		tea.WithColorProfile(colorprofile.ANSI256),
		tea.WithFilter(func(_ tea.Model, msg tea.Msg) tea.Msg {
			if k, ok := msg.(tea.KeyPressMsg); ok {
				keys <- keyName(k)
			}
			return msg
		}))
	finished := make(chan error, 1)
	go func() {
		_, err := p.Run()
		finished <- err
	}()
	want := func(name string) {
		t.Helper()
		select {
		case k := <-keys:
			if k != name {
				t.Fatalf("key %q, want %q", k, name)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no key %q", name)
		}
	}
	_, _ = out.Write([]byte("?"))
	want("?")
	_, _ = out.Write([]byte("\x1b[27u"))
	want("esc")
	_, _ = out.Write([]byte("q"))
	want("q")
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		p.Kill()
		t.Fatal("q did not quit: the overlay was still open, so esc did not close it")
	}
	if m.mode != ModeList {
		t.Fatalf("mode %d", m.mode)
	}
}

func TestRotationThroughTheRenderer(t *testing.T) {
	m := realModel(core.NewFakeClient())
	tm := teatest.NewTestModel(t, m,
		teatest.WithInitialTermSize(93, 14),
		teatest.WithProgramOptions(tea.WithColorProfile(colorprofile.ANSI256)))
	waitScreen(t, tm, "worktree")
	tm.Send(tea.WindowSizeMsg{Width: 48, Height: 41})
	waitScreen(t, tm, "delete")
	tm.Type("q")
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
	if m.layout.Width != 48 || m.layout.Height != 41 || m.layout.Short {
		t.Fatalf("%+v", m.layout)
	}
}

func TestManyEventsCoalesce(t *testing.T) {
	var data bytes.Buffer
	data.WriteString(`{"v":1,"event":"stage","stage":"structure"}` + "\n")
	for i := 0; i < 600; i++ {
		data.WriteString(`{"v":1,"event":"stack","stack":{"project":"p` + strconv.Itoa(i) + `","dir":null,"dirs":[],"kind":"volumes","worktree":null,"common":null,"worktrees":[],"states":{},"containers":[],"volumes":1}}` + "\n")
	}
	data.WriteString(`{"v":1,"event":"stage","stage":"structure","done":true,"ms":1}` + "\n" + `{"v":1,"event":"end","ms":1}` + "\n")
	h := newHarness(t, 80, 24)
	h.fake.On("list", core.FakeScript{Data: data.Bytes()})
	h.start()
	if h.m.tree.Summary().Stacks != 600 {
		t.Fatalf("%d", h.m.tree.Summary().Stacks)
	}
	h.checkFrame()
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// A terminal that says that it knows DEC 2026 gets each frame in one synchronized update.
func TestSynchronizedFramesOnATerminalThatKnowsThem(t *testing.T) {
	in, reply := io.Pipe()
	defer reply.Close()
	var out lockedBuffer
	p := tea.NewProgram(realModel(core.NewFakeClient()), tea.WithInput(in), tea.WithOutput(&out), tea.WithWindowSize(120, 35),
		tea.WithoutSignalHandler(), tea.WithColorProfile(colorprofile.ANSI256), tea.WithEnvironment([]string{"TERM=xterm-256color"}))
	finished := make(chan error, 1)
	go func() {
		_, err := p.Run()
		finished <- err
	}()
	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(out.String(), "\x1b[?2026$p") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(out.String(), "\x1b[?2026$p") {
		t.Fatal("no query for DEC 2026")
	}
	_, _ = reply.Write([]byte("\x1b[?2026;2$y")) // known, and off now
	time.Sleep(50 * time.Millisecond)
	_, _ = reply.Write([]byte("?"))
	for !strings.Contains(out.String(), "\x1b[?2026h") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(out.String(), "\x1b[?2026h") {
		t.Fatal("frames are not synchronized")
	}
	_, _ = reply.Write([]byte("q"))
	_, _ = reply.Write([]byte("q"))
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		p.Kill()
	}
}
