# What --list and --stack-list show. These are not interactive, so COLUMNS sets the width.
# shellcheck shell=bash

setup_list_fixture() {
  make_repo proj
  add_wt alpha
  stack proj-alpha "$WT" running 1.5 100MiB
  stack proj-alpha "$WT" running 2.0 200MiB
  stack proj-alpha "$WT" exited
  volume proj-alpha proj-alpha_data
  add_detached_wt detached
  add_wt lost
  rm -rf "$WT"
  stack proj-main "$REPO" exited
  volume proj-main proj-main_data
  volume proj-main proj-main_cache
  stack proj-old "$HOME/gone/place" exited
  volume proj-unused proj-unused_data
  volume stranger stranger_data
  make_repo elsewhere
  stack elsewhere "$REPO" running
  REPO=$HOME/code/proj
  COMMON=$REPO/.git
  cd "$REPO" || exit 2
}

test_golden_worktree_list_wide() {
  setup_list_fixture
  COLUMNS=120 wt --list
  assert_rc 0
  assert_golden worktree-list-wide
}

test_golden_worktree_list_narrow() {
  setup_list_fixture
  COLUMNS=50 wt --list
  assert_rc 0
  assert_golden worktree-list-narrow
}

test_golden_stack_list_wide() {
  setup_list_fixture
  COLUMNS=120 wt --stack-list
  assert_rc 0
  assert_golden stack-list-wide
}

test_golden_stack_list_narrow() {
  setup_list_fixture
  COLUMNS=50 wt --stack-list
  assert_rc 0
  assert_golden stack-list-narrow
}

test_golden_stack_list_global() {
  setup_list_fixture
  COLUMNS=120 wt -g --stack-list
  assert_rc 0
  assert_golden stack-list-global
}

test_list_rows_are_nul_separated_with_path_and_repo_first() {
  make_repo
  add_wt alpha
  wt --list
  local rows
  rows=$(tr -cd '\0' <"$T/out" | wc -c | tr -d ' ')
  assert_eq "$rows" 2 "rows"
  assert_contains "$WT	$COMMON	"
  assert_contains "$REPO	$COMMON	"
}

test_list_shows_stack_counts_on_the_right_worktree() {
  make_repo
  add_wt alpha
  stack p "$WT/sub" running
  stack p "$WT/sub" exited
  wt --list
  assert_contains "alpha  docker: p 1/2 up"
}

test_list_of_nested_worktrees_uses_the_longest_path() {
  local outer inner
  make_repo
  add_wt outer
  outer=$WT
  inner=$outer/nested
  must git -C "$REPO" worktree add -q -b inner "$inner"
  stack innerstack "$inner" running
  wt --list
  assert_contains "nested  docker: innerstack 1/1 up"
  if grep -q 'outer  docker' <<<"$OUT"; then fail "the outer worktree shows the stack"; fi
}

test_list_marks_a_missing_directory() {
  make_repo
  add_wt alpha
  rm -rf "$WT"
  wt --list
  assert_contains "[missing]"
}

test_list_works_when_docker_fails() {
  make_repo
  add_wt alpha
  shim_fail 'ps *'
  wt --list
  assert_rc 0
  assert_contains "alpha"
  assert_not_contains "docker:"
}

test_list_opens_within_the_watchdog_when_docker_hangs() {
  local started=$SECONDS
  make_repo
  add_wt alpha
  shim_hang 'ps *'
  WT_TIMEOUT=15 wt --list
  assert_rc 0
  assert_contains "alpha"
  ((SECONDS - started < 10)) || fail "it took $((SECONDS - started)) seconds"
}

test_list_outside_a_repo_is_an_error() {
  mkdir -p "$HOME/plain"
  cd "$HOME/plain" || exit 2
  wt --list
  assert_rc 1
  assert_contains "not a git repository"
}

test_bare_entry_appears_without_g_and_not_with_g() {
  make_repo
  must git clone -q --bare "$REPO" "$HOME/code/bare.git"
  must git -C "$HOME/code/bare.git" worktree add -q "$HOME/code/bare-wt" -b wt
  cd "$HOME/code/bare-wt" || exit 2
  wt --list
  assert_contains "$HOME/code/bare.git	$HOME/code/bare.git"
  wt -g --list
  assert_not_contains "$HOME/code/bare.git	$HOME/code/bare.git"
  assert_contains "$HOME/code/bare-wt	"
  assert_contains "bare-wt  bare"
}

test_global_list_finds_repos_under_the_roots() {
  make_repo one
  make_repo two
  add_wt extra
  cd "$HOME" || exit 2
  wt -g --list
  assert_rc 0
  assert_contains "$HOME/code/one	"
  assert_contains "$HOME/code/two	"
  assert_contains "extra  two"
}

test_global_list_works_outside_a_repo() {
  make_repo
  cd "$T" || exit 2
  wt -g --list
  assert_rc 0
  assert_contains "$REPO	"
}

test_global_list_skips_a_submodule() {
  make_repo inner
  local inner=$REPO
  make_repo outer
  must git -c protocol.file.allow=always -C "$REPO" submodule add -q "$inner" vendored
  must git -C "$REPO" commit -q -am submodule
  cd "$HOME" || exit 2
  wt -g --list
  assert_contains "$REPO	"
  assert_contains "$inner	"
  assert_not_contains "vendored"
}

test_global_list_skips_pruned_directories() {
  make_repo real
  make_repo hidden
  mkdir -p "$HOME/code/node_modules"
  mv "$REPO" "$HOME/code/node_modules/hidden"
  cd "$HOME" || exit 2
  wt -g --list
  assert_contains "real"
  assert_not_contains "hidden"
}

test_global_stack_outside_the_roots_brings_its_repo() {
  make_repo
  add_wt alpha
  stack p "$WT" running
  export WTREE_ROOTS=$T/empty
  mkdir -p "$WTREE_ROOTS"
  cd "$T" || exit 2
  wt -g --list
  assert_contains "alpha  proj  docker: p 1/1 up"
}

test_global_cache_filters_repos_that_are_gone() {
  make_repo
  mkdir -p "$XDG_CACHE_HOME/wtree"
  printf '%s\n%s\n' "$COMMON" "$HOME/code/vanished/.git" >"$XDG_CACHE_HOME/wtree/repos"
  cd "$T" || exit 2
  wt -g --list
  assert_rc 0
  assert_contains "$REPO	"
  assert_not_contains "vanished"
}

test_fresh_repo_cache_is_used_as_it_is() {
  make_repo one
  cd "$HOME" || exit 2
  wt -g --list
  make_repo two
  cd "$HOME" || exit 2
  wt -g --list
  assert_not_contains "$HOME/code/two"
}

test_rescan_finds_new_repos_and_leaves_no_temp_file() {
  make_repo one
  cd "$HOME" || exit 2
  wt -g --list
  make_repo two
  cd "$HOME" || exit 2
  wt -g --rescan
  assert_contains "$HOME/code/two	"
  OUT=$(ls "$XDG_CACHE_HOME/wtree")
  assert_eq "$OUT" "repos"
}

test_old_repo_cache_updates_in_the_background() {
  make_repo one
  cd "$HOME" || exit 2
  wt -g --list
  make_repo two
  cd "$HOME" || exit 2
  touch -d '40 minutes ago' "$XDG_CACHE_HOME/wtree/repos"
  wt -g --list
  assert_not_contains "$HOME/code/two"
  local tries=0
  until grep -q 'two' "$XDG_CACHE_HOME/wtree/repos"; do
    tries=$((tries + 1))
    ((tries < 100)) || fail "the cache did not update"
    sleep 0.05
  done
}

test_stack_list_needs_docker() {
  make_repo
  shim_fail 'ps *'
  wt --stack-list
  [[ $RC != 0 ]] || fail "it should fail"
}

test_stack_list_keeps_stacks_of_this_repo_only() {
  setup_list_fixture
  wt --stack-list
  assert_contains "proj-alpha"
  assert_contains "proj-main"
  assert_not_contains "elsewhere"
  assert_not_contains "stranger"
}

test_stack_list_shows_gone_by_name_and_by_path() {
  make_repo proj
  stack proj-byname "$HOME/none/x" exited
  stack thing "$HOME/none/proj-feature" exited
  stack other "$HOME/none/unrelated" exited
  wt --stack-list
  grep -q 'proj-byname.*worktree gone' <<<"$OUT" || fail "proj-byname is not marked gone"
  grep -q 'thing.*worktree gone' <<<"$OUT" || fail "thing is not marked gone"
  assert_not_contains "other"
}

test_stack_list_matches_the_repo_name_in_any_case() {
  make_repo Proj
  stack PROJ-upper "$HOME/none/x" exited
  volume proj-VOL proj-VOL_data
  wt --stack-list
  grep -q 'PROJ-upper.*worktree gone' <<<"$OUT" || fail "PROJ-upper is not marked gone"
  grep -q 'proj-VOL.*volumes only' <<<"$OUT" || fail "proj-VOL is not marked volumes only"
}

test_stack_list_volumes_only_needs_the_repo_name() {
  make_repo proj
  volume proj-dev proj-dev_data
  volume projector projector_data
  volume stranger stranger_data
  wt --stack-list
  grep -q 'proj-dev.*volumes only' <<<"$OUT" || fail "proj-dev is not marked volumes only"
  assert_not_contains "projector"
  assert_not_contains "stranger"
}

test_stack_list_shows_usage_and_the_branch() {
  setup_list_fixture
  wt --stack-list
  assert_contains "CPU 3.5%  memory 300MiB"
  assert_contains "volumes 1"
  assert_contains "on alpha"
  assert_contains "running 2, exited 1"
}

test_stack_list_puts_running_stacks_first() {
  setup_list_fixture
  wt --stack-list
  local running stopped
  running=$(grep -n 'proj-alpha' <<<"$OUT" | head -n 1 | cut -d: -f1)
  stopped=$(grep -n 'proj-main' <<<"$OUT" | head -n 1 | cut -d: -f1)
  ((running < stopped)) || fail "the stopped stack comes first"
}

test_stack_list_rows_carry_project_and_directory() {
  make_repo
  add_wt alpha
  stack p "$WT" running
  wt --stack-list
  assert_contains "p	$WT	"
}

test_stack_list_has_two_header_rows_without_a_project() {
  make_repo
  wt --stack-list
  local rows
  rows=$(tr -cd '\0' <"$T/out" | wc -c | tr -d ' ')
  assert_eq "$rows" 2 "rows"
  assert_contains "Docker  CPU 0% of 800%   memory 0MiB of 8.0GiB"
  assert_contains "disk: Docker is reading the sizes, press ctrl-r later"
}

test_stack_list_reads_the_disk_cache() {
  make_repo
  add_wt alpha
  stack p "$WT" running
  mkdir -p "$XDG_CACHE_HOME/wtree"
  printf 'p\t2000000000\n' >"$XDG_CACHE_HOME/wtree/disk-projects"
  printf 'Images\t10.5GB\nContainers\t1.2GB\nLocal Volumes\t5GB\nBuild Cache\t200MB\n' >"$XDG_CACHE_HOME/wtree/disk-totals"
  touch -d '5 minutes ago' "$XDG_CACHE_HOME/wtree/disk-totals"
  wt --stack-list
  assert_contains "disk 2.0GB"
  assert_contains "disk: images 10.5GB, containers 1.2GB, volumes 5GB, build 200MB (5 min old)"
  mkdir "$XDG_CACHE_HOME/wtree/lock"
  wt --stack-list
  assert_contains "update in progress"
}

test_stack_list_does_not_refresh_the_disk_cache() {
  make_repo
  wt --stack-list
  if grep -q '^system df' "$WTREE_SHIM_DIR/docker.log"; then fail "docker system df ran"; fi
}

test_stack_list_changes_nothing() {
  setup_list_fixture
  wt --stack-list
  wt -g --stack-list
  wt --list
  wt -g --list
  assert_no_docker_changes
}

test_global_stack_list_shows_all_and_marks_gone() {
  setup_list_fixture
  wt -g --stack-list
  assert_contains "elsewhere"
  assert_contains "stranger"
  grep -q 'proj-old.*worktree gone' <<<"$OUT" || fail "proj-old is not marked gone"
  grep -q 'stranger.*volumes only' <<<"$OUT" || fail "stranger is not marked volumes only"
  assert_contains "on proj alpha"
}

test_global_stack_list_shows_a_plain_directory() {
  mkdir -p "$HOME/plain"
  stack plainstack "$HOME/plain" running
  cd "$HOME/plain" || exit 2
  wt -g --stack-list
  assert_contains "plainstack"
  assert_not_contains "worktree gone"
}

test_narrow_list_cuts_a_long_path_from_the_start() {
  local long=deep/deeper/deepest/and/deeper/than/the/screen/is/wide
  make_repo
  must git -C "$REPO" worktree add -q -b alpha "$HOME/code/$long"
  COLUMNS=40 wt --list
  assert_contains "…"
  assert_contains "is/wide"
  assert_not_contains "~/code/deep"
}
