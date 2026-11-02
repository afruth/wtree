# The real wtree-tui and the real bin/wtree, behind the docker shim, in a private tmux. Each state
# change is asserted on the log of the shim, with its exact words. All safety is in the script, so
# these tests check that the interface shows what the plan says and sends the plan back unchanged.
# shellcheck shell=bash

test_tui_go_runs_behind_the_guard() {
  tg_require
  tg_repo
  tg_start
  tg_loaded alpha
  assert_eq "$(type -ap docker)" "$TEST_ROOT/shim/docker"
  assert_core_log "core version"
  assert_core_log "core list --stream --cwd $REPO"
  if ! grep -Fq 'ps -a' "$WTREE_SHIM_DIR/docker.log"; then fail "the list did not read the shim"; fi
  assert_no_docker_changes
  [[ $(tmux_cmd display-message -p '#{socket_path}') == "$TMUX_TMPDIR/"* ]] || fail "not the private tmux"
  tui_send q
  tg_wait_exit
  assert_rc 0
}

test_tui_go_load_shows_rows_and_stats() {
  tg_require
  tg_repo_with_stacks
  stack idle "$WT_B" exited
  tg_start
  tg_loaded alpha
  tui_expect beta
  tui_expect "1/1"
  tui_expect "1.0%"
  tui_expect "! cleanup"
  OUT=$(tui_text)
  assert_contains "@ proj"
  assert_no_docker_changes
}

test_tui_go_filter_narrows_and_backspace_clears() {
  tg_require
  tg_repo
  tg_start
  tg_loaded alpha
  tui_send /
  tui_type alpha
  tui_expect_gone " beta"
  tui_expect alpha
  tui_send Enter
  tui_expect "filter: alpha"
  tui_send BSpace
  tui_expect " beta"
  tui_expect_gone "filter: alpha"
  assert_no_docker_changes
}

test_tui_go_filter_with_a_typo_matches_nothing() {
  tg_require
  tg_repo
  tg_start
  tg_loaded alpha
  tui_send /
  tui_type foo:bar
  tui_expect "unknown filter: foo:"
  tui_send Enter
  tui_expect "nothing matches"
  tui_send A
  tui_expect "marked 0"
  OUT=$(tui_text)
  assert_not_contains "■"
}

test_tui_go_keys_are_inert_before_the_structure_is_in() {
  tg_require
  tg_repo_with_stacks
  shim_delay 'ps *' 4
  tg_start
  tui_expect "loading worktrees"
  # with no row and no plan, each of these keys is dropped and it is not queued
  local key
  for key in d s n; do
    tui_send "$key"
    tui_expect "loading"
  done
  # the names come before the stage ends, and the keys that act on them stay inert until it does
  tui_expect alpha
  tg_move_to alpha
  for key in d s n; do
    tui_send "$key"
    tui_expect "try again"
  done
  sleep 0.1
  assert_core_count "core plan" 0
  assert_core_count "core new" 0
  tg_loaded alpha
  sleep 0.3
  assert_core_count "core plan" 0
  OUT=$(tui_text)
  assert_not_contains "GOES"
  assert_no_docker_changes
  # the same key works when the list is in
  tui_send d
  tui_expect "GOES"
  assert_core_count "core plan" 1
}

test_tui_go_delete_flow_runs_the_exact_commands() {
  tg_require
  tg_repo_with_stacks
  tg_start
  tg_loaded alpha
  tg_move_to alpha
  tui_send d
  tui_expect "GOES"
  tui_expect "delete 1 worktree"
  # nothing has run, the sheet is the dry run
  assert_no_docker_changes
  assert_dir "$WT_A"
  tg_pick "and stacks with volumes"
  tui_expect "data lost"
  tui_send y
  tui_expect "2 done"
  assert_core_log "core plan delete-worktrees --cwd $REPO -- $WT_A"
  assert_core_count "core apply" 1
  assert_core_log "--choice worktrees+stacks+volumes"
  assert_docker_changes "compose -p proj-alpha down" "volume rm proj-alpha_data"
  assert_no_dir "$WT_A"
  assert_not_registered "$WT_A"
  assert_dir "$WT_B"
  assert_eq "$(git -C "$REPO" branch --list alpha --format='%(refname:short)')" alpha "the branch stays"
  # the list is read again after the end, and what went is gone
  tui_send Enter
  tui_expect_gone " alpha"
  tui_expect " beta"
  tui_send q
  tg_wait_exit
  assert_rc 0
}

test_tui_go_delete_needs_two_enters_and_cancel_runs_nothing() {
  tg_require
  tg_repo
  tg_start
  tg_loaded alpha
  tg_move_to alpha
  tui_send d
  tui_expect "GOES"
  tui_send Enter
  sleep 0.3
  assert_core_count "core apply" 0
  tui_send BSpace
  tui_expect_gone "GOES"
  assert_core_count "core apply" 0
  assert_dir "$WT_A"
  tui_send d
  tui_expect "GOES"
  tui_send Enter
  sleep 0.2
  tui_send Enter
  tui_expect "1 done"
  assert_core_count "core apply" 1
  assert_no_dir "$WT_A"
}

test_tui_go_stop_of_one_stack_runs_at_once_and_says_so() {
  tg_require
  tg_repo_with_stacks
  tg_start
  tg_loaded alpha
  tg_move_to alpha
  tui_send s
  tui_expect "stopped proj-alpha"
  assert_docker_changes "compose -p proj-alpha stop"
  assert_dir "$WT_A"
}

# two stacks with volumes: a phrase, and the sheet opens from marks on the stack rows
tg_two_stacks_with_volumes_sheet() {
  tg_repo_with_stacks
  tg_start
  tg_loaded alpha
  tui_send z
  tui_expect "proj-alpha"
  tui_expect "proj-beta"
  tg_move_to proj-alpha
  tui_send Space
  tui_expect "1 marked"
  tg_move_to proj-beta
  tui_send Space
  tui_expect "2 marked"
  tui_send d
  tui_expect "GOES"
  tg_pick "with volumes"
  tui_expect "data lost"
  tui_send y
  tui_expect 'Type "2 stacks" to delete'
}

test_tui_go_phrase_field_is_never_filled_and_near_miss_is_refused() {
  tg_require
  tg_two_stacks_with_volumes_sheet
  OUT=$(tui_text)
  grep -Eq '^ *.? *> *.? *$|│ > +│' <<<"$OUT" || fail "the field must be empty"
  local near
  for near in "2 stack" "2  stacks" "3 stacks" "y" "2 stacks.."; do
    tui_type "$near"
    tui_send Enter
    tui_expect "expected: 2 stacks"
    assert_core_count "core apply" 0
    assert_no_docker_changes
    # the near miss stays in the field, and backspace takes it away one key at a time
    local i
    for ((i = 0; i < ${#near}; i++)); do tui_send BSpace; done
    sleep 0.1
  done
  tui_send BSpace
  tui_expect_gone "Type "
  tui_expect "GOES"
  assert_core_count "core apply" 0
  assert_volume_kept
}

assert_volume_kept() {
  grep -Fq proj-alpha_data "$WTREE_SHIM_DIR/volumes" || fail "the volume is gone"
  grep -Fq proj-beta_data "$WTREE_SHIM_DIR/volumes" || fail "the volume is gone"
}

test_tui_go_phrase_folds_for_a_phone_and_sends_the_exact_phrase() {
  tg_require
  tg_two_stacks_with_volumes_sheet
  tui_type "  2 STACKS. "
  tui_send Enter
  tui_expect "2 done"
  assert_core_log "--phrase 2 stacks --volumes"
  assert_core_count "core apply" 1
  assert_no_core_log "STACKS"
  assert_docker_changes "compose -p proj-alpha down" "volume rm proj-alpha_data" "compose -p proj-beta down" "volume rm proj-beta_data"
}

test_tui_go_stale_plan_shows_the_new_plan_and_runs_nothing() {
  tg_require
  tg_repo
  tg_start
  tg_loaded alpha
  tg_move_to alpha
  tui_send d
  tui_expect "GOES"
  printf 'new\n' >"$WT_A/made-after-the-sheet.txt"
  tui_send y
  tui_expect "Something changed since this sheet opened"
  tui_expect "it has changed files"
  assert_core_count "core apply" 1
  assert_dir "$WT_A"
  assert_registered "$WT_A"
  assert_no_docker_changes
  # never on its own, and y on a plan with nothing to run does nothing
  tui_send y
  sleep 0.3
  assert_core_count "core apply" 1
  assert_dir "$WT_A"
  # the force level takes a new plan
  tg_pick force
  tui_expect "force"
  assert_core_log "core plan delete-worktrees --force"
  assert_core_count "core apply" 1
  assert_dir "$WT_A"
}

test_tui_go_failed_step_shows_it_and_skips_what_depends_on_it() {
  tg_require
  tg_repo_with_stacks
  shim_fail 'compose -p proj-alpha down' 'volume is in use'
  tg_start
  tg_loaded alpha
  tui_send z
  tui_expect proj-beta
  tg_move_to proj-alpha
  tui_send Space
  tg_move_to proj-beta
  tui_send Space
  tui_expect "2 marked"
  tui_send d
  tui_expect "GOES"
  tg_pick "and their worktrees"
  tui_send y
  tui_expect "failed: volume is in use"
  tui_expect "skipped: proj-alpha failed"
  tui_expect "2 done"
  tui_expect "1 failed"
  tui_expect "1 skipped"
  # a stack that cannot go keeps its worktree, and what does not depend on it still runs
  assert_docker_changes "compose -p proj-alpha down" "compose -p proj-beta down"
  assert_dir "$WT_A"
  assert_registered "$WT_A"
  assert_no_dir "$WT_B"
  # what failed stays marked for a retry
  tui_send Enter
  tui_expect "marked"
}

# ---- touch

test_tui_go_tap_moves_the_cursor_and_does_nothing_else() {
  tg_require
  tg_repo_with_stacks
  tg_start
  tg_loaded alpha
  tg_expect_cursor "@ proj"
  tg_tap_text "▸ beta" 4
  tg_expect_cursor " beta"
  OUT=$(tui_text)
  assert_not_contains "0 cancel"
  assert_not_contains "marked"
  assert_core_count "core plan" 0
  assert_core_count "core open" 0
  assert_no_docker_changes
}

test_tui_go_tap_on_the_cursor_row_opens_the_sheet() {
  tg_require
  tg_repo_with_stacks
  tg_start
  tg_loaded alpha
  tg_tap_text "▸ beta" 4
  tg_expect_cursor " beta"
  tg_tap_text "▸ beta" 4
  tui_expect "0 cancel"
  tui_expect "delete worktree"
  assert_core_count "core plan" 0
  assert_core_count "core open" 0
  assert_no_docker_changes
  # a tap on an item is the key of that item: it opens the sheet of consequences, no more
  tg_tap_text "delete worktree" 2
  tui_expect "GOES"
  assert_core_count "core apply" 0
  assert_no_docker_changes
}

test_tui_go_tap_in_the_footer_zone_is_the_key_of_the_button() {
  tg_require
  tg_repo_with_stacks
  tg_start
  tg_loaded alpha
  tg_move_to alpha
  tg_tap_bar "d delete"
  tui_expect "GOES"
  assert_core_count "core plan" 1
  # one tap on the confirm button only moves the focus: a stray tap cannot confirm
  tg_tap_bar "y  delete"
  sleep 0.3
  assert_core_count "core apply" 0
  tg_tap_bar "backspace cancel"
  tui_expect_gone "GOES"
  assert_core_count "core apply" 0
  assert_no_docker_changes
  assert_dir "$WT_A"
  # a button that cannot work gives its reason in the status line
  tg_move_to proj k
  tg_tap_bar "d delete"
  tui_expect "delete: proj is the main checkout"
  assert_core_count "core plan" 1
}

test_tui_go_tap_on_the_gutter_marks_and_keeps_the_cursor() {
  tg_require
  tg_repo_with_stacks
  tg_start
  tg_loaded alpha
  tg_find "▸ beta"
  tg_tap 1 "$TG_ROW"
  tui_expect "1 marked"
  tg_expect_cursor "@ proj"
  tui_expect "■"
  tg_tap 1 "$TG_ROW"
  tui_expect_gone "1 marked"
  tg_expect_cursor "@ proj"
  assert_core_count "core plan" 0
}

test_tui_go_tap_on_the_chevron_expands_and_keeps_the_cursor() {
  tg_require
  tg_repo_with_stacks
  tg_start
  tg_loaded alpha
  tg_find "▸ alpha"
  tg_tap 3 "$TG_ROW"
  tui_expect "proj-alpha"
  tg_expect_cursor "@ proj"
  tg_tap 3 "$TG_ROW"
  sleep 0.2
  tg_expect_cursor "@ proj"
  assert_no_docker_changes
}

test_tui_go_tap_outside_closes_a_sheet_and_runs_nothing() {
  tg_require
  tg_repo_with_stacks
  TUI_COLS=120 TUI_ROWS=40 tg_start
  tg_loaded alpha
  tg_move_to alpha
  # outside of a sheet of actions
  tui_send a
  tui_expect "0 cancel"
  tg_tap 2 2
  tui_expect_gone "0 cancel"
  tg_expect_cursor " alpha"
  # on the empty lines of the list: nothing moves and nothing opens
  tg_tap 30 30
  sleep 0.3
  tg_expect_cursor " alpha"
  OUT=$(tui_text)
  assert_not_contains "0 cancel"
  # outside of the consequences: it stays open, and nothing runs
  tui_send d
  tui_expect "GOES"
  tg_tap 2 2
  sleep 0.3
  tui_expect "GOES"
  assert_core_count "core apply" 0
  assert_no_docker_changes
  assert_dir "$WT_A"
}

test_tui_go_press_with_no_release_counts_as_a_tap() {
  tg_require
  tg_repo
  tg_start
  tg_loaded alpha
  tg_find " beta"
  tg_press "$((TG_COL + 4))" "$TG_ROW"
  tg_expect_cursor " beta"
  OUT=$(tui_text)
  assert_not_contains "0 cancel"
}

test_tui_go_wheel_scrolls_and_never_acts() {
  tg_require
  tg_repo_with_stacks
  TUI_COLS=48 TUI_ROWS=12 tg_start
  tg_loaded alpha
  tg_send_seq '\033[<65;5;5M\033[<65;5;5M\033[<65;5;5M'
  sleep 0.2
  tg_send_seq '\033[<64;5;5M\033[<64;5;5M\033[<64;5;5M'
  sleep 0.2
  assert_core_count "core plan" 0
  assert_core_count "core apply" 0
  assert_no_docker_changes
}

test_tui_go_phone_tap_on_a_two_line_row_and_on_the_two_row_bar() {
  tg_require
  tg_repo_with_stacks
  TUI_COLS=48 TUI_ROWS=23 tg_start
  tg_loaded alpha
  # the second line of a row is the same target
  tg_find "▸ beta"
  tg_tap 10 "$((TG_ROW + 1))"
  tg_expect_cursor " beta"
  # the lower line of a button is the same target
  tg_find "actions"
  tg_tap "$((TG_COL + 1))" 23
  tui_expect "0 cancel"
  tui_send 0
  tui_expect_gone "0 cancel"
  assert_no_docker_changes
}

# ---- with no mouse

test_tui_go_a_client_with_no_mouse_reaches_everything_with_plain_keys() {
  tg_require
  tg_repo_with_stacks
  tg_start
  tg_loaded alpha
  # search, mark, the sheet of actions with digits, and back with backspace
  tui_send /
  tui_type beta
  tui_send Enter
  tui_expect "filter: beta"
  tui_send BSpace
  tui_expect_gone "filter: beta"
  tg_move_to alpha
  tui_send Space
  tui_expect "1 marked"
  tui_send Space
  tui_expect_gone "1 marked"
  tui_send ?
  tui_expect "? close"
  tui_send BSpace
  tui_expect_gone "? close"
  tui_send a
  tui_expect "0 cancel"
  tui_send 0
  tui_expect_gone "0 cancel"
  tui_send a
  tui_expect "3 delete worktree"
  tui_send 3
  tui_expect "GOES"
  tg_pick "and stacks, keep volumes"
  tui_send y
  tui_expect "2 done"
  assert_docker_changes "compose -p proj-alpha down"
  assert_no_dir "$WT_A"
  tui_send Enter
  tui_expect_gone " alpha"
  tui_send q
  tg_wait_exit
  assert_rc 0
}

test_tui_go_esc_arrives_as_csi_27_u() {
  tg_require
  tg_repo
  tg_start
  tg_loaded alpha
  tui_send ?
  tui_expect "? close"
  tg_send_seq '\033[27u'
  tui_expect_gone "? close"
  tg_move_to alpha
  tui_send d
  tui_expect "GOES"
  tg_send_seq '\033[27u'
  tui_expect_gone "GOES"
  assert_core_count "core apply" 0
  assert_dir "$WT_A"
  sleep 0.4 # an esc right after the one that closed the sheet counts as that key held down
  tg_send_seq '\033[27u'
  tg_wait_exit
  assert_rc 0
}

test_tui_go_plain_esc_closes_too() {
  tg_require
  tg_repo
  tg_start
  tg_loaded alpha
  tui_send a
  tui_expect "0 cancel"
  tui_send Escape
  tui_expect_gone "0 cancel"
  sleep 0.4 # an esc right after the one that closed the sheet counts as that key held down
  tui_send Escape
  tg_wait_exit
  assert_rc 0
}

test_tui_go_tab_and_space_both_toggle_a_mark() {
  tg_require
  tg_repo
  tg_start
  tg_loaded alpha
  tg_move_to alpha
  tui_send Space
  tui_expect "1 marked"
  tui_send Space
  tui_expect_gone "1 marked"
  tui_send Tab
  tui_expect "1 marked"
  sleep 0.4
  tui_send Tab
  tui_expect_gone "1 marked"
  sleep 0.4
  # a double tap of Termius is two tabs at once: one mark, not a mark and an unmark
  tui_send Tab Tab
  tui_expect "1 marked"
  sleep 0.2
  tui_expect "1 marked"
}

# ---- the terminal comes back

# args: the rc that is expected
tg_assert_terminal_restored() {
  tg_wait_exit
  assert_rc "$1"
  local flag
  for flag in alternate_on mouse_any_flag mouse_button_flag mouse_all_flag mouse_sgr_flag mouse_standard_flag pane_in_mode; do
    assert_eq "$(tg_flag "$flag")" 0 "#{$flag} after the exit"
  done
  assert_eq "$(tg_flag cursor_flag)" 1 "the cursor must show"
  wait_file "$T/t.stty"
  grep -Eq '(^|[ ;])icanon( |;|$)' "$T/t.stty" || fail "the terminal stays out of line mode: $(cat "$T/t.stty")"
  grep -Eq '(^|[ ;])echo( |;|$)' "$T/t.stty" || fail "the terminal stays without echo: $(cat "$T/t.stty")"
  tui_expect "shell>"
  # A terminal sends a mouse report only to a program with tracking on, and tracking is off. A line
  # typed now reads as typed, on a clean screen.
  tui_type "printf '\\033[2J\\033[Hcl%s\\n' ean"
  tui_send Enter
  tui_expect "clean"
  OUT=$(tui_text)
  assert_not_contains "[<"
  assert_not_contains "^["
  assert_not_contains "0;"
}

test_tui_go_program_asks_for_button_events_only() {
  tg_require
  tg_repo
  tg_start
  tg_loaded alpha
  assert_eq "$(tg_flag mouse_button_flag)" 1 "button events"
  assert_eq "$(tg_flag mouse_all_flag)" 0 "never all motion"
  assert_eq "$(tg_flag mouse_sgr_flag)" 1 "SGR"
  assert_eq "$(tg_flag alternate_on)" 1 "alternate screen"
}

test_tui_go_terminal_is_restored_after_a_quit() {
  tg_require
  tg_repo
  tg_start
  tg_loaded alpha
  tui_send q
  tg_assert_terminal_restored 0
}

test_tui_go_terminal_is_restored_after_sigterm() {
  tg_require
  tg_repo
  tg_start
  tg_loaded alpha
  kill -TERM "$(tg_pid)"
  tg_assert_terminal_restored 143
}

test_tui_go_terminal_is_restored_after_sighup() {
  tg_require
  tg_repo
  tg_start
  tg_loaded alpha
  kill -HUP "$(tg_pid)"
  tg_assert_terminal_restored 129
}

test_tui_go_terminal_is_restored_after_a_panic() {
  tg_require
  tg_repo
  tg_start --panic-after 700ms
  tui_expect "alpha"
  tg_wait_exit
  assert_contains "panic-after"
  tg_assert_terminal_restored 2
}

# the bytes, through a pty: the reset is the last thing on the wire
tg_assert_reset_is_last() {
  wait_file "$T/raw.log"
  local tries=0
  until python3 -I - "$T/raw.log" <<'PY' >"$T/raw.check" 2>&1
import sys
data = open(sys.argv[1], 'rb').read()
off = b"\x1b[?1006l\x1b[?1003l\x1b[?1002l\x1b[?1000l\x1b[?2004l\x1b[?1004l\x1b[?2026l\x1b[?25h"
# a program may still say in plain words what happened, and script ends its log with a line
after = data[data.rindex(off) + len(off):]
if b"\x1b" in after:
    sys.exit("a sequence came after the reset: %r" % after[:80])
if b"\x1b[?1049h" in data and b"\x1b[?1049l" not in data:
    sys.exit("the alternate screen stays on")
PY
  do
    tries=$((tries + 1))
    if ((tries > 40)); then
      OUT=$(cat "$T/raw.check")
      fail "the reset is not the last thing written: $OUT"
    fi
    sleep 0.05
  done
}

test_tui_go_reset_is_the_last_thing_written_after_sigterm() {
  tg_require
  tg_repo
  tg_start_script
  tg_loaded alpha
  kill -TERM "$(tg_pid)"
  tg_wait_exit
  tg_assert_reset_is_last
}

test_tui_go_reset_is_the_last_thing_written_after_a_panic() {
  tg_require
  tg_repo
  tg_start_script --panic-after 700ms
  tui_expect alpha
  tg_wait_exit
  tg_assert_reset_is_last
}

# ---- resize

test_tui_go_resize_during_an_apply_keeps_it_going() {
  tg_require
  tg_repo_with_stacks
  shim_delay 'compose -p proj-alpha down' 2
  tg_start
  tg_loaded alpha
  tg_move_to alpha
  tui_send d
  tui_expect "GOES"
  tg_pick "and stacks, keep volumes"
  tui_send y
  tui_expect "deleting"
  wait_log_count '^compose -p proj-alpha down' 1
  tmux_cmd resize-window -t t -x 48 -y 23
  tui_expect "deleting"
  tmux_cmd resize-window -t t -x 93 -y 14
  tui_expect "deleting"
  tmux_cmd resize-window -t t -x 120 -y 40
  tui_expect "2 done"
  assert_docker_changes "compose -p proj-alpha down"
  assert_core_count "core apply" 1
  assert_no_dir "$WT_A"
}

# capture without what changes with the clock
tg_mask() {
  sed -E 's/live [0-9]+s/live Ns/; s/disk [0-9]+[smh]/disk N/; s/[0-9]+s /Ns /g'
}

test_tui_go_rotation_leaves_nothing_of_the_old_frame() {
  tg_require
  tg_repo_with_stacks
  tg_scan_disk
  TUI_COLS=93 TUI_ROWS=14 tg_start
  tg_loaded alpha
  OUT=$(tui_text)
  assert_contains "STACK"
  tmux_cmd resize-window -t t -x 48 -y 41
  tui_expect_gone "STACK"
  sleep 0.5
  # the terminal is the judge: tmux holds the cells, and they must be those of a fresh start
  local got want
  got=$(tg_text | tg_mask)
  tg_start_in fresh 48 41
  local tries=0
  # a phone has no room for the age: the header says loading until the list has ended
  until tmux_cmd capture-pane -p -t fresh | grep -q "alpha" && ! tmux_cmd capture-pane -p -t fresh | grep -q "loading"; do
    tries=$((tries + 1))
    ((tries < 200)) || broken "the fresh start never loaded"
    sleep 0.05
  done
  sleep 0.2
  want=$(tmux_cmd capture-pane -p -t fresh | python3 -I -c 'import sys; sys.stdout.write(sys.stdin.read().expandtabs(8))' | tg_mask)
  OUT=$(diff <(printf '%s\n' "$want") <(printf '%s\n' "$got")) || fail "the rotated screen differs from a fresh start at that size"
  # and back
  tmux_cmd resize-window -t t -x 93 -y 14
  tui_expect "STACK"
}

test_tui_go_each_size_draws_inside_the_pane() {
  tg_require
  tg_repo_with_stacks
  local size cols rows line
  tg_start
  tg_loaded alpha
  for size in 100x30 80x24 60x20 48x23 48x40 93x14 40x12 120x40; do
    cols=${size%x*}
    rows=${size#*x}
    tmux_cmd resize-window -t t -x "$cols" -y "$rows"
    sleep 0.3
    OUT=$(tui_text)
    assert_contains "alpha"
    assert_eq "$(tmux_cmd display-message -p -t t '#{pane_width}x#{pane_height}')" "$size"
    while IFS= read -r line; do
      ((${#line} <= cols)) || fail "a line of $size is wider: $line"
    done <<<"$OUT"
  done
  assert_no_docker_changes
}

# ---- what else a phone sees

test_tui_go_docker_down_keeps_the_worktrees_and_says_how_to_retry() {
  tg_require
  tg_repo
  shim_fail 'ps *' 'Cannot connect to the Docker daemon'
  shim_fail 'volume ls *' 'Cannot connect to the Docker daemon'
  shim_fail 'info*' 'Cannot connect to the Docker daemon'
  shim_fail 'stats *' 'Cannot connect to the Docker daemon'
  tg_start
  tui_expect "docker is not running"
  tui_expect "r retry"
  tui_expect alpha
  tui_expect beta
  # the worktree actions still work, and the plan has what the core says
  tg_move_to alpha
  tui_send a
  tui_expect "delete worktree"
  tui_send BSpace
  : >"$WTREE_SHIM_DIR/inject"
  tui_send r
  tui_expect_gone "docker is not running"
  assert_no_docker_changes
}

test_tui_go_cleanup_view_follows_the_tiers_of_the_core() {
  tg_require
  tg_repo
  volume proj-lonely proj-lonely_data
  tg_start
  tg_loaded alpha
  tui_send c
  tui_expect "c back"
  tui_expect "needs a look"
  # a stack with only volumes is never safe: A takes the safe rows only
  tui_send A
  sleep 0.2
  OUT=$(tui_text)
  assert_contains "proj-lonely"
  if tg_marked_row proj-lonely; then fail "A marked a row that is not safe"; fi
  tui_send c
  tui_expect_gone "c back"
}

tg_marked_row() {
  tui_text | grep -F "$1" | grep -Fq "■"
}

test_tui_go_sigterm_during_an_apply_stops_after_the_step() {
  tg_require
  tg_repo_with_stacks
  shim_delay 'compose -p proj-alpha down' 2
  tg_start
  tg_loaded alpha
  tui_send z
  tui_expect proj-beta
  tg_move_to proj-alpha
  tui_send Space
  tg_move_to proj-beta
  tui_send Space
  tui_expect "2 marked"
  tui_send d
  tui_expect "GOES"
  tui_send y
  tui_expect "deleting"
  wait_log_count '^compose -p proj-alpha down' 1
  kill -TERM "$(tg_pid)"
  tg_wait_exit
  assert_rc 143
  # the step in flight ended, the next one never started, and the terminal is back
  assert_docker_changes "compose -p proj-alpha down"
  assert_eq "$(tg_flag alternate_on)" 0
  assert_contains "it stops after the current step"
}

test_tui_go_quit_during_an_apply_asks_first() {
  tg_require
  tg_repo_with_stacks
  shim_delay 'compose -p proj-alpha down' 2
  tg_start
  tg_loaded alpha
  tui_send z
  tui_expect proj-beta
  tg_move_to proj-alpha
  tui_send Space
  tg_move_to proj-beta
  tui_send Space
  tui_expect "2 marked"
  tui_send d
  tui_expect "GOES"
  tui_send y
  tui_expect "deleting"
  tui_send q
  tui_expect "keep going"
  tui_send 1
  tui_expect_gone "keep going"
  tui_expect "2 done"
  assert_docker_changes "compose -p proj-alpha down" "compose -p proj-beta down"
}

# ---- sheets that end in nothing

test_tui_go_d_on_the_main_checkout_says_why_and_asks_for_no_plan() {
  tg_require
  tg_repo_with_stacks
  tg_start
  tg_loaded alpha
  tg_expect_cursor "@ proj"
  # once the core has said that this row can never go. Before that, the plan says it: see below
  tui_expect "nothing to clean"
  tui_send d
  tui_expect "delete: proj is the main checkout"
  OUT=$(tui_text)
  assert_not_contains "GOES"
  assert_core_count "core plan" 0
  assert_no_docker_changes
}

test_tui_go_keep_volumes_on_a_volumes_only_stack_does_nothing() {
  tg_require
  tg_repo
  volume proj-lonely proj-lonely_data
  tg_start
  tg_loaded alpha
  tg_move_to proj-lonely
  tui_send d
  tui_expect "nothing goes at this level"
  tui_expect "choose with volumes"
  # y cannot run it, and enter only moves to the level that loses data: its own sheet and phrase
  tui_send y
  sleep 0.3
  assert_core_count "core apply" 0
  assert_no_docker_changes
  tui_send Enter
  tui_expect "data lost"
  assert_core_count "core apply" 0
  assert_no_docker_changes
}

# ---- round 2 of the design review, against the real core

test_tui_go_backspace_at_the_top_does_not_quit() {
  tg_require
  tg_repo
  tg_start
  tg_loaded alpha
  tui_send BSpace
  tui_expect "q quits"
  sleep 0.2
  tui_send BSpace
  tui_send BSpace
  tui_expect "alpha"
  tui_send q
  tg_wait_exit
  assert_rc 0
  assert_no_docker_changes
}

# The level "with volumes" on a stack with no volumes takes no data: the core counts 0, and the
# sheet does not say that data is lost.
test_tui_go_with_volumes_on_a_stack_with_no_volumes_says_no_loss() {
  tg_require
  tg_repo_with_stacks
  tg_start
  tg_loaded alpha
  tg_move_to alpha
  tui_send l
  tui_expect proj-alpha
  tg_move_to proj-alpha
  tui_send d
  tui_expect "GOES"
  tg_pick "with volumes"
  tui_expect "containers, networks"
  tui_expect_gone "data lost"
  tui_expect_gone "0 volumes"
  assert_core_count "core apply" 0
  assert_no_docker_changes
}
