#!/usr/bin/env bash
# Tests of init.lua. They need the Lua of Rex, so they start a private Rex server with its own socket,
# state and home, and stop it at the end. They never touch another server. See test/README.md.
set -uo pipefail

HERE=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
REX=$(command -v rex) || {
  printf 'test/lua: no rex on PATH, so the tests of init.lua did not run\n'
  exit 0
}
LAB=$(mktemp -d -t wtree-lua.XXXXXXXX) || exit 2
SOCKET=$LAB/s.sock
SERVER=

cleanup() {
  local _
  if [[ -n $SERVER ]]; then
    kill "$SERVER" 2>/dev/null
    for _ in $(seq 1 30); do
      kill -0 "$SERVER" 2>/dev/null || break
      sleep 0.1
    done
    kill -9 "$SERVER" 2>/dev/null
  fi
  rm -rf "$LAB" 2>/dev/null || { sleep 0.5; rm -rf "$LAB" 2>/dev/null; }
}
trap cleanup EXIT

CLEAN_ENV=(env -i PATH=/usr/bin:/bin HOME="$LAB" XDG_RUNTIME_DIR="$LAB" XDG_STATE_HOME="$LAB/state" XDG_CONFIG_HOME="$LAB/config"
  XDG_CACHE_HOME="$LAB/cache" DOCKER_HOST="unix://$LAB/no-engine.sock"
  WTREE_LUA_INIT="$(dirname "$(dirname "$HERE")")/init.lua" WTREE_LUA_STUB="$LAB/stub")

lab_rex() {
  "${CLEAN_ENV[@]}" "$REX" -S "unix://$SOCKET" --autostart=false "$@"
}

mkdir -p "$LAB/state" "$LAB/config" "$LAB/cache"
printf '#!/bin/sh\nexec sleep 60\n' >"$LAB/stub"
chmod +x "$LAB/stub"
: >"$LAB/config/init.lua"
"${CLEAN_ENV[@]}" "$REX" server run --listen "unix://$SOCKET" --state-dir "$LAB/state" --restore=false --config "$LAB/config/init.lua" >"$LAB/server.log" 2>&1 &
SERVER=$!
for _ in $(seq 1 50); do
  [[ -S $SOCKET ]] && break
  sleep 0.1
done
[[ -S $SOCKET ]] || {
  cat "$LAB/server.log"
  printf 'test/lua: the private Rex server did not start\n'
  exit 2
}

lab_rex new lab --cwd "$LAB" -- sleep 60 >/dev/null || exit 2
out=$(lab_rex do "$HERE/floats.lua" -s lab 2>&1)
status=$?
printf '%s\n' "$out"
[[ $status == 0 && $out == *', 0 failed'* ]] || exit 1

# the real program in a real float, behind the same guard as test/run.sh
TUI=${WTREE_TEST_TUI:-}
if [[ -z $TUI ]] && command -v go >/dev/null 2>&1; then
  TUI=$LAB/wtree-tui
  (cd "$(dirname "$(dirname "$HERE")")/tui" && CGO_ENABLED=0 go build -o "$TUI" ./cmd/wtree-tui) || TUI=
fi
if [[ -z $TUI ]]; then
  printf 'test/lua: no go and no WTREE_TEST_TUI, so the test of the real float did not run\n'
  exit 0
fi
# shellcheck source=test/lua/real_float.sh
source "$HERE/real_float.sh"
real_float_test
