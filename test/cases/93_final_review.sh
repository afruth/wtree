# The real wtree-tui and the real bin/wtree, behind the docker shim: what a late sheet, a held key, a
# refusal and a signal must not do. The cases of the model alone are in final_review_test.go.
# shellcheck shell=bash

# sends one key many times, as a key repeat does
tg_repeat() {
  local key=$1 times=$2 i
  for ((i = 0; i < times; i++)); do
    tmux_cmd send-keys -t t "$key"
    sleep 0.03
  done
}

tg_on_stack_of() {
  tg_move_to "$1"
  tui_send l
  tui_expect "$2"
  tg_move_to "$2"
}

test_final_held_y_does_not_confirm_the_new_plan_of_a_refusal() {
  tg_require
  tg_repo_with_stacks
  tg_start
  tg_loaded alpha
  tg_on_stack_of alpha proj-alpha
  tui_send d
  tui_expect GOES
  tg_pick "with volumes"
  tui_expect "1 stack and 1 volume"
  volume proj-alpha proj-alpha_secret
  sleep 0.4
  tg_repeat y 60
  tui_expect "Something changed"
  assert_core_count "core apply" 1
  assert_no_docker_changes
  tui_expect "and 2 volumes"
}

test_final_two_enters_in_one_send_do_not_confirm() {
  tg_require
  tg_repo
  tg_start
  tg_loaded alpha
  tg_move_to alpha
  tui_send d
  tui_expect GOES
  sleep 0.4
  tmux_cmd send-keys -t t Enter Enter
  sleep 0.5
  assert_core_count "core apply" 0
  assert_dir "$WT_A"
  tui_expect "▸ y"
  tui_send Enter
  tui_expect "1 done"
  assert_no_dir "$WT_A"
}

test_final_a_letter_for_the_search_does_not_answer_a_late_sheet() {
  tg_require
  tg_repo
  tg_start
  tg_loaded alpha
  tg_move_to alpha
  shim_delay 'ps *' 1.5
  tui_send d
  tui_expect "backspace cancels"
  tui_send /
  tui_type pro
  sleep 2
  tui_type y
  tui_expect "/ proy"
  OUT=$(tui_text)
  assert_not_contains GOES
  assert_core_count "core apply" 0
  assert_dir "$WT_A"
}

test_final_backspace_and_a_new_search_cancel_a_late_sheet_of_marks() {
  tg_require
  tg_repo
  echo secret >"$WT_A/.env"
  tg_start
  tg_loaded alpha
  tg_move_to alpha
  tui_send A
  tui_expect marked
  shim_delay 'ps *' 1
  tui_send d
  tui_expect "backspace cancels"
  tui_send BSpace
  tui_expect "cancelled"
  tui_send u
  tui_send /
  tg_repeat y 60
  sleep 1
  assert_core_count "core apply" 0
  assert_dir "$WT_A"
  [[ -f $WT_A/.env ]] || fail "the file is gone"
}

test_final_phrase_does_not_apply_the_old_targets() {
  tg_require
  tg_repo_with_stacks
  tg_start
  tg_loaded alpha
  tg_on_stack_of alpha proj-alpha
  tui_send Space
  tg_on_stack_of beta proj-beta
  tui_send Space
  tui_send /
  tui_type proj-beta
  tui_send Enter
  tui_expect "hidden by filter"
  tui_send d
  tui_expect GOES
  tg_pick "with volumes"
  tui_expect "and 2 volumes"
  tui_send y
  tui_expect "Type "
  shim_delay 'ps *' 1
  tui_type "2 stacks"
  tg_tap_text "HIDDEN BY FILTER"
  sleep 0.1
  tmux_cmd send-keys -t t Enter
  sleep 2
  assert_core_count "core apply" 0
  assert_no_docker_changes
  tui_expect "1 marked"
}

test_final_a_tap_after_a_resize_does_not_confirm() {
  tg_require
  tg_repo_with_stacks
  TUI_COLS=40 TUI_ROWS=44 tg_start
  tg_loaded alpha
  tg_on_stack_of alpha proj-alpha
  tui_send d
  tui_expect GOES
  tg_pick "with volumes"
  tui_expect "and 1 volume"
  tui_send Tab
  tg_find "backspace cancel"
  local col=$TG_COL row=$TG_ROW
  tmux_cmd resize-window -t t -x 40 -y 42
  # what was cancel can be confirm now, and the focus is back on cancel: one tap is the first of two
  tg_tap "$((col + 3))" "$row"
  sleep 0.6
  assert_core_count "core apply" 0
  assert_no_docker_changes
}

test_final_a_hidden_mark_does_not_stop_with_no_sheet() {
  tg_require
  tg_repo_with_stacks
  tg_start
  tg_loaded alpha
  tg_move_to alpha
  tui_send Space
  tui_expect "1 marked"
  tui_send /
  tui_type beta
  tui_send Enter
  tui_expect "hidden by filter"
  tui_send s
  tui_expect "HIDDEN BY FILTER"
  tui_expect "not included"
  assert_core_count "core apply" 0
  assert_no_docker_changes
}

test_final_a_mark_from_a_failure_does_not_stop_with_no_sheet() {
  tg_require
  tg_repo_with_stacks
  shim_fail 'compose -p proj-alpha down*'
  tg_start
  tg_loaded alpha
  tg_on_stack_of alpha proj-alpha
  tui_send d
  tui_expect GOES
  tui_send y
  tui_expect "failed"
  tui_send Enter
  tui_expect "d retries"
  tg_move_to beta
  tui_send s
  tui_expect "not included"
  assert_core_count "core apply stop-stacks" 0
}

test_final_the_sheet_says_what_the_plan_says_of_a_stack_that_came_back() {
  tg_require
  tg_repo
  stack proj-beta "$WT_B" running
  volume proj-ghost proj-ghost_data
  tg_start
  tg_loaded alpha
  tg_move_to proj-ghost
  tui_send Space
  tg_on_stack_of beta proj-beta
  tui_send Space
  stack proj-ghost "$WT_A" running
  tui_send d
  tui_expect GOES
  tui_expect "delete 2 stacks"
  OUT=$(tui_text)
  assert_not_contains "only volumes are left"
  tui_send y
  tui_expect "2 done"
  assert_log "compose -p proj-ghost down"
  assert_log "compose -p proj-beta down"
}

test_final_a_second_signal_ends_a_wait_for_a_wedged_step() {
  tg_require
  tg_repo_with_stacks
  tg_start
  tg_loaded alpha
  tg_on_stack_of alpha proj-alpha
  shim_hang 'compose *' 25
  tui_send s
  local pid tries=0
  pid=$(tg_pid)
  until grep -Fq 'core apply stop-stacks' "$T/core.log"; do
    tries=$((tries + 1))
    ((tries < 100)) || fail "no apply"
    sleep 0.05
  done
  sleep 0.3
  kill -TERM "$pid"
  tui_expect "a batch is running"
  tui_expect "ctrl-c leaves without waiting"
  kill -0 "$pid" || fail "it does not wait for the step at the first signal"
  kill -INT "$pid"
  tries=0
  while kill -0 "$pid" 2>/dev/null; do
    tries=$((tries + 1))
    ((tries < 60)) || fail "only SIGKILL ends it"
    sleep 0.05
  done
  tui_expect "left without waiting"
  assert_eq "$(cat "$T/t.rc")" 130
}

test_final_marks_are_true_after_a_stop_from_worktree_rows() {
  tg_require
  tg_repo_with_stacks
  shim_fail 'compose -p proj-beta *'
  TUI_COLS=48 TUI_ROWS=24 tg_start
  tg_loaded alpha
  tg_move_to alpha
  tui_send Space
  tg_move_to beta
  tui_send Space
  tui_expect "stop 2"
  tui_send s
  tui_expect GOES
  tui_send y
  tui_expect "1 failed"
  tui_expect "s retries"
  tui_send Enter
  tui_expect "1 marked"
  tui_expect "stop 1"
}

test_final_print_refuses_a_path_with_an_escape() {
  tg_require
  make_repo
  local dir=$HOME/code/worktrees/$'esc\033]0;PWNED\007name'
  mkdir -p "$HOME/code/worktrees"
  git -C "$REPO" worktree add -q -b esc "$dir"
  commit_in "$dir"
  tg_start_script
  tg_loaded escname
  tg_move_to escname
  tui_send a
  tui_expect "its path cannot be passed on"
  OUT=$(tui_text)
  assert_not_contains 'cd $(wtree'
  tui_send q
  sleep 0.5
  tui_send q
  wait_file "$T/t.rc"
  if grep -aq $'\033]0;PWNED' "$T/raw.log"; then fail "the sequence reached the terminal"; fi
}

test_final_d_in_a_repo_with_no_stacks_says_so() {
  tg_require
  tg_repo
  TUI_COLS=48 TUI_ROWS=24 tg_start -d
  tui_expect "no stacks here"
  tui_expect "o shows the tree"
  tui_send o
  tui_expect alpha
}

test_final_plain_outside_a_repo_opens_the_machine() {
  need_real_tui
  make_repo
  add_wt alpha
  mkdir -p "$HOME/plain"
  cd "$HOME/plain" || broken "no directory"
  TUI_ENV=(WTREE_UI= "WTREE_TUI=$WTREE_TEST_TUI")
  tui_start
  tui_expect "proj/alpha"
  OUT=$(tui_text)
  assert_not_contains "not a git repository"
  tui_send g
  tui_expect "wtree did not start in a repo"
}
