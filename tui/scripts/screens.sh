#!/usr/bin/env bash
# Screenshots of the real interface, for a human to look at. It runs the real program on recorded
# data in a private tmux at each size, captures what the terminal holds, and renders that with a
# headless browser. Nothing here is drawn by hand: each image is a capture of the app.
#
#   tui/scripts/screens.sh [OUT_DIR]          (default: dist/screens)
#   SCREENS_SCENES='loaded phrase' SCREENS_SIZES='48x40' SCREENS_THEMES=dark tui/scripts/screens.sh
#
# The program is wtree-demo: the model of wtree-tui with core.FakeClient. It starts no process, so
# it cannot reach docker or git. tmux uses a socket of its own, and the environment of the pane is
# clean. The script exits 1 if a scene does not come up or an image is not made.
set -uo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
OUT=${1:-$ROOT/dist/screens}
SIZES=(${SCREENS_SIZES:-120x40 100x30 80x24 60x20 48x23 48x40 93x14})
THEMES=(${SCREENS_THEMES:-dark light})
SCENES=(${SCREENS_SCENES:-loading loaded rich machine filtered action_sheet consequences phrase applying_failure results cleanup help docker_down})
JOBS=${SCREENS_JOBS:-6}

die() {
  printf 'screens.sh: %s\n' "$*" >&2
  exit 1
}

for tool in go tmux; do
  command -v "$tool" >/dev/null 2>&1 || die "$tool is not on PATH"
done
mkdir -p "$OUT/ansi" "$OUT/html" "$OUT/png" || die "cannot write to $OUT"
OUT=$(cd "$OUT" && pwd -P)
WORK=$(mktemp -d -t wtree-screens.XXXXXXXX) || die "mktemp failed"
SOCKET=wtree-screens-$$
export TMUX_TMPDIR=$WORK
unset TMUX
TMUX_BIN=$(command -v tmux)

t() {
  "$TMUX_BIN" -L "$SOCKET" -f /dev/null "$@"
}

cleanup() {
  t kill-server >/dev/null 2>&1
  rm -rf "$WORK"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

(cd "$ROOT/tui" && go build -buildvcs=false -o "$WORK/wtree-demo" ./cmd/wtree-demo && go build -buildvcs=false -o "$WORK/ansi2html" ./cmd/ansi2html) || die "the tools do not build"

# ---- the browser

browser_args() {
  local path=$1
  case $path in
    *headless-shell*) printf '%s\n' --no-sandbox --disable-gpu --hide-scrollbars ;;
    *) printf '%s\n' --headless=new --no-sandbox --disable-gpu --hide-scrollbars ;;
  esac
}

# args: html, png, width, height, scale
shoot() {
  local html=$1 png=$2 w=$3 h=$4 scale=${5:-2} args
  mapfile -t args < <(browser_args "$BROWSER")
  rm -f "$png"
  timeout 60 "$BROWSER" "${args[@]}" --user-data-dir="$WORK/profile-$BASHPID" --force-device-scale-factor="$scale" \
    --window-size="$w,$h" --screenshot="$png" "file://$html" >"$WORK/browser-$BASHPID.log" 2>&1
  rm -rf "$WORK/profile-$BASHPID"
  [[ -s $png ]]
}

find_browser() {
  local candidate probe=$OUT/.probe
  mkdir -p "$probe"
  printf '<!doctype html><body>probe</body>' >"$probe/probe.html"
  for candidate in /snap/bin/chromium \
    "$HOME"/.cache/puppeteer/chrome-headless-shell/*/*/chrome-headless-shell \
    "$HOME"/.cache/puppeteer/chrome/*/*/chrome; do
    [[ -x $candidate ]] || continue
    BROWSER=$candidate
    # a snap browser can refuse this directory, and that shows only here
    if shoot "$probe/probe.html" "$probe/probe.png" 400 300 1; then
      rm -rf "$probe"
      return 0
    fi
    printf 'screens.sh: %s made no image, trying the next browser\n' "$candidate" >&2
  done
  rm -rf "$probe"
  die "no headless browser works: looked for /snap/bin/chromium and for ~/.cache/puppeteer"
}

find_browser
printf 'browser: %s\n' "$BROWSER"
if command -v fc-match >/dev/null 2>&1; then
  printf 'font:    %s\n' "$(fc-match -f '%{family[0]}' 'JetBrains Mono')"
fi

# ---- the scenes

# the cells of a line of the capture, without the panel on its right
reverse_lines() {
  t capture-pane -e -p -t "$1" 2>/dev/null | grep -aE $'\x1b\\[([0-9;:]*;)?7(;[0-9;:]*)?m' |
    sed -E $'s/\x1b\\[[0-9;:]*[A-Za-z]//g; s/│.*//' | grep -v '^ wtree ' | grep -v 'NAME'
}

screen_text() {
  t capture-pane -p -t "$1" 2>/dev/null
}

wait_text() {
  local session=$1 text=$2 tries=0
  until screen_text "$session" | grep -Fq -- "$text"; do
    tries=$((tries + 1))
    if ((tries > 200)); then
      printf 'screens.sh: %s: the screen never showed [%s]\n' "$session" "$text" >&2
      screen_text "$session" >&2
      return 1
    fi
    sleep 0.03
  done
}

move_to() {
  local session=$1 name=$2 tries key=j before
  for ((tries = 0; tries < 60; tries++)); do
    reverse_lines "$session" | grep -Fq -- "$name" && return 0
    before=$(reverse_lines "$session")
    t send-keys -t "$session" "$key"
    sleep 0.05
    if [[ $(reverse_lines "$session") == "$before" ]]; then key=k; fi
  done
  printf 'screens.sh: %s: the cursor never came to [%s]\n' "$session" "$name" >&2
  return 1
}

# presses the digit that the sheet prints in front of this label
pick() {
  local session=$1 digit
  digit=$(screen_text "$session" | grep -oE "\\([0-9]\\) $2" | head -n 1 | cut -c2)
  [[ -n $digit ]] || {
    printf 'screens.sh: %s: no choice [%s]\n' "$session" "$2" >&2
    return 1
  }
  t send-keys -t "$session" "$digit"
}

scene_flags() {
  case $1 in
    loading) echo '-scene recorded -list-pause -1' ;;
    rich) echo '-scene repo' ;;
    machine) echo '-scene machine -g' ;;
    applying_failure) echo '-scene recorded -apply apply-failed.ndjson -apply-pause 6' ;;
    results) echo '-scene recorded -apply apply-failed.ndjson' ;;
    docker_down) echo '-scene docker-down' ;;
    *) echo '-scene recorded' ;;
  esac
}

# the sheet with the phrase, from a stack row, with the worktrees
to_phrase() {
  local s=$1
  wait_text "$s" "proj-gone" && move_to "$s" "proj-gone" || return 1
  t send-keys -t "$s" d
  wait_text "$s" "GOES" || return 1
  pick "$s" "with volumes" || return 1
  sleep 0.2
  pick "$s" "${2:-stacks only}" || return 1
  sleep 0.5 # what confirms a sheet waits until it has held still for a moment
  t send-keys -t "$s" y
  wait_text "$s" "Type "
}

play() {
  local s=$1 scene=$2
  case $scene in
    loading) wait_text "$s" "loading worktrees" ;;
    loaded) wait_text "$s" "alpha" ;;
    rich | machine)
      # the count in the header says that the names are in, at each width
      local tries=0
      until screen_text "$s" | grep -Eq '[0-9]+ (wt|worktrees)'; do
        tries=$((tries + 1))
        ((tries < 200)) || return 1
        sleep 0.03
      done
      ;;
    filtered)
      wait_text "$s" "alpha" || return 1
      t send-keys -t "$s" /
      t send-keys -t "$s" -l alpha
      t send-keys -t "$s" Enter
      sleep 0.1
      ;;
    action_sheet)
      wait_text "$s" "alpha" && move_to "$s" "alpha" || return 1
      t send-keys -t "$s" a
      wait_text "$s" "0 cancel"
      ;;
    consequences)
      wait_text "$s" "alpha" && move_to "$s" "alpha" || return 1
      t send-keys -t "$s" d
      wait_text "$s" "GOES"
      ;;
    phrase) to_phrase "$s" ;;
    applying_failure | results)
      to_phrase "$s" "and their worktrees" || return 1
      t send-keys -t "$s" -l "2 stacks w"
      t send-keys -t "$s" Enter
      wait_text "$s" "failed: volume" || return 1
      if [[ $scene == results ]]; then wait_text "$s" "skipped"; fi
      ;;
    cleanup)
      wait_text "$s" "alpha" || return 1
      t send-keys -t "$s" c
      wait_text "$s" "c back"
      ;;
    help)
      wait_text "$s" "alpha" || return 1
      t send-keys -t "$s" '?'
      wait_text "$s" "? close"
      ;;
    docker_down) wait_text "$s" "docker is not running" ;;
    *) return 1 ;;
  esac
}

start_app() {
  local session=$1 cols=$2 rows=$3 theme=$4 flags=$5 light=''
  [[ $theme == light ]] && light=-light
  t new-session -d -s "$session" -x "$cols" -y "$rows" \
    "env -i PATH=/usr/bin:/bin HOME=$WORK TERM=tmux-256color COLORTERM=truecolor LC_ALL=C.UTF-8 $WORK/wtree-demo $flags $light; sleep 300" >/dev/null
}

# -N keeps the spaces at the end of a line: the cursor bar has them, in reverse video
capture() {
  t capture-pane -e -N -p -t "$1"
}

to_html() {
  local name=$1 theme=$2 cols=$3 rows=$4
  shift 4
  "$WORK/ansi2html" -theme "$theme" -cols "$cols" -rows "$rows" -title "$name" "$@" <"$OUT/ansi/$name.ansi" >"$OUT/html/$name.html"
}

# the pixels of the grid: 0.6 em for each column of 14 px, 18 px for each row, 8 px on each side
shoot_grid() {
  local name=$1 cols=$2 rows=$3
  shoot "$OUT/html/$name.html" "$OUT/png/$name.png" "$(((cols * 84 + 9) / 10 + 16))" "$((rows * 18 + 16))" 2
}

one() {
  local scene=$1 size=$2 theme=$3 cols=${2%x*} rows=${2#*x} name session
  name=${scene}_${size}_${theme}
  session=${name//[^a-z0-9]/_}
  start_app "$session" "$cols" "$rows" "$theme" "$(scene_flags "$scene")" || return 1
  if ! play "$session" "$scene"; then
    t kill-session -t "$session" 2>/dev/null
    return 1
  fi
  sleep 0.25
  capture "$session" >"$OUT/ansi/$name.ansi"
  t kill-session -t "$session" 2>/dev/null
  to_html "$name" "$theme" "$cols" "$rows" && shoot_grid "$name" "$cols" "$rows"
}

rotation() {
  local theme=$1 name=rotation_93x14_to_48x41_$theme session
  session=${name//[^a-z0-9]/_}
  start_app "$session" 93 14 "$theme" "-scene recorded" || return 1
  wait_text "$session" "alpha" || return 1
  sleep 0.2
  t resize-window -t "$session" -x 48 -y 41
  sleep 0.6
  capture "$session" >"$OUT/ansi/$name.ansi"
  t kill-session -t "$session" 2>/dev/null
  to_html "$name" "$theme" 48 41 && shoot_grid "$name" 48 41
}

# a page of the size of a phone, with the font that its width allows. args: scene, size, theme,
# width, height, keyboard height
phone() {
  local scene=$1 size=$2 theme=$3 w=$4 h=$5 kb=${6:-0} cols=${2%x*} rows=${2#*x} name=${1}_${2}_${3}
  local page=phone_${name}
  [[ -s $OUT/ansi/$name.ansi ]] || return 1
  cp "$OUT/ansi/$name.ansi" "$OUT/ansi/$page.ansi"
  to_html "$page" "$theme" "$cols" "$rows" -phone-width "$w" -phone-height "$h" -keyboard "$kb" &&
    shoot "$OUT/html/$page.html" "$OUT/png/$page.png" "$w" "$h" 3
  rm -f "$OUT/ansi/$page.ansi"
}

t start-server \; set-option -s escape-time 0 >/dev/null 2>&1
# a session that keeps the server up while the others come and go
t new-session -d -s keep -x 20 -y 5 "sleep 3600" \; set-option -s escape-time 0 >/dev/null || die "tmux did not start"

failed=0
running=0
run_job() {
  "$@" || {
    printf 'FAILED: %s\n' "$*" >&2
    exit 1
  }
}
reap() {
  wait -n || failed=$((failed + 1))
  running=$((running - 1))
}
spawn() {
  while ((running >= JOBS)); do reap; done
  run_job "$@" &
  running=$((running + 1))
}

for theme in "${THEMES[@]}"; do
  for scene in "${SCENES[@]}"; do
    for size in "${SIZES[@]}"; do
      spawn one "$scene" "$size" "$theme"
    done
  done
  spawn rotation "$theme"
done
while ((running > 0)); do reap; done

# phones: an iPhone 15 is 390x844 points. The sizes are those that the tiers of the design name.
for theme in "${THEMES[@]}"; do
  for scene in "${SCENES[@]}"; do
    case " ${SIZES[*]} " in
      *" 48x40 "*) spawn phone "$scene" 48x40 "$theme" 390 844 ;;
    esac
    case " ${SIZES[*]} " in
      *" 48x23 "*) spawn phone "$scene" 48x23 "$theme" 390 844 336 ;;
    esac
    case " ${SIZES[*]} " in
      *" 93x14 "*) spawn phone "$scene" 93x14 "$theme" 844 390 ;;
    esac
  done
done
while ((running > 0)); do reap; done

# a page to look through them
{
  printf '<!doctype html><meta charset="utf-8"><title>wtree screens</title><style>body{font:14px sans-serif;background:#222;color:#ddd}img{max-width:100%%;display:block;margin:4px 0 24px;border:1px solid #555}</style>\n'
  for png in "$OUT"/png/*.png; do
    printf '<div>%s<img loading="lazy" src="png/%s"></div>\n' "$(basename "$png")" "$(basename "$png")"
  done
} >"$OUT/index.html"

count=$(find "$OUT/png" -name '*.png' -size +0 | wc -l)
printf '%d images in %s/png, %d of them phone-sized, %d failures. Index: %s/index.html\n' \
  "$count" "$OUT" "$(find "$OUT/png" -name 'phone_*.png' -size +0 | wc -l)" "$failed" "$OUT"
((failed == 0))
