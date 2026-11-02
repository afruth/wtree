# How wtree chooses between wtree-tui and fzf. A small fake stands in for the program in most cases.
# shellcheck shell=bash

# a copy of the script, with a fake program beside it. FAKE_CHECK is the exit code of --check.
fake_beside() {
  mkdir -p "$T/side"
  cp "$WTREE" "$T/side/wtree"
  WTREE=$T/side/wtree
  fake_tui "$T/side/wtree-tui"
}

fake_tui() {
  cat >"$1" <<FAKE
#!/bin/sh
case "\${1:-}" in --check) printf '%s\n' "\$WTREE_SCRIPT" >"$T/tui.checked"; exit \${FAKE_CHECK:-0} ;; esac
printf 'args=[%s] script=[%s] global=[%s]\n' "\$*" "\$WTREE_SCRIPT" "\${WTREE_GLOBAL:-}" >"$T/tui.tmp"
mv "$T/tui.tmp" "$T/tui.ran"
FAKE
  chmod +x "$1"
}

expect_tui() {
  wait_file "$T/tui.ran"
  assert_eq "$(cat "$T/tui.ran")" "$1"
}

expect_no_tui() {
  [[ ! -e $T/tui.ran ]] || fail "the program ran: $(cat "$T/tui.ran")"
}

test_chooser_terminal_and_beside_starts_the_program() {
  make_repo
  fake_beside
  TUI_ENV=(WTREE_UI=)
  tui_start
  expect_tui "args=[] script=[$WTREE] global=[]"
}

test_chooser_passes_global_and_docker_and_rex() {
  make_repo
  fake_beside
  TUI_ENV=(WTREE_UI=)
  tui_start --rex -g -d
  expect_tui "args=[-g -d --rex] script=[$WTREE] global=[]"
}

test_chooser_passes_gd_and_runs_outside_a_repo() {
  fake_beside
  TUI_ENV=(WTREE_UI=)
  tui_start -gd
  expect_tui "args=[-g -d] script=[$WTREE] global=[]"
}

test_chooser_finds_the_program_on_path() {
  make_repo
  mkdir -p "$T/onpath"
  fake_tui "$T/onpath/wtree-tui"
  TUI_ENV=(WTREE_UI= "PATH=$T/onpath:$PATH")
  tui_start -d
  expect_tui "args=[-d] script=[$WTREE] global=[]"
}

test_chooser_follows_a_symlink_to_the_script() {
  make_repo
  fake_beside
  mkdir -p "$T/link"
  ln -s "$WTREE" "$T/link/wtree"
  local real=$WTREE
  WTREE=$T/link/wtree
  TUI_ENV=(WTREE_UI=)
  tui_start
  expect_tui "args=[] script=[$real] global=[]"
}

test_chooser_no_terminal_uses_fzf() {
  make_repo
  fake_beside
  export WTREE_UI=
  WT_TIMEOUT=5 wt
  expect_no_tui
}

test_chooser_tui_flag_needs_no_terminal() {
  make_repo
  fake_beside
  export WTREE_UI=fzf
  wt --tui
  expect_tui "args=[] script=[$WTREE] global=[]"
}

test_chooser_env_tui_needs_no_terminal() {
  make_repo
  fake_beside
  export WTREE_UI=tui
  wt -d
  expect_tui "args=[-d] script=[$WTREE] global=[]"
}

test_chooser_fzf_flag_beats_the_env_and_the_program() {
  make_repo
  fake_beside
  TUI_ENV=(WTREE_UI=tui)
  tui_start --fzf
  tui_expect "worktree >"
  expect_no_tui
}

test_chooser_env_fzf_beats_the_program() {
  make_repo
  fake_beside
  TUI_ENV=(WTREE_UI=fzf)
  tui_start
  tui_expect "worktree >"
  expect_no_tui
}

test_chooser_no_program_uses_fzf_and_says_nothing() {
  make_repo
  TUI_ENV=(WTREE_UI=)
  tui_start
  tui_expect "worktree >"
  [[ $(tui_text) != *"does not match"* ]] || fail "a note with no program"
}

test_chooser_tui_flag_with_no_program_stops() {
  make_repo
  wt --tui
  assert_rc 1
  assert_contains "wtree-tui is not beside this script or on PATH"
}

test_chooser_bad_ui_value_stops() {
  make_repo
  export WTREE_UI=curses
  wt
  assert_rc 1
  assert_contains "WTREE_UI is curses"
}

test_chooser_mismatch_falls_back_to_fzf_with_one_line() {
  make_repo
  fake_beside
  export FAKE_CHECK=2 WTREE_UI=tui
  wt_keys '\e' 2>/dev/null
  expect_no_tui
  assert_contains "does not match this script, so wtree uses fzf"
  [[ $(grep -c 'does not match' <<<"$OUT") == 1 ]] || fail "more than one note"
}

test_chooser_a_file_that_cannot_run_falls_back() {
  make_repo
  fake_beside
  printf 'not a program' >"$T/side/wtree-tui"
  export WTREE_UI=tui
  wt_keys '\e'
  expect_no_tui
  assert_contains "does not match this script"
}

test_chooser_new_never_starts_the_program() {
  make_repo
  fake_beside
  export WTREE_UI=tui
  wt -n
  expect_no_tui
  assert_rc 0
}

test_chooser_list_and_core_never_start_the_program() {
  make_repo
  fake_beside
  export WTREE_UI=tui
  wt --list
  assert_rc 0
  wt core version
  assert_rc 0
  assert_contains '"contract"'
  expect_no_tui
}

test_chooser_rex_flag_is_quiet_with_fzf() {
  make_repo
  TUI_ENV=(WTREE_UI=fzf)
  tui_start --rex
  tui_expect "worktree >"
}

test_chooser_outside_a_repo_starts_the_program_and_it_takes_the_machine() {
  fake_beside
  export WTREE_UI=tui
  wt
  assert_rc 0
  expect_tui "args=[] script=[$WTREE] global=[]"
}

test_chooser_outside_a_repo_stops_with_fzf() {
  fake_beside
  export WTREE_UI=fzf
  wt
  assert_rc 1
  assert_contains "not a git repository"
  expect_no_tui
}

test_chooser_new_outside_a_repo_stops() {
  fake_beside
  export WTREE_UI=tui
  wt -n
  assert_rc 1
  assert_contains "not a git repository"
  expect_no_tui
}

test_missing_fzf_and_no_program_names_both_ways() {
  make_repo
  mkdir -p "$T/nofzf"
  local tool
  for tool in "$WTREE_TEST_SCRATCH"/tools/*; do
    [[ ${tool##*/} == fzf ]] || ln -s "$tool" "$T/nofzf/${tool##*/}"
  done
  export PATH=$T/nofzf WTREE_UI=
  wt
  assert_rc 1
  assert_contains "fzf is not on PATH"
  assert_contains "wtree-tui"
}

# ---- the real program

need_real_tui() {
  [[ -n ${WTREE_TEST_TUI:-} && -x $WTREE_TEST_TUI ]] && return 0
  printf 'wtree-tui is not available: %s\n' "${WTREE_TEST_TUI_NOTE:-no binary}"
  exit 77
}

test_chooser_real_program_opens_the_tree() {
  need_real_tui
  make_repo
  add_wt alpha
  stack zulu "$WT" running
  volume zulu zulu_data
  TUI_ENV=(WTREE_UI= "WTREE_TUI=$WTREE_TEST_TUI")
  tui_start
  tui_expect alpha
  [[ $(tui_text) != *"sort: name"* ]] || fail "a plain start has a sort"
  [[ $(tui_text) != *"worktree >"* ]] || fail "this is fzf"
  tui_send q
  tui_wait_exit
  assert_rc 0
  assert_no_docker_changes
}

test_chooser_real_program_d_has_stacks_in_focus_for_one_run() {
  need_real_tui
  make_repo
  add_wt alpha
  stack zulu "$WT" running
  volume zulu zulu_data
  TUI_ENV=(WTREE_UI= "WTREE_TUI=$WTREE_TEST_TUI")
  tui_start -d
  tui_expect "sort: name"
  tui_expect zulu
  # a change of the panel writes ui.json, and the sort of this run is never in it: so a program that
  # dies before its end leaves nothing for the next plain wtree either
  tui_send p
  local ui=$HOME/.local/state/wtree/ui.json tries=0
  until grep -qs '"panel"' "$ui"; do
    tries=$((tries + 1))
    ((tries < 100)) || fail "the panel key wrote nothing to ui.json"
    sleep 0.05
  done
  if grep -q '"sort"' "$ui"; then fail "ui.json has the sort of -d: $(cat "$ui")"; fi
  tui_send q
  tui_wait_exit
  assert_rc 0
  if grep -q '"sort"' "$ui"; then fail "ui.json keeps the sort of -d: $(cat "$ui")"; fi
  assert_no_docker_changes
}

test_chooser_real_program_refuses_a_script_with_another_contract() {
  need_real_tui
  make_repo
  mkdir -p "$T/newer"
  sed 's/^CORE_CONTRACT=.*/CORE_CONTRACT=99/' "$WTREE" >"$T/newer/wtree"
  chmod +x "$T/newer/wtree"
  grep -q '^CORE_CONTRACT=99' "$T/newer/wtree" || broken "no contract line in the script"
  WTREE=$T/newer/wtree
  TUI_ENV=(WTREE_UI= "WTREE_TUI=$WTREE_TEST_TUI")
  tui_start
  tui_expect "worktree >"
  assert_no_docker_changes
}
