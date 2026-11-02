# The real bin/wtree and wtree-tui in a float of the private Rex server. Sourced by run.sh.
# shellcheck shell=bash

real_float_fail() {
  printf 'FAIL   real float: %s\n--- screen\n%s\n' "$1" "${2:-}"
  exit 1
}

# WTREE_LUA_SHOW=1 prints the screens, to look at them by hand
real_float_show() {
  if [[ -n ${WTREE_LUA_SHOW:-} ]]; then printf '%s\n' "$SCREEN"; fi
}

# waits until the screen of block $1 shows text $2, and sets SCREEN
real_float_wait() {
  local _
  for _ in $(seq 1 150); do
    SCREEN=$(lab_rex capture -b "$1" --trim 2>&1)
    [[ $SCREEN == *"$2"* ]] && return 0
    sleep 0.1
  done
  real_float_fail "the screen never showed [$2]" "$SCREEN"
}

real_float_test() {
  local root repo guard_dir tools shim_dir tool found block _
  root=$(dirname "$(dirname "$HERE")")
  guard_dir=$LAB/guard
  tools=$guard_dir/tools
  shim_dir=$guard_dir/shim
  repo=$guard_dir/home/code/proj
  mkdir -p "$tools" "$shim_dir" "$repo" "$guard_dir/home/.cache" "$guard_dir/docker-config" "$guard_dir/home/code/worktrees"
  for tool in awk basename bash cat chmod cksum cp cut date diff dirname env find git grep head id ln ls lsof mkdir \
    mktemp mv od ps python3 readlink rm rmdir sed seq sh sha256sum sleep sort stat stty tail tee timeout touch tr \
    uname uniq wc xargs; do
    found=$(PATH=/usr/bin:/bin:$PATH command -v "$tool") || continue
    [[ $found == /* ]] && ln -s "$found" "$tools/$tool"
  done

  # fixtures: real git in the private directory, and a fake engine
  local git_env=(env -i PATH="$tools" HOME="$guard_dir/home" GIT_CONFIG_NOSYSTEM=1 GIT_AUTHOR_NAME=t GIT_AUTHOR_EMAIL=t@example.invalid
    GIT_COMMITTER_NAME=t GIT_COMMITTER_EMAIL=t@example.invalid)
  "${git_env[@]}" git -C "$repo" init -q -b main
  printf 'hello\n' >"$repo/file.txt"
  "${git_env[@]}" git -C "$repo" add -A
  "${git_env[@]}" git -C "$repo" commit -q -m init
  "${git_env[@]}" git -C "$repo" worktree add -q -b alpha "$guard_dir/home/code/worktrees/alpha"
  printf 'zulu\t%s\trunning\tid0\t1.0\t100MiB\n' "$guard_dir/home/code/worktrees/alpha" >"$shim_dir/containers"
  printf 'zulu_data\tzulu\n' >"$shim_dir/volumes"
  : >"$shim_dir/docker.log"

  # the layout of the plugin: the script, with the program beside it, and no setting that names it
  mkdir -p "$guard_dir/plugin/bin"
  cp "$root/bin/wtree" "$guard_dir/plugin/bin/wtree"
  cp "$TUI" "$guard_dir/plugin/bin/wtree-tui"

  cat >"$guard_dir/wtree" <<WRAP
#!/bin/sh
# no environment from Rex or the host: only the shims and the private directory
exec $tools/env -i PATH="$(dirname "$HERE")/shim:$tools" HOME="$guard_dir/home" XDG_CACHE_HOME="$guard_dir/home/.cache" \\
  XDG_STATE_HOME="$guard_dir/home/.state" WTREE_ROOTS="$guard_dir/home" WTREE_SHIM_DIR="$shim_dir" \\
  DOCKER_HOST="unix://$guard_dir/no-engine.sock" DOCKER_CONFIG="$guard_dir/docker-config" \\
  TERM="\${TERM:-xterm-256color}" LC_ALL=C.UTF-8 GIT_CONFIG_NOSYSTEM=1 \\
  $tools/bash -c '[ "\$(docker --wtree-shim-id)" = wtree-test-docker-shim ] || { echo "docker is not the shim"; exit 3; }; exec "\$0" "\$@"' "$guard_dir/plugin/bin/wtree" "\$@"
WRAP
  chmod +x "$guard_dir/wtree"

  lab_rex new tuilab --cwd "$repo" -- sleep 120 >/dev/null || real_float_fail "no session"
  out=$("${CLEAN_ENV[@]}" WTREE_LUA_WRAP="$guard_dir/wtree" "$REX" -S "unix://$SOCKET" --autostart=false do "$HERE/real_float.lua" -s tuilab 2>&1) ||
    real_float_fail "the action failed" "$out"
  block=${out##*block=}
  block=${block%%[[:space:]]*}
  [[ -n $block ]] || real_float_fail "no float" "$out"

  # -d in a float: the TUI, on stack rows, in a repo of the fixtures
  real_float_wait "$block" "sort: name"
  real_float_wait "$block" zulu
  grep -q fzf <<<"$SCREEN" && real_float_fail "this is not the TUI" "$SCREEN"
  printf 'ok     the float runs the real wtree-tui on the fixtures\n'
  real_float_show

  # the same float on a phone width
  out=$("${CLEAN_ENV[@]}" WTREE_LUA_BLOCK="$block" "$REX" -S "unix://$SOCKET" --autostart=false do "$HERE/real_float_narrow.lua" -s tuilab 2>&1) ||
    real_float_fail "the resize failed" "$out"
  # the wrapper gives the program no variable of Rex, so only --rex can turn this on
  [[ $out == *'"app":"wtree"'* ]] || real_float_fail "--rex gave no status" "$out"
  printf 'ok     --rex reports the status of the program to Rex\n'
  for _ in $(seq 1 50); do
    SCREEN=$(lab_rex capture -b "$block" --trim 2>&1)
    ! grep -qE '.{45,}' <<<"$SCREEN" && [[ $SCREEN == *zulu* ]] && break
    sleep 0.1
  done
  if grep -qE '.{45,}' <<<"$SCREEN"; then real_float_fail "the list did not fit 44 columns" "$SCREEN"; fi
  printf 'ok     the list follows a phone width in the float\n'
  real_float_show

  # the sheet opens with a plain key, and it closes with nothing done
  lab_rex send-key -b "$block" a >/dev/null 2>&1
  real_float_wait "$block" cancel
  real_float_show
  lab_rex send-key -b "$block" 0 >/dev/null 2>&1
  for _ in $(seq 1 50); do
    SCREEN=$(lab_rex capture -b "$block" --trim 2>&1)
    [[ $SCREEN != *cancel* ]] && break
    sleep 0.1
  done
  [[ $SCREEN != *cancel* ]] || real_float_fail "the sheet stays after 0" "$SCREEN"
  printf 'ok     the actions sheet opens and closes with plain keys\n'

  # plain keys: esc or q ends the program, and the float closes
  lab_rex send-key -b "$block" q >/dev/null 2>&1
  for _ in $(seq 1 50); do
    lab_rex capture -b "$block" >/dev/null 2>&1 || break
    sleep 0.1
  done
  lab_rex capture -b "$block" >/dev/null 2>&1 && real_float_fail "the float stays after q"
  printf 'ok     q closes the float\n'

  changes=$(grep -E '^(compose|volume rm|rm|stop|kill|run|exec|system prune|volume prune|container rm|network rm)( |$)' "$shim_dir/docker.log") || changes=
  [[ -z $changes ]] || real_float_fail "the fake engine saw a change" "$changes"
  [[ ! -s $shim_dir/refused ]] || real_float_fail "the shim refused a call" "$(cat "$shim_dir/refused")"
  printf 'ok     the fake engine saw no change\n'
}
