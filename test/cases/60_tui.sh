# The real fzf lists in a private tmux. Each test waits for the list before it sends a key.
# shellcheck shell=bash

setup_two_worktrees() {
  make_repo
  add_wt alpha
  WT_A=$WT
  add_wt beta
  WT_B=$WT
}

setup_two_stacks() {
  make_repo
  add_wt alpha
  WT_A=$WT
  add_wt beta
  WT_B=$WT
  stack zulu "$WT_A" running
  stack yankee "$WT_B" running
  volume zulu zulu_data
  volume yankee yankee_data
}

test_tui_worktree_list_loads_and_esc_exits_zero() {
  setup_two_worktrees
  tui_start
  tui_expect "3/3"
  tui_expect alpha
  tui_expect beta
  tui_expect "worktree >"
  tui_expect "ctrl-d: delete"
  tui_send Escape
  tui_wait_exit
  assert_rc 0
  assert_no_docker_changes
}

test_tui_search_narrows_the_list() {
  setup_two_worktrees
  tui_start
  tui_wait "3/3"
  tui_type alpha
  tui_expect "1/3"
  tui_expect_gone "beta"
}

test_tui_enter_opens_the_entry_in_an_unknown_terminal() {
  setup_two_worktrees
  tui_start
  tui_wait "3/3"
  tui_type alpha
  tui_wait "1/3"
  tui_send Enter
  tui_wait_exit
  assert_rc 0
  assert_contains "this terminal has no tab support, open this directory:"
  assert_dir "$WT_A"
}

test_tui_enter_on_the_current_worktree_says_so() {
  setup_two_worktrees
  term_rule tmux 'list-panes *' 0
  TUI_ENV=(TMUX=/fake,1,0)
  cd "$WT_A" || exit 2
  tui_start
  tui_wait "3/3"
  tui_type alpha
  tui_wait "1/3"
  tui_send Enter
  tui_wait_exit
  assert_contains "you are already in"
  assert_not_term_log_like 'new-window'
}

test_tui_ctrl_d_deletes_after_y_and_reloads() {
  setup_two_worktrees
  tui_start
  tui_wait "3/3"
  tui_type alpha
  tui_wait "1/3"
  tui_send C-d
  tui_expect "Continue?"
  tui_expect "Delete 1 worktree:"
  tui_wait_prompt
  tui_send y
  tui_expect "0/2"
  assert_no_dir "$WT_A"
  assert_dir "$WT_B"
}

test_tui_ctrl_d_and_n_changes_nothing_and_returns_to_the_list() {
  setup_two_worktrees
  tui_start
  tui_wait "3/3"
  tui_type alpha
  tui_wait "1/3"
  tui_send C-d
  tui_wait "Continue?"
  tui_send n
  tui_expect "1/3"
  assert_dir "$WT_A"
}

test_tui_ctrl_d_on_main_refuses_and_waits_for_a_key() {
  make_repo
  add_wt alpha
  tui_start
  tui_wait "2/2"
  # the main checkout is the first row
  tui_send C-d
  tui_expect "it is the main checkout"
  tui_expect "Press a key to continue."
  tui_send x
  tui_expect "worktree >"
  assert_dir "$REPO"
}

test_tui_ctrl_f_takes_a_dirty_worktree() {
  setup_two_worktrees
  printf 'edit\n' >>"$WT_A/file.txt"
  tui_start
  tui_wait "3/3"
  tui_type alpha
  tui_wait "1/3"
  tui_send C-f
  tui_expect "FORCE delete 1 worktree:"
  tui_wait_prompt
  tui_send y
  tui_expect "0/2"
  assert_no_dir "$WT_A"
}

test_tui_tab_selects_several_for_one_confirmation() {
  setup_two_worktrees
  tui_start
  tui_wait "3/3"
  tui_type alpha
  tui_wait "1/3"
  tui_send Tab
  tui_expect "(1)"
  tui_send BSpace BSpace BSpace BSpace BSpace
  tui_wait "3/3"
  tui_type beta
  tui_wait "1/3"
  tui_send Tab
  tui_expect "(2)"
  tui_send C-d
  tui_expect "Delete 2 worktrees:"
  tui_wait_prompt
  tui_send y
  tui_expect "0/1"
  assert_no_dir "$WT_A"
  assert_no_dir "$WT_B"
  assert_dir "$REPO"
}

test_tui_ctrl_a_selects_all_and_main_still_stays() {
  setup_two_worktrees
  tui_start
  tui_wait "3/3"
  tui_send C-a
  tui_expect "(3)"
  tui_send C-d
  tui_expect "proj: it is the main checkout"
  tui_expect "Delete 2 worktrees:"
  tui_wait_prompt
  tui_send n
  tui_expect "worktree >"
  assert_dir "$WT_A"
  assert_dir "$WT_B"
  assert_dir "$REPO"
}

test_tui_ctrl_a_takes_only_the_matches() {
  setup_two_worktrees
  tui_start
  tui_wait "3/3"
  tui_type alpha
  tui_wait "1/3"
  tui_send C-a
  tui_expect "(1)"
  tui_send C-d
  tui_expect "Delete 1 worktree:"
  tui_wait_prompt
  tui_send n
  tui_expect "worktree >"
}

test_tui_enter_with_several_selected_confirms_once() {
  setup_two_worktrees
  tui_start
  tui_wait "3/3"
  tui_send C-a
  tui_wait "(3)"
  tui_send Enter
  tui_expect "Open 3 tabs:"
  tui_wait_prompt
  tui_send n
  tui_wait_exit
  assert_rc 0
}

test_tui_tmux_jump_to_an_open_tab() {
  setup_two_worktrees
  term_rule tmux 'list-panes *' 0 "$WT_A\t%9\tmain:@3\n"
  term_rule tmux 'switch-client *' 0
  term_rule tmux 'select-window *' 0
  term_rule tmux 'select-pane *' 0
  TUI_ENV=(TMUX=/fake,1,0 TMUX_PANE=%1)
  tui_start
  tui_wait "3/3"
  tui_type alpha
  tui_wait "1/3"
  tui_send Enter
  tui_wait_exit
  assert_rc 0
  assert_term_log "tmux select-window -t main:@3"
  assert_term_log "tmux select-pane -t %9"
  assert_not_term_log_like 'new-window'
}

test_tui_tmux_skips_its_own_pane() {
  setup_two_worktrees
  term_rule tmux 'list-panes *' 0 "$WT_A\t%1\tmain:@3\n"
  term_rule tmux 'new-window *' 0
  TUI_ENV=(TMUX=/fake,1,0 TMUX_PANE=%1)
  tui_start
  tui_wait "3/3"
  tui_type alpha
  tui_wait "1/3"
  tui_send Enter
  tui_wait_exit
  assert_term_log "tmux new-window -c $WT_A -n alpha"
  assert_not_term_log_like 'select-pane'
}

test_tui_tmux_subdirectory_of_a_nested_worktree_does_not_count() {
  local inner
  setup_two_worktrees
  inner=$WT_A/nested
  must git -C "$REPO" worktree add -q -b inner "$inner"
  term_rule tmux 'list-panes *' 0 "$inner/src\t%9\tmain:@3\n"
  term_rule tmux 'new-window *' 0
  TUI_ENV=(TMUX=/fake,1,0 TMUX_PANE=%1)
  tui_start
  tui_wait "4/4"
  tui_type '!nested alpha'
  tui_wait "1/4"
  tui_send Enter
  tui_wait_exit
  assert_term_log "tmux new-window -c $WT_A -n alpha"
  assert_not_term_log_like 'select-pane'
}

test_tui_rex_jump_to_an_open_tab() {
  setup_two_worktrees
  term_rule rex 'api call session.list' 0 '{"session_id": "s1"}\n'
  term_rule rex 'block ls -s s1 --json --short=false' 0 '{"block_id": "b7", "window_id": "w7"}\n'
  term_rule rex 'api call -s s1 com.superlogical.terminal.process *' 0 "{\"foreground\": {\"cwd\": \"$WT_A\"}}\n"
  term_rule rex 'block focus -s s1 b7' 0
  term_rule rex 'api call client.list *' 0 '{"client_id": "c1"}\n'
  term_rule rex 'api call client.perform *' 0
  TUI_ENV=(TERM_PROGRAM=rex REX_SESSION=s1 REX_BLOCK=b0)
  tui_start
  tui_wait "3/3"
  tui_type alpha
  tui_wait "1/3"
  tui_send Enter
  tui_wait_exit
  assert_rc 0
  assert_term_log "rex block focus -s s1 b7"
  assert_not_term_log_like 'window new'
}

test_tui_rex_skips_its_own_block() {
  setup_two_worktrees
  term_rule rex 'api call session.list' 0 '{"session_id": "s1"}\n'
  term_rule rex 'block ls -s s1 --json --short=false' 0 '{"block_id": "b0", "window_id": "w7"}\n'
  term_rule rex 'window new --cwd *' 0
  TUI_ENV=(TERM_PROGRAM=rex REX_SESSION=s1 REX_BLOCK=b0)
  tui_start
  tui_wait "3/3"
  tui_type alpha
  tui_wait "1/3"
  tui_send Enter
  tui_wait_exit
  assert_term_log "rex window new --cwd $WT_A alpha"
  assert_not_term_log_like 'block focus'
}

test_tui_rex_without_remote_control_says_so() {
  setup_two_worktrees
  term_rule rex 'api call session.list' 0 '{"session_id": "s1"}\n'
  term_rule rex 'block ls -s s1 --json --short=false' 0 '{"block_id": "b7", "window_id": "w7"}\n'
  term_rule rex 'api call -s s1 com.superlogical.terminal.process *' 0 "{\"foreground\": {\"cwd\": \"$WT_A\"}}\n"
  term_rule rex 'block focus -s s1 b7' 0
  term_rule rex 'api call client.list *' 0
  TUI_ENV=(TERM_PROGRAM=rex REX_SESSION=s1 REX_BLOCK=b0)
  tui_start
  tui_wait "3/3"
  tui_type alpha
  tui_wait "1/3"
  tui_send Enter
  tui_wait_exit
  assert_rc 0
  assert_term_log "rex block focus -s s1 b7"
}

test_tui_narrow_enter_opens_the_menu() {
  setup_two_worktrees
  TUI_COLS=50
  tui_start
  tui_wait "3/3"
  tui_type alpha
  tui_wait "1/3"
  tui_send Enter
  tui_expect "alpha >"
  tui_expect "force delete"
  tui_expect "select several"
  tui_expect "back"
}

test_tui_narrow_menu_back_returns_to_the_list() {
  setup_two_worktrees
  TUI_COLS=50
  tui_start
  tui_wait "3/3"
  tui_type alpha
  tui_wait "1/3"
  tui_send Enter
  tui_wait "select several"
  tui_type back
  tui_wait "> back"
  tui_send Enter
  tui_expect "worktree >"
  tui_expect "3/3"
  assert_dir "$WT_A"
}

test_tui_narrow_menu_delete_then_y() {
  setup_two_worktrees
  TUI_COLS=50
  tui_start
  tui_wait "3/3"
  tui_type alpha
  tui_wait "1/3"
  tui_send Enter
  tui_wait "select several"
  tui_send Down
  tui_send Enter
  tui_expect "Delete 1 worktree:"
  tui_wait_prompt
  tui_send y
  tui_expect "2/2"
  assert_no_dir "$WT_A"
}

test_tui_narrow_tap_opens_the_menu_and_tap_picks_an_action() {
  setup_two_worktrees
  TUI_COLS=50
  tui_start
  tui_wait "3/3"
  tui_click_row "beta"
  tui_expect "force delete"
  tui_expect "beta >"
  tui_click_row "back"
  tui_expect "worktree >"
  assert_dir "$WT_B"
}

test_tui_narrow_select_several_by_tap() {
  setup_two_worktrees
  TUI_COLS=50
  tui_start
  tui_wait "3/3"
  tui_click_row "beta"
  tui_wait "select several"
  tui_click_row "select several"
  tui_expect "tap: select"
  tui_wait "3/3"
  tui_click_row "alpha"
  tui_expect "(1)"
  tui_click_row "beta"
  tui_expect "(2)"
  tui_send Enter
  tui_expect "2 worktrees >"
  tui_expect "force delete"
  tui_click_row "back"
  tui_expect "worktree >"
  assert_dir "$WT_A"
  assert_dir "$WT_B"
}

test_tui_stack_list_loads_and_esc_exits_zero() {
  setup_two_stacks
  tui_start -d
  tui_expect "stack >"
  tui_expect "zulu"
  tui_expect "yankee"
  tui_expect "2/2"
  tui_expect "Docker  CPU"
  tui_send Escape
  tui_wait_exit
  assert_rc 0
  assert_no_docker_changes
}

test_tui_stack_header_rows_cannot_be_selected() {
  setup_two_stacks
  tui_start -d
  tui_wait "2/2"
  tui_send C-a
  tui_expect "(2)"
  tui_send C-d
  tui_expect "of 2 stacks. The volumes stay:"
  tui_wait_prompt
  tui_send n
  tui_expect "stack >"
  assert_no_docker_changes
}

test_tui_stack_ctrl_s_stops_the_current_stack() {
  setup_two_stacks
  tui_start -d
  tui_wait "2/2"
  tui_type zulu
  tui_wait "1/2"
  tui_send C-s
  tui_expect "exited 1"
  assert_log "compose -p zulu stop"
  assert_not_log "compose -p yankee stop"
}

test_tui_stack_ctrl_d_keeps_the_volumes() {
  setup_two_stacks
  tui_start -d
  tui_wait "2/2"
  tui_type zulu
  tui_wait "1/2"
  tui_send C-d
  tui_expect "The volumes stay:"
  tui_wait_prompt
  tui_send y
  tui_expect "0/1"
  assert_log "compose -p zulu down"
  assert_not_log "volume rm zulu_data"
}

test_tui_stack_ctrl_f_on_one_linked_stack_takes_one_key() {
  setup_two_stacks
  tui_start -d
  tui_wait "2/2"
  tui_type zulu
  tui_wait "1/2"
  tui_send C-f
  tui_expect "Delete 1 stack and 1 volume:"
  tui_wait_prompt
  tui_send y
  tui_expect "0/1"
  assert_log "volume rm zulu_data"
  assert_not_log "volume rm yankee_data"
}

test_tui_stack_ctrl_a_ctrl_f_needs_the_typed_phrase() {
  setup_two_stacks
  tui_start -d
  tui_wait "2/2"
  tui_send C-a
  tui_wait "(2)"
  tui_send C-f
  tui_expect 'Type "2 stacks" to delete'
  tui_type y
  tui_send Enter
  tui_expect "stack >"
  assert_no_docker_changes
}

test_tui_stack_typed_phrase_deletes() {
  setup_two_stacks
  tui_start -d
  tui_wait "2/2"
  tui_send C-a
  tui_wait "(2)"
  tui_send C-f
  tui_wait 'Type "2 stacks" to delete'
  tui_type "2 stacks"
  tui_send Enter
  tui_expect "0/0"
  assert_log "volume rm zulu_data"
  assert_log "volume rm yankee_data"
  assert_dir "$WT_A"
}

test_tui_stack_narrow_enter_opens_the_menu() {
  setup_two_stacks
  TUI_COLS=50
  tui_start -d
  tui_wait "2/2"
  tui_type zulu
  tui_wait "1/2"
  tui_send Enter
  tui_expect "zulu >"
  tui_expect "delete, keep volumes"
  tui_expect "delete with volumes"
  tui_expect "select several"
  tui_type stop
  tui_wait "> stop"
  tui_send Enter
  tui_expect "stack >"
  wait_log_count '^compose -p zulu stop$' 1
}

test_tui_stack_enter_with_no_directory_is_an_error_that_ends_the_list() {
  make_repo
  volume proj-vol proj-vol_data
  tui_start -d
  tui_wait "1/1"
  tui_send Enter
  tui_wait_exit
  assert_rc 1
  assert_contains "the selection has no directory"
  assert_no_docker_changes
}

test_tui_docker_mode_starts_a_disk_refresh() {
  setup_two_stacks
  tui_start -d
  tui_wait "2/2"
  wait_file "$XDG_CACHE_HOME/wtree/disk-totals"
}

test_tui_global_list() {
  setup_two_worktrees
  make_repo other
  cd "$T" || exit 2
  tui_start -g
  tui_expect "all worktrees >"
  tui_expect "4/4"
  tui_send Escape
  tui_wait_exit
  assert_rc 0
}

test_tui_global_stack_list() {
  setup_two_stacks
  cd "$T" || exit 2
  tui_start -g -d
  tui_expect "all stacks >"
  tui_expect "2/2"
  tui_send Escape
  tui_wait_exit
  assert_rc 0
}
