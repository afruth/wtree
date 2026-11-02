// Command wtree-tui is the full-screen interface of wtree. It is a view: it runs the
// script `wtree core` and shows what that says. All safety policy is in the script.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/afruth/wtree/tui/internal/core"
	"github.com/afruth/wtree/tui/internal/model"
	tuiterm "github.com/afruth/wtree/tui/internal/term"
	"github.com/afruth/wtree/tui/internal/theme"
	"github.com/charmbracelet/x/term"
)

// version is set by the build.
var version = "dev"

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	fs := flag.NewFlagSet("wtree-tui", flag.ContinueOnError)
	var global, stacks, ascii, rex, showVersion, noColorOK, check bool
	var cwd, themeName string
	var panicAfter time.Duration
	fs.BoolVar(&global, "g", false, "machine scope: everything on this host")
	fs.BoolVar(&global, "global", false, "machine scope: everything on this host")
	fs.BoolVar(&stacks, "d", false, "start with the stacks in focus: a flat list of stack rows")
	fs.BoolVar(&stacks, "docker", false, "start with the stacks in focus: a flat list of stack rows")
	fs.BoolVar(&check, "check", false, "exit 0 if the script answers with a contract that this program knows")
	fs.BoolVar(&ascii, "ascii", false, "ASCII glyphs")
	fs.BoolVar(&rex, "rex", false, "report the status to Rex")
	fs.BoolVar(&showVersion, "version", false, "print the version")
	fs.BoolVar(&noColorOK, "no-color", false, "no colour")
	fs.StringVar(&cwd, "cwd", "", "start from this directory")
	fs.StringVar(&themeName, "theme", "", "light or dark")
	fs.DurationVar(&panicAfter, "panic-after", 0, "panic after this time, for the test of the terminal restore")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if showVersion {
		fmt.Println(version)
		return 0
	}
	script, err := findScript()
	if err != nil {
		fmt.Fprintln(os.Stderr, "wtree:", err)
		return 2
	}
	client := core.NewExecClient(core.Config{Path: script})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := client.Version(ctx); err != nil {
		var contract *core.ContractError
		if errors.As(err, &contract) {
			fmt.Fprintf(os.Stderr, "wtree: %v. Install wtree and wtree-tui from the same release.\n", err)
			return 2
		}
		fmt.Fprintf(os.Stderr, "wtree: %s does not answer: %v\n", script, err)
		return 2
	}
	if check {
		return 0
	}

	getenv := os.Getenv
	if noColorOK {
		getenv = func(name string) string {
			if name == "NO_COLOR" {
				return "1"
			}
			return os.Getenv(name)
		}
	}
	opts := theme.Detect(getenv)
	forced := os.Getenv("WTREE_THEME") != ""
	switch strings.ToLower(themeName) {
	case "light":
		opts.Light, forced = true, true
	case "dark":
		opts.Light, forced = false, true
	}
	asciiForced := os.Getenv("WTREE_ASCII") != ""
	if ascii {
		opts.ASCII, asciiForced = true, true
	}
	if cwd == "" {
		if dir, err := os.Getwd(); err == nil {
			cwd = dir
		}
	}
	home, _ := os.UserHomeDir()
	stateDir := stateDirOf(home)
	prefs := model.LoadPrefs(stateDir)
	// The screen goes to stdout. Captured, as in cd $(wtree --tui), it would draw into the capture
	// and leave a blank terminal that still takes keys.
	if !term.IsTerminal(os.Stdout.Fd()) {
		fmt.Fprintln(os.Stderr, "wtree: the full screen needs a terminal on stdout. Use --fzf, or wtree --list for text.")
		return 2
	}
	return model.Run(model.Options{
		Client:      client,
		Cwd:         cwd,
		Home:        home,
		StateDir:    stateDir,
		Global:      global,
		Stacks:      stacks,
		Theme:       opts,
		ThemeForced: forced,
		ASCIIForced: asciiForced,
		Caps:        tuiterm.Detect(os.Getenv, rex),
		Prefs:       prefs,
		PanicAfter:  panicAfter,
	}, model.RunConfig{})
}

func stateDirOf(home string) string {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "wtree")
	}
	if home != "" {
		return filepath.Join(home, ".local", "state", "wtree")
	}
	return ""
}

// findScript finds the script: WTREE_SCRIPT, or the file next to this binary.
func findScript() (string, error) {
	if path := os.Getenv("WTREE_SCRIPT"); path != "" {
		return path, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	path := filepath.Join(filepath.Dir(exe), "wtree")
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		return path, nil
	}
	return "", fmt.Errorf("cannot find the script %s. Install it next to this program, or set WTREE_SCRIPT", path)
}
