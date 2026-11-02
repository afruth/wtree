#!/bin/sh
# Installs wtree and, where a release has one for this host, wtree-tui. Use: curl ... | sh
# Options: --no-tui. Environment: WTREE_BIN_DIR, WTREE_VERSION (a tag such as v0.5.0).
# All of it is in functions, so a download that breaks off runs nothing, and nothing eats stdin.
set -eu

BASE=${WTREE_RELEASE_URL:-https://github.com/afruth/wtree/releases}
RAW=${WTREE_RAW_URL:-https://raw.githubusercontent.com/afruth/wtree/main/bin/wtree}
DIR=${WTREE_BIN_DIR:-$HOME/.local/bin}
WORK=

say() { printf '%s\n' "$*"; }
die() {
  printf 'wtree: %s\n' "$*" >&2
  exit 1
}

fetch() { curl -fsSL "$1" -o "$2"; }

# GitHub sends releases/latest to the page of the tag, or to the list if there is no release.
# It returns 1 only when GitHub said that there is no release, and 2 when it did not say.
latest_tag() {
  url=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "$BASE/latest" 2>/dev/null) || return 2
  case $url in
    */releases/tag/*) ;;
    *) return 1 ;;
  esac
  tag=${url##*/}
  case $tag in
    "" | *[!A-Za-z0-9._-]*) return 2 ;;
  esac
  printf '%s\n' "$tag"
}

sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | cut -d' ' -f1
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | cut -d' ' -f1
  else
    return 1
  fi
}

can_verify() {
  command -v sha256sum >/dev/null 2>&1 || command -v shasum >/dev/null 2>&1
}

# args: file, its name in checksums.txt. A file with no line in checksums.txt fails.
verified() {
  want=$(awk -v name="$2" '$2 == name || $2 == "*" name { print $1; exit }' "$WORK/checksums.txt")
  got=$(sha256_of "$1") || return 1
  [ -n "$want" ] && [ "$want" = "$got" ]
}

platform() {
  case $(uname -s) in
    Linux) os=linux ;;
    Darwin) os=darwin ;;
    *) return 1 ;;
  esac
  case $(uname -m) in
    x86_64 | amd64) arch=amd64 ;;
    aarch64 | arm64) arch=arm64 ;;
    *) return 1 ;;
  esac
  printf '%s_%s\n' "$os" "$arch"
}

# args: tag. It leaves the program in $WORK/x/wtree-tui, or sets REASON and returns 1.
fetch_tui() {
  command -v tar >/dev/null 2>&1 || {
    REASON="tar is not on PATH"
    return 1
  }
  target=$(platform) || {
    REASON="no wtree-tui for $(uname -s) $(uname -m)"
    return 1
  }
  name=wtree-tui_${1#v}_$target.tar.gz
  fetch "$BASE/download/$1/$name" "$WORK/$name" 2>/dev/null || {
    REASON="the download of $name failed"
    return 1
  }
  verified "$WORK/$name" "$name" || {
    REASON="the checksum of $name is wrong"
    return 1
  }
  mkdir "$WORK/x"
  tar -xzf "$WORK/$name" -C "$WORK/x" wtree-tui 2>/dev/null || {
    REASON="$name has no wtree-tui in it"
    return 1
  }
  if [ ! -f "$WORK/x/wtree-tui" ] || [ -L "$WORK/x/wtree-tui" ]; then
    REASON="$name has no wtree-tui in it"
    return 1
  fi
  chmod 755 "$WORK/x/wtree-tui"
  # Gatekeeper can stop an unsigned program that curl fetched
  if [ "${target%_*}" = darwin ] && command -v xattr >/dev/null 2>&1; then
    xattr -d com.apple.quarantine "$WORK/x/wtree-tui" 2>/dev/null || true
  fi
  "$WORK/x/wtree-tui" --version </dev/null >/dev/null 2>&1 || {
    REASON="wtree-tui does not run on this host"
    return 1
  }
}

main() {
  no_tui=
  for arg in "$@"; do
    case $arg in
      --no-tui) no_tui=1 ;;
      *) die "unknown argument: $arg. The option is --no-tui" ;;
    esac
  done

  command -v curl >/dev/null 2>&1 || die "curl is not on PATH"
  command -v bash >/dev/null 2>&1 || die "bash is not on PATH"

  mkdir -p "$DIR"
  WORK=$(mktemp -d "$DIR/.wtree-install.XXXXXX")
  trap 'rm -rf "$WORK"' EXIT
  trap 'exit 130' HUP INT TERM

  # the script and the program of one tag always match. Only with no release at all, take main.
  tag=${WTREE_VERSION:-}
  if [ -n "$tag" ]; then
    case $tag in
      v*) ;;
      *) tag=v$tag ;;
    esac
  else
    status=0
    tag=$(latest_tag) || status=$?
    case $status in
      0) ;;
      1) tag= ;;
      *) die "cannot read the latest release from $BASE, so nothing was changed. Try again, or set WTREE_VERSION" ;;
    esac
  fi

  REASON="this repo has no release yet"
  if [ -z "$tag" ]; then
    fetch "$RAW" "$WORK/wtree" || die "the download of $RAW failed"
    say "Warning: with no release there are no checksums, so the script of the main branch is not verified."
  else
    # This script holds all the safety rules of wtree, so it is never installed unchecked.
    fetch "$BASE/download/$tag/wtree" "$WORK/wtree" || die "the download of wtree $tag failed"
    can_verify || die "this host has no sha256sum or shasum, so wtree $tag cannot be verified. Nothing was changed"
    fetch "$BASE/download/$tag/checksums.txt" "$WORK/checksums.txt" 2>/dev/null ||
      die "cannot read the checksums of wtree $tag, so it cannot be verified. Nothing was changed"
    verified "$WORK/wtree" wtree || die "the checksum of wtree $tag is wrong, so nothing was installed"
    if [ -n "$no_tui" ]; then
      REASON="--no-tui"
    elif fetch_tui "$tag"; then
      REASON=
    fi
  fi

  chmod 755 "$WORK/wtree"
  if [ -z "$REASON" ]; then
    mv "$WORK/x/wtree-tui" "$DIR/wtree-tui"
    say "Installed $DIR/wtree-tui"
  else
    say "fzf UI only: $REASON."
    # an old program must not stay beside a new script
    if [ -e "$DIR/wtree-tui" ]; then
      rm -f "$DIR/wtree-tui"
      say "Removed the old $DIR/wtree-tui."
    fi
  fi
  mv "$WORK/wtree" "$DIR/wtree"
  say "Installed $DIR/wtree${tag:+ $tag}"

  command -v git >/dev/null 2>&1 || say "Install git: wtree needs it."
  if [ -n "$REASON" ]; then
    command -v fzf >/dev/null 2>&1 || say "Install fzf 0.53 or later: wtree needs it without wtree-tui."
  fi
  command -v docker >/dev/null 2>&1 || say "Install Docker if you want to see stacks."
  case ":$PATH:" in
    *":$DIR:"*) ;;
    *) say "Add $DIR to your PATH." ;;
  esac
}

main "$@"
