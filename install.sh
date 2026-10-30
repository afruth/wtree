#!/bin/sh
set -eu

url=https://raw.githubusercontent.com/afruth/wtree/main/bin/wtree
dir=${WTREE_BIN_DIR:-$HOME/.local/bin}

command -v curl >/dev/null 2>&1 || { echo "wtree: curl is not on PATH" >&2; exit 1; }
command -v bash >/dev/null 2>&1 || { echo "wtree: bash is not on PATH" >&2; exit 1; }

mkdir -p "$dir"
tmp=$(mktemp "$dir/.wtree.XXXXXX")
trap 'rm -f "$tmp"' EXIT
curl -fsSL "$url" -o "$tmp"
chmod 755 "$tmp"
mv "$tmp" "$dir/wtree"
trap - EXIT
echo "Installed $dir/wtree"

command -v git >/dev/null 2>&1 || echo "Install git: wtree needs it."
command -v fzf >/dev/null 2>&1 || echo "Install fzf: wtree needs it."
command -v docker >/dev/null 2>&1 || echo "Install Docker if you want to use wtree -d."
case ":$PATH:" in
  *":$dir:"*) ;;
  *) echo "Add $dir to your PATH." ;;
esac
