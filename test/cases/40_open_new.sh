# wtree -n, the terminal adapters behind it, and the command line.
# shellcheck shell=bash

new_worktree_path() {
  printf '%s\n' "$OUT" | grep -m 1 "^$HOME/code/worktrees/"
}

test_new_creates_a_detached_worktree_at_head() {
  make_repo
  commit_in "$REPO"
  wt -n
  assert_rc 0
  local path
  path=$(new_worktree_path)
  [[ -n $path ]] || fail "no path on stdout"
  assert_dir "$path"
  assert_registered "$path"
  assert_eq "$(git -C "$path" rev-parse HEAD)" "$(git -C "$REPO" rev-parse HEAD)"
  if git -C "$path" symbolic-ref -q HEAD >/dev/null; then fail "HEAD is not detached"; fi
}

test_new_from_a_worktree_goes_beside_the_main_checkout() {
  make_repo
  add_wt alpha
  mkdir -p "$WT/sub"
  cd "$WT/sub" || exit 2
  wt -n
  assert_rc 0
  [[ -n $(new_worktree_path) ]] || fail "the worktree is not in ../worktrees of the main checkout"
}

test_new_gives_each_worktree_its_own_name() {
  local seen=' ' path i
  make_repo
  for i in 1 2 3 4 5; do
    wt -n
    path=$(new_worktree_path)
    [[ $seen != *" $path "* ]] || fail "the name came twice: $path"
    seen+="$path "
  done
}

test_new_in_an_unknown_terminal_prints_the_path() {
  make_repo
  wt -n
  assert_rc 0
  assert_contains "this terminal has no tab support, open this directory: $HOME/code/worktrees/"
}

test_new_is_refused_with_g() {
  make_repo
  wt -g -n
  assert_rc 1
  assert_contains "does not work with -g"
  assert_eq "$(git -C "$REPO" worktree list --porcelain | grep -c '^worktree ')" 1
}

test_new_outside_a_repo_is_an_error() {
  mkdir -p "$HOME/plain"
  cd "$HOME/plain" || exit 2
  wt -n
  assert_rc 1
  assert_contains "not a git repository"
}

# args: a glob for the call that must open the tab
expect_new_tab() {
  make_repo
  wt -n
  assert_rc 0
  assert_not_contains "no tab support"
  grep -q -- "$1" "$WTREE_SHIM_DIR/term.log" || fail "no call matches [$1]"
}

rex_rules() {
  term_rule rex 'api call session.list' 0 '{"session_id": "s1"}\n'
  term_rule rex 'block ls -s s1 --json --short=false' 0
  term_rule rex 'window new --cwd *' 0
}

tmux_rules() {
  term_rule tmux 'list-panes *' 0
  term_rule tmux 'new-window *' 0
}

zellij_rules() {
  term_rule zellij 'action list-panes --json' 0
  term_rule zellij 'action new-tab *' 0
}

herdr_rules() {
  term_rule herdr 'pane list' 0
  term_rule herdr 'tab create *' 0
}

test_terminal_rex_opens_a_window() {
  rex_rules
  export TERM_PROGRAM=rex REX_SESSION=s1 REX_BLOCK=b0
  expect_new_tab '^rex window new --cwd .*/code/worktrees/'
}

test_terminal_rex_found_by_its_session_alone() {
  rex_rules
  export REX_SESSION=s1
  expect_new_tab '^rex window new'
}

test_terminal_rex_comes_before_a_tmux_variable() {
  rex_rules
  export TERM_PROGRAM=rex REX_SESSION=s1 TMUX=/fake,1,0
  expect_new_tab '^rex window new'
  assert_not_term_log_like 'tmux'
}

test_terminal_tmux_program_comes_before_a_rex_session() {
  tmux_rules
  export TERM_PROGRAM=tmux REX_SESSION=s1 TMUX=/fake,1,0
  expect_new_tab '^tmux new-window -c .*/code/worktrees/'
  assert_not_term_log_like 'rex '
}

test_terminal_tmux_comes_before_the_outer_terminal() {
  tmux_rules
  export TMUX=/fake,1,0 TERM_PROGRAM=ghostty KITTY_WINDOW_ID=1
  expect_new_tab '^tmux new-window'
  assert_not_term_log_like 'kitten'
  assert_not_term_log_like 'osascript'
}

test_terminal_zellij_comes_before_tmux() {
  zellij_rules
  export ZELLIJ=0 TMUX=/fake,1,0
  expect_new_tab '^zellij action new-tab --cwd .*--name '
  assert_not_term_log_like 'tmux'
}

test_terminal_herdr_comes_first() {
  herdr_rules
  export TERM_PROGRAM=herdr HERDR_WORKSPACE_ID=w1 ZELLIJ=0
  expect_new_tab '^herdr tab create --workspace w1 --cwd .*--focus'
}

test_terminal_cmux() {
  term_rule cmux 'list-workspaces' 0
  term_rule cmux 'new-workspace *' 0
  export CMUX_WORKSPACE_ID=w1
  expect_new_tab '^cmux new-workspace --cwd '
}

test_terminal_wezterm() {
  term_rule wezterm 'cli list --format json' 0
  term_rule wezterm 'cli spawn *' 0
  export WEZTERM_PANE=1
  expect_new_tab '^wezterm cli spawn --cwd '
}

test_terminal_kitty() {
  term_rule kitten '@ focus-window *' 1
  term_rule kitten '@ launch *' 0
  export KITTY_WINDOW_ID=1
  expect_new_tab '^kitten @ launch --type=tab --cwd='
}

test_terminal_ghostty_uses_osascript() {
  term_rule osascript '- *' 0
  export TERM_PROGRAM=ghostty
  expect_new_tab '^osascript - .*/code/worktrees/'
}

test_terminal_iterm_uses_osascript() {
  term_rule osascript '*' 0
  export TERM_PROGRAM=iTerm.app
  expect_new_tab '^osascript - .*/code/worktrees/'
}

test_terminal_apple_terminal_uses_osascript() {
  term_rule osascript '*' 0
  export TERM_PROGRAM=Apple_Terminal
  expect_new_tab '^osascript - .*/code/worktrees/'
}

test_failed_tab_prints_the_path_and_succeeds() {
  term_rule tmux 'list-panes *' 0
  term_rule tmux 'new-window *' 1
  export TMUX=/fake,1,0
  make_repo
  wt -n
  assert_rc 0
  assert_contains "could not open a tab in tmux, open this directory: $HOME/code/worktrees/"
}

test_kitty_without_remote_control_explains_the_setting() {
  term_rule kitten '@ focus-window *' 1
  term_rule kitten '@ launch *' 1
  export KITTY_WINDOW_ID=1
  make_repo
  wt -n
  assert_rc 0
  assert_contains "set allow_remote_control in kitty.conf"
}

test_help_exits_zero_and_names_the_keys() {
  wt -h
  assert_rc 0
  assert_contains "usage: wtree"
  assert_contains "ctrl-d"
  # the keys of the full screen come first, and fzf is not a need when the program is there
  assert_contains "In wtree-tui all keys are plain"
  assert_contains "Needs git, and fzf only without wtree-tui"
  assert_not_contains "Needs git and fzf"
  wt --help
  assert_rc 0
}

test_cli_rejects_unknown_arguments() {
  make_repo
  wt --nonsense
  assert_rc 1
  assert_contains "unknown argument: --nonsense"
  assert_contains "usage: wtree"
  wt --list extra
  assert_rc 1
  wt -d extra
  assert_rc 1
}

test_cli_rejects_n_with_d() {
  make_repo
  wt -n -d
  assert_rc 1
  assert_contains "use one of -n and -d"
}

test_cli_needs_a_pair_for_each_entry() {
  make_repo
  wt --delete only-one
  assert_rc 1
  assert_contains "needs a repo for each path"
  wt --force-delete a b c
  assert_rc 1
  wt --stack-delete only-one
  assert_rc 1
  assert_contains "needs a directory for each stack"
  wt --stack-purge a b c
  assert_rc 1
  wt --stack-stop
  assert_rc 1
  assert_contains "needs a stack"
  assert_no_docker_changes
}

test_cli_joins_g_and_d_in_all_spellings() {
  local spelling
  make_repo
  shim_fail 'info'
  for spelling in '-g -d' '-gd' '-dg' '--global --docker'; do
    # shellcheck disable=SC2086
    wt $spelling
    assert_rc 1
    assert_contains "Docker is not running"
  done
}

test_docker_mode_needs_docker_on_path() {
  make_repo
  export PATH=$WTREE_TEST_SCRATCH/tools
  wt -d
  assert_rc 1
  assert_contains "docker is not on PATH"
}

test_docker_mode_says_when_docker_is_not_running() {
  make_repo
  shim_fail 'info' 'Cannot connect to the Docker daemon'
  wt -d
  assert_rc 1
  assert_contains "Docker is not running"
  assert_no_docker_changes
}

test_old_fzf_is_refused() {
  make_repo
  mkdir -p "$T/oldfzf"
  printf '#!/bin/sh\necho "0.44.1 (debian)"\n' >"$T/oldfzf/fzf"
  chmod +x "$T/oldfzf/fzf"
  export PATH=$T/oldfzf:$PATH
  wt
  assert_rc 1
  assert_contains "wtree needs fzf 0.53 or later, this is fzf 0.44.1"
}

test_missing_fzf_is_refused() {
  make_repo
  mkdir -p "$T/nofzf"
  local tool
  for tool in "$WTREE_TEST_SCRATCH"/tools/*; do
    [[ ${tool##*/} == fzf ]] || ln -s "$tool" "$T/nofzf/${tool##*/}"
  done
  export PATH=$T/nofzf
  wt
  assert_rc 1
  assert_contains "fzf is not on PATH"
}
