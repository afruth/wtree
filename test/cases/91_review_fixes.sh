# Each test states a defect that a review found, in the words of its reproducer.
# shellcheck shell=bash

# ---- the phone menu runs many actions in one process

test_menu_action_does_not_replay_the_steps_of_an_earlier_action() {
  local alpha
  make_repo
  add_wt alpha
  alpha=$WT
  add_wt beta
  stack zulu "$alpha" running
  stack yankee "$WT" running
  volume zulu zulu_data
  TUI_COLS=50 TUI_ROWS=30
  tui_start -d
  tui_wait yankee
  tui_type zulu
  tui_expect_gone yankee
  tui_send Enter
  tui_wait "select several"
  tui_type "delete with"
  tui_expect_gone "select several"
  tui_send Enter
  tui_wait "stacks only"
  tui_type y
  wait_log_count '^volume rm zulu_data' 1
  tui_wait yankee
  stack zulu "$alpha" running
  volume zulu zulu_newdata
  tui_type yankee
  tui_expect_gone zulu
  tui_send Enter
  tui_wait "select several"
  tui_type stop
  tui_expect_gone "select several"
  tui_send Enter
  wait_log_count '^compose -p yankee stop' 1
  sleep 0.5
  assert_log_count "compose -p zulu down" 1
  assert_not_log "volume rm zulu_newdata"
}

# ---- a delete takes all that is inside the directory

setup_ignored_locked_inner() {
  make_repo
  printf '.wt/\n' >>"$REPO/.gitignore"
  must git -C "$REPO" add -A
  must git -C "$REPO" commit -qm ignore
  add_wt outer
  OUTER=$WT
  INNER=$OUTER/.wt/inner
  must git -C "$REPO" worktree add -q -b inner "$INNER"
  printf 'precious\n' >"$INNER/uncommitted.txt"
  must git -C "$REPO" worktree lock --reason 'keep me' "$INNER"
  worktree_args "$OUTER"
}

test_plain_delete_keeps_a_worktree_with_a_worktree_inside_it() {
  setup_ignored_locked_inner
  wt_keys 'y\n' --delete "${ARGS[@]}"
  assert_contains "is inside it, delete that one first"
  assert_dir "$OUTER"
  assert_dir "$INNER"
  [[ -f $INNER/uncommitted.txt ]] || fail "the file in the inner worktree is gone"
}

test_force_delete_keeps_a_worktree_with_a_worktree_inside_it() {
  setup_ignored_locked_inner
  wt_keys 'outer\ny\n' --force-delete "${ARGS[@]}"
  assert_contains "is inside it, delete that one first"
  assert_not_contains 'Type "outer"'
  assert_dir "$INNER"
}

test_force_delete_keeps_a_worktree_when_the_current_directory_is_inside_it() {
  make_repo
  add_wt outer
  must mkdir "$WT/other"
  must git -C "$WT/other" init -q -b main
  cd "$WT/other" || broken "cd"
  worktree_args "$WT"
  wt_keys 'outer\n' --force-delete "${ARGS[@]}"
  assert_contains "you are in this worktree"
  assert_dir "$WT/other"
}

test_a_missing_inner_worktree_does_not_stop_the_delete() {
  make_repo
  add_wt outer
  must git -C "$REPO" worktree add -q -b inner "$WT/inner"
  must rm -rf "$WT/inner"
  worktree_args "$WT"
  wt_keys y --delete "${ARGS[@]}"
  assert_no_dir "$WT"
}

setup_ignored_inner_repo() {
  make_repo
  add_wt outer
  must mkdir -p "$WT/node_modules/dep"
  must git -C "$WT/node_modules/dep" init -q -b main
  worktree_args "$WT"
}

test_plain_delete_keeps_a_worktree_with_an_ignored_repo_inside_it() {
  setup_ignored_inner_repo
  wt_keys 'y\n' --delete "${ARGS[@]}"
  assert_contains "another git repo is inside it: ~/code/worktrees/outer/node_modules/dep"
  assert_contains "press ctrl-f to delete it"
  assert_dir "$WT/node_modules/dep"
}

test_force_delete_of_an_inner_repo_needs_the_name_of_the_worktree() {
  setup_ignored_inner_repo
  wt_keys 'y\n' --force-delete "${ARGS[@]}"
  assert_contains 'Type "outer" to delete:'
  assert_dir "$WT/node_modules/dep"
  wt_keys 'outer\n' --force-delete "${ARGS[@]}"
  assert_contains "Another git repo inside a worktree is deleted too"
  assert_no_dir "$WT"
}

test_stack_delete_keeps_a_worktree_with_a_worktree_inside_it() {
  setup_ignored_locked_inner
  stack s "$OUTER" running
  wt_keys w --stack-delete s "$OUTER"
  assert_contains "These worktrees stay:"
  assert_contains "is inside it, delete that one first"
  assert_not_contains "w: and worktrees"
  assert_dir "$OUTER"
}

# ---- protection of volumes

test_row_of_a_project_with_a_main_checkout_directory_needs_the_name() {
  make_repo
  add_wt alpha
  stack shared "$REPO" running
  stack shared "$WT" running
  volume shared shared_data
  wt_keys 'y\n' --stack-purge shared "$WT"
  assert_contains 'Type "shared" to delete:'
  assert_no_docker_changes
}

test_purge_stops_when_docker_cannot_say_where_a_stack_runs() {
  setup_linked_stack
  shim_fail 'ps *'
  wt_keys 'y\n' --stack-purge p "$WT"
  assert_contains "Docker is not running, so nothing is deleted"
  assert_no_docker_changes
}

test_main_checkout_stack_keeps_its_protection_after_a_down() {
  make_repo proj
  stack proj "$REPO" running
  volume proj proj_pgdata
  wt_keys y --stack-delete proj "$REPO"
  assert_not_log "volume rm proj_pgdata"
  wt --stack-list
  assert_contains "volumes only"
  wt_keys 'y\n' --stack-purge proj ""
  assert_contains 'Type "proj" to delete:'
  assert_not_log "volume rm proj_pgdata"
}

test_containers_with_no_directory_label_need_the_name() {
  make_repo
  stack nodir "" running
  volume nodir nodir_data
  wt_keys 'y\n' --stack-purge nodir ""
  assert_contains 'Type "nodir" to delete:'
  assert_no_docker_changes
}

test_volumes_of_another_project_with_the_repo_prefix_need_the_name() {
  make_repo proj
  volume proj-prod proj-prod_pgdata
  wt --stack-list
  assert_contains "proj-prod"
  wt_keys 'y\n' --stack-purge proj-prod ""
  assert_contains 'Type "proj-prod" to delete:'
  assert_no_docker_changes
}

test_main_checkout_through_a_symlink_is_protected() {
  make_repo
  must ln -s "$REPO" "$HOME/link"
  stack mainstack "$HOME/link" running
  volume mainstack mainstack_data
  wt_keys 'y\n' --stack-purge mainstack "$HOME/link"
  assert_contains 'Type "mainstack" to delete:'
  assert_no_docker_changes
}

test_worktree_through_a_symlink_keeps_its_stacks() {
  make_repo
  add_wt alpha
  must ln -s "$WT" "$HOME/link"
  stack linked "$HOME/link" running
  volume linked linked_data
  worktree_args "$WT"
  wt_keys n --delete "${ARGS[@]}"
  assert_contains "linked  (alpha)"
}

test_stale_row_directory_gets_no_worktree_offer() {
  setup_linked_stack
  must git -C "$REPO" worktree add -q -b other "$(worktree_path other)"
  wt_keys w --stack-delete p "$(worktree_path other)"
  assert_not_contains "w: and worktrees"
  assert_dir "$(worktree_path other)"
}

test_v_names_a_directory_outside_the_selection_and_needs_the_phrase() {
  local alpha beta
  make_repo
  add_wt alpha
  alpha=$WT
  add_wt beta
  beta=$WT
  stack shared "$alpha" running
  stack shared "$beta" running
  volume shared shared_data
  worktree_args "$alpha"
  wt_keys 'vy\n' --delete "${ARGS[@]}"
  assert_contains "also runs in ~/code/worktrees/beta"
  assert_contains "Delete 1 stack and 1 volume:"
  assert_contains 'Type "shared" to delete:'
  assert_no_docker_changes
  assert_dir "$alpha"
}

test_v_on_one_key_shows_the_volumes_and_asks_again() {
  setup_linked_stack
  worktree_args "$WT"
  wt_keys vn --delete "${ARGS[@]}"
  assert_contains "Delete 1 stack and"
  assert_contains "The data in the volumes is lost."
  assert_no_docker_changes
  assert_dir "$WT"
}

test_w_keeps_a_worktree_where_another_stack_runs() {
  make_repo
  add_wt alpha
  stack one "$WT" running
  stack two "$WT" running
  volume two two_data
  wt_keys w --stack-delete one "$WT"
  assert_contains "alpha: the stack two also has containers there, and it stays"
  assert_not_contains "w: and worktrees"
  assert_dir "$WT"
}

test_w_takes_the_worktree_when_all_its_stacks_are_selected() {
  make_repo
  add_wt alpha
  stack one "$WT" running
  stack two "$WT" running
  wt_keys w --stack-delete one two "$WT" "$WT"
  assert_log "compose -p one down"
  assert_log "compose -p two down"
  assert_no_dir "$WT"
}

# ---- odd paths

setup_trailing_space() {
  make_repo
  add_wt plain
  PLAIN=$WT
  printf 'precious\n' >"$PLAIN/uncommitted.txt"
  SPACEY="$PLAIN "
  must git -C "$REPO" worktree add -q -b spacey "$SPACEY"
  tui_start
  tui_wait spacey
  tui_type spacey
  tui_expect "1/"
}

test_tui_force_delete_of_a_path_with_a_trailing_space_keeps_its_sibling() {
  setup_trailing_space
  tui_send C-f
  tui_wait "Continue?"
  tui_type y
  tui_wait "worktree >"
  [[ -f $PLAIN/uncommitted.txt ]] || fail "the sibling lost its file"
  assert_no_dir "$SPACEY"
}

test_tui_stack_delete_of_a_directory_with_a_trailing_space_keeps_its_sibling() {
  local spacey
  make_repo
  add_wt plain
  spacey="$WT "
  must git -C "$REPO" worktree add -q -b spacey "$spacey"
  stack spaced "$spacey" running
  tui_start -d
  tui_wait spaced
  tui_send C-d
  tui_wait "w: and worktrees"
  tui_type w
  tui_wait "stack >"
  assert_log "compose -p spaced down"
  assert_no_dir "$spacey"
  assert_dir "$WT"
}

setup_odd_path() {
  make_repo
  add_wt alpha
  ODD=$WT$1x
  must git -C "$REPO" worktree add -q -b oddbranch "$ODD"
}

expect_odd_path_stays() {
  tui_start
  tui_wait oddbranch
  tui_expect "wtree cannot act on it"
  tui_type oddbranch
  tui_expect "1/"
  tui_send C-d
  tui_wait "Press a key to continue."
  tui_expect "nothing to delete"
  assert_dir "$WT"
  assert_dir "$ODD"
  assert_registered "$WT"
}

test_tui_a_newline_in_a_path_acts_on_nothing() {
  setup_odd_path $'\n'
  expect_odd_path_stays
}

test_tui_a_tab_in_a_path_acts_on_nothing() {
  make_repo
  must mkdir "$REPO/x"
  add_wt alpha
  ODD=$WT$'\t'x
  must git -C "$REPO" worktree add -q -b oddbranch "$ODD"
  expect_odd_path_stays
}

test_a_newline_in_a_path_without_git_list_z_acts_on_nothing() {
  local real
  setup_odd_path $'\n'
  real=$(command -v git)
  must mkdir "$T/oldgit"
  cat >"$T/oldgit/git" <<SCRIPT
#!/bin/bash
for arg in "\$@"; do [[ \$arg != -z ]] || exit 129; done
"$real" "\$@"
SCRIPT
  must chmod +x "$T/oldgit/git"
  export PATH=$T/oldgit:$PATH
  wt --list
  assert_contains "wtree cannot act on it"
  assert_contains "alpha"
}

test_lock_reason_with_unicode_is_readable_and_cut_whole() {
  local reason
  make_repo
  add_wt alpha
  printf -v reason '%0100d' 0
  reason=${reason//0/ü}
  must git -C "$REPO" worktree lock --reason "$reason" "$WT"
  COLUMNS=50 wt --list
  assert_contains "[locked: üüü"
  assert_contains "…]"
  assert_not_contains '\303'
  worktree_args "$WT"
  wt_keys '\n' --delete "${ARGS[@]}"
  assert_contains "it is locked (üüü"
}

# ---- keys

test_tui_a_key_from_before_the_prompt_answers_nothing() {
  make_repo
  add_wt alpha
  stack zulu "$WT" running
  volume zulu zulu_data
  tui_start -d
  tui_wait zulu
  shim_delay 'volume ls -q*' 2
  tui_send C-f
  sleep 0.8
  tui_type y
  tui_expect "w: and worktrees"
  tui_expect_absent_for "delete stack" 2.5
  assert_no_docker_changes
  tui_type y
  wait_log_count '^volume rm zulu_data' 1
}

test_tui_a_key_from_before_the_typed_phrase_answers_nothing() {
  make_repo
  stack mainstack "$REPO" running
  volume mainstack mainstack_data
  tui_start -d
  tui_wait mainstack
  shim_delay 'volume ls -q*' 2
  tui_send C-f
  sleep 0.8
  tui_type mainstack
  tui_send Enter
  tui_expect 'to delete:'
  sleep 0.5
  assert_no_docker_changes
}

test_tui_shift_click_during_a_reload_selects_nothing() {
  local row seq hex
  make_repo
  add_wt alpha
  add_wt beta
  tui_start
  tui_wait "3/3"
  shim_delay 'ps *' 2
  tui_send C-r
  tui_expect ' … > '
  row=$(tui_row_of beta)
  [[ -n $row ]] || broken "the old row is not on the screen"
  printf -v seq '\033[<4;6;%dM\033[<4;6;%dm' "$row" "$row"
  hex=$(printf '%s' "$seq" | od -An -tx1 | tr -d '\n')
  # shellcheck disable=SC2086
  tmux_cmd send-keys -t t -H $hex
  tui_expect_absent_for "(1)" 0.5
  tui_wait "worktree >"
  tui_expect "(0)"
}

test_tui_a_key_right_after_the_prompt_is_ready_counts() {
  local round
  make_repo
  add_wt alpha
  for round in 1 2 3 4 5; do
    tui_send Escape 2>/dev/null
    tui_stop
    tui_start
    tui_wait "worktree >"
    tui_send Tab
    tui_expect "(1)"
  done
}

test_tui_the_prompt_names_the_repo() {
  make_repo
  tui_start
  tui_expect "proj worktree >"
}

test_tui_an_odd_repo_name_does_not_break_the_keys() {
  make_repo 'we)ird+name'
  add_wt alpha
  tui_start
  tui_expect "we_ird_name worktree >"
  tui_send C-r
  tui_expect "we_ird_name worktree >"
  tui_wait "2/2"
}

# ---- when Docker does not answer

test_a_worktree_delete_says_so_when_docker_cannot_list_the_stacks() {
  make_repo
  add_wt alpha
  stack p "$WT" running
  shim_fail 'ps *'
  worktree_args "$WT"
  wt_keys n --delete "${ARGS[@]}"
  assert_contains "Docker is not running, so wtree cannot see the stacks of these worktrees"
  assert_contains "Continue?"
  assert_dir "$WT"
}

test_a_worktree_delete_sees_stacks_of_a_slow_docker() {
  make_repo
  add_wt alpha
  stack p "$WT" running
  shim_delay 'ps *' 4
  worktree_args "$WT"
  wt_keys n --delete "${ARGS[@]}"
  assert_contains "p  (alpha)"
}

test_a_worktree_delete_needs_no_docker_on_the_host() {
  make_repo
  add_wt alpha
  worktree_args "$WT"
  local tools=$T/nodocker
  must mkdir "$tools"
  local tool
  for tool in "$WTREE_TEST_SCRATCH"/tools/*; do must ln -s "$tool" "$tools/"; done
  PATH=$tools wt_keys y --delete "${ARGS[@]}"
  assert_not_contains "cannot see the stacks"
  assert_no_dir "$WT"
}

test_a_failed_volume_read_in_the_step_leaves_the_containers_and_says_why() {
  setup_linked_stack
  shim_fail_from 'volume ls -q *' 2
  wt_keys y --stack-purge p "$WT"
  assert_contains "Docker is not running, so nothing is deleted"
  assert_not_log "compose -p p down"
}

test_the_step_reads_the_volumes_before_the_down() {
  setup_linked_stack
  wt_keys y --stack-purge p "$WT"
  local last down
  last=$(grep -n '^volume ls -q' "$WTREE_SHIM_DIR/docker.log" | tail -n 1 | cut -d: -f1)
  down=$(grep -n '^compose -p p down' "$WTREE_SHIM_DIR/docker.log" | cut -d: -f1)
  ((last < down)) || fail "a volume read came after the down"
}

test_a_failed_volume_rm_names_the_volumes_that_remain() {
  setup_linked_stack
  volume p p_extra
  shim_fail 'volume rm p_extra'
  wt_keys y --stack-purge p "$WT"
  assert_contains "The containers are gone, and these volumes can remain:"
  assert_contains "p_extra"
  assert_contains "wtree -g -d lists them."
}

test_tui_narrow_message_of_a_docker_that_hangs_fits_and_a_tap_retries() {
  make_repo
  add_wt alpha
  stack p "$WT" running
  shim_hang 'ps *' 8
  TUI_COLS=40 TUI_ROWS=20
  tui_start -d
  tui_wait "Tap here to try again."
  tui_expect "Docker: no answer in 5 s"
  rm -f "$WTREE_SHIM_DIR/inject"
  tui_click_row "Tap here"
  tui_wait "p"
  tui_expect "running 1"
  tui_expect_gone "Tap here"
}

test_tui_wide_message_of_a_docker_that_fails_retries_on_a_click() {
  make_repo
  add_wt alpha
  stack p "$WT" running
  shim_fail 'ps *'
  tui_start -d
  tui_wait "Press ctrl-r or click here to try again"
  rm -f "$WTREE_SHIM_DIR/inject"
  tui_click_row "Press ctrl-r"
  tui_expect "running 1"
}

# ---- the watchdog of a docker read

# a race of a fork and a kill, so no test can force it: bash runs an inherited EXIT trap in a watchdog
# that is killed before it resets its traps. 1 list in about 600 came out empty at 48 jobs.
test_an_exit_trap_acts_only_in_the_process_that_set_it() {
  OUT=$(grep -n "trap '" "$WTREE" | grep EXIT | grep -v 'BASHPID') || OUT=
  assert_eq "$OUT" ""
}

test_a_docker_that_ignores_term_is_killed() {
  local started=$SECONDS
  make_repo
  shim_wedge 'ps *' 15
  wt --stack-list
  assert_contains "Docker did not answer within 5 seconds"
  ((SECONDS - started < 12)) || fail "it took $((SECONDS - started)) seconds"
}

test_the_disk_read_keeps_its_long_limit() {
  assert_eq "$(grep -c 'limited "$DOCKER_DISK_LIMIT" docker system df' "$WTREE")" 2
  grep -q '^DOCKER_DISK_LIMIT=600$' "$WTREE" || fail "the limit changed"
}

test_a_fast_read_leaves_no_long_sleep_behind() {
  local left
  setup_linked_stack
  tui_start -d
  tui_wait "p"
  wait_file "$XDG_CACHE_HOME/wtree/disk-totals"
  left=$(ps -eo args | grep -cx 'sleep 600')
  assert_eq "$left" 0
}
