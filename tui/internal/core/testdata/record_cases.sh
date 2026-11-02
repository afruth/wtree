# The cases of guarded.sh. It puts this file in a copy of test/cases, behind the guard, with WP2_OUT
# and WP2_BIN set above this line. The helpers come from test/lib/harness.sh and test/cases/50_core.sh.
# shellcheck shell=bash

# args: the fixture, then the arguments of wtree. Keeps the output and the exit code of the last call.
rec() {
  local name=$1
  shift
  wt "$@"
  rec_keep "$name"
}

rec_keep() {
  grep -q . "$T/out" || broken "no output for $1"
  sed "s#$T#/fixture#g" "$T/out" >"$WP2_OUT/$1"
  printf '%s\n' "$RC" >"$WP2_OUT/$1.rc"
}

wp2_fixed_dates() {
  export GIT_AUTHOR_DATE=2026-01-02T03:04:05Z GIT_COMMITTER_DATE=2026-01-02T03:04:05Z
}

# a repo with one worktree of each kind and one stack of each kind
wp2_scene() {
  wp2_fixed_dates
  make_repo
  add_wt alpha
  ALPHA=$WT
  commit_in "$ALPHA"
  add_wt beta
  BETA=$WT
  printf 'edit\n' >>"$BETA/file.txt"
  printf 'new\n' >"$BETA/new.txt"
  add_wt merged
  MERGED=$WT
  # its own text: with the fixed dates, the same text would give the same commit as alpha, which holds it
  add_detached_wt gamma
  printf 'gamma\n' >>"$WT/file.txt"
  must git -C "$WT" commit -q -am gamma
  add_wt locked
  must git -C "$REPO" worktree lock --reason $'on the usb \033[31mdisk' "$WT"
  add_wt vanished
  must rm -rf "$WT"
  stack proj-alpha "$ALPHA" running 2.5 200MiB
  stack proj-alpha "$ALPHA" exited
  container_meta id0 api 'Up 2 hours (healthy)' '0.0.0.0:8080->80/tcp, 9000/tcp' 2026-01-01T00:00:00.000000000Z 0001-01-01T00:00:00Z proj-alpha-api-1
  container_meta id1 db 'Exited (1) 3 days ago' '' 2026-01-01T00:00:00.000000000Z 2026-02-01T00:00:00.000000000Z proj-alpha-db-1
  volume proj-alpha proj-alpha_data
  stack proj-old "$BETA" exited
  container_meta id2 web 'Exited (0) 9 months ago' '' 2026-01-01T00:00:00.000000000Z 2026-01-02T00:00:00.000000000Z proj-old-web-1
  stack proj-gone "$HOME/nowhere" exited
  container_meta id3 web 'Exited (0) 9 months ago' '' 2026-01-01T00:00:00.000000000Z 2026-01-02T00:00:00.000000000Z proj-gone-web-1
  volume proj-vols proj-vols_data
  mkdir -p "$XDG_CACHE_HOME/wtree"
  printf 'proj-alpha\t3000000000\nproj-old\t1000\n' >"$XDG_CACHE_HOME/wtree/disk-projects"
  printf 'Images\t10.5GB\nContainers\t1.2GB\nLocal Volumes\t5GB\nBuild Cache\t200MB\n' >"$XDG_CACHE_HOME/wtree/disk-totals"
}

wp2_two_stacks() {
  wp2_fixed_dates
  setup_core_two_linked
}

# ---- recordings

test_wp2_record_version() {
  rec version.json core version
}

test_wp2_record_errors() {
  mkdir -p "$HOME/plain"
  cd "$HOME/plain" || exit 2
  rec error-usage.ndjson core nosuch
  rec error-not-a-repo.ndjson core list --stream
  rec error-no-such-cwd.ndjson core list --stream --cwd "$HOME/nowhere"
}

test_wp2_record_lists() {
  wp2_scene
  rec list-stream.ndjson core list --stream --no-refresh
  rec list-document.json core list --no-refresh
  rec list-only-stats.ndjson core list --stream --only stats --no-refresh
  mkdir -p "$HOME/plain"
  stack plain-app "$HOME/plain" running
  rec list-global.ndjson core list --stream --global --no-refresh
}

test_wp2_record_list_first_scan() {
  wp2_fixed_dates
  setup_core_list
  rec list-first-scan.ndjson core list --stream --no-refresh
}

test_wp2_record_list_docker_down() {
  wp2_fixed_dates
  setup_core_list
  shim_fail 'ps *' 'Cannot connect to the Docker daemon'
  shim_fail 'volume ls*' 'Cannot connect to the Docker daemon'
  rec list-docker-down.ndjson core list --stream --no-refresh
}

test_wp2_record_list_stats_fail() {
  wp2_fixed_dates
  setup_core_list
  shim_fail 'stats *' 'no stats'
  shim_fail 'info *' 'no info'
  shim_fail 'inspect *' 'no inspect'
  rec list-stats-fail.ndjson core list --stream --no-refresh
}

test_wp2_record_plans() {
  wp2_scene
  rec plan-delete-worktrees.json core plan delete-worktrees -- "$ALPHA" "$BETA" "$MERGED" "$REPO"
  rec plan-delete-worktrees-force.json core plan delete-worktrees --force -- "$BETA"
  rec plan-delete-stacks.json core plan delete-stacks -- proj-alpha
  rec plan-delete-stacks-protected.json core plan delete-stacks --volumes -- proj-vols
  rec plan-stop-stacks.json core plan stop-stacks -- proj-alpha proj-old
  rec plan-open.json core plan open -- "$ALPHA"
  rec plan-open-several.json core plan open -- "$ALPHA" "$BETA"
  rec plan-nothing-to-do.json core plan delete-worktrees -- "$REPO"
}

test_wp2_record_plan_nested() {
  wp2_fixed_dates
  make_repo
  add_wt alpha
  must git -C "$WT" init -q inner
  rec plan-delete-worktrees-nested.json core plan delete-worktrees --force -- "$WT"
}

test_wp2_record_plan_volumes() {
  wp2_two_stacks
  rec plan-delete-stacks-volumes.json core plan delete-stacks --volumes -- pa pb
}

test_wp2_record_apply_ok() {
  wp2_two_stacks
  plan_ok delete-stacks -- pa pb
  apply_ok delete-stacks "$PLAN_ID_OF" --choice stacks -- pa pb
  assert_rc 0
  rec_keep apply-ok.ndjson
}

test_wp2_record_apply_volumes() {
  wp2_two_stacks
  plan_ok delete-stacks --volumes -- pa pb
  apply_ok delete-stacks "$PLAN_ID_OF" --choice stacks --phrase '2 stacks' --volumes -- pa pb
  assert_rc 0
  rec_keep apply-volumes.ndjson
}

test_wp2_record_apply_worktree() {
  wp2_fixed_dates
  make_repo
  add_wt alpha
  plan_ok delete-worktrees -- "$WT"
  apply_ok delete-worktrees "$PLAN_ID_OF" --choice "$(q 'doc["choices"][0]["id"]')" -- "$WT"
  assert_rc 0
  rec_keep apply-worktree.ndjson
}

# the failed down of a stack keeps its worktree, and a stack that depends on nothing still goes
test_wp2_record_apply_failed() {
  local choice
  wp2_two_stacks
  shim_fail 'compose -p pa down*' 'volume is in use'
  plan_ok delete-stacks -- pa pb
  choice=$(q 'doc["choices"][-1]["id"]')
  apply_ok delete-stacks "$PLAN_ID_OF" --choice "$choice" -- pa pb
  assert_rc 1
  assert_q 'events("step")[1]["state"]' failed
  assert_q 'lines[-1]["skipped"] >= 1' true
  rec_keep apply-failed.ndjson
}

test_wp2_record_apply_cancelled() {
  wp2_two_stacks
  plan_ok delete-stacks -- pa pb
  shim_delay 'compose -p pa down' 1.5
  "$WTREE" core apply delete-stacks --plan "$PLAN_ID_OF" --choice stacks -- pa pb >"$T/out" 2>&1 </dev/null &
  local job=$!
  wait_log_count '^compose -p pa down' 1
  kill -TERM "$job"
  RC=0
  wait "$job" || RC=$?
  assert_rc 1
  assert_q 'lines[-1]["cancelled"]' true
  rec_keep apply-cancelled.ndjson
}

test_wp2_record_apply_refusals() {
  local id
  make_repo
  add_wt alpha
  plan_ok delete-worktrees -- "$WT"
  id=$PLAN_ID_OF
  rec apply-refused-no-choice.ndjson core apply delete-worktrees --plan "$id" -- "$WT"
  rec apply-refused-bad-choice.ndjson core apply delete-worktrees --plan "$id" --choice nosuch -- "$WT"
  printf 'new\n' >"$WT/new.txt"
  rec apply-refused-stale.ndjson core apply delete-worktrees --plan "$id" --choice worktrees -- "$WT"
  assert_rc 4
  rec apply-refused-nothing.ndjson core apply delete-worktrees --plan "$(core_plan_id_of_main)" --choice worktrees -- "$REPO"
  assert_rc 4
}

core_plan_id_of_main() {
  wt core plan delete-worktrees -- "$REPO"
  q 'doc["plan_id"]'
}

test_wp2_record_apply_refused_phrase() {
  wp2_two_stacks
  plan_ok delete-stacks --volumes -- pa pb
  rec apply-refused-phrase.ndjson core apply delete-stacks --plan "$PLAN_ID_OF" --choice stacks --phrase '2 STACKS' --volumes -- pa pb
  assert_rc 4
  assert_no_docker_changes
}

test_wp2_record_open() {
  local a
  make_repo
  add_wt aa
  a=$WT
  rec open.ndjson core open -- "$a"
  rec open-failed.ndjson core open -- "$a" "$HOME/nowhere"
}

test_wp2_record_new() {
  wp2_fixed_dates
  make_repo
  rec new-dry-run.ndjson core new --name fresh --dry-run
  rec new.ndjson core new --name fresh
  rec new-no-open.ndjson core new --name other --no-open
  rec new-refused-taken.ndjson core new --name fresh --no-open
  rec new-refused-bad-name.ndjson core new --name 'a b' --no-open
}

# ---- guarded_test.go against the real script. The test binary reads WTREE_TEST_BIN.

wp2_go() {
  RC=0
  OUT=$("$WP2_BIN" -test.run "^$1\$" -test.count=1 -test.v 2>&1) || RC=$?
  printf '%s\n' "$OUT"
  [[ $OUT == *"--- PASS: $1"* ]] || fail "the Go test did not pass: $1"
  assert_rc 0
}

test_wp2_test_list_events() {
  wp2_scene
  wp2_go TestGuardedListEvents
}

test_wp2_test_list_cancel_ends_all_children() {
  setup_core_list
  shim_hang 'ps *' 30
  wp2_go TestGuardedListCancelEndsAllChildren
}

test_wp2_test_list_cancel_kills_a_child_that_ignores_term() {
  setup_core_list
  shim_wedge 'ps *' 30
  wp2_go TestGuardedListCancelEndsAllChildren
}

test_wp2_test_list_leaves_the_scan_behind() {
  setup_core_list
  shim_hang 'system df*' 20
  wp2_go TestGuardedListLeavesTheScanBehind
}

test_wp2_test_list_outside_a_repo() {
  mkdir -p "$HOME/plain"
  cd "$HOME/plain" || exit 2
  wp2_go TestGuardedListOutsideARepo
}

test_wp2_test_plan_usage_error() {
  make_repo
  wp2_go TestGuardedPlanUsageError
}

test_wp2_test_plan_cancel() {
  wp2_two_stacks
  shim_hang 'ps *' 30
  shim_hang 'volume ls*' 30
  wp2_go TestGuardedPlanCancel
}

test_wp2_test_apply_cancel_lets_the_step_finish() {
  wp2_two_stacks
  shim_delay 'compose -p pa down' 1.5
  wp2_go TestGuardedApplyCancelLetsTheStepFinish
  assert_log 'compose -p pa down'
  assert_not_log 'compose -p pb down'
}

test_wp2_test_apply_stale_plan() {
  make_repo
  add_wt alpha
  export WP2_WT=$WT
  wp2_go TestGuardedApplyStalePlan
  assert_dir "$WT"
}

test_wp2_test_apply_phrases_reach_the_script() {
  wp2_two_stacks
  wp2_go TestGuardedApplyPhrases
  assert_log 'volume rm pa_data'
  assert_log 'volume rm pb_data'
}

test_wp2_test_new() {
  make_repo
  wp2_go TestGuardedNew
}

test_wp2_test_open_failed_step() {
  make_repo
  add_wt aa
  export WP2_WT=$WT
  wp2_go TestGuardedOpenFailedStep
}
