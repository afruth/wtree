# install.sh against fake release files on disk. No case uses the network.
# shellcheck shell=bash

# makes a release in $T/rel: a script, one archive for this host and checksums.txt
fake_release() {
  local tag=${1:-v9.9.9} ver host work
  ver=${tag#v}
  host=$(host_target)
  RELEASE=$T/rel/download/$tag
  mkdir -p "$RELEASE" "$T/bin"
  printf '#!/usr/bin/env bash\necho fake script %s\n' "$tag" >"$RELEASE/wtree"
  work=$T/archive
  mkdir -p "$work"
  printf '#!/bin/sh\necho fake tui %s\n' "$tag" >"$work/wtree-tui"
  chmod +x "$work/wtree-tui"
  tar -czf "$RELEASE/wtree-tui_${ver}_$host.tar.gz" -C "$work" wtree-tui
  (cd "$RELEASE" && sha256sum wtree "wtree-tui_${ver}_$host.tar.gz" >checksums.txt)
  ARCHIVE=$RELEASE/wtree-tui_${ver}_$host.tar.gz
}

host_target() {
  local os arch
  case $(uname -s) in
    Linux) os=linux ;;
    Darwin) os=darwin ;;
    *) broken "this host is not linux or darwin" ;;
  esac
  case $(uname -m) in
    x86_64 | amd64) arch=amd64 ;;
    aarch64 | arm64) arch=arm64 ;;
    *) broken "this architecture has no target" ;;
  esac
  printf '%s_%s' "$os" "$arch"
}

# the script is read from stdin, as with curl | sh
run_install() {
  RC=0
  OUT=$(WTREE_BIN_DIR=$T/bin WTREE_RELEASE_URL=file://$T/rel WTREE_RAW_URL=file://$T/raw/wtree \
    timeout -k 1 25 "${INSTALL_SH:-dash}" -s -- "$@" <"$TEST_ROOT/../install.sh" 2>&1) || RC=$?
}

assert_nothing_left() {
  local left
  left=$(find "$T/bin" -mindepth 1 -name '.wtree*')
  [[ -z $left ]] || fail "temporary files stay: $left"
}

test_install_gets_the_script_and_the_program() {
  fake_release
  export WTREE_VERSION=v9.9.9
  run_install
  assert_rc 0
  assert_contains "Installed $T/bin/wtree-tui"
  assert_contains "Installed $T/bin/wtree v9.9.9"
  assert_eq "$("$T/bin/wtree")" "fake script v9.9.9"
  assert_eq "$("$T/bin/wtree-tui")" "fake tui v9.9.9"
  assert_nothing_left
}

test_install_runs_in_bash_too() {
  fake_release
  export WTREE_VERSION=v9.9.9
  INSTALL_SH=bash run_install
  assert_rc 0
  assert_eq "$("$T/bin/wtree-tui")" "fake tui v9.9.9"
}

test_install_accepts_a_version_with_no_v() {
  fake_release
  export WTREE_VERSION=9.9.9
  run_install
  assert_rc 0
  assert_eq "$("$T/bin/wtree-tui")" "fake tui v9.9.9"
}

test_install_wrong_archive_checksum_gives_the_script_only() {
  fake_release
  printf 'other' >>"$ARCHIVE"
  export WTREE_VERSION=v9.9.9
  mkdir -p "$T/bin"
  printf 'old' >"$T/bin/wtree-tui"
  run_install
  assert_rc 0
  assert_contains "fzf UI only: the checksum of"
  assert_contains "Removed the old"
  assert_eq "$("$T/bin/wtree")" "fake script v9.9.9"
  assert_no_dir "$T/bin/wtree-tui"
  assert_nothing_left
}

test_install_wrong_script_checksum_installs_nothing() {
  fake_release
  printf '# other\n' >>"$RELEASE/wtree"
  export WTREE_VERSION=v9.9.9
  printf 'old script' >"$T/bin/wtree"
  printf 'old program' >"$T/bin/wtree-tui"
  run_install
  assert_rc 1
  assert_contains "nothing was installed"
  assert_eq "$(cat "$T/bin/wtree")" "old script"
  assert_eq "$(cat "$T/bin/wtree-tui")" "old program"
  assert_nothing_left
}

test_install_archive_with_no_line_in_the_checksums_gives_the_script_only() {
  fake_release
  sed -i '/tar.gz/d' "$RELEASE/checksums.txt"
  export WTREE_VERSION=v9.9.9
  run_install
  assert_rc 0
  assert_contains "fzf UI only: the checksum of"
  assert_no_dir "$T/bin/wtree-tui"
}

old_install() {
  mkdir -p "$T/bin"
  printf 'old script\n' >"$T/bin/wtree"
  printf 'old program\n' >"$T/bin/wtree-tui"
}

assert_old_install() {
  assert_eq "$(cat "$T/bin/wtree")" "old script"
  assert_eq "$(cat "$T/bin/wtree-tui")" "old program"
  assert_nothing_left
}

# the script holds all the rules of wtree: it does not come in unchecked
test_install_no_checksums_installs_nothing() {
  fake_release
  old_install
  rm "$RELEASE/checksums.txt"
  printf '#!/bin/sh\necho TAMPERED\n' >"$RELEASE/wtree"
  export WTREE_VERSION=v9.9.9
  run_install
  [[ $RC != 0 ]] || fail "it installed what it cannot check: $OUT"
  assert_contains "cannot be verified"
  assert_contains "Nothing was changed"
  assert_not_contains "Installed"
  assert_old_install
}

test_install_with_no_hash_tool_installs_nothing() {
  fake_release
  old_install
  local dir tool
  mkdir -p "$T/nohash"
  for dir in ${PATH//:/ }; do
    for tool in "$dir"/*; do
      case ${tool##*/} in
        sha256sum | shasum) ;;
        *) if [[ ! -e $T/nohash/${tool##*/} ]]; then ln -s "$tool" "$T/nohash/${tool##*/}"; fi ;;
      esac
    done
  done
  export WTREE_VERSION=v9.9.9
  PATH=$T/nohash run_install
  [[ $RC != 0 ]] || fail "it installed what it cannot check: $OUT"
  assert_contains "no sha256sum or shasum"
  assert_old_install
}

test_install_failed_lookup_of_the_latest_changes_nothing() {
  mkdir -p "$T/raw"
  old_install
  printf '#!/bin/sh\necho MAIN-UNVERIFIED\n' >"$T/raw/wtree"
  mkdir -p "$T/fakecurl"
  cat >"$T/fakecurl/curl" <<FAKE
#!/bin/sh
case " \$* " in *url_effective*) exit 7 ;; esac
exec $WTREE_TEST_SCRATCH/tools/curl "\$@"
FAKE
  chmod +x "$T/fakecurl/curl"
  PATH=$T/fakecurl:$PATH run_install
  [[ $RC != 0 ]] || fail "a failure of the network counted as no release: $OUT"
  assert_contains "cannot read the latest release"
  assert_not_contains "no release yet"
  assert_old_install
}

test_install_a_tag_that_makes_no_sense_changes_nothing() {
  old_install
  fake_redirect 'https://github.com/afruth/wtree/releases/tag/v1;rm'
  run_install
  [[ $RC != 0 ]] || fail "$OUT"
  assert_old_install
}

test_install_missing_archive_gives_the_script_only() {
  fake_release
  rm "$ARCHIVE"
  export WTREE_VERSION=v9.9.9
  run_install
  assert_rc 0
  assert_contains "fzf UI only: the download of"
  assert_eq "$("$T/bin/wtree")" "fake script v9.9.9"
}

test_install_unknown_platform_gives_the_script_only() {
  fake_release
  mkdir -p "$T/fakebin"
  printf '#!/bin/sh\ncase "$1" in -s) echo Plan9 ;; *) echo mips ;; esac\n' >"$T/fakebin/uname"
  chmod +x "$T/fakebin/uname"
  export WTREE_VERSION=v9.9.9 PATH=$T/fakebin:$PATH
  run_install
  assert_rc 0
  assert_contains "fzf UI only: no wtree-tui for Plan9 mips"
  assert_eq "$("$T/bin/wtree")" "fake script v9.9.9"
}

test_install_program_that_cannot_run_gives_the_script_only() {
  fake_release
  local work=$T/archive
  printf 'not a program' >"$work/wtree-tui"
  chmod +x "$work/wtree-tui"
  tar -czf "$ARCHIVE" -C "$work" wtree-tui
  (cd "$RELEASE" && sha256sum wtree "${ARCHIVE##*/}" >checksums.txt)
  export WTREE_VERSION=v9.9.9
  run_install
  assert_rc 0
  assert_contains "fzf UI only: wtree-tui does not run on this host"
  assert_no_dir "$T/bin/wtree-tui"
}

test_install_no_tui_flag_removes_an_old_program() {
  fake_release
  export WTREE_VERSION=v9.9.9
  printf 'old' >"$T/bin/wtree-tui"
  run_install --no-tui
  assert_rc 0
  assert_contains "fzf UI only: --no-tui"
  assert_no_dir "$T/bin/wtree-tui"
}

test_install_bad_argument_stops() {
  run_install --what
  assert_rc 1
  assert_contains "unknown argument: --what"
}

test_install_missing_tag_stops_and_keeps_what_is_there() {
  mkdir -p "$T/bin" "$T/rel"
  printf 'old script' >"$T/bin/wtree"
  export WTREE_VERSION=v1.0.0
  run_install
  assert_rc 1
  assert_contains "the download of wtree v1.0.0 failed"
  assert_eq "$(cat "$T/bin/wtree")" "old script"
}

# curl reads file:// for all but the redirect of releases/latest
fake_redirect() {
  mkdir -p "$T/fakecurl"
  cat >"$T/fakecurl/curl" <<FAKE
#!/bin/sh
case " \$* " in *url_effective*) printf '%s' "$1"; exit 0 ;; esac
exec $WTREE_TEST_SCRATCH/tools/curl "\$@"
FAKE
  chmod +x "$T/fakecurl/curl"
  export PATH=$T/fakecurl:$PATH
}

test_install_latest_follows_the_redirect_to_a_tag() {
  fake_release v9.9.9
  fake_redirect "https://github.com/afruth/wtree/releases/tag/v9.9.9"
  run_install
  assert_rc 0
  assert_contains "Installed $T/bin/wtree v9.9.9"
  assert_eq "$("$T/bin/wtree-tui")" "fake tui v9.9.9"
}

test_install_with_no_release_takes_the_script_of_main() {
  mkdir -p "$T/raw" "$T/bin"
  printf '#!/usr/bin/env bash\necho script of main\n' >"$T/raw/wtree"
  fake_redirect "https://github.com/afruth/wtree/releases"
  run_install
  assert_rc 0
  assert_contains "fzf UI only: this repo has no release yet"
  assert_contains "is not verified"
  assert_eq "$("$T/bin/wtree")" "script of main"
}

test_install_says_when_the_directory_is_not_on_path() {
  fake_release
  export WTREE_VERSION=v9.9.9
  run_install
  assert_contains "Add $T/bin to your PATH."
}
