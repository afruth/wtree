# What wtree --nuke, -c and -a take, after one summary and one confirmation.
# shellcheck shell=bash

# merged: on main, with a stopped stack and a volume. feature: its own commit. dirty: a changed file.
# proj-old: a stack whose directory is gone, with no volume. proj-gonevol: the same, with a volume.
# proj-main: a stack of the main checkout, with a volume.
setup_nuke_fixture() {
  make_repo proj
  add_wt merged
  WT_MERGED=$WT
  stack proj-merged "$WT" exited
  volume proj-merged proj-merged_data
  add_wt feature
  WT_FEATURE=$WT
  commit_in "$WT"
  add_wt dirty
  WT_DIRTY=$WT
  printf 'change\n' >>"$WT/file.txt"
  stack proj-old "$HOME/gone/proj-old" exited
  stack proj-gonevol "$HOME/gone/proj-gonevol" exited
  volume proj-gonevol proj-gonevol_data
  stack proj-main "$REPO" running
  volume proj-main proj-main_data
  cd "$REPO" || exit 2
}

test_nuke_takes_all_that_goes_without_force_after_one_key() {
  setup_nuke_fixture
  wt_keys y --nuke
  assert_rc 0
  assert_contains "Delete 2 worktrees:"
  assert_contains "With their stacks. The volumes stay:"
  assert_contains "dirty: it has changed files. wtree --nuke -a takes it"
  assert_contains "Delete 2 stacks without a worktree. The volumes stay:"
  assert_contains "Delete all of it? [y/N]"
  assert_eq "$(grep -o 'y/N' "$T/out" | wc -l | tr -d ' ')" 1
  assert_not_registered "$WT_MERGED"
  assert_not_registered "$WT_FEATURE"
  assert_registered "$WT_DIRTY"
  assert_log "compose -p proj-merged down"
  assert_log "compose -p proj-old down"
  assert_log "compose -p proj-gonevol down"
  assert_not_log "compose -p proj-main down"
  assert_not_log "volume rm proj-merged_data"
  assert_not_log "volume rm proj-gonevol_data"
  assert_not_log "volume rm proj-main_data"
}

test_nuke_changes_nothing_without_a_yes() {
  local key
  setup_nuke_fixture
  for key in n '\n'; do
    wt_keys "$key" --nuke
    assert_contains "Nothing was deleted."
    assert_registered "$WT_MERGED"
    assert_registered "$WT_FEATURE"
    assert_no_docker_changes
  done
}

test_nuke_cleanup_takes_only_the_safe_rows() {
  make_repo proj
  add_wt merged
  WT_MERGED=$WT
  add_wt feature
  WT_FEATURE=$WT
  commit_in "$WT"
  stack proj-old "$HOME/gone/proj-old" exited
  stack proj-gonevol "$HOME/gone/proj-gonevol" exited
  volume proj-gonevol proj-gonevol_data
  cd "$REPO" || exit 2
  wt_keys y --nuke -c
  assert_rc 0
  assert_contains "Delete 1 worktree:"
  assert_not_registered "$WT_MERGED"
  assert_registered "$WT_FEATURE"
  assert_log "compose -p proj-old down"
  assert_not_log "compose -p proj-gonevol down"
}

test_nuke_all_needs_the_typed_word() {
  setup_nuke_fixture
  wt_keys 'yes\n' --nuke -a
  assert_contains 'Type "nuke" to delete:'
  assert_contains "Nothing was deleted."
  assert_registered "$WT_DIRTY"
  assert_no_docker_changes
}

test_nuke_all_forces_and_takes_the_volumes() {
  setup_nuke_fixture
  wt_keys 'nuke\n' --nuke -a
  assert_rc 0
  assert_contains "FORCE delete 3 worktrees:"
  assert_contains "With their stacks and the volumes of these stacks:"
  assert_contains "The data in the volumes is lost."
  assert_not_registered "$WT_MERGED"
  assert_not_registered "$WT_FEATURE"
  assert_not_registered "$WT_DIRTY"
  assert_log "volume rm proj-merged_data"
  assert_log "volume rm proj-gonevol_data"
  assert_not_log "compose -p proj-main down"
  assert_not_log "volume rm proj-main_data"
  assert_registered "$REPO"
}

test_nuke_keeps_the_worktree_you_are_in() {
  setup_nuke_fixture
  cd "$WT_FEATURE" || exit 2
  wt_keys y --nuke
  assert_registered "$WT_FEATURE"
  assert_not_registered "$WT_MERGED"
  assert_contains "No delete takes 2 worktrees"
}

test_nuke_with_nothing_to_do_asks_nothing() {
  make_repo proj
  wt_keys '' --nuke
  assert_rc 0
  assert_contains "Nothing to delete."
  assert_not_contains "[y/N]"
}

test_nuke_levels_need_nuke() {
  make_repo proj
  wt -c
  assert_rc 1
  assert_contains "-c and -a go with --nuke"
}

test_nuke_goes_on_after_a_failure_and_asks_nothing_more() {
  setup_nuke_fixture
  shim_fail 'compose -p proj-old down' 'the daemon said no'
  wt_keys y --nuke
  assert_rc 1
  assert_contains "✗ delete stack proj-old"
  assert_contains "✓ delete stack proj-gonevol"
  assert_not_contains "Press a key to continue"
  assert_not_registered "$WT_MERGED"
}
