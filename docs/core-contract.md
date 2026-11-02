# The wtree core contract

`wtree core` is the machine interface of `bin/wtree`. A UI spawns these commands and reads JSON. The script holds all the policy: it decides what a delete may take, what a plan costs, and which phrase confirms it. A UI renders a plan and sends a choice back.

This file is the single reference, and it stays in step with the code. `tui/internal/core/types.go` mirrors it in Go, and `test/cases/50_core.sh` tests it.

## General rules

| Rule | Detail |
| - | - |
| Version | Every document and event has `"v": 1`. |
| Compatibility | New fields and new events can come without a version bump, so ignore what you do not know. A field that goes or changes its type bumps `contract`. |
| Streams | `core list --stream`, `core apply`, `core open` and `core new` write NDJSON: one JSON object on each line. `core version`, `core plan` and `core list` write one object. |
| Encoding | Output is valid UTF-8. A byte that is not valid UTF-8 becomes U+FFFD. |
| Text is not safe to draw | A string can hold control characters and escape sequences, such as `\u001b[31m` from a lock reason. Sanitize text before you draw it. |
| Null | `null` means not known or not applicable. Show it as such, and never as zero. |
| Terminal | No core command reads `/dev/tty`. All input is on the command line. |
| Time limits | Each read has a limit, so a read cannot hang a command. A read that ran out of time leaves no process behind, except one that the kernel holds. A step that changes state has no limit, except an `open` step at 20 s. |
| Docker | No core command needs Docker to start. A slow or absent engine gives warnings and a usable list. |
| Signals | A `TERM` to `core apply` stops it before the next step, and the step that runs finishes. Send it to the process and not to its group. Other signals end it at once. |

### Exit codes

| Code | Meaning |
| - | - |
| 0 | Success. A list with warnings is a success. |
| 1 | A step failed or was cancelled, or an internal error. |
| 2 | A bad command line. |
| 3 | The environment: not a git repository, or a `--cwd` that does not exist. |
| 4 | Refused: nothing ran. |

### Errors

A command that fails writes one `error` line as its last line on stdout. A line that is not valid JSON is an internal error: show its text in the status area.

```json
{"v":1,"event":"error","code":"usage","message":"unknown core command: nosuch. Use version, list, plan, apply, open or new."}
```

Codes: `usage`, `not_a_repo`, `internal`.

## `wtree core version`

Run it first. It needs no repo and no Docker. Refuse to start on a `contract` that you do not know.

```json
{"v":1,"script":"0.4.0","contract":1}
```

## `wtree core list`

```
wtree core list [--global] [--stream] [--cwd DIR] [--only STAGES] [--no-refresh]
```

| Flag | Effect |
| - | - |
| `--global` | Machine scope: all known repos and all stacks. Without it, the scope is the repo of the directory, and outside a repo the command fails with exit 3. |
| `--stream` | NDJSON events, in stages. Without it, one document when all stages are done. |
| `--cwd DIR` | Run from this directory. |
| `--only A,B` | Run and show only these stages. The core still reads the structure, without showing it, so that the rows have owners. `cleanup` runs only when `git` runs too. A name that is not a stage is a usage error. |
| `--no-refresh` | Do not start a scan of the disk sizes. |

`core list` only reads. The docker calls are `ps`, `volume ls`, `stats`, `info` and `inspect`, and a detached `system df` when the cache is old and there is no `--no-refresh`. The git calls take no optional locks, so they do not write the index.

### Stages

| Stage | What it reads | Events | Use |
| - | - | - | - |
| `structure` | `git worktree list` for each repo, then one `docker ps -a` and one `docker volume ls` that started together | `repo`, `worktree`, `stack` | The names come first, and a slow engine does not hold them back. Keep the keys that act on rows inert until this stage is done. |
| `git` | For each worktree, at most 8 at a time, with 10 s for each | `worktree_git` | Changed files, ahead and behind, last commit, merged, branch gone. |
| `stats` | `docker stats`, `docker info`, `docker inspect`, together | `stack_stats`, `engine`, `stack_age` | Live numbers. |
| `disk` | The cache files | `stack_disk`, `disk_totals` | Sizes, with their age. It starts a scan behind the list if the cache is older than 30 minutes. |
| `cleanup` | What the stages above found | `cleanup`, `cleanup_totals` | The tiers of the cleanup view. |

A `warning` can come at any point. A stage that fails warns and ends normally. When `docker ps` fails, the `stats` stage reads nothing.

### Stream events

Each stage starts with a line, and ends with `done` and its time in milliseconds. The last line is `end`.

```json
{"v":1,"event":"stage","stage":"structure"}
{"v":1,"event":"stage","stage":"structure","done":true,"ms":118}
{"v":1,"event":"end","ms":260}
```

`repo`:

```json
{"v":1,"event":"repo","repo":{"common":"/abs/proj/.git","name":"proj","main":"/abs/proj","default_branch":"main"}}
```

The default branch is `origin/HEAD`, else `main`, else `master`, else `null`.

`worktree`, in the order of git, with the main checkout first:

```json
{"v":1,"event":"worktree","worktree":{"path":"/abs/worktrees/alpha","common":"/abs/proj/.git","repo":"proj","main":false,"branch":"alpha","detached":false,"head":"44b5b6881c2abe459ea1310eee675287b79110d7","missing":false,"bare":false,"locked":"on the usb disk","current":false,"odd":false}}
```

| Field | Meaning |
| - | - |
| `locked` | `null` when it is not locked. `""` when it is locked with no reason. |
| `current` | The deepest worktree of this repo that holds the directory of the caller. |
| `missing` | Its directory is gone, also when it is locked. |
| `odd` | The path has a tab, a newline, or bytes that are not UTF-8. Such a path cannot come back as a target, so offer no action on it. The plan refuses it too. |
| `bare` | A bare entry. It has no directory to open, and a UI usually hides it. |

`stack`:

```json
{"v":1,"event":"stack","stack":{"project":"proj-alpha","dir":"/abs/worktrees/alpha","dirs":["/abs/worktrees/alpha"],"kind":"live","worktree":"/abs/worktrees/alpha","common":"/abs/proj/.git","worktrees":["/abs/worktrees/alpha"],"states":{"running":1,"exited":1},"containers":[{"id":"845341ef3380","name":"proj-alpha-api-1","service":"api","state":"running","health":"healthy","status":"Up 2 hours (healthy)","ports":"0.0.0.0:8080->80/tcp, 9000/tcp","published":["8080"]}],"volumes":1}}
```

| Field | Meaning |
| - | - |
| `kind` | `live`, `plain` (its directory is in no repo, machine scope only), `gone` (its directory is gone), `volumes` (no containers are left). |
| `dir`, `dirs` | The directory to show, and all the directories of the project. `dir` is `null` for `volumes`. Among directories of the same kind, `dir` is the first by byte order, so it does not change with the order of the rows of docker. |
| `worktree` | The longest-prefix owner of `dir`. This is the parent in the tree. `null` is a top-level row. |
| `worktrees` | The owners of all of `dirs`, in the order of the list. A stack in several directories belongs to each of these worktrees, and a plan for any of them finds it. |
| `states` | Only states with a count. They are `running`, `paused`, `restarting`, `created`, `exited`, `dead`, `removing`. |
| `volumes` | `null` when `docker volume ls` did not answer. |
| `published` | Host ports, without duplicates. `health` is `healthy`, `unhealthy`, `starting` or `null`. |

`worktree_git`:

```json
{"v":1,"event":"worktree_git","path":"/abs/worktrees/alpha","missing":false,"changed":2,"untracked":1,"upstream":"origin/alpha","upstream_gone":false,"ahead":3,"behind":0,"last_commit_at":1793553388,"last_commit_subject":"fix token refresh","index_mtime":1793553388,"merged_into":null,"head_held":true,"unreadable":false,"timed_out":false}
```

| Field | Meaning |
| - | - |
| `changed`, `untracked` | Lines of `git status --porcelain --untracked-files=normal --ignore-submodules=none`, whatever the config of the repo says, and how many of them are untracked. `changed` also counts the tracked files on the disk with an assume-unchanged or skip-worktree flag, which that status does not check. Both are `null` when `unreadable`. |
| `merged_into` | The default branch, if HEAD is an ancestor of it. It is `null` on the default branch itself. A squash merge does not count. |
| `upstream_gone` | The branch has an upstream that is gone. |
| `head_held` | `false` when no local branch, tag or remote branch holds a detached HEAD. A stash does not hold it. `null` when `unreadable`. |
| `timed_out` | The facts took more than 10 s. Only `path`, `missing`, `unreadable` and `timed_out` are there. |

On a missing directory, the line is `{"v":1,"event":"worktree_git","path":"...","missing":true}`.

`stack_stats`, for a stack with running containers:

```json
{"v":1,"event":"stack_stats","project":"proj-alpha","cpu_pct":2.50,"mem_bytes":209715200,"containers":[{"id":"845341ef3380","cpu_pct":2.50,"mem_bytes":209715200}]}
```

`engine`, for the whole engine, also what is out of scope:

```json
{"v":1,"event":"engine","ncpu":16,"mem_total":32848117760,"cpu_pct":166.05,"mem_bytes":6851539239}
```

`stack_age`. The times are seconds since the epoch, and `stale` means stopped for 7 days or more:

```json
{"v":1,"event":"stack_age","project":"proj-alpha","last_started_at":1793397786,"last_finished_at":1793517187,"running":true,"stale":false}
```

`stack_disk` and `disk_totals`, in bytes. `age_s` is the age of the cache. Before the first scan, all sizes are `null`:

```json
{"v":1,"event":"stack_disk","project":"proj-alpha","bytes":3000000000,"age_s":1080,"refreshing":false}
{"v":1,"event":"disk_totals","images":10500000000,"containers":1200000000,"volumes":5000000000,"build":200000000,"age_s":1080,"refreshing":false}
```

`cleanup`, one row of the cleanup view, and `cleanup_totals`:

```json
{"v":1,"event":"cleanup","id":"worktree:/abs/worktrees/alpha","kind":"worktree","label":"alpha","tier":"needs_look","reasons":[{"code":"merged_running","text":"merged into main, and a stack still runs"}],"bytes":3000000000,"stacks":["proj-alpha"]}
{"v":1,"event":"cleanup_totals","safe":0,"needs_look":3000000000,"never":0,"unknown":true}
```

`bytes` is what the disk cache knows. A worktree row counts its stacks, and those stacks get no row of their own. A stack in several worktrees is in the `stacks` of each row, and its `bytes` count in the first. `unknown` means that a row has no size, so a total can be low.

| Tier | Row | Reason codes |
| - | - | - |
| `never` | The main checkout, the current worktree, a locked worktree, an odd path | `main_checkout`, `current`, `locked`, `odd_path` |
| `safe` | A worktree that is merged, with no stack or with all stacks stopped for 7+ days and no volumes | `merged` |
| `safe` | A stack whose directory is gone, with no volumes, and a name that holds the name of the repo | `worktree_gone` |
| `needs_look` | A worktree | `missing_dir`, `unreadable`, `dirty`, `unheld_commit`, `merged_running`, `merged_recent_stack`, `upstream_gone`, `stacks_unknown` (`docker ps` failed or timed out), and `volumes` or `volumes_unknown` (`docker volume ls` failed or timed out) as a second reason on a merged worktree |
| `needs_look` | A stack | `volumes_only`, `gone_volumes`, `volumes_unknown`, `worktree_gone_unnamed`, `stale` |

A worktree with its own commits and nothing wrong has no row. `safe` means that wtree expects no refusal, and it never skips a plan: the list does not look for repos inside a worktree, so a plan can still refuse a `safe` row. Volumes can hold the only copy of their data and no fact in a list rules that out, so a row with volumes is never `safe`, and neither is a row when docker did not answer and it could have stacks or volumes. A host with no docker has no stacks, so that does not count.

`warning`:

```json
{"v":1,"event":"warning","code":"docker_timeout","message":"Docker did not answer within 5 seconds. Reload the list to try again."}
```

Codes: `docker_missing`, `docker_timeout`, `docker_down`, `volumes_unavailable`, `stats_unavailable`, `info_unavailable`, `inspect_unavailable`, `git_unreadable`, `repo_scan_running`, `odd_stack_row`.

### The document

Without `--stream`, the same data is one object. Each array holds the objects of its event, without `v` and `event`. `engine`, `disk_totals` and `cleanup_totals` are objects, or `null` when their stage did not run or read nothing.

```json
{"v":1,"repos":[],"worktrees":[],"stacks":[],"worktree_git":[],"stack_stats":[],"stack_age":[],"stack_disk":[],"engine":null,"disk_totals":null,"cleanup":[],"cleanup_totals":null,"warnings":[]}
```

## `wtree core plan`

```
wtree core plan <action> [flags] [--cwd DIR] -- <targets...>
```

| Action | Flags | Targets |
| - | - | - |
| `delete-worktrees` | `--force`, `--repo COMMON` once for each target | Paths. Without `--repo`, the repo comes from the path, then from the directory. A worktree whose directory is gone needs `--repo` in machine scope. |
| `delete-stacks` | `--volumes`, `--dir DIR` once for each target | Projects. Without `--dir`, all directories of the project can offer their worktrees. With `--dir`, only that one, and only if docker still labels the project with it. |
| `stop-stacks` | | Projects |
| `open` | | Directories |

A plan only reads.

### The plan

```json
{"v":1,"plan_id":"04717aaf55d1adc7","action":"delete-worktrees","flags":{"force":false,"volumes":false},"cwd":"/abs/proj",
 "items":[
  {"id":"worktree:/abs/worktrees/alpha","kind":"worktree","label":"alpha","go":true,"offered":false,"path":"/abs/worktrees/alpha","common":"/abs/proj/.git",
   "consequences":[{"code":"ignored_files","text":"Ignored files, such as .env and node_modules, are deleted too.","severe":false}],"steps":["s1"]},
  {"id":"worktree:/abs/proj","kind":"worktree","label":"proj","go":false,"offered":false,"reason":{"code":"main_checkout","text":"it is the main checkout"},"path":"/abs/proj","common":"/abs/proj/.git","consequences":[],"steps":[]},
  {"id":"worktree:/abs/worktrees/beta","kind":"worktree","label":"beta","go":false,"offered":false,"reason":{"code":"dirty","text":"it has changed files"},"force_ok":true,"codes":["dirty"],"path":"/abs/worktrees/beta","common":"/abs/proj/.git","consequences":[],"steps":[]},
  {"id":"stack:proj-alpha","kind":"stack","label":"proj-alpha","go":true,"offered":true,
   "consequences":[{"code":"containers_removed","text":"Its containers and networks are deleted.","severe":false}],"steps":["s2","s3"]}],
 "steps":[
  {"id":"s1","label":"delete worktree alpha","kind":"delete_worktree","item":"worktree:/abs/worktrees/alpha","force":false,"volumes":false,"after":[]},
  {"id":"s2","label":"delete stack proj-alpha","kind":"stack_down","item":"stack:proj-alpha","force":false,"volumes":false,"after":["s1"]},
  {"id":"s3","label":"delete stack proj-alpha","kind":"stack_down","item":"stack:proj-alpha","force":false,"volumes":true,"after":["s1"]}],
 "choices":[
  {"id":"worktrees","text":"worktrees only","default":true,"available":true,"adds":[],"steps":["s1"],"confirm":{"kind":"key","phrases":[]},"consequences":[]},
  {"id":"worktrees+stacks","text":"and stacks, keep volumes","default":false,"available":true,"adds":["stack:proj-alpha"],"steps":["s1","s2"],"confirm":{"kind":"key","phrases":[]},"consequences":[]},
  {"id":"worktrees+stacks+volumes","text":"and stacks with volumes","default":false,"available":true,"adds":["stack:proj-alpha"],"steps":["s1","s3"],"confirm":{"kind":"key","phrases":[]},
   "consequences":[{"code":"volumes_removed","item":"stack:proj-alpha","text":"proj-alpha: 1 volume deleted. The data in the volumes is lost.","severe":true,"count":1,"names":["proj-alpha_data"],"bytes":3000000000,"protected":false}]}],
 "confirm":{"kind":"key","phrases":[]},
 "notes":[{"code":"ignored_files","text":"Ignored files, such as .env and node_modules, are deleted too."}],
 "nothing_to_do":false}
```

| Field | Meaning |
| - | - |
| `items[].go` | `false` lists it under what stays, with `reason`. `true` means it goes, under the choice that holds it. |
| `items[].offered` | Only a choice adds this item: see `adds`. |
| `items[].force_ok` | The `--force` level can take it. Ask for a new plan with `--force`. |
| `items[].note` | Why this needs force, on an item that goes. |
| `items[].only_volumes` | A stack with no containers left. A delete that keeps its volumes changes nothing there. It is `true` or absent, and it is what the plan read, so use it and not an older list. |
| `items[].consequences` | What it costs, whichever choice takes it. |
| `items[].steps` | All the steps that can act on it. |
| `steps` | All the steps of all the choices. A step runs when each step of `after` is `ok`. |
| `choices` | The first is the default, and it is the mildest. A choice with `available: false` has a `reason`. |
| `choices[].steps` | The steps that it runs, in order. |
| `choices[].consequences` | What this choice costs beyond the items. Volume loss is here, and `bytes` is the disk cache size of the project, which also counts its containers. |
| `confirm` | The confirmation of the default choice. Each choice has its own, because volumes can need a phrase and worktrees not. |
| `nothing_to_do` | No choice can run. |

The `reason.text` of an item names no key of a UI. The fzf UI adds its own key hint to it.

### Reason codes

| Code | Where | Force can take it |
| - | - | - |
| `main_checkout`, `current`, `not_registered`, `locked`, `nested_worktree`, `odd_path` | A worktree. `odd_path` is a tab, a newline, or bytes that are not UTF-8, in a target or in an offered worktree | No |
| `dirty`, `unheld_commit`, `unreadable` | A worktree. `dirty` also means a file on the disk with an assume-unchanged or skip-worktree flag. Only a local branch, a tag or a remote branch holds a commit | Yes |
| `nested_repo`, `scan_timeout` | A worktree | Yes, with its name typed |
| `other_stack` | An offered worktree: a stack that is not selected runs from it | No |
| `protected_stack` | An offered worktree: a protected stack in this plan keeps all the worktrees | No |
| `docker_unavailable` | A stack: docker cannot say what a delete with volumes takes | No |
| `no_directory` | A directory to open | No |

A missing directory is not a refusal. The item goes with the note `its directory is missing`, and its step `remove_missing` removes that one entry of git, and never prunes. The exception is a detached entry with a commit that nothing else holds: that entry is the last hold on it, so it is `unheld_commit`, and only the force level takes it, with a `force_note` consequence.

The `nested_repo` check finds a `.git` entry and a bare repo at any depth, also when they are ignored. A submodule whose git directory is inside this repo is not another repo. A repo with its own git directory is one, also when the worktree tracks it as a gitlink.

A target that comes twice is one item with one step.

### Confirmation

| `kind` | A UI must |
| - | - |
| `none` | Run it. `--choice` is optional. |
| `key` | Ask for one key, then send `--choice`. |
| `phrase` | Show each phrase, ask for it, then send `--choice` and `--phrase` once for each phrase, in the order of `phrases`. |

```json
{"kind":"phrase","phrases":[{"phrase":"alpha","hint":"Type \"alpha\" to delete"},{"phrase":"2 stacks","hint":"Type \"2 stacks\" to delete"}]}
```

A phrase is compared exactly, with no trimming and no folding of case. A UI never computes a phrase. The rules that the core applies are:

| Plan | Confirmation |
| - | - |
| Stop or open of one target | `none` |
| Stop or open of several targets | `key` |
| A clean worktree, with or without keeping volumes | `key` |
| Force delete of a worktree with another repo inside | A phrase for each such worktree: its name, on every choice. The volume phrase comes after them. |
| Delete with volumes, of more than one stack | A phrase: `2 stacks`. With the worktrees: `2 stacks w` |
| Delete with volumes, of a protected stack | A phrase: its name, or the count for several |
| From the worktree side, volumes of a project with a directory outside the selection | A phrase: its name, or the count for several |

A stack is protected when it has no containers, or when a directory of it is empty, missing, in no repo, or in a main checkout. A protected stack in a plan with volumes clears all the worktree offers.

## `wtree core apply`

```
wtree core apply <action> --plan PLAN_ID [--choice ID] [--phrase TEXT]... [flags] [--cwd DIR] -- <targets...>
```

The action, the flags and the targets are the ones of the plan. `apply` computes the plan again, and:

1. It refuses `plan_stale` if the `plan_id` differs. The event has the new plan.
2. It refuses `nothing_to_do` if no choice can run.
3. It refuses `bad_choice`, `no_choice` or `choice_unavailable` if the choice does not fit. Only a `none` plan needs no `--choice`.
4. It refuses `phrase_mismatch` unless the phrases are exact, complete and in order.
5. It runs the steps of the choice. Each step asks its guards again, and it fails if its answer is not the one in the plan.

The `plan_id` covers the action, the flags, the directory, and for each target the facts that a guard reads: the state of a worktree with the list of its changes, the diff of its tracked files and the repos inside it, its lock, its head, the directories of each stack, whether it is protected, and the names of its volumes. It also covers the outcome: what goes and what stays with its reason, the steps, and what each choice adds, runs and asks. So a stack that comes to or leaves an offered worktree makes a plan stale. It does not cover the state of a container, so a stack that stops does not make a plan stale, and it does not cover the content of an untracked file.

What a step asks again:

| Step | It fails when |
| - | - |
| `delete_worktree`, `remove_missing` | The facts of the worktree changed. On the worktree side, a stack that the plan did not show runs from it. On the stack side, any stack runs from it, also a selected one whose down failed. |
| `stack_down` | The directories of the stack changed, or the stack with its volumes became protected after the plan. |

### Refused, exit 4

```json
{"v":1,"event":"refused","code":"plan_stale","message":"The state changed after the plan. Nothing was done. This is the new plan.","plan":{"v":1,"plan_id":"...","...":"..."}}
{"v":1,"event":"refused","code":"phrase_mismatch","message":"The phrases do not match this plan. Nothing was done."}
```

Only `plan_stale` has a `plan`. Show its consequences and ask again.

### The stream

```json
{"v":1,"event":"start","plan_id":"04717aaf55d1adc7","steps":3}
{"v":1,"event":"step","id":"s1","state":"running","label":"delete stack proj-alpha"}
{"v":1,"event":"output","id":"s1","line":"volume is in use"}
{"v":1,"event":"step","id":"s1","state":"failed","ms":105,"status":1,"tail":["volume is in use"]}
{"v":1,"event":"step","id":"s2","state":"skipped","because":"s1"}
{"v":1,"event":"step","id":"s3","state":"running","label":"delete stack other"}
{"v":1,"event":"step","id":"s3","state":"ok","ms":2140}
{"v":1,"event":"end","ok":1,"failed":1,"skipped":1,"disk_refresh":true,"cancelled":false}
```

| Event | Detail |
| - | - |
| `output` | Each line of a running step, as it comes. |
| `step` `failed` | `tail` has the last 8 lines. A guard that stopped the step explains itself here: `It changed after the plan, so it stays: it is locked...`. |
| `step` `skipped` | `because` is the first step that failed, or `cancelled` after a `TERM`. Later steps that do not depend on it still run, unless it was cancelled. |
| `end` | `disk_refresh` says that a scan of the disk sizes started. After it, a size in the list can be old. |

The exit code is 0 when no step failed and it was not cancelled, else 1.

### The order of steps

| Side | Order | Why |
| - | - | - |
| Worktree | The worktrees, then their stacks. Each stack waits for all its selected worktrees. | A worktree that cannot go keeps its stack. |
| Stack | The stacks, then the worktrees. A worktree waits for all the selected stacks that run from it. | A stack that cannot go keeps its worktree. |
| A stack with volumes | Read the volumes, `compose down`, then `volume rm` one at a time. | A failed down keeps the data. |

## `wtree core open`

```
wtree core open [--cwd DIR] -- <dir...>
```

It needs no plan, and it streams the same `start`, `step`, `output` and `end` events, with one step for each directory. A directory that does not exist gets a step that fails. A message of the terminal adapter, such as `this terminal has no tab support`, is an `output` line.

## `wtree core new`

```
wtree core new [--cwd DIR] [--repo COMMON] [--name NAME] [--no-open] [--dry-run]
```

| Flag | Effect |
| - | - |
| `--dry-run` | Pick a name and report it, and change nothing. Use it to fill a field. |
| `--repo` | Create in this repo, at the HEAD of its main checkout. Without it, in the repo of the directory, at its HEAD. |
| `--name` | Use this name. It has only letters, digits, dots, dashes and underscores, and it does not start with a dot or a dash. |
| `--no-open` | Do not open a tab. |

```json
{"v":1,"event":"plan_new","path":"/abs/worktrees/rosy-glacier","name":"rosy-glacier","head":"1977898b37153d4e4562fc96c3672b2ec1510e33"}
{"v":1,"event":"created","path":"/abs/worktrees/misty-raven","name":"misty-raven","head":"1977898b37153d4e4562fc96c3672b2ec1510e33"}
```

`created` comes first, and then the events of `core open` unless `--no-open`. A bad name is refused with `bad_name`, and a path that exists with `name_taken`, both with exit 4.

## For a UI

- Run `core version` first.
- Start `core list --stream`, and draw each event as it comes. Keep the keys that act on rows inert until `structure` is done.
- Key rows by `worktree:<path>` and `stack:<project>`.
- Before a destructive action, run `core plan`, and show `items`, the consequences of the chosen choice, and `notes`.
- Send the plan back unchanged: the same action, flags and targets, with `--plan`, `--choice` and `--phrase`.
- On `plan_stale`, show the new plan. Never retry on your own.
- Stop with `TERM` to the process of `core apply`, so that the step in flight can finish.
- Reload after an `end`.
