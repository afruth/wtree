# The net under all other tests. If these fail, nothing else may run.
# shellcheck shell=bash

test_guard_passes_in_the_sandbox() {
  bash "$RUN_SH" --guard-only >"$T/out" 2>&1
  RC=$?
  OUT=$(cat "$T/out")
  assert_rc 0
}

expect_guard_abort() {
  bash "$RUN_SH" --guard-only >"$T/out" 2>&1
  RC=$?
  OUT=$(cat "$T/out")
  assert_rc 3
  assert_contains "guard failed"
}

test_guard_aborts_when_another_docker_comes_first() {
  mkdir -p "$T/evil"
  printf '#!/bin/sh\ntouch %s/evil-ran\n' "$T" >"$T/evil/docker"
  chmod +x "$T/evil/docker"
  (
    export PATH=$T/evil:$PATH
    expect_guard_abort
  ) || exit
  assert_no_dir "$T/evil-ran"
}

test_guard_aborts_when_a_second_docker_is_on_path() {
  mkdir -p "$T/later"
  printf '#!/bin/sh\ntouch %s/later-ran\n' "$T" >"$T/later/docker"
  chmod +x "$T/later/docker"
  (
    export PATH=$PATH:$T/later
    expect_guard_abort
  ) || exit
  assert_no_dir "$T/later-ran"
}

test_guard_aborts_on_a_reachable_docker_host() {
  export DOCKER_HOST=unix:///var/run/docker.sock
  expect_guard_abort
}

test_guard_aborts_when_home_is_real() {
  export HOME=/root
  expect_guard_abort
}

test_guard_aborts_when_the_cache_or_roots_are_real() {
  (
    export XDG_CACHE_HOME=/root/.cache
    expect_guard_abort
  ) || exit
  export WTREE_ROOTS=/root
  expect_guard_abort
}

test_guard_aborts_when_a_terminal_variable_leaks() {
  local var
  for var in REX_SESSION REX_BLOCK TMUX TERM_PROGRAM ZELLIJ COMPOSE_FILE; do
    (
      export "$var=leak"
      expect_guard_abort
    ) || exit
  done
}

test_guard_aborts_on_settings_that_reach_a_real_engine_or_repo() {
  local setting
  for setting in DOCKER_CONFIG=/root/.docker DOCKER_TLS_VERIFY=1 BASH_ENV=/nonexistent GIT_DIR=/nonexistent/.git GIT_WORK_TREE=/nonexistent; do
    (
      export "$setting"
      expect_guard_abort
    ) || exit
  done
}

test_wtree_finds_docker_only_on_path_and_names_no_engine() {
  OUT=$(grep -nE '/[[:alnum:]_/.-]*bin/docker|docker +(--context|--host|-H|-c)( |=)|DOCKER_HOST|DOCKER_CONTEXT' "$WTREE") || OUT=
  assert_eq "$OUT" ""
}

test_real_docker_and_rex_are_not_on_path() {
  local tool
  for tool in docker rex tmux zellij herdr wezterm kitten cmux osascript; do
    assert_eq "$(type -ap "$tool")" "$TEST_ROOT/shim/$tool"
  done
}

test_shim_never_runs_a_real_program() {
  OUT=$(grep -nE '(^|[^[:alnum:]_])(exec|eval)([^[:alnum:]_]|$)|command +docker|/usr/(local/)?bin|/var/run' "$TEST_ROOT/shim/docker" "$TEST_ROOT/shim/_term_cli" | grep -v ':1:#!') || OUT=
  assert_eq "$OUT" ""
}

test_shim_denies_what_it_does_not_know() {
  local call status
  allow_refusals
  stack fixture /tmp/fixture running
  volume fixture fixture_data
  while IFS= read -r call; do
    # shellcheck disable=SC2086
    docker $call >/dev/null 2>&1
    status=$?
    assert_eq "$status" 125 "docker $call"
    grep -Fxq -- "$call" "$WTREE_SHIM_DIR/refused" || fail "not logged as refused: $call"
  done <<'CALLS'
rm -f real
stop real
kill real
run alpine
exec real sh
compose -p real-stack down
compose -p fixture down -v
compose -p fixture up -d
compose down
system prune -af
volume prune -f
volume rm real_data
volume rm -f fixture_data
network rm real
container prune
ps
ps --format {{.Names}}
info --format {{.Names}}
CALLS
  assert_eq "$(cat "$WTREE_SHIM_DIR/containers")" "fixture	/tmp/fixture	running	id0	1.0	100MiB"
  assert_eq "$(cat "$WTREE_SHIM_DIR/volumes")" "fixture_data	fixture"
}

test_shim_denies_everything_without_a_fixture_dir() {
  local status
  (
    unset WTREE_SHIM_DIR
    docker ps -a --filter label=com.docker.compose.project --format '{{.ID}}' >/dev/null 2>&1
    exit $?
  )
  status=$?
  assert_eq "$status" 125
}

test_a_refused_call_fails_the_test_that_made_it() {
  docker rm -f real >/dev/null 2>&1
  harness_saw_refusal >/dev/null 2>&1 || fail "the harness did not notice a refused call"
  rm -f "$WTREE_SHIM_DIR/refused"
}

test_term_shims_deny_without_a_rule() {
  local tool status
  allow_refusals
  for tool in rex tmux zellij herdr wezterm kitten cmux osascript; do
    "$tool" list >/dev/null 2>&1
    status=$?
    assert_eq "$status" 125 "$tool"
  done
}

test_the_private_tmux_is_not_the_default_server() {
  make_repo
  tui_start -h
  tui_wait wtree
  local socket
  socket=$(tmux_cmd display-message -p '#{socket_path}')
  assert_eq "$socket" "$TMUX_TMPDIR/tmux-$(id -u)/$TUI_SOCKET"
}
