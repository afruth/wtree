# Fixtures of the wtree core client

The `.json` and `.ndjson` files here are the output of `bin/wtree core`, recorded by `record_cases.sh`. `core.FakeClient` replays them, and the tests of `internal/core` decode them.

Do not write them by hand. Record them again:

```sh
tui/internal/core/testdata/guarded.sh record
```

`exit-codes.tsv` has the exit code of each recording.

`guarded.sh test` runs `guarded_test.go` against the real script. These tests skip themselves outside the guard.

## Safety

The recordings run delete, stop and purge paths, so they run only behind the guard of `test/run.sh`. `guarded.sh` copies `test/` to a new `mktemp` directory, adds its cases there, and starts that copy of `run.sh`. The guard is the same: only the docker shim on `PATH`, a scratch `HOME`, `XDG_CACHE_HOME` and `WTREE_ROOTS`, and an unreachable `DOCKER_HOST`. See `test/README.md`. The repo's `test/` directory does not change.

Do not run `bin/wtree` by hand to make a fixture.

## Normalized values

So that a new recording gives a small diff, these change:

| Value | In the file |
| - | - |
| The scratch directory of a test | `/fixture` |
| `plan_id` | A counter for each file, in the order of the first use |
| `ms`, `age_s`, `index_mtime`, `last_started_at`, `last_finished_at`, when not null | A fixed number |
| Commit hashes and dates | Real values, from fixed dates in the setup |
| The order of `worktree_git` lines | Sorted by path, because they come from parallel jobs |
