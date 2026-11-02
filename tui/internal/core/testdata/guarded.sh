#!/usr/bin/env bash
# Runs the cases of record_cases.sh behind the guard of test/run.sh. See README.md.
#
#   guarded.sh record   record the fixtures in this directory again
#   guarded.sh test     run guarded_test.go against the real script
set -euo pipefail

HERE=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT=$(cd "$HERE/../../../.." && pwd -P)
MODE=${1:-}
case $MODE in
  record | test) ;;
  *)
    printf 'usage: guarded.sh record|test\n' >&2
    exit 2
    ;;
esac

WORK=$(mktemp -d -t wtree-wp2.XXXXXXXX)
trap 'rm -rf "$WORK"' EXIT
cp -a "$ROOT/test" "$WORK/test"
mkdir -p "$WORK/out"

if [[ $MODE == test ]]; then
  (cd "$ROOT/tui" && go test -c -o "$WORK/core.test" ./internal/core)
fi

{
  printf 'WP2_OUT=%q\nWP2_BIN=%q\n' "$WORK/out" "$WORK/core.test"
  cat "$HERE/record_cases.sh"
} >"$WORK/test/cases/99_wp2.sh"

# run.sh checks the guard before the first test, and it stops if the guard is wrong
WTREE_TEST_BIN=$ROOT/bin/wtree WTREE_TEST_JOBS=4 "$WORK/test/run.sh" "^test_wp2_${MODE}_"

if [[ $MODE == record ]]; then
  python3 -I "$HERE/normalize.py" "$WORK/out" "$HERE"
fi
