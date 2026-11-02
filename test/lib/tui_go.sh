# Helpers for the Bubble Tea interface: the real wtree-tui and the real bin/wtree, behind the docker
# shim, in the private tmux of the harness. See cases/70_tui_go.sh.
# shellcheck shell=bash

TUI_GO=${WTREE_TEST_TUI:-}

tg_require() {
  WTREE_FAIL_TAIL=50
  TG_SHEET_WAIT=1
  [[ -n $TUI_GO && -x $TUI_GO ]] && return 0
  printf 'wtree-tui is not available: %s\n' "${WTREE_TEST_TUI_NOTE:-no binary}"
  exit 77
}

# The script that wtree-tui runs: it logs each call with its arguments, and then it is bin/wtree.
# exec keeps the pid, so that a TERM to a core apply still reaches the script itself.
tg_write_logger() {
  cat >"$T/wtree-logged" <<LOGGER
#!$(command -v bash)
printf '%s\\n' "\$*" >>$(printf '%q' "$T/core.log")
exec $(printf '%q' "$WTREE") "\$@"
LOGGER
  chmod +x "$T/wtree-logged"
  : >"$T/core.log"
}

# args: the size, then the flags of wtree-tui. TG_ENV (VAR=value ...) changes the pane. After the
# program ends, the pane keeps a plain shell, as a terminal does: a test types in it.
tg_start_in() {
  local session=$1 cols=$2 rows=$3 arg args='' env_line=''
  shift 3
  tg_require
  [[ -x $T/wtree-logged ]] || tg_write_logger
  for arg in "$@"; do args+=" $(printf '%q' "$arg")"; done
  for arg in "${TG_ENV[@]:-}"; do
    if [[ -n $arg ]]; then env_line+="export $(printf '%q' "$arg")"$'\n'; fi
  done
  cat >"$T/$session.sh" <<PANE
unset TMUX TMUX_PANE TERM_PROGRAM TERM_PROGRAM_VERSION
export TERM=tmux-256color WTREE_SCRIPT=$(printf '%q' "$T/wtree-logged") PS1='shell> '
$env_line
cd $(printf '%q' "$PWD") || exit 1
bash -c 'printf "%s\\n" "\$\$" >"\$0"; exec "\$@"' $(printf '%q' "$T/$session.pid") $(printf '%q' "$TUI_GO")$args
printf '%s\\n' "\$?" >$(printf '%q' "$T/$session.rc")
stty -a >$(printf '%q' "$T/$session.stty") 2>&1
exec bash --norc --noprofile
PANE
  if tmux_cmd has-session 2>/dev/null; then
    tmux_cmd new-session -d -s "$session" -x "$cols" -y "$rows" "bash $T/$session.sh" >/dev/null || broken "tmux did not start"
  else
    tmux_cmd new-session -d -s "$session" -x "$cols" -y "$rows" "bash $T/$session.sh" \; set-option -s escape-time 0 >/dev/null || broken "tmux did not start"
  fi
}

# the session is named t, as in the helpers of the harness
tg_start() {
  tg_start_in t "${TUI_COLS:-120}" "${TUI_ROWS:-40}" "$@"
}

tg_pid() {
  wait_file "$T/t.pid"
  cat "$T/t.pid"
}

# the first line of the list is there, and the stats have come, so the core list is near its end
tg_loaded() {
  local tries=0 screen
  tui_wait "${1:-proj}"
  while :; do
    screen=$(tui_text)
    # a phone has no room for the age: there the header says loading until the list has ended
    [[ $screen =~ live\ [0-9] || $screen != *loading* ]] && return 0
    tries=$((tries + 1))
    if ((tries > 200)); then
      printf 'BROKEN SETUP: the list never loaded\n--- screen\n%s\n' "$screen"
      exit 2
    fi
    sleep 0.05
  done
}

# the disk cache is there and no scan runs, so that two starts in a row read the same sizes
tg_scan_disk() {
  must "$WTREE" core list --cwd "$PWD"
  local tries=0
  until [[ ! -d $XDG_CACHE_HOME/wtree/lock ]] && compgen -G "$XDG_CACHE_HOME/wtree/*" >/dev/null; do
    tries=$((tries + 1))
    ((tries < 100)) || broken "the scan of the disk never ended"
    sleep 0.05
  done
}

# presses the digit that the sheet prints in front of this label
tg_pick() {
  local digit
  digit=$(tui_text | grep -oE "\([0-9]\) $1" | head -n 1 | cut -c2)
  if [[ -z $digit ]]; then
    OUT=$(tui_text)
    fail "no choice [$1] on the sheet"
  fi
  tui_send "$digit"
}

tg_wait_exit() {
  local tries=0
  until [[ -s $T/t.rc ]]; do
    tries=$((tries + 1))
    if ((tries > 200)); then
      OUT=$(tui_text)
      fail "wtree-tui did not exit"
    fi
    sleep 0.05
  done
  RC=$(cat "$T/t.rc")
  OUT=$(tui_text)
}

tg_flag() {
  tmux_cmd display-message -p -t t "#{$1}"
}

# ---- what a call did

tg_core_log() {
  cat "$T/core.log"
}

tg_core_count() {
  grep -c -- "$1" "$T/core.log"
}

assert_core_log() {
  if ! grep -Fq -- "$1" "$T/core.log"; then
    OUT=$(tg_core_log)
    fail "wtree core was not called with: $1"
  fi
}

assert_no_core_log() {
  if grep -Fq -- "$1" "$T/core.log"; then
    OUT=$(tg_core_log)
    fail "wtree core was called with: $1"
  fi
}

assert_core_count() {
  local count
  count=$(tg_core_count "$1")
  if [[ $count != "$2" ]]; then
    OUT=$(tg_core_log)
    fail "expected $2 calls of wtree core with [$1], got $count"
  fi
}

# the calls that change the engine, in their order, each with its exact words
tg_docker_changes() {
  grep -E '^(compose|volume rm|rm|stop|kill|run|exec|system prune|volume prune|container|network)( |$)' "$WTREE_SHIM_DIR/docker.log" || true
}

assert_docker_changes() {
  local want='' line got
  for line in "$@"; do want+=$line$'\n'; done
  want=${want%$'\n'}
  got=$(tg_docker_changes)
  if [[ $got != "$want" ]]; then
    OUT=$(cat "$WTREE_SHIM_DIR/docker.log")
    fail "the docker calls that change state are [$got], and they must be exactly [$want]"
  fi
}

# ---- the screen

# the lines that have reverse video: the cursor row, a button in focus
tg_reverse_lines() {
  tmux_cmd capture-pane -e -p -t t 2>/dev/null | grep -aE $'\x1b\\[([0-9;:]*;)?7(;[0-9;:]*)?m' | strip_ansi | grep -v '^ wtree ' | sed 's/│.*//'
}

tg_cursor_on() {
  tg_reverse_lines | grep -Fq -- "$1"
}

tg_expect_cursor() {
  local tries=0
  until tg_cursor_on "$1"; do
    tries=$((tries + 1))
    if ((tries > 100)); then
      OUT=$(tui_text)
      fail "the cursor is not on [$1]: $(tg_reverse_lines)"
    fi
    sleep 0.05
  done
}

# the cursor moves with plain keys only, one row at a time: down to the end, and then up
tg_move_to() {
  local tries before key=j
  tg_cursor_on "$1" && return 0
  for ((tries = 0; tries < 60; tries++)); do
    before=$(tg_reverse_lines)
    tui_send "$key"
    tg_settled_from "$before"
    tg_cursor_on "$1" && return 0
    if [[ $(tg_reverse_lines) == "$before" ]]; then key=k; fi
  done
  OUT=$(tui_text)
  broken "the cursor never came to [$1]"
}

tg_settled_from() {
  local tries=0
  while [[ $(tg_reverse_lines) == "$1" ]] && ((tries < 20)); do
    tries=$((tries + 1))
    sleep 0.02
  done
}

# the screen by cells: tmux gives back a tab that the renderer used, and a cell count needs spaces
tg_text() {
  tui_text | python3 -I -c 'import sys; sys.stdout.write(sys.stdin.read().expandtabs(8))'
}

# ---- touch: SGR mouse reports on the wire, with cells from 1

tg_send_seq() {
  local hex
  hex=$(printf '%b' "$1" | od -An -tx1 | tr -d '\n')
  # shellcheck disable=SC2086
  tmux_cmd send-keys -t t -H $hex
}

tg_press() {
  tg_send_seq "\033[<0;$1;$2M"
}

tg_tap() {
  sleep 0.35 # a person has read the sheet: what confirms waits for a sheet that just came
  tg_send_seq "\033[<0;$1;$2M\033[<0;$1;$2m"
}

# sets TG_COL and TG_ROW to the cell of the text, from 1
tg_find() {
  local line row=0 before
  while IFS= read -r line; do
    row=$((row + 1))
    if [[ $line == *"$1"* ]]; then
      before=${line%%"$1"*}
      TG_COL=$((${#before} + 1))
      TG_ROW=$row
      return 0
    fi
  done < <(tg_text)
  OUT=$(tui_text)
  fail "no [$1] on the screen"
}

# the same from the last line up: the action bar, and not the panel that has the same words
tg_find_last() {
  local line row=0 before found=''
  while IFS= read -r line; do
    row=$((row + 1))
    if [[ $line == *"$1"* ]]; then
      before=${line%%"$1"*}
      TG_COL=$((${#before} + 1))
      TG_ROW=$row
      found=1
    fi
  done < <(tg_text)
  [[ -n $found ]] && return 0
  OUT=$(tui_text)
  fail "no [$1] on the screen"
}

tg_tap_bar() {
  tg_find_last "$1"
  tg_tap "$((TG_COL + 1))" "$TG_ROW"
}

tg_tap_text() {
  tg_find "$1"
  tg_tap "$((TG_COL + ${2:-0}))" "$TG_ROW"
}

# ---- fixtures

# a repo with two worktrees, each with its own commit: a worktree with none is merged, and it is in
# the cleanup group. Sets WT_A and WT_B
tg_repo() {
  make_repo
  add_wt alpha
  WT_A=$WT
  commit_in "$WT_A"
  add_wt beta
  WT_B=$WT
  commit_in "$WT_B"
}

# each worktree has a running stack with a volume, and the worktree of the cursor stays the main one
tg_repo_with_stacks() {
  tg_repo
  stack proj-alpha "$WT_A" running
  stack proj-beta "$WT_B" running
  volume proj-alpha proj-alpha_data
  volume proj-beta proj-beta_data
}

# the program in a pty of its own inside the pane, with each byte in raw.log
tg_start_script() {
  local arg args=''
  tg_require
  [[ -x $T/wtree-logged ]] || tg_write_logger
  for arg in "$@"; do args+=" $(printf '%q' "$arg")"; done
  cat >"$T/inner.sh" <<INNER
printf '%s\\n' "\$\$" >$(printf '%q' "$T/t.pid")
exec $(printf '%q' "$TUI_GO")$args
INNER
  cat >"$T/t.sh" <<PANE
unset TMUX TMUX_PANE TERM_PROGRAM TERM_PROGRAM_VERSION
export TERM=tmux-256color WTREE_SCRIPT=$(printf '%q' "$T/wtree-logged")
cd $(printf '%q' "$PWD") || exit 1
script -qfec "bash $(printf '%q' "$T/inner.sh")" $(printf '%q' "$T/raw.log")
printf '%s\\n' "\$?" >$(printf '%q' "$T/t.rc")
sleep 300
PANE
  tmux_cmd new-session -d -s t -x "${TUI_COLS:-120}" -y "${TUI_ROWS:-40}" "bash $T/t.sh" \; set-option -s escape-time 0 >/dev/null || broken "tmux did not start"
}
