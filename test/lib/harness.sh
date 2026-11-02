# Helpers for the tests. Each test runs in its own process, with its own scratch and HOME.
# shellcheck shell=bash

WTREE=${WTREE_TEST_BIN:-}
RUN_SH=$TEST_ROOT/run.sh
OUT=
RC=0

harness_setup() {
  T=$(mktemp -d "$WTREE_TEST_SCRATCH/t.XXXXXXXX") || exit 2
  export HOME=$T/home XDG_CACHE_HOME=$T/cache WTREE_ROOTS=$T/home WTREE_SHIM_DIR=$T/shim
  export GIT_CEILING_DIRECTORIES=$T
  mkdir -p "$HOME" "$XDG_CACHE_HOME" "$WTREE_SHIM_DIR"
  : >"$WTREE_SHIM_DIR/containers"
  : >"$WTREE_SHIM_DIR/volumes"
  : >"$WTREE_SHIM_DIR/docker.log"
  : >"$WTREE_SHIM_DIR/term.log"
  TUI_SOCKET=wtree-$$
  export T TUI_SOCKET
  cd "$T" || exit 2
}

harness_teardown() {
  tui_stop
  cd / || true
  rm -rf "$T" 2>/dev/null || true
}

harness_saw_refusal() {
  [[ -s $WTREE_SHIM_DIR/refused && ! -e $T/allow-refusals ]] || return 1
  printf 'the shim refused calls that no test expects:\n' >&2
  cat "$WTREE_SHIM_DIR/refused" >&2
}

allow_refusals() {
  : >"$T/allow-refusals"
}

# ---- assertions: exit 1 is a failed assertion, exit 2 is a broken setup

# the verdict comes last, so that a long output cannot push it out of the summary
fail() {
  printf -- '--- output\n'
  printf '%s\n' "$OUT" | cat -s | tail -n "${WTREE_FAIL_TAIL:-25}"
  printf -- '--- docker.log\n'
  tail -n 15 "$WTREE_SHIM_DIR/docker.log" 2>/dev/null
  printf -- '--- term.log\n'
  tail -n 15 "$WTREE_SHIM_DIR/term.log" 2>/dev/null
  printf 'FAILED: %s\n' "$*"
  exit 1
}

broken() {
  printf 'BROKEN SETUP: %s\n' "$*"
  exit 2
}

must() {
  "$@" >"$T/must.out" 2>&1 || {
    cat "$T/must.out"
    broken "this failed: $*"
  }
}

assert_eq() {
  [[ $1 == "$2" ]] || fail "expected [$2], got [$1]${3:+: $3}"
}

assert_rc() {
  [[ $RC == "$1" ]] || fail "expected exit code $1, got $RC"
}

assert_contains() {
  [[ $OUT == *"$1"* ]] || fail "the output does not contain [$1]"
}

assert_not_contains() {
  [[ $OUT != *"$1"* ]] || fail "the output contains [$1]"
}

assert_dir() {
  [[ -d $1 ]] || fail "the directory is gone: $1"
}

assert_no_dir() {
  [[ ! -e $1 ]] || fail "the path still exists: $1"
}

assert_registered() {
  git -C "$REPO" worktree list --porcelain | grep -Fxq "worktree $1" || fail "git no longer lists the worktree: $1"
}

assert_not_registered() {
  if git -C "$REPO" worktree list --porcelain | grep -Fxq "worktree $1"; then fail "git still lists the worktree: $1"; fi
}

assert_log() {
  grep -Fxq -- "$1" "$WTREE_SHIM_DIR/docker.log" || fail "docker was not called with: $1"
}

assert_not_log() {
  if grep -Fxq -- "$1" "$WTREE_SHIM_DIR/docker.log"; then fail "docker was called with: $1"; fi
}

assert_log_count() {
  local count
  count=$(grep -Fxc -- "$1" "$WTREE_SHIM_DIR/docker.log")
  [[ $count == "$2" ]] || fail "expected $2 calls of [$1], got $count"
}

assert_log_order() {
  local first second
  first=$(grep -Fxn -- "$1" "$WTREE_SHIM_DIR/docker.log" | head -n 1 | cut -d: -f1)
  second=$(grep -Fxn -- "$2" "$WTREE_SHIM_DIR/docker.log" | head -n 1 | cut -d: -f1)
  [[ -n $first && -n $second ]] || fail "both calls must happen: [$1] and [$2]"
  ((first < second)) || fail "[$1] must come before [$2]"
}

# compose, a volume rm, and anything else that changes the engine
assert_no_docker_changes() {
  local changes
  changes=$(grep -E '^(compose|volume rm|rm|stop|kill|run|exec|system prune|volume prune|container|network)( |$)' "$WTREE_SHIM_DIR/docker.log") || changes=
  [[ -z $changes ]] || fail "docker calls that change state: $changes"
}

assert_term_log() {
  grep -Fxq -- "$1" "$WTREE_SHIM_DIR/term.log" || fail "the terminal CLI was not called with: $1"
}

assert_not_term_log_like() {
  if grep -Fq -- "$1" "$WTREE_SHIM_DIR/term.log"; then fail "the terminal CLI was called with: $1"; fi
}

assert_golden() {
  local file=$TEST_ROOT/golden/$1.txt actual
  actual=${OUT//"$T"/<T>}
  if [[ -n ${WTREE_UPDATE_GOLDEN:-} ]]; then
    printf '%s\n' "$actual" >"$file"
    return 0
  fi
  [[ -f $file ]] || fail "no golden file $file, run test/run.sh --update-golden"
  if ! diff -u "$file" <(printf '%s\n' "$actual") >"$T/golden.diff"; then
    OUT=$(cat "$T/golden.diff")
    fail "the output differs from golden/$1.txt"
  fi
}

# ---- fixtures: real git, inside the scratch of this test

make_repo() {
  REPO=$HOME/code/${1:-proj}
  must mkdir -p "$REPO"
  must git -C "$REPO" init -q -b main
  printf '.env\nnode_modules/\n' >"$REPO/.gitignore"
  printf 'hello\n' >"$REPO/file.txt"
  must git -C "$REPO" add -A
  must git -C "$REPO" commit -q -m init
  COMMON=$REPO/.git
  cd "$REPO" || exit 2
}

worktree_path() {
  printf '%s/code/worktrees/%s' "$HOME" "$1"
}

# sets WT, on a branch of the same name
add_wt() {
  WT=$(worktree_path "$1")
  must git -C "$REPO" worktree add -q -b "$1" "$WT"
}

add_detached_wt() {
  WT=$(worktree_path "$1")
  must git -C "$REPO" worktree add -q --detach "$WT"
}

# a repo with a submodule. git refuses a plain remove of a worktree that has checked it out, and no
# risk check sees this, so a step fails after the prompts. Call init_submodule_in after add_wt.
make_repo_with_submodule() {
  make_repo vendored
  local vendored=$REPO
  make_repo
  must git -c protocol.file.allow=always -C "$REPO" submodule add -q "$vendored" sub
  must git -C "$REPO" commit -q -am submodule
}

init_submodule_in() {
  must git -c protocol.file.allow=always -C "$1" submodule update -q --init
}

commit_in() {
  printf 'more\n' >>"$1/file.txt"
  must git -C "$1" commit -q -am more
}

# args: project, directory, state, and optionally cpu and memory
stack() {
  local n
  n=$(wc -l <"$WTREE_SHIM_DIR/containers" | tr -d ' ')
  printf '%s\t%s\t%s\tid%s\t%s\t%s\n' "$1" "$2" "$3" "$n" "${4:-1.0}" "${5:-100MiB}" >>"$WTREE_SHIM_DIR/containers"
}

volume() {
  printf '%s\t%s\n' "$2" "$1" >>"$WTREE_SHIM_DIR/volumes"
}

# args: container id (idN, in the order of the stack calls), service, status, ports, started, finished, name
container_meta() {
  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\n' "$1" "$2" "$3" "${4:-}" "${5:-2026-01-01T00:00:00.000000000Z}" "${6:-0001-01-01T00:00:00Z}" "${7:-$1}" >>"$WTREE_SHIM_DIR/meta"
}

shim_fail() {
  printf '%s\tfail\t%s\n' "$1" "${2:-injected failure}" >>"$WTREE_SHIM_DIR/inject"
}

shim_delay() {
  printf '%s\tdelay\t%s\n' "$1" "$2" >>"$WTREE_SHIM_DIR/inject"
}

shim_hang() {
  printf '%s\thang\t%s\n' "$1" "${2:-30}" >>"$WTREE_SHIM_DIR/inject"
}

shim_wedge() {
  printf '%s\twedge\t%s\n' "$1" "$2" >>"$WTREE_SHIM_DIR/inject"
}

# args: glob, and the first call that fails
shim_fail_from() {
  printf '%s\tfail_from\t%s\n' "$1" "$2" >>"$WTREE_SHIM_DIR/inject"
}

# args: tool, glob of the arguments, exit code, stdout with \n escapes
term_rule() {
  printf '%s\t%s\t%s\t%s\n' "$1" "$2" "$3" "${4:-}" >>"$WTREE_SHIM_DIR/term.rules"
}

log_count() {
  grep -c -- "$1" "$WTREE_SHIM_DIR/docker.log"
}

wait_log_count() {
  local pattern=$1 want=$2 tries=0
  until (($(log_count "$pattern") >= want)); do
    tries=$((tries + 1))
    ((tries < 150)) || broken "docker log never reached $want calls of [$pattern]"
    sleep 0.05
  done
}

wait_file() {
  local tries=0
  until [[ -s $1 ]]; do
    tries=$((tries + 1))
    ((tries < 200)) || fail "the file never appeared: $1"
    sleep 0.05
  done
}

# ---- running wtree

strip_ansi() {
  sed -E 's/\x1b\[[0-9;?]*[A-Za-z]//g; s/\r//g'
}

# no terminal: each prompt sees end of file, so nothing is confirmed
wt() {
  RC=0
  timeout -k 1 "${WT_TIMEOUT:-25}" "$WTREE" "$@" >"$T/out" 2>&1 </dev/null || RC=$?
  OUT=$(tr '\0' '\n' <"$T/out" | strip_ansi)
}

# the number of prompts on the screen, and a word for the kind of the last one
prompts_on_screen() {
  grep -aoE '\[y/N\] |N: no\] |Press a key to continue\. |to delete: |worktrees too: ' "$T/out" |
    awk '{ last = $0; n++ } END { print n + 0, (last ~ /delete|too/ ? "line" : "key") }'
}

# wtree drops the keys that come before a prompt, so each answer waits for its prompt: one key, or
# the text up to a newline when the prompt asks for a line. After the last answer each prompt gets a
# newline, which is its default, so that no test waits on a prompt that it did not expect.
feed_answers() {
  local keys=$1 job=$2 seen=0 count kind size last='' answer
  while kill -0 "$job" 2>/dev/null; do
    size=$(stat -c %s "$T/out" 2>/dev/null) || size=
    if [[ $size != "$last" ]]; then
      last=$size
      read -r count kind < <(prompts_on_screen)
      if ((count > seen)); then
        seen=$count
        if [[ -z $keys ]]; then
          answer=$'\n'
        elif [[ $kind == line ]]; then
          answer=${keys%%$'\n'*}$'\n'
          keys=${keys#"${answer%$'\n'}"}
          keys=${keys#$'\n'}
        else
          answer=${keys:0:1}
          keys=${keys:1}
        fi
        printf '%s' "$answer"
      fi
    fi
    sleep 0.02
  done
}

# the first argument is the answers for /dev/tty.
#
# A pty starts at 0 columns, so give it a size. When the shell in it ends, the kernel hangs up on the
# whole session, so it waits for a disk refresh first. Under fzf, wtree exits and the terminal stays.
wt_keys() {
  local keys cmd job
  printf -v keys '%b' "$1"
  shift
  printf -v cmd '%q ' "$WTREE" "$@"
  cat >"$T/pty.sh" <<EOF
stty cols 120 rows 40
$cmd
status=\$?
for _ in \$(seq 1 40); do
  [ -d "$XDG_CACHE_HOME/wtree/lock" ] || break
  sleep 0.05
done
exit \$status
EOF
  rm -f "$T/answers"
  mkfifo "$T/answers"
  : >"$T/out"
  timeout -k 1 "${WT_TIMEOUT:-25}" script -qfec "bash $T/pty.sh" /dev/null <"$T/answers" >"$T/out" 2>&1 &
  job=$!
  feed_answers "$keys" "$job" >"$T/answers"
  RC=0
  wait "$job" || RC=$?
  OUT=$(tr '\0' '\n' <"$T/out" | strip_ansi)
}

# sets ARGS to the paths and then the repo of each path
worktree_args() {
  local path
  ARGS=("$@")
  for path in "$@"; do ARGS+=("$COMMON"); done
}

# ---- a full screen in a private tmux server. It never uses the default server.

tmux_cmd() {
  "$WTREE_TEST_TMUX" -L "$TUI_SOCKET" -f /dev/null "$@"
}

tui_stop() {
  tmux_cmd kill-server >/dev/null 2>&1 || true
}

# TUI_COLS, TUI_ROWS and TUI_ENV (an array of VAR=value) change the pane. tmux sets its own
# variables in a pane, and wtree must see only the ones that the test gives it.
tui_start() {
  local env_line='' arg args=''
  for arg in "${TUI_ENV[@]:-}"; do
    if [[ -n $arg ]]; then env_line+="export $(printf '%q' "$arg")"$'\n'; fi
  done
  for arg in "$@"; do args+=" $(printf '%q' "$arg")"; done
  cat >"$T/pane.sh" <<EOF
unset TMUX TMUX_PANE TERM_PROGRAM TERM_PROGRAM_VERSION
$env_line
cd $(printf '%q' "$PWD") || exit 1
$(printf '%q' "$WTREE")$args
printf '%s\n' "\$?" >"$T/pane.rc"
sleep 300
EOF
  tmux_cmd new-session -d -s t -x "${TUI_COLS:-120}" -y "${TUI_ROWS:-40}" "bash $T/pane.sh" \; set-option -s escape-time 0 >/dev/null ||
    broken "tmux did not start"
}

tui_text() {
  tmux_cmd capture-pane -p -t t 2>/dev/null
}

tui_send() {
  # wtree-tui holds what confirms a sheet that just came, and a test is a person who has read it
  if [[ -n ${TG_SHEET_WAIT:-} && ($1 == y || $1 == Enter) ]]; then sleep 0.35; fi
  tmux_cmd send-keys -t t "$@"
}

tui_type() {
  tmux_cmd send-keys -t t -l "$1"
}

# for a step on the way to the behaviour that the test is about. The dots in the prompt mean that the
# list has not loaded, and that fzf still drops the keys.
tui_wait() {
  local tries=0 screen
  while :; do
    screen=$(tui_text)
    [[ $screen == *"$1"* && $screen != *' … > '* ]] && return 0
    tries=$((tries + 1))
    if ((tries > 200)); then
      printf 'BROKEN SETUP: the screen never showed [%s]\n--- screen\n%s\n--- docker.log\n' "$1" "$screen"
      cat "$WTREE_SHIM_DIR/docker.log"
      exit 2
    fi
    sleep 0.05
  done
}

# for the behaviour that the test is about
tui_expect() {
  local tries=0 screen
  while :; do
    screen=$(tui_text)
    [[ $screen == *"$1"* ]] && return 0
    tries=$((tries + 1))
    if ((tries > 200)); then
      OUT=$screen
      fail "the screen never showed [$1]"
    fi
    sleep 0.05
  done
}

tui_expect_gone() {
  local tries=0 screen
  while :; do
    screen=$(tui_text)
    [[ $screen != *"$1"* ]] && return 0
    tries=$((tries + 1))
    if ((tries > 200)); then
      OUT=$screen
      fail "the screen still shows [$1]"
    fi
    sleep 0.05
  done
}

# wtree drops the keys that come before its prompt, and text above the prompt comes first
tui_wait_prompt() {
  local tries=0 screen
  while :; do
    screen=$(tui_text)
    [[ $screen == *'[y/N]'* || $screen == *'N: no]'* ]] && return 0
    tries=$((tries + 1))
    if ((tries > 200)); then
      OUT=$screen
      fail "the screen never showed a prompt"
    fi
    sleep 0.05
  done
}

# the text must not appear for this many seconds
tui_expect_absent_for() {
  local text=$1 seconds=$2 tries=0 screen limit
  limit=$(awk -v s="$seconds" 'BEGIN { printf "%d", s * 20 }')
  while ((tries < limit)); do
    screen=$(tui_text)
    if [[ $screen == *"$text"* ]]; then
      OUT=$screen
      fail "the screen showed [$text]"
    fi
    tries=$((tries + 1))
    sleep 0.05
  done
}

# the screen can show a list a moment before fzf takes keys again, so press ctrl-r until docker sees it
tui_reload_when_ready() {
  local before tries=0
  before=$(log_count '^ps ')
  until (($(log_count '^ps ') > before)); do
    tries=$((tries + 1))
    ((tries < 40)) || fail "ctrl-r never started a reload"
    tui_send C-r
    sleep 0.25
  done
}

tui_wait_exit() {
  local tries=0
  until [[ -s $T/pane.rc ]]; do
    tries=$((tries + 1))
    if ((tries > 200)); then
      OUT=$(tui_text)
      fail "wtree did not exit"
    fi
    sleep 0.05
  done
  RC=$(cat "$T/pane.rc")
  OUT=$(tui_text)
}

tui_row_of() {
  tui_text | grep -nF -- "$1" | head -n 1 | cut -d: -f1
}

# a click and a release, in the mouse protocol that fzf turns on
tui_click_row() {
  local row seq hex
  row=$(tui_row_of "$1")
  [[ -n $row ]] || broken "no row on the screen has [$1]"
  printf -v seq '\033[<0;6;%dM\033[<0;6;%dm' "$row" "$row"
  hex=$(printf '%s' "$seq" | od -An -tx1 | tr -d '\n')
  # shellcheck disable=SC2086
  tmux_cmd send-keys -t t -H $hex
}

# the helpers of the Bubble Tea interface
# shellcheck source=test/lib/tui_go.sh
source "$TEST_ROOT/lib/tui_go.sh"
