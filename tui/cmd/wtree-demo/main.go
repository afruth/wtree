// Command wtree-demo runs the interface on recorded data. It starts no process and it
// cannot touch anything, because it uses core.FakeClient. It is for looking at the
// screens, and for tests: the flags say which data and how slowly it comes.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/afruth/wtree/tui/internal/core"
	"github.com/afruth/wtree/tui/internal/model"
	"github.com/afruth/wtree/tui/internal/term"
	"github.com/afruth/wtree/tui/internal/theme"
	"github.com/afruth/wtree/tui/internal/tree/treetest"
)

func main() {
	scene := flag.String("scene", "repo", "repo, machine, recorded, docker-down")
	delay := flag.Duration("delay", 0, "time before each line of a list")
	applyDelay := flag.Duration("apply-delay", 0, "time before each line of an apply")
	apply := flag.String("apply", "apply-ok.ndjson", "the recording that an apply replays")
	listPause := flag.Int("list-pause", 0, "hold a list after this many lines, -1: before the first")
	applyPause := flag.Int("apply-pause", 0, "hold an apply after this many lines")
	stateDir := flag.String("state", "", "where to keep ui.json and logs")
	ascii := flag.Bool("ascii", false, "ASCII glyphs")
	rex := flag.Bool("rex", false, "report the status to Rex")
	light := flag.Bool("light", false, "light theme")
	global := flag.Bool("g", false, "machine scope")
	panicAfter := flag.Duration("panic-after", 0, "panic after this time")
	flag.Parse()

	client := core.NewFakeClient()
	now := time.Now
	home := treetest.Home
	switch *scene {
	case "repo":
		client.On("list", core.FakeScript{Data: ndjson(treetest.Repo()), Delay: *delay})
		client.On("list:global", core.FakeScript{Data: ndjson(treetest.Machine()), Delay: *delay})
		now = func() time.Time { return treetest.Now }
	case "machine":
		client.On("list", core.FakeScript{Data: ndjson(treetest.Machine()), Delay: *delay})
		client.On("list:global", core.FakeScript{Data: ndjson(treetest.Machine()), Delay: *delay})
		now = func() time.Time { return treetest.Now }
	case "recorded":
		home = "/fixture/home"
		client.On("list", core.FakeScript{Fixture: "list-stream.ndjson", Delay: *delay, PauseAfter: *listPause})
		now = func() time.Time { return time.Unix(1767323045+3600, 0) }
	case "docker-down":
		home = "/fixture/home"
		client.On("list", core.FakeScript{Fixture: "list-docker-down.ndjson", Delay: *delay, PauseAfter: *listPause})
		client.On("list:global", core.FakeScript{Fixture: "list-docker-down.ndjson", Delay: *delay})
		now = func() time.Time { return time.Unix(1767323045+3600, 0) }
	default:
		fmt.Fprintln(os.Stderr, "unknown scene")
		os.Exit(2)
	}
	client.On("apply", core.FakeScript{Fixture: *apply, Delay: *applyDelay, PauseAfter: *applyPause, OnCancel: "apply-cancelled.ndjson"})

	opts := theme.Detect(os.Getenv)
	if *ascii {
		opts.ASCII = true
	}
	if *light {
		opts.Light = true
	}
	code := model.Run(model.Options{
		Client: client, Cwd: "/fixture/home/code/proj", Home: home, StateDir: *stateDir,
		Global: *global, Theme: opts, ThemeForced: true, ASCIIForced: *ascii,
		Caps: term.Detect(os.Getenv, *rex), HoldNow: time.Now,
		Now: now, PanicAfter: *panicAfter,
	}, model.RunConfig{})
	os.Exit(code)
}

// ndjson writes a fixture of treetest as the lines that the core would write.
func ndjson(set treetest.Set) []byte {
	var out []byte
	for _, ev := range set.Events() {
		name := eventName(ev)
		if name == "" {
			continue
		}
		data, err := json.Marshal(ev)
		if err != nil {
			panic(err)
		}
		var fields map[string]any
		if err := json.Unmarshal(data, &fields); err != nil {
			panic(err)
		}
		fields["v"] = core.ContractVersion
		fields["event"] = name
		line, err := json.Marshal(fields)
		if err != nil {
			panic(err)
		}
		out = append(append(out, line...), '\n')
	}
	return out
}

func eventName(ev any) string {
	switch ev.(type) {
	case core.StageEvent:
		return core.EventStage
	case core.RepoEvent:
		return core.EventRepo
	case core.WorktreeEvent:
		return core.EventWorktree
	case core.StackEvent:
		return core.EventStack
	case core.WorktreeGit:
		return core.EventWorktreeGit
	case core.StackStats:
		return core.EventStackStats
	case core.StackAge:
		return core.EventStackAge
	case core.StackDisk:
		return core.EventStackDisk
	case core.DiskTotals:
		return core.EventDiskTotals
	case core.CleanupRow:
		return core.EventCleanup
	case core.CleanupTotals:
		return core.EventCleanupTotals
	case core.EndEvent:
		return core.EventEnd
	}
	return ""
}
