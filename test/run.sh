#!/usr/bin/env bash
# The entry point of the wtree tests. See test/README.md.
#
# Stage 1 makes a scratch directory and starts stage 2 with a clean environment, in a new session.
# Stage 2 checks the guard and runs each test as its own process.
set -uo pipefail

TEST_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
SHIM=$TEST_ROOT/shim
SHIMMED_TOOLS=(docker rex tmux zellij herdr wezterm kitten cmux osascript)

# only these tools are on PATH, so the real docker and rex are not on it at all
PATH_TOOLS=(awk basename bash cat chmod cksum cp cut date diff dirname env find fzf git grep head id ln ls
  lsof mkdir mkfifo mktemp mv od ps python3 readlink rm rmdir script sed seq setsid sh sha256sum sleep sort
  stat stty tail tee timeout touch tr uname uniq wc xargs curl dash gzip tar)

die() {
  printf 'test/run.sh: %s\n' "$*" >&2
  exit 3
}

build_tools() {
  local dir=$1 tool found
  mkdir -p "$dir"
  for tool in "${PATH_TOOLS[@]}"; do
    found=$(command -v "$tool") || continue
    [[ $found == /* ]] && ln -s "$found" "$dir/$tool"
  done
}

sweep_tmux() {
  local socket
  for socket in "$SCRATCH"/tmux/tmux-*/*; do
    [[ -S $socket ]] || continue
    TMUX_TMPDIR=$SCRATCH/tmux "$REAL_TMUX" -L "${socket##*/}" kill-server >/dev/null 2>&1 || true
  done
}

# a test can leave a background job that still writes for a moment
remove_scratch() {
  case $SCRATCH in
    */wtree-test.*) rm -rf "$SCRATCH" 2>/dev/null || { sleep 0.5; rm -rf "$SCRATCH" 2>/dev/null; } ;;
  esac
}

# wtree-tui is built here, with the real go, because nothing after this point can reach it. The
# binary goes in the scratch directory. A test of it skips when it cannot be built, and says why.
build_tui() {
  local dir tui
  dir=$(dirname "$TEST_ROOT")/tui
  mkdir -p "$SCRATCH/tui-bin"
  TUI_BIN=
  TUI_NOTE=
  if [[ ! -f $dir/go.mod ]]; then
    TUI_NOTE="there is no tui/go.mod next to these tests"
  elif ! command -v go >/dev/null 2>&1; then
    TUI_NOTE="go is not on PATH, so wtree-tui cannot be built"
  elif ! (cd "$dir" && go build -buildvcs=false -o "$SCRATCH/tui-bin/wtree-tui" ./cmd/wtree-tui) >"$SCRATCH/tui-bin/build.log" 2>&1; then
    TUI_NOTE="wtree-tui does not build: $(tail -n 1 "$SCRATCH/tui-bin/build.log")"
  else
    TUI_BIN=$SCRATCH/tui-bin/wtree-tui
  fi
}

stage1_cleanup() {
  sweep_tmux
  remove_scratch
}

stage1() {
  local tool status=0 child
  for tool in tmux fzf git script setsid timeout; do
    command -v "$tool" >/dev/null 2>&1 || die "$tool is not on PATH"
  done
  REAL_TMUX=$(command -v tmux)
  SCRATCH=$(mktemp -d -t wtree-test.XXXXXXXX) || die "mktemp failed"
  SCRATCH=$(cd "$SCRATCH" && pwd -P)
  trap stage1_cleanup EXIT
  build_tools "$SCRATCH/tools"
  mkdir -p "$SCRATCH/home" "$SCRATCH/cache" "$SCRATCH/tmux" "$SCRATCH/docker-config"
  build_tui
  # test/lua/run.sh uses the same build, after this stage
  export WTREE_TEST_TUI=$TUI_BIN

  # the new session has no controlling terminal, so /dev/tty can never be the terminal of the user
  setsid env -i \
    WTREE_TEST_STAGE=2 \
    WTREE_TEST_SCRATCH="$SCRATCH" \
    WTREE_TEST_TMUX="$REAL_TMUX" \
    WTREE_TEST_BIN="${WTREE_TEST_BIN:-$(dirname "$TEST_ROOT")/bin/wtree}" \
    WTREE_TEST_JOBS="${WTREE_TEST_JOBS:-8}" \
    WTREE_TEST_TUI="$TUI_BIN" \
    WTREE_TEST_TUI_NOTE="$TUI_NOTE" \
    WTREE_UI=fzf \
    PATH="$SHIM:$SCRATCH/tools" \
    HOME="$SCRATCH/home" \
    XDG_CACHE_HOME="$SCRATCH/cache" \
    WTREE_ROOTS="$SCRATCH/home" \
    DOCKER_HOST="unix://$SCRATCH/no-engine.sock" \
    DOCKER_CONFIG="$SCRATCH/docker-config" \
    TMUX_TMPDIR="$SCRATCH/tmux" \
    GIT_CONFIG_NOSYSTEM=1 \
    GIT_CEILING_DIRECTORIES="$SCRATCH" \
    GIT_AUTHOR_NAME=wtree-test GIT_AUTHOR_EMAIL=test@example.invalid \
    GIT_COMMITTER_NAME=wtree-test GIT_COMMITTER_EMAIL=test@example.invalid \
    TERM=xterm-256color LC_ALL=C.UTF-8 SHELL="$SCRATCH/tools/bash" \
    bash "$TEST_ROOT/run.sh" "$@" </dev/null &
  child=$!
  trap 'kill -TERM "$child" 2>/dev/null' INT TERM
  wait "$child" || status=$?
  # init.lua needs the Lua of Rex, and these tests use a private server. A filter means only some tests.
  if (($# == 0)); then bash "$TEST_ROOT/lua/run.sh" || status=1; fi
  return "$status"
}

guard_fail() {
  printf 'test/run.sh: guard failed: %s\n' "$*" >&2
  exit 3
}

in_scratch() {
  [[ $1 == "$WTREE_TEST_SCRATCH" || $1 == "$WTREE_TEST_SCRATCH"/* ]]
}

# nothing here may run a docker that is not the shim: the path check comes first
verify_guard() {
  local tool all var
  case ${WTREE_TEST_SCRATCH:-} in
    */wtree-test.*) [[ -d $WTREE_TEST_SCRATCH ]] || guard_fail "the scratch directory is missing" ;;
    *) guard_fail "WTREE_TEST_SCRATCH is not a test scratch directory" ;;
  esac
  for tool in "${SHIMMED_TOOLS[@]}"; do
    all=$(type -ap "$tool")
    [[ $all == "$SHIM/$tool" ]] || guard_fail "$tool on PATH is not only the shim: ${all:-nothing}"
  done
  [[ -z $(type -ap wtree-tui) ]] || guard_fail "wtree-tui is on PATH"
  [[ $(docker --wtree-shim-id 2>/dev/null) == wtree-test-docker-shim ]] || guard_fail "the docker shim does not answer"
  [[ ${DOCKER_HOST:-} == "unix://$WTREE_TEST_SCRATCH/"* ]] || guard_fail "DOCKER_HOST is not in the scratch directory: ${DOCKER_HOST:-unset}"
  [[ ! -e ${DOCKER_HOST#unix://} ]] || guard_fail "DOCKER_HOST is reachable"
  [[ -z ${DOCKER_CONTEXT:-} ]] || guard_fail "DOCKER_CONTEXT is set"
  in_scratch "${DOCKER_CONFIG:-}" || guard_fail "DOCKER_CONFIG is not in the scratch directory: ${DOCKER_CONFIG:-unset}"
  in_scratch "${HOME:-}" || guard_fail "HOME is not in the scratch directory: ${HOME:-unset}"
  in_scratch "${XDG_CACHE_HOME:-}" || guard_fail "XDG_CACHE_HOME is not in the scratch directory"
  in_scratch "${WTREE_ROOTS:-}" || guard_fail "WTREE_ROOTS is not in the scratch directory"
  in_scratch "${TMUX_TMPDIR:-}" || guard_fail "TMUX_TMPDIR is not in the scratch directory"
  for var in REX_SESSION REX_BLOCK REX_SERVER REX_BIN_DIR TMUX TMUX_PANE TERM_PROGRAM TERM_PROGRAM_VERSION \
    ZELLIJ ZELLIJ_PANE_ID KITTY_WINDOW_ID WEZTERM_PANE WEZTERM_EXECUTABLE_DIR CMUX_WORKSPACE_ID \
    CMUX_BUNDLED_CLI_PATH HERDR_PANE_ID HERDR_WORKSPACE_ID COMPOSE_FILE COMPOSE_PROJECT_NAME \
    FZF_DEFAULT_OPTS FZF_DEFAULT_COMMAND WTREE_GLOBAL WTREE_SELF DOCKER_TLS_VERIFY DOCKER_CERT_PATH \
    GIT_DIR GIT_WORK_TREE GIT_COMMON_DIR BASH_ENV ENV; do
    [[ -z ${!var:-} ]] || guard_fail "$var is set"
  done
  if [[ -n ${WTREE_TEST_TUI:-} ]]; then in_scratch "$WTREE_TEST_TUI" || guard_fail "WTREE_TEST_TUI is not in the scratch directory"; fi
  if { : </dev/tty; } 2>/dev/null; then guard_fail "the tests have a controlling terminal"; fi
}

load_tests() {
  local file
  declare -gA XFAIL=()
  # shellcheck source=test/lib/harness.sh
  source "$TEST_ROOT/lib/harness.sh"
  for file in "$TEST_ROOT"/cases/*.sh; do
    # shellcheck disable=SC1090
    source "$file"
  done
}

# a known bug: the test states the right behaviour and fails today
xfail() {
  XFAIL[$1]=$2
}

test_names() {
  declare -F | awk '$3 ~ /^test_/ { print $3 }' | sort
}

# exit 0: passed, 1: an assertion failed, 2: the harness failed or the shim refused a call,
# 77: skipped, because what it tests cannot run here
run_one() {
  local name=$1 status=0
  load_tests
  harness_setup
  trap harness_teardown EXIT
  (
    set +e
    "$name"
  )
  status=$?
  if ((status == 0 || status == 1)) && harness_saw_refusal; then status=2; fi
  exit "$status"
}

report() {
  local kind=$1 name=$2 note=${3:-}
  printf '%-6s %s%s\n' "$kind" "$name" "${note:+  ($note)}"
}

classify() {
  local name=$1 status=$2
  if ((status == 77)); then
    report skip "$name" "$(tail -n 1 "$LOGS/$name")"
    SKIPPED+=("$name")
  elif [[ -n ${XFAIL[$name]:-} ]]; then
    case $status in
      0)
        report XPASS "$name" "passes now: remove its xfail line"
        XPASSED+=("$name")
        ;;
      1)
        report xfail "$name" "${XFAIL[$name]}"
        XFAILED+=("$name")
        ;;
      *)
        report FAIL "$name" "exit $status, not the expected failure"
        FAILED+=("$name")
        ;;
    esac
  elif ((status == 0)); then
    report ok "$name"
    PASSED+=("$name")
  else
    report FAIL "$name"
    FAILED+=("$name")
  fi
}

# many short tests in parallel keep the suite fast, and each has a private tmux and scratch
run_all() {
  local filter=$1 name started=$SECONDS names=()
  PASSED=() XFAILED=() FAILED=() XPASSED=() SKIPPED=()
  LOGS=$WTREE_TEST_SCRATCH/logs
  JOB_NAME=()
  mkdir -p "$LOGS"
  mapfile -t names < <(test_names)
  for name in "${names[@]}"; do
    [[ -z $filter || $name =~ $filter ]] || continue
    while ((${#JOB_NAME[@]} >= WTREE_TEST_JOBS)); do reap_one; done
    timeout -k 3 "${WTREE_TEST_LIMIT:-60}" bash "$TEST_ROOT/run.sh" --one "$name" >"$LOGS/$name" 2>&1 &
    JOB_NAME[$!]=$name
  done
  while ((${#JOB_NAME[@]} > 0)); do reap_one; done
  summarize "$((SECONDS - started))"
}

reap_one() {
  local finished status=0
  wait -n -p finished || status=$?
  [[ -n ${finished:-} && -n ${JOB_NAME[$finished]:-} ]] || return 0
  classify "${JOB_NAME[$finished]}" "$status"
  unset "JOB_NAME[$finished]"
}

summarize() {
  local seconds=$1 name
  for name in "${FAILED[@]:-}"; do
    [[ -n $name ]] || continue
    printf '\n--- %s\n' "$name"
    tail -n 80 "$LOGS/$name"
  done
  printf '\n%d passed, %d expected failures, %d failed, %d unexpected passes, %d skipped, in %ds\n' \
    "${#PASSED[@]}" "${#XFAILED[@]}" "${#FAILED[@]}" "${#XPASSED[@]}" "${#SKIPPED[@]}" "$seconds"
  ((${#PASSED[@]} + ${#XFAILED[@]} + ${#FAILED[@]} + ${#XPASSED[@]} + ${#SKIPPED[@]} > 0)) || die "no test matches"
  ((${#FAILED[@]} == 0 && ${#XPASSED[@]} == 0))
}

stage2() {
  local filter=''
  verify_guard
  case ${1:-} in
    --guard-only) exit 0 ;;
    --one)
      run_one "$2"
      ;;
  esac
  cd "$WTREE_TEST_SCRATCH" || die "cannot enter the scratch directory"
  while (($#)); do
    case $1 in
      -l | --list)
        load_tests
        test_names
        exit 0
        ;;
      --update-golden) export WTREE_UPDATE_GOLDEN=1 ;;
      *) filter=$1 ;;
    esac
    shift
  done
  # the suite is in a new session, so a kill must reach the running tests here
  trap 'kill $(jobs -p) 2>/dev/null; exit 130' INT TERM
  load_tests
  run_all "$filter"
}

if [[ ${WTREE_TEST_STAGE:-} == 2 ]]; then
  stage2 "$@"
else
  stage1 "$@"
fi
