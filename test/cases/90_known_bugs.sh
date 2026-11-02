# Regression tests for bugs that were found and fixed. A new known bug goes here with an xfail line
# above its test. When its fix lands, the runner reports XPASS and the suite fails until the line goes.
# shellcheck shell=bash

# ---- bug 1: destructive keys and ctrl-a acted on stale rows while a reload ran

test_bug1_delete_key_during_a_reload_does_nothing() {
  local before
  make_repo
  add_wt alpha
  add_wt beta
  tui_start
  tui_wait "3/3"
  # the main checkout is the first row, and it never reaches this prompt
  tui_type alpha
  tui_wait "1/3"
  before=$(log_count '^ps ')
  shim_delay 'ps *STACK*' 2.5
  tui_send C-r
  wait_log_count '^ps ' "$((before + 1))"
  tui_send C-d
  tui_expect_absent_for "Continue?" 1.5
}

test_bug1_force_key_after_a_delete_does_not_take_the_stale_row() {
  local before
  make_repo
  add_wt alpha
  add_wt beta
  tui_start
  tui_wait "3/3"
  tui_type alpha
  tui_wait "1/3"
  tui_send C-d
  tui_wait "Continue?"
  before=$(log_count '^ps ')
  shim_delay 'ps *STACK*' 2.5
  tui_send y
  wait_log_count '^ps ' "$((before + 1))"
  tui_send C-f
  tui_expect_absent_for "FORCE delete" 1.5
}

test_bug1_select_all_during_a_reload_selects_nothing() {
  local before
  make_repo
  add_wt alpha
  add_wt beta
  tui_start
  tui_wait "3/3"
  # this proves that the screen shows a selection when there is one
  tui_send C-a
  tui_wait "(3)"
  tui_send C-a
  tui_wait "3/3"
  before=$(log_count '^ps ')
  shim_delay 'ps *STACK*' 2.5
  tui_send C-r
  wait_log_count '^ps ' "$((before + 1))"
  tui_send C-a
  tui_expect_absent_for "(3)" 1
}

test_bug1_stack_purge_key_during_a_reload_does_nothing() {
  local before
  make_repo
  add_wt alpha
  stack pa "$WT" running
  volume pa pa_data
  tui_start -d
  tui_wait "1/1"
  before=$(log_count '^ps ')
  shim_delay 'ps *' 2.5
  tui_send C-r
  wait_log_count '^ps ' "$((before + 1))"
  tui_send C-f
  tui_expect_absent_for "Delete 1 stack" 1.5
  assert_no_docker_changes
}

test_bug1_stop_key_during_a_reload_does_nothing() {
  local before
  make_repo
  add_wt alpha
  stack pa "$WT" running
  tui_start -d
  tui_wait "1/1"
  before=$(log_count '^ps ')
  shim_delay 'ps *' 2.5
  tui_send C-r
  wait_log_count '^ps ' "$((before + 1))"
  tui_send C-s
  tui_expect_absent_for "stop stack" 1.5
  assert_no_docker_changes
}

test_bug1_enter_during_a_reload_does_not_open() {
  local before
  make_repo
  add_wt alpha
  tui_start
  tui_wait "2/2"
  before=$(log_count '^ps ')
  shim_delay 'ps *STACK*' 2.5
  tui_send C-r
  wait_log_count '^ps ' "$((before + 1))"
  tui_send Enter
  tui_expect_absent_for "open this directory" 1.5
  [[ ! -e $T/pane.rc ]] || fail "wtree exited"
}

test_bug1_tab_during_a_reload_selects_nothing() {
  local before
  make_repo
  add_wt alpha
  tui_start
  tui_wait "2/2"
  before=$(log_count '^ps ')
  shim_delay 'ps *STACK*' 2.5
  tui_send C-r
  wait_log_count '^ps ' "$((before + 1))"
  tui_send Tab
  tui_expect_absent_for "(1)" 1
}

test_bug1_tap_during_a_reload_opens_no_menu() {
  local before
  make_repo
  add_wt alpha
  TUI_COLS=50
  tui_start
  tui_wait "2/2"
  before=$(log_count '^ps ')
  shim_delay 'ps *STACK*' 2.5
  tui_send C-r
  wait_log_count '^ps ' "$((before + 1))"
  tui_click_row alpha
  tui_expect_absent_for "force delete" 1
}

test_bug1_double_click_during_a_reload_does_not_open() {
  local before
  make_repo
  add_wt alpha
  tui_start
  tui_wait "2/2"
  before=$(log_count '^ps ')
  shim_delay 'ps *STACK*' 2.5
  tui_send C-r
  wait_log_count '^ps ' "$((before + 1))"
  tui_click_row alpha
  tui_click_row alpha
  tui_expect_absent_for "open this directory" 1
  [[ ! -e $T/pane.rc ]] || fail "wtree exited"
}

# this proves that the test above would see it
test_double_click_after_the_load_opens() {
  make_repo
  add_wt alpha
  tui_start
  tui_wait "2/2"
  sleep 0.3
  tui_click_row alpha
  tui_click_row alpha
  tui_expect "open this directory"
}

test_bug1_a_second_reload_waits_for_the_first() {
  local before
  make_repo
  add_wt alpha
  tui_start
  tui_wait "2/2"
  before=$(log_count '^ps ')
  shim_delay 'ps *STACK*' 2
  tui_send C-r
  wait_log_count '^ps ' "$((before + 1))"
  tui_send C-r
  sleep 1
  assert_eq "$(log_count '^ps ')" "$((before + 1))"
}

# the first load has no rows yet, and enter on an empty list would end fzf and start it again
test_bug1_keys_during_the_first_load_do_nothing() {
  make_repo
  add_wt alpha
  shim_delay 'ps *STACK*' 2.5
  tui_start
  wait_log_count '^ps ' 1
  sleep 0.3
  tui_send C-d
  tui_send C-a
  tui_send Enter
  tui_send Tab
  tui_expect_absent_for "Continue?" 1
  tui_wait "2/2"
  assert_eq "$(log_count '^ps ')" 1
  tui_expect_absent_for "(1)" 0.3
  tui_expect_absent_for "(2)" 0.3
}

test_bug1_keys_work_after_the_first_load() {
  make_repo
  add_wt alpha
  shim_delay 'ps *STACK*' 1
  tui_start
  tui_wait "2/2"
  tui_send C-a
  tui_expect "(2)"
}

test_bug1_keys_work_again_after_a_reload() {
  local before
  make_repo
  add_wt alpha
  tui_start
  tui_wait "2/2"
  before=$(log_count '^ps ')
  shim_delay 'ps *STACK*' 1
  tui_send C-r
  wait_log_count '^ps ' "$((before + 1))"
  add_wt beta
  tui_wait "3/3"
  tui_send C-a
  tui_expect "(3)"
}

test_bug1_keys_work_again_after_a_delete_and_its_reload() {
  make_repo
  add_wt alpha
  tui_start
  tui_wait "2/2"
  tui_type alpha
  tui_wait "1/2"
  tui_send C-d
  tui_wait "Continue?"
  tui_send y
  tui_wait "0/1"
  tui_send C-u
  tui_wait "1/1"
  tui_send C-a
  tui_expect "(1)"
}

test_bug1_keys_work_after_an_empty_list() {
  make_repo
  add_wt alpha
  tui_start -d
  tui_wait "Docker  CPU"
  stack pa "$WT" running
  tui_reload_when_ready
  tui_expect "1/1"
}

test_bug1_a_reload_clears_the_selection_at_once() {
  local before
  make_repo
  add_wt alpha
  tui_start
  tui_wait "2/2"
  tui_send Tab
  tui_wait "(1)"
  before=$(log_count '^ps ')
  shim_delay 'ps *STACK*' 3
  tui_send C-r
  wait_log_count '^ps ' "$((before + 1))"
  # the list is still loading, so only the action itself can have cleared it
  sleep 0.5
  OUT=$(tui_text)
  assert_contains "2/2 (0)"
}

test_bug1_a_cancelled_delete_clears_the_selection() {
  make_repo
  add_wt alpha
  add_wt beta
  tui_start
  tui_wait "3/3"
  tui_send Tab
  tui_wait "(1)"
  tui_send C-d
  tui_wait "Press a key to continue."
  tui_send Enter
  tui_expect "3/3 (0)"
}

# ---- bug 2: in delete_worktrees, v deleted the volumes of several stacks with one key

setup_two_stacks_for_v() {
  make_repo
  add_wt aa
  WT_A=$WT
  add_wt bb
  WT_B=$WT
  stack pa "$WT_A" running
  stack pb "$WT_B" running
  volume pa pa_data
  volume pb pb_data
}

test_bug2_v_on_two_worktrees_does_not_take_volumes_on_one_key() {
  setup_two_stacks_for_v
  worktree_args "$WT_A" "$WT_B"
  wt_keys v --delete "${ARGS[@]}"
  assert_not_log "volume rm pa_data"
  assert_not_log "volume rm pb_data"
  assert_no_docker_changes
  assert_dir "$WT_A"
  assert_dir "$WT_B"
}

test_bug2_v_on_one_worktree_with_two_stacks_does_not_take_volumes_on_one_key() {
  make_repo
  add_wt alpha
  stack pa "$WT" running
  stack pb "$WT" running
  volume pa pa_data
  volume pb pb_data
  worktree_args "$WT"
  wt_keys v --delete "${ARGS[@]}"
  assert_not_log "volume rm pa_data"
  assert_not_log "volume rm pb_data"
  assert_no_docker_changes
  assert_dir "$WT"
}

test_bug2_v_with_the_typed_phrase_takes_both_stacks() {
  setup_two_stacks_for_v
  worktree_args "$WT_A" "$WT_B"
  wt_keys 'v2 stacks\n' --delete "${ARGS[@]}"
  assert_contains "Delete 2 stacks and 2 volumes:"
  assert_contains "pa  (1 volume)"
  assert_contains 'Type "2 stacks" to delete: '
  assert_log "volume rm pa_data"
  assert_log "volume rm pb_data"
  assert_no_dir "$WT_A"
  assert_no_dir "$WT_B"
}

test_bug2_v_with_a_near_match_deletes_nothing() {
  local answer
  setup_two_stacks_for_v
  worktree_args "$WT_A" "$WT_B"
  for answer in '2 stack' '2' 'y' '2 stacks w' '2 Stacks'; do
    wt_keys "v$answer\\n" --delete "${ARGS[@]}"
    assert_no_docker_changes
    assert_dir "$WT_A"
    assert_dir "$WT_B"
  done
}

test_bug2_s_on_two_stacks_stays_one_key_because_volumes_stay() {
  setup_two_stacks_for_v
  worktree_args "$WT_A" "$WT_B"
  wt_keys s --delete "${ARGS[@]}"
  assert_log "compose -p pa down"
  assert_log "compose -p pb down"
  assert_not_log "volume rm pa_data"
  assert_not_log "volume rm pb_data"
}

# a repo inside a worktree: the stack belongs to the worktree, but it has its own main checkout
setup_protected_stack_in_a_worktree() {
  make_repo
  add_wt alpha
  INNER=$WT/node_modules/inner
  must mkdir -p "$INNER"
  must git -C "$INNER" init -q -b main
  stack inner "$INNER" running
  volume inner inner_data
  worktree_args "$WT"
}

test_bug2_v_on_one_protected_stack_needs_its_name() {
  setup_protected_stack_in_a_worktree
  wt_keys 'alpha\nv' --force-delete "${ARGS[@]}"
  assert_contains 'Type "inner" to delete: '
  assert_no_docker_changes
  assert_dir "$WT"
}

test_bug2_v_on_one_protected_stack_takes_it_with_its_name() {
  setup_protected_stack_in_a_worktree
  wt_keys 'alpha\nvinner\n' --force-delete "${ARGS[@]}"
  assert_log "volume rm inner_data"
  assert_no_dir "$WT"
}

test_bug2_v_stops_if_docker_cannot_count_the_volumes() {
  setup_two_stacks_for_v
  shim_hang 'volume ls -q *'
  worktree_args "$WT_A" "$WT_B"
  wt_keys 'v2 stacks\n' --delete "${ARGS[@]}"
  assert_contains "Docker did not answer within 5 seconds, so nothing is deleted"
  assert_no_docker_changes
  assert_dir "$WT_A"
  assert_dir "$WT_B"
}

# ---- bug 3: "remove missing worktree X" ran a repo-wide git worktree prune

test_bug3_removing_one_missing_worktree_keeps_the_other() {
  local a b
  make_repo
  add_wt aa
  a=$WT
  add_wt bb
  b=$WT
  rm -rf "$a" "$b"
  worktree_args "$a"
  wt_keys y --delete "${ARGS[@]}"
  assert_not_registered "$a"
  assert_registered "$b"
}

test_bug3_removes_the_entry_when_the_parent_directory_is_gone_too() {
  make_repo
  add_wt aa
  rm -rf "$(dirname "$WT")"
  worktree_args "$WT"
  wt_keys y --delete "${ARGS[@]}"
  assert_contains "✓ remove missing worktree aa"
  assert_not_registered "$WT"
}

# ---- bug 4: locked worktrees got no badge or reason

test_bug4_list_shows_a_locked_badge() {
  make_repo
  add_wt alpha
  must git -C "$REPO" worktree lock "$WT"
  wt --list
  assert_contains "locked"
}

test_bug4_list_shows_the_lock_reason() {
  make_repo
  add_wt alpha
  must git -C "$REPO" worktree lock --reason "on the usb disk" "$WT"
  wt --list
  assert_contains "on the usb disk"
}

test_bug4_delete_names_the_lock_reason_before_it_asks() {
  make_repo
  add_wt alpha
  must git -C "$REPO" worktree lock --reason "on the usb disk" "$WT"
  worktree_args "$WT"
  wt_keys n --delete "${ARGS[@]}"
  assert_contains "on the usb disk"
  assert_dir "$WT"
}

test_bug4_badge_on_a_narrow_screen() {
  make_repo
  add_wt alpha
  must git -C "$REPO" worktree lock --reason "on the usb disk" "$WT"
  COLUMNS=50 wt --list
  assert_contains "[locked: on the usb disk]"
}

test_bug4_no_badge_without_a_lock() {
  make_repo
  add_wt alpha
  wt --list
  assert_not_contains "locked"
}

test_bug4_a_long_reason_fits_a_narrow_screen() {
  local line
  make_repo
  add_wt alpha
  must git -C "$REPO" worktree lock --reason "$(printf 'x%.0s' {1..200})" "$WT"
  COLUMNS=50 wt --list
  line=$(grep -F '[locked' <<<"$OUT")
  [[ $line == *"…]" ]] || fail "the reason is not cut: $line"
  ((${#line} <= 50)) || fail "the line has ${#line} columns: $line"
}

test_bug4_a_reason_cannot_reach_the_terminal_as_an_escape() {
  make_repo
  add_wt alpha
  must git -C "$REPO" worktree lock --reason "$(printf 'bad\033[31mred')" "$WT"
  wt --list
  assert_contains "[locked"
  if grep -q "$(printf '\033')\\[31m" "$T/out"; then fail "the reason has a raw escape"; fi
}

test_bug4_force_delete_keeps_it_and_says_how_to_unlock() {
  make_repo
  add_wt alpha
  must git -C "$REPO" worktree lock --reason "on the usb disk" "$WT"
  worktree_args "$WT"
  wt_keys y --force-delete "${ARGS[@]}"
  assert_contains "alpha: it is locked (on the usb disk)."
  assert_contains "git -C $REPO worktree unlock $WT"
  assert_not_contains "remove -f -f"
  assert_dir "$WT"
  assert_registered "$WT"
}

test_bug4_the_printed_unlock_command_works() {
  local command
  make_repo
  add_wt alpha
  must git -C "$REPO" worktree lock "$WT"
  worktree_args "$WT"
  wt_keys n --delete "${ARGS[@]}"
  command=$(sed -n 's/.*To unlock it, run: //p' <<<"$OUT" | head -n 1)
  [[ $command == "git -C "* ]] || fail "no command in the output"
  # shellcheck disable=SC2086
  must $command
  worktree_args "$WT"
  wt_keys y --delete "${ARGS[@]}"
  assert_no_dir "$WT"
}

# git holds this line if a lock comes after wtree looked, but only while wtree does not force it twice
test_bug4_a_lock_after_the_plan_stops_a_force_delete() {
  make_repo
  add_wt alpha
  shim_delay 'ps *' 1.5
  worktree_args "$WT"
  (
    wait_log_count '^ps ' 1
    git -C "$REPO" worktree lock "$WT"
  ) >/dev/null 2>&1 &
  wt_keys y --force-delete "${ARGS[@]}"
  assert_contains "✗ delete worktree alpha"
  assert_dir "$WT"
  assert_registered "$WT"
}

test_bug4_a_locked_worktree_with_a_missing_directory_stays_registered() {
  make_repo
  add_wt alpha
  must git -C "$REPO" worktree lock --reason "on the usb disk" "$WT"
  rm -rf "$WT"
  worktree_args "$WT"
  wt_keys y --force-delete "${ARGS[@]}"
  assert_contains "alpha: it is locked (on the usb disk)."
  assert_registered "$WT"
}

test_bug4_a_batch_deletes_the_others_and_keeps_the_locked_one() {
  local locked free
  make_repo
  add_wt locked
  locked=$WT
  add_wt free
  free=$WT
  must git -C "$REPO" worktree lock "$locked"
  worktree_args "$locked" "$free"
  wt_keys y --delete "${ARGS[@]}"
  assert_contains "locked: it is locked."
  assert_contains "Delete 1 worktree:"
  assert_dir "$locked"
  assert_no_dir "$free"
}

test_bug4_the_stacks_of_a_locked_worktree_are_not_offered() {
  make_repo
  add_wt alpha
  stack p "$WT" running
  must git -C "$REPO" worktree lock "$WT"
  worktree_args "$WT"
  wt_keys s --delete "${ARGS[@]}"
  assert_not_contains "Docker stacks of these worktrees:"
  assert_no_docker_changes
}

test_bug4_stack_delete_keeps_a_locked_worktree() {
  make_repo
  add_wt alpha
  stack p "$WT" running
  must git -C "$REPO" worktree lock --reason "on the usb disk" "$WT"
  wt_keys y --stack-delete p "$WT"
  assert_contains "These worktrees stay:"
  assert_contains "alpha: it is locked (on the usb disk)."
  assert_not_contains "w: and worktrees"
  assert_log "compose -p p down"
  assert_dir "$WT"
}

test_bug4_badge_on_the_full_screen() {
  local cols
  make_repo
  add_wt alpha
  must git -C "$REPO" worktree lock --reason "usb" "$WT"
  for cols in 120 50; do
    TUI_COLS=$cols
    tui_start
    tui_expect "[locked: usb]"
    tui_stop
  done
}

# ---- bug 5: docker calls in -d mode had no timeout, so a wedged engine hung

# 124 is what timeout returns, and 137 is its kill
assert_did_not_hang() {
  [[ $RC != 124 && $RC != 137 ]] || fail "wtree hung until the $WT_TIMEOUT second limit: $1"
}

test_bug5_docker_mode_does_not_hang_on_docker_info() {
  make_repo
  shim_hang 'info'
  WT_TIMEOUT=10
  wt -d
  assert_did_not_hang "docker info"
  assert_rc 1
  assert_contains "Docker"
}

test_bug5_stack_list_does_not_hang_on_docker_ps() {
  make_repo
  shim_hang 'ps *'
  WT_TIMEOUT=10
  wt --stack-list
  assert_did_not_hang "docker ps"
}

test_bug5_stack_list_does_not_hang_on_docker_stats() {
  make_repo
  stack p "$REPO" running
  shim_hang 'stats *'
  WT_TIMEOUT=10
  wt --stack-list
  assert_did_not_hang "docker stats"
  assert_contains "p"
}

test_bug5_purge_does_not_hang_on_the_volume_count() {
  make_repo
  add_wt alpha
  stack p "$WT" running
  volume p p_data
  shim_hang 'volume ls -q *'
  WT_TIMEOUT=10
  wt_keys y --stack-purge p "$WT"
  assert_did_not_hang "docker volume ls"
  assert_not_log "volume rm p_data"
}

test_bug5_a_hung_docker_info_says_so() {
  make_repo
  shim_hang 'info'
  WT_TIMEOUT=10
  wt -d
  assert_contains "Docker did not answer within 5 seconds"
}

test_bug5_a_stopped_docker_says_so() {
  make_repo
  shim_fail 'info' 'cannot connect'
  wt -d
  assert_rc 1
  assert_contains "Docker is not running"
}

test_bug5_a_hung_docker_ps_shows_a_message_in_the_list() {
  make_repo
  shim_hang 'ps *'
  WT_TIMEOUT=10
  wt --stack-list
  assert_rc 1
  assert_contains "Docker did not answer within 5 seconds"
  assert_contains "ctrl-r"
}

test_bug5_the_full_screen_shows_the_message_and_a_retry_works() {
  make_repo
  add_wt alpha
  stack pa "$WT" running
  shim_hang 'ps *' 8
  tui_start -d
  tui_expect "Docker did not answer"
  : >"$WTREE_SHIM_DIR/inject"
  tui_reload_when_ready
  tui_expect "1/1"
  tui_expect_gone "Docker did not answer"
}

test_bug5_the_purge_message_says_nothing_is_deleted() {
  make_repo
  add_wt alpha
  stack p "$WT" running
  volume p p_data
  shim_hang 'volume ls -q *'
  WT_TIMEOUT=15
  wt_keys y --stack-purge p "$WT"
  assert_contains "Docker did not answer within 5 seconds, so nothing is deleted"
  assert_no_docker_changes
}

test_bug5_a_slow_compose_down_gets_no_time_limit() {
  make_repo
  add_wt alpha
  stack p "$WT" running
  shim_delay 'compose -p p down' 7
  wt_keys y --stack-delete p "$WT"
  assert_contains "✓ delete stack p"
  assert_not_contains "✗"
}

test_bug5_a_slow_volume_rm_gets_no_time_limit() {
  make_repo
  add_wt alpha
  stack p "$WT" running
  volume p p_data
  shim_delay 'volume rm p_data' 7
  wt_keys y --stack-purge p "$WT"
  assert_contains "✓ delete stack p"
  assert_log "volume rm p_data"
}

test_bug5_all_reads_hung_after_ps_cost_one_limit() {
  local started=$SECONDS
  make_repo
  stack p "$REPO" running
  shim_hang 'stats *'
  shim_hang 'volume ls *'
  shim_hang 'info --format*'
  WT_TIMEOUT=15
  wt --stack-list
  assert_rc 0
  ((SECONDS - started < 10)) || fail "it took $((SECONDS - started)) seconds"
}

# ---- bug 6: run_steps || true turns off set -e in each step, so stack_down went on after a failure

setup_stack_with_volumes() {
  make_repo
  add_wt alpha
  stack p "$WT" running
  volume p p_data
  volume p p_cache
}

test_bug6_failed_down_keeps_the_volumes() {
  setup_stack_with_volumes
  shim_fail 'compose -p p down'
  wt_keys y --stack-purge p "$WT"
  assert_log "compose -p p down"
  assert_not_log "volume rm p_data"
  assert_not_log "volume rm p_cache"
}

test_bug6_failed_down_is_reported() {
  setup_stack_with_volumes
  shim_fail 'compose -p p down' 'the daemon said no'
  wt_keys y --stack-delete p "$WT"
  assert_contains "✗ delete stack p"
  assert_contains "the daemon said no"
  assert_contains "1 step failed, 0 skipped."
}

test_bug6_failed_down_keeps_the_worktree() {
  setup_stack_with_volumes
  shim_fail 'compose -p p down'
  wt_keys w --stack-delete p "$WT"
  assert_contains "- delete worktree alpha: skipped, an earlier step failed"
  assert_dir "$WT"
  assert_registered "$WT"
}

test_bug6_worktree_of_two_stacks_stays_when_one_fails() {
  make_repo
  add_wt alpha
  stack one "$WT" running
  stack two "$WT" running
  shim_fail 'compose -p two down'
  wt_keys w --stack-delete one two "$WT" "$WT"
  assert_contains "- delete worktree alpha: skipped"
  assert_dir "$WT"
}

test_bug6_failed_volume_rm_stops_and_is_reported() {
  setup_stack_with_volumes
  shim_fail 'volume rm p_data' 'volume is in use'
  wt_keys y --stack-purge p "$WT"
  assert_contains "✗ delete stack p"
  assert_contains "volume is in use"
  assert_not_log "volume rm p_cache"
}

test_bug6_worktree_side_reports_a_failed_down() {
  setup_stack_with_volumes
  shim_fail 'compose -p p down'
  worktree_args "$WT"
  wt_keys s --delete "${ARGS[@]}"
  assert_contains "✗ delete stack p"
}

# ---- issue: a failed docker stats, volume ls or info must leave a readable list

setup_stack_for_usage() {
  make_repo
  add_wt alpha
  stack p "$WT" running
  volume p p_data
}

test_failed_stats_keeps_the_rows_and_leaves_usage_blank() {
  setup_stack_for_usage
  shim_fail 'stats *'
  wt --stack-list
  assert_rc 0
  assert_contains "running 1"
  assert_contains "volumes 1"
  assert_contains "docker stats did not answer"
  assert_not_contains "CPU 0.0%"
  assert_not_contains "memory 0MiB"
}

test_hung_stats_keeps_the_rows_and_leaves_usage_blank() {
  setup_stack_for_usage
  shim_hang 'stats *'
  WT_TIMEOUT=10
  wt --stack-list
  assert_rc 0
  assert_contains "running 1"
  assert_not_contains "CPU 0.0%"
}

test_failed_stats_on_a_narrow_screen() {
  setup_stack_for_usage
  shim_fail 'stats *'
  COLUMNS=50 wt --stack-list
  assert_rc 0
  assert_contains "running 1"
  assert_contains "vol 1"
  assert_contains "no answer from Docker"
  assert_not_contains "CPU 0.0%"
}

test_failed_volume_read_shows_no_false_zero() {
  setup_stack_for_usage
  shim_fail 'volume ls *'
  wt --stack-list
  assert_rc 0
  assert_contains "volumes ?"
  assert_not_contains "volumes 0"
  assert_contains "CPU 1.0%"
}

test_failed_info_drops_the_totals_and_keeps_the_rows() {
  setup_stack_for_usage
  shim_fail 'info --format*'
  wt --stack-list
  assert_rc 0
  assert_contains "Docker  CPU 1%   memory 100MiB"
  assert_not_contains " of 0%"
  assert_contains "running 1"
}
