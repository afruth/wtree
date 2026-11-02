# wtree tests

The tests drive the `wtree` command with real git and real fzf. They need no Docker.

Run all the tests:

```sh
test/run.sh
```

| Command | Result |
| - | - |
| `test/run.sh` | Run all the tests. The exit code is not zero if a test fails |
| `test/run.sh stack_purge` | Run the tests whose name matches this regular expression |
| `test/run.sh --list` | Show the names of the tests |
| `test/run.sh --update-golden` | Write the files in `golden/` again. Read the diff before you commit |

You need Linux, Bash 5.1 or later, git, fzf, tmux, and `script` and `setsid` from util-linux.

## Safety

This suite can run on a host with real Docker stacks. These protections apply to each test:

- `run.sh` starts the tests with an empty environment, in a new session with no terminal.
- `PATH` has only the shims and a scratch directory of links to the tools that the tests need. The real `docker` and `rex` are not on it.
- `shim/docker` denies by default. It answers only from the fixture files, and it exits 125 on all else. It never starts a real program.
- `DOCKER_HOST` is a socket that does not exist, in the scratch directory.
- `HOME`, `XDG_CACHE_HOME`, `WTREE_ROOTS`, and all git repos are in a new `mktemp` directory.
- tmux uses a private socket in the scratch directory. It never uses the default server.

`run.sh` checks these points before the first test. It stops with exit code 3 if one is wrong.

Always use `run.sh` to run a test. Do not run `bin/wtree` by hand to try a delete, a stop, or a purge on a host with real stacks.

## Tests of init.lua

`test/lua/run.sh` tests `init.lua`. It needs the Lua that is in Rex, so it starts a private Rex server with its own socket, state, and home, and stops it at the end. It does not connect to another server. Without `rex` on `PATH`, it says so and passes. `test/run.sh` runs it when you give no filter.

`floats.lua` runs a stub in the float. `real_float.sh` then runs the real `bin/wtree` and `wtree-tui` in a real float, and reads the screen with `rex capture`. The float starts a wrapper with an empty environment: `PATH` has the shims and links to the tools, `docker` is the shim, and `HOME`, the fixtures, and `DOCKER_HOST` are in the private directory. The wrapper stops if `docker` is not the shim. The test presses no key that changes a thing.

## Tests of the UI choice and the installer

`cases/62_chooser.sh` tests how `wtree` chooses between `wtree-tui` and fzf. All the other cases run with `WTREE_UI=fzf`, so a `bin/wtree-tui` in the checkout does not change them. Most cases use a small fake program. The cases that need the real program use the one that `run.sh` builds with `go` before it starts the guard, and they skip without it.

`cases/70_install.sh` runs `install.sh` against fake release files on disk, with `file://` URLs and with `WTREE_BIN_DIR` in the scratch directory. It needs no network.

## Tests of the full-screen interface

`cases/70_tui_go.sh` runs the real `wtree-tui` and the real `bin/wtree` behind the docker shim, in a private tmux. `run.sh` builds `wtree-tui` into the scratch directory before the guard starts, because `go` is not on the `PATH` of the tests. If it cannot build, these tests show `skip` with the reason. The guard is the same for them.

`wtree-tui` runs the script through a small logger, so `core.log` has each `wtree core` call with its arguments. A test asserts the changes to the engine with `assert_docker_changes`, which takes the whole list, in order, with the exact words.

The golden frames and the screenshots of the interface are not here. See `tui/internal/model/golden_test.go` and `tui/scripts/screens.sh`.

Not tested: fzf 0.53, the oldest version that `wtree` accepts. The tests use the fzf of the host.

## Results

| Result | Meaning |
| - | - |
| `ok` | The test passed |
| `FAIL` | The test failed, or the harness failed, or the shim refused a call. The suite fails |
| `xfail` | A known bug: the test states the right behaviour, and it fails today |
| `XPASS` | A test marked `xfail` passes. The suite fails until you remove its `xfail` line |
| `skip` | The test cannot run here, and it says why. Only the tests of `wtree-tui` do this, when it does not build |

## Fix a known bug

The tests for known bugs are in `cases/90_known_bugs.sh`. Each has an `xfail` line above it. There are none now, and the file keeps the tests of the bugs that were fixed.

1. Fix the bug.
2. Run `test/run.sh`. The tests of the bug show `XPASS`.
3. Remove their `xfail` lines.

An `xfail` test must fail on its assertion. If its setup breaks, the result is `FAIL`.

## Add a test

Add a function named `test_...` to a file in `cases/`. Each test runs in its own process, with its own scratch directory and tmux server.

| Helper | Use |
| - | - |
| `make_repo`, `add_wt`, `add_detached_wt` | Make real git fixtures |
| `stack`, `volume`, `container_meta` | Add a fixture to the fake engine |
| `shim_fail`, `shim_delay`, `shim_hang`, `shim_wedge`, `shim_fail_from` | Make a docker call fail, slow, or hang, ignore a stop signal, or fail from a later call on |
| `term_rule` | Let a terminal CLI shim answer one call |
| `wt` | Run `wtree` with no terminal, so each prompt sees end of file |
| `wt_keys` | Run `wtree` in a pty and answer each prompt when it appears: one key, or a line if it asks for text. Later prompts get a newline, which means no |
| `tui_start`, `tui_wait`, `tui_send`, `tui_expect`, `tui_wait_prompt` | Run the full screen in a private tmux |
| `tg_start`, `tg_loaded`, `tg_move_to`, `tg_pick`, `tg_tap`, `tg_tap_text`, `tg_tap_bar`, `tg_send_seq` | Run `wtree-tui` in a private tmux, move with plain keys, and tap with SGR mouse reports |
| `assert_core_log`, `assert_core_count`, `assert_docker_changes` | Check what `wtree-tui` asked of the script, and what reached the engine |
| `q`, `plan_ok`, `apply_ok` (in `cases/50_core.sh`) | Read the JSON of a `wtree core` call with `python3 -I`, get a plan, and apply it |

`wtree-tui` holds what confirms a sheet for 300 ms after the sheet opens or changes, so `tui_send` waits before `y` and `Enter` in those tests, and `tg_tap` waits before a tap. To send a held key or a buffered key, use `tmux_cmd send-keys` directly. A key that comes while a plan is on its way cancels the plan, so wait for the sheet before you send one.

Wait for the list before you send a key. While a list loads, the prompt has dots, the keys that act on rows do nothing, and typed text goes to the search. `tui_wait` also waits until the dots are gone.

Wait for a prompt before you answer it. `wtree` drops a key that comes before its prompt, and the text above a prompt can appear first.

A test also fails if the shim refuses a call. Add the new docker call to the shim only if it is correct for wtree to make it.
