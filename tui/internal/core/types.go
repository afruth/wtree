// Package core is the client side of the wtree core contract.
//
// This file mirrors docs/core-contract.md, contract version 1. It holds the
// types and DecodeEvent, and no policy: the script bin/wtree decides what is
// safe, and a UI renders a Plan and sends a choice back. A UI must not add, drop
// or reorder a check, and must not compute a confirmation phrase.
//
// Rules for a consumer:
//
//   - Ignore unknown fields and unknown events. New ones can come without a
//     version bump. A field that goes or changes its type bumps the version.
//   - Treat a stdout line that is not valid JSON as an internal error, and show
//     its text in the status area.
//   - Sanitize text before you draw it. A string can hold control characters and
//     escape sequences from a lock reason, a commit subject or a directory.
//   - A pointer field is null in JSON when the value is not known, or does not
//     apply. Show that as "not known" and never as zero.
package core

import (
	"encoding/json"
	"fmt"
)

// ContractVersion is the contract that this package understands. Compare it with
// Version.Contract before anything else, and refuse to start on a mismatch.
const ContractVersion = 1

// Exit codes of the wtree core commands.
const (
	ExitOK          = 0 // success
	ExitFailure     = 1 // a step failed, a step was cancelled, or an internal error
	ExitUsage       = 2 // a bad command line
	ExitEnvironment = 3 // not a git repository, or a directory that does not exist
	ExitRefused     = 4 // apply or new refused, and nothing ran
)

// Version is the answer of `wtree core version`.
type Version struct {
	V        int    `json:"v"`
	Script   string `json:"script"`
	Contract int    `json:"contract"`
}

// ---- core list: objects

// Repo is one repository. Common is its git common directory, and it is the key
// that a worktree and a stack refer to.
type Repo struct {
	Common        string  `json:"common"`
	Name          string  `json:"name"`
	Main          string  `json:"main"`
	DefaultBranch *string `json:"default_branch"` // origin/HEAD, else main, else master
}

// Worktree is one worktree, in the order of git, with the main checkout first.
type Worktree struct {
	Path     string  `json:"path"`
	Common   string  `json:"common"`
	Repo     string  `json:"repo"`
	Main     bool    `json:"main"`
	Branch   *string `json:"branch"` // null when detached, or on a bare entry
	Detached bool    `json:"detached"`
	Head     *string `json:"head"`    // the full hash
	Missing  bool    `json:"missing"` // its directory is gone, also when it is locked
	Bare     bool    `json:"bare"`
	Locked   *string `json:"locked"` // null: not locked. "": locked with no reason
	Current  bool    `json:"current"`
	// Odd means that this path cannot come back as a target: it has a tab or a
	// newline, or bytes that are not UTF-8 and show as U+FFFD. Offer no action on it.
	Odd bool `json:"odd"`
}

// Container is one container of a Stack.
type Container struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Service   string   `json:"service"`
	State     string   `json:"state"`
	Health    *string  `json:"health"` // healthy, unhealthy, starting, or null
	Status    string   `json:"status"` // the text of docker, such as "Up 2 hours (healthy)"
	Ports     string   `json:"ports"`  // the text of docker
	Published []string `json:"published"`
}

// Stack kinds.
const (
	StackLive    = "live"    // its directory is in a git repo
	StackPlain   = "plain"   // its directory exists, in no repo (machine scope)
	StackGone    = "gone"    // its directory is gone
	StackVolumes = "volumes" // only volumes are left
)

// Stack is one Compose project in scope. Project is its key.
type Stack struct {
	Project    string         `json:"project"`
	Dir        *string        `json:"dir"`
	Dirs       []string       `json:"dirs"`
	Kind       string         `json:"kind"`
	Worktree   *string        `json:"worktree"` // the longest-prefix owner of Dir, the parent in the tree
	Common     *string        `json:"common"`
	Worktrees  []string       `json:"worktrees"` // the owners of all of Dirs, in the order of the list
	States     map[string]int `json:"states"`    // only states with a count above zero
	Containers []Container    `json:"containers"`
	Volumes    *int           `json:"volumes"` // null if docker volume ls did not answer
}

// WorktreeGit is the event worktree_git, one for each worktree that is not bare.
type WorktreeGit struct {
	Path    string `json:"path"`
	Missing bool   `json:"missing"`
	// Changed and Untracked are null when Unreadable. Changed also counts the tracked
	// files on the disk with an assume-unchanged or skip-worktree flag.
	Changed           *int    `json:"changed"`
	Untracked         *int    `json:"untracked"`
	Upstream          *string `json:"upstream"`
	UpstreamGone      bool    `json:"upstream_gone"`
	Ahead             *int    `json:"ahead"`
	Behind            *int    `json:"behind"`
	LastCommitAt      *int64  `json:"last_commit_at"` // seconds since the epoch
	LastCommitSubject *string `json:"last_commit_subject"`
	IndexMtime        *int64  `json:"index_mtime"`
	MergedInto        *string `json:"merged_into"` // the default branch, if HEAD is an ancestor of it
	HeadHeld          *bool   `json:"head_held"`   // false: no branch, tag or remote branch holds HEAD. Null when Unreadable
	Unreadable        bool    `json:"unreadable"`
	TimedOut          bool    `json:"timed_out"`
}

// ContainerStats is one container of a StackStats.
type ContainerStats struct {
	ID       string  `json:"id"`
	CPUPct   float64 `json:"cpu_pct"`
	MemBytes int64   `json:"mem_bytes"`
}

// StackStats is the event stack_stats, for a stack with running containers.
type StackStats struct {
	Project    string           `json:"project"`
	CPUPct     float64          `json:"cpu_pct"`
	MemBytes   int64            `json:"mem_bytes"`
	Containers []ContainerStats `json:"containers"`
}

// StackAge is the event stack_age, from the containers of a stack.
type StackAge struct {
	Project        string `json:"project"`
	LastStartedAt  *int64 `json:"last_started_at"`
	LastFinishedAt *int64 `json:"last_finished_at"`
	Running        bool   `json:"running"`
	Stale          bool   `json:"stale"` // stopped for 7 days or more
}

// Engine is the event engine: the whole engine, also what is out of scope.
type Engine struct {
	NCPU     *int     `json:"ncpu"`
	MemTotal *int64   `json:"mem_total"`
	CPUPct   *float64 `json:"cpu_pct"`
	MemBytes *int64   `json:"mem_bytes"`
}

// StackDisk is the event stack_disk, from the cache of the disk sizes.
type StackDisk struct {
	Project    string `json:"project"`
	Bytes      int64  `json:"bytes"`
	AgeS       *int64 `json:"age_s"`
	Refreshing bool   `json:"refreshing"`
}

// DiskTotals is the event disk_totals. All sizes are null before the first scan.
type DiskTotals struct {
	Images     *int64 `json:"images"`
	Containers *int64 `json:"containers"`
	Volumes    *int64 `json:"volumes"`
	Build      *int64 `json:"build"`
	AgeS       *int64 `json:"age_s"`
	Refreshing bool   `json:"refreshing"`
}

// Reason is a machine code with a sentence for a human.
type Reason struct {
	Code string `json:"code"`
	Text string `json:"text"`
}

// Cleanup tiers. Safe means that wtree expects no refusal. It does not skip the
// plan, and the plan is the authority.
const (
	TierSafe      = "safe"
	TierNeedsLook = "needs_look"
	TierNever     = "never"
)

// CleanupRow is the event cleanup: one row of the cleanup view. A stack in several
// worktrees is in Stacks of each row, and its Bytes count in the first.
type CleanupRow struct {
	ID      string   `json:"id"`   // worktree:<path> or stack:<project>
	Kind    string   `json:"kind"` // worktree or stack
	Label   string   `json:"label"`
	Tier    string   `json:"tier"`
	Reasons []Reason `json:"reasons"`
	Bytes   *int64   `json:"bytes"`  // the reclaimable disk that the cache knows
	Stacks  []string `json:"stacks"` // the stacks whose disk a worktree row counts
}

// CleanupTotals is the event cleanup_totals. Unknown means that a row has no size.
type CleanupTotals struct {
	Safe      int64 `json:"safe"`
	NeedsLook int64 `json:"needs_look"`
	Never     int64 `json:"never"`
	Unknown   bool  `json:"unknown"`
}

// Warning is the event warning. A stage that fails warns and the list goes on.
type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Warning codes.
const (
	WarnDockerMissing      = "docker_missing"
	WarnDockerTimeout      = "docker_timeout"
	WarnDockerDown         = "docker_down"
	WarnVolumesUnavailable = "volumes_unavailable"
	WarnStatsUnavailable   = "stats_unavailable"
	WarnInfoUnavailable    = "info_unavailable"
	WarnInspectUnavailable = "inspect_unavailable"
	WarnGitUnreadable      = "git_unreadable"
	WarnRepoScanRunning    = "repo_scan_running"
	WarnOddStackRow        = "odd_stack_row"
)

// ListDoc is the answer of `wtree core list` without --stream.
type ListDoc struct {
	V             int            `json:"v"`
	Repos         []Repo         `json:"repos"`
	Worktrees     []Worktree     `json:"worktrees"`
	Stacks        []Stack        `json:"stacks"`
	WorktreeGit   []WorktreeGit  `json:"worktree_git"`
	StackStats    []StackStats   `json:"stack_stats"`
	StackAge      []StackAge     `json:"stack_age"`
	StackDisk     []StackDisk    `json:"stack_disk"`
	Engine        *Engine        `json:"engine"`
	DiskTotals    *DiskTotals    `json:"disk_totals"`
	Cleanup       []CleanupRow   `json:"cleanup"`
	CleanupTotals *CleanupTotals `json:"cleanup_totals"`
	Warnings      []Warning      `json:"warnings"`
}

// Stages of core list --stream, in their order.
const (
	StageStructure = "structure"
	StageGit       = "git"
	StageStats     = "stats"
	StageDisk      = "disk"
	StageCleanup   = "cleanup"
)

// ---- core plan

// Plan actions.
const (
	ActionDeleteWorktrees = "delete-worktrees"
	ActionDeleteStacks    = "delete-stacks"
	ActionStopStacks      = "stop-stacks"
	ActionOpen            = "open"
)

// Confirm kinds.
const (
	ConfirmNone   = "none"   // apply needs no --choice
	ConfirmKey    = "key"    // ask one key, then send --choice
	ConfirmPhrase = "phrase" // ask for each phrase, then send --choice and --phrase for each, in order
)

// Reason codes of an Item that has Go false.
const (
	ReasonMainCheckout      = "main_checkout"
	ReasonCurrent           = "current"
	ReasonNotRegistered     = "not_registered"
	ReasonLocked            = "locked"
	ReasonNestedWorktree    = "nested_worktree"
	ReasonOddPath           = "odd_path"
	ReasonDirty             = "dirty"              // Force level can take it
	ReasonUnheldCommit      = "unheld_commit"      // Force level can take it, also on a missing directory
	ReasonUnreadable        = "unreadable"         // Force level can take it
	ReasonNestedRepo        = "nested_repo"        // Force level can take it, with a phrase
	ReasonScanTimeout       = "scan_timeout"       // Force level can take it, with a phrase
	ReasonOtherStack        = "other_stack"        // a stack that is not selected runs from this worktree
	ReasonProtectedStack    = "protected_stack"    // a protected stack in the plan keeps all the worktrees
	ReasonDockerUnavailable = "docker_unavailable" // docker cannot say what the delete takes
	ReasonNoDirectory       = "no_directory"
)

// Flags are the flags of the plan, which apply needs again.
type Flags struct {
	Force   bool `json:"force"`
	Volumes bool `json:"volumes"`
}

// Consequence is one thing that a plan costs.
type Consequence struct {
	Code   string `json:"code"`
	Text   string `json:"text"`
	Severe bool   `json:"severe"`
	// The fields below are only on a volumes_removed consequence of a Choice.
	Item      string   `json:"item,omitempty"`
	Count     *int     `json:"count,omitempty"`
	Names     []string `json:"names,omitempty"`
	Bytes     *int64   `json:"bytes,omitempty"`
	Protected bool     `json:"protected,omitempty"`
}

// Item is one worktree, stack or directory in a Plan. Go false lists it under
// what stays, with its Reason. Offered marks an item that only a Choice adds.
type Item struct {
	ID           string        `json:"id"`   // worktree:<path>, stack:<project> or dir:<path>
	Kind         string        `json:"kind"` // worktree, stack or dir
	Label        string        `json:"label"`
	Go           bool          `json:"go"`
	Offered      bool          `json:"offered"`
	Reason       *Reason       `json:"reason,omitempty"`
	ForceOK      bool          `json:"force_ok,omitempty"` // the force level of this action can take it
	Note         string        `json:"note,omitempty"`     // why this needs force, on a go item
	Codes        []string      `json:"codes,omitempty"`    // all risk codes
	Path         string        `json:"path,omitempty"`
	Common       string        `json:"common,omitempty"`
	OnlyVolumes  bool          `json:"only_volumes,omitempty"` // a stack with no containers left
	Consequences []Consequence `json:"consequences"`
	Steps        []string      `json:"steps"`
}

// Step kinds.
const (
	StepDeleteWorktree = "delete_worktree"
	StepRemoveMissing  = "remove_missing"
	StepStackDown      = "stack_down"
	StepStackStop      = "stack_stop"
	StepOpen           = "open"
)

// Step is a step that a Choice can run. It runs when all steps in After are ok.
type Step struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Kind    string   `json:"kind"`
	Item    string   `json:"item"`
	Force   bool     `json:"force"`
	Volumes bool     `json:"volumes"`
	After   []string `json:"after"`
}

// Phrase is what the human must type, exactly: no trimming and no folding of case.
type Phrase struct {
	Phrase string `json:"phrase"`
	Hint   string `json:"hint"`
}

// Confirm says how to confirm a Choice. A UI shows the phrases, and never derives one.
type Confirm struct {
	Kind    string   `json:"kind"`
	Phrases []Phrase `json:"phrases"`
}

// Choice is one way to run a Plan. The first has Default true, and it is the mildest.
type Choice struct {
	ID           string        `json:"id"`
	Text         string        `json:"text"`
	Default      bool          `json:"default"`
	Available    bool          `json:"available"`
	Reason       string        `json:"reason,omitempty"` // why it is not available
	Adds         []string      `json:"adds"`             // item ids that it adds
	Steps        []string      `json:"steps"`            // step ids, in order
	Confirm      Confirm       `json:"confirm"`
	Consequences []Consequence `json:"consequences"`
}

// Note is a sentence that belongs to the whole Plan.
type Note struct {
	Code string `json:"code"`
	Text string `json:"text"`
}

// Plan is the answer of `wtree core plan`. It is advice: apply computes it again,
// and it refuses when the PlanID differs.
type Plan struct {
	V           int      `json:"v"`
	PlanID      string   `json:"plan_id"`
	Action      string   `json:"action"`
	Flags       Flags    `json:"flags"`
	Cwd         string   `json:"cwd"`
	Items       []Item   `json:"items"`
	Steps       []Step   `json:"steps"`
	Choices     []Choice `json:"choices"`
	Confirm     Confirm  `json:"confirm"` // the Confirm of the default Choice
	Notes       []Note   `json:"notes"`
	NothingToDo bool     `json:"nothing_to_do"`
}

// ---- events

// Event names.
const (
	EventStage         = "stage"
	EventRepo          = "repo"
	EventWorktree      = "worktree"
	EventStack         = "stack"
	EventWorktreeGit   = "worktree_git"
	EventStackStats    = "stack_stats"
	EventStackAge      = "stack_age"
	EventEngine        = "engine"
	EventStackDisk     = "stack_disk"
	EventDiskTotals    = "disk_totals"
	EventCleanup       = "cleanup"
	EventCleanupTotals = "cleanup_totals"
	EventWarning       = "warning"
	EventStart         = "start"
	EventStep          = "step"
	EventOutput        = "output"
	EventEnd           = "end"
	EventRefused       = "refused"
	EventError         = "error"
	EventCreated       = "created"
	EventPlanNew       = "plan_new"
)

// Step states.
const (
	StepRunning = "running"
	StepOK      = "ok"
	StepFailed  = "failed"
	StepSkipped = "skipped"
)

// Codes of a RefusedEvent, for all of which nothing ran.
const (
	RefusedPlanStale         = "plan_stale"
	RefusedPhraseMismatch    = "phrase_mismatch"
	RefusedNoChoice          = "no_choice"
	RefusedBadChoice         = "bad_choice"
	RefusedChoiceUnavailable = "choice_unavailable"
	RefusedNothingToDo       = "nothing_to_do"
	RefusedBadName           = "bad_name"
	RefusedNameTaken         = "name_taken"
)

// Codes of an ErrorEvent.
const (
	ErrorUsage    = "usage"
	ErrorNotARepo = "not_a_repo"
	ErrorInternal = "internal"
)

// StageEvent starts a stage, or ends it with Done and MS.
type StageEvent struct {
	Stage string `json:"stage"`
	Done  bool   `json:"done"`
	MS    int64  `json:"ms"`
}

// RepoEvent carries a Repo.
type RepoEvent struct {
	Repo Repo `json:"repo"`
}

// WorktreeEvent carries a Worktree.
type WorktreeEvent struct {
	Worktree Worktree `json:"worktree"`
}

// StackEvent carries a Stack.
type StackEvent struct {
	Stack Stack `json:"stack"`
}

// StartEvent begins an apply, an open, or the open step of a new.
type StartEvent struct {
	PlanID string `json:"plan_id"`
	Steps  int    `json:"steps"`
}

// StepEvent is one change of state of one step.
type StepEvent struct {
	ID      string   `json:"id"`
	State   string   `json:"state"`
	Label   string   `json:"label,omitempty"`   // on running
	MS      int64    `json:"ms,omitempty"`      // on ok and failed
	Status  int      `json:"status,omitempty"`  // on failed
	Tail    []string `json:"tail,omitempty"`    // on failed: the last 8 lines of output
	Because string   `json:"because,omitempty"` // on skipped: a step id, or "cancelled"
}

// OutputEvent is one line of output of a running step.
type OutputEvent struct {
	ID   string `json:"id"`
	Line string `json:"line"`
}

// EndEvent is the last line of a stream. A list has MS, and an apply has the counts.
type EndEvent struct {
	MS          int64 `json:"ms,omitempty"`
	OK          int   `json:"ok"`
	Failed      int   `json:"failed"`
	Skipped     int   `json:"skipped"`
	DiskRefresh bool  `json:"disk_refresh"`
	Cancelled   bool  `json:"cancelled"`
}

// RefusedEvent says that apply or new did nothing. On plan_stale, Plan is the new
// plan: show its consequences, and ask again.
type RefusedEvent struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Plan    *Plan  `json:"plan,omitempty"`
}

// ErrorEvent is the last line of a command that failed.
type ErrorEvent struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// NewWorktreeEvent is the event created, or plan_new for --dry-run.
type NewWorktreeEvent struct {
	Created bool   `json:"-"` // true for the event created
	Path    string `json:"path"`
	Name    string `json:"name"`
	Head    string `json:"head"`
}

// UnknownEvent is an event of a newer script. Ignore it.
type UnknownEvent struct {
	Name string
	Raw  json.RawMessage
}

// DecodeEvent decodes one line of a stream into the type of its event: a value of
// StageEvent, RepoEvent, WorktreeEvent, StackEvent, WorktreeGit, StackStats,
// StackAge, Engine, StackDisk, DiskTotals, CleanupRow, CleanupTotals, Warning,
// StartEvent, StepEvent, OutputEvent, EndEvent, RefusedEvent, ErrorEvent,
// NewWorktreeEvent or UnknownEvent. A line that is not valid JSON is an error.
func DecodeEvent(line []byte) (any, error) {
	var head struct {
		V     int    `json:"v"`
		Event string `json:"event"`
	}
	if err := json.Unmarshal(line, &head); err != nil {
		return nil, fmt.Errorf("core: not an event: %w", err)
	}
	if head.V != ContractVersion {
		return nil, fmt.Errorf("core: contract %d, this program knows %d", head.V, ContractVersion)
	}
	switch head.Event {
	case EventStage:
		return decode[StageEvent](line)
	case EventRepo:
		return decode[RepoEvent](line)
	case EventWorktree:
		return decode[WorktreeEvent](line)
	case EventStack:
		return decode[StackEvent](line)
	case EventWorktreeGit:
		return decode[WorktreeGit](line)
	case EventStackStats:
		return decode[StackStats](line)
	case EventStackAge:
		return decode[StackAge](line)
	case EventEngine:
		return decode[Engine](line)
	case EventStackDisk:
		return decode[StackDisk](line)
	case EventDiskTotals:
		return decode[DiskTotals](line)
	case EventCleanup:
		return decode[CleanupRow](line)
	case EventCleanupTotals:
		return decode[CleanupTotals](line)
	case EventWarning:
		return decode[Warning](line)
	case EventStart:
		return decode[StartEvent](line)
	case EventStep:
		return decode[StepEvent](line)
	case EventOutput:
		return decode[OutputEvent](line)
	case EventEnd:
		return decode[EndEvent](line)
	case EventRefused:
		return decode[RefusedEvent](line)
	case EventError:
		return decode[ErrorEvent](line)
	case EventCreated, EventPlanNew:
		ev, err := decode[NewWorktreeEvent](line)
		ev.Created = head.Event == EventCreated
		return ev, err
	}
	return UnknownEvent{Name: head.Event, Raw: json.RawMessage(line)}, nil
}

func decode[T any](line []byte) (T, error) {
	var value T
	err := json.Unmarshal(line, &value)
	return value, err
}
