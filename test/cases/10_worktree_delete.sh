# Which worktrees wtree --delete and --force-delete take, and what happens to their stacks.
# shellcheck shell=bash

assert_all_kept() {
  local path
  for path in "$@"; do
    assert_dir "$path"
    assert_registered "$path"
  done
  assert_no_docker_changes
}

test_delete_clean_worktree_after_y() {
  make_repo
  add_wt alpha
  worktree_args "$WT"
  wt_keys y --delete "${ARGS[@]}"
  assert_rc 0
  assert_contains "Delete 1 worktree:"
  assert_contains "~/code/worktrees/alpha"
  assert_no_dir "$WT"
  assert_not_registered "$WT"
}

test_delete_keeps_the_branch() {
  make_repo
  add_wt alpha
  worktree_args "$WT"
  wt_keys y --delete "${ARGS[@]}"
  must git -C "$REPO" rev-parse --verify -q refs/heads/alpha
}

test_delete_defaults_to_no() {
  local key
  make_repo
  add_wt alpha
  worktree_args "$WT"
  for key in n N x ' ' '\n' '\033'; do
    wt_keys "$key" --delete "${ARGS[@]}"
    assert_rc 0
    assert_all_kept "$WT"
  done
}

test_delete_without_a_terminal_deletes_nothing() {
  make_repo
  add_wt alpha
  add_wt beta
  stack p "$WT" running
  volume p p_data
  worktree_args "$(worktree_path alpha)" "$WT"
  wt --delete "${ARGS[@]}"
  assert_all_kept "$(worktree_path alpha)" "$WT"
  wt --force-delete "${ARGS[@]}"
  assert_all_kept "$(worktree_path alpha)" "$WT"
}

test_main_checkout_is_never_deleted_by_plain_delete() {
  make_repo
  add_wt alpha
  cd "$WT" || exit 2
  wt_keys 'y' --delete "$REPO" "$COMMON"
  assert_contains "proj: it is the main checkout"
  assert_contains "nothing to delete"
  assert_dir "$REPO"
  assert_registered "$REPO"
}

test_main_checkout_is_never_deleted_by_force_delete() {
  make_repo
  add_wt alpha
  cd "$WT" || exit 2
  wt_keys 'y' --force-delete "$REPO" "$COMMON"
  assert_contains "proj: it is the main checkout"
  assert_contains "nothing to delete"
  assert_dir "$REPO"
}

test_main_checkout_of_a_bare_repo_is_never_deleted() {
  make_repo
  must git clone -q --bare "$REPO" "$HOME/code/bare.git"
  must git -C "$HOME/code/bare.git" worktree add -q "$HOME/code/bare-wt" -b wt
  wt_keys 'y' --force-delete "$HOME/code/bare.git" "$HOME/code/bare.git"
  assert_contains "it is the main checkout"
  assert_dir "$HOME/code/bare.git"
}

test_current_worktree_is_never_deleted_by_plain_delete() {
  make_repo
  add_wt alpha
  cd "$WT" || exit 2
  worktree_args "$WT"
  wt_keys 'y' --delete "${ARGS[@]}"
  assert_contains "alpha: you are in this worktree"
  assert_contains "nothing to delete"
  assert_dir "$WT"
}

test_current_worktree_is_never_deleted_by_force_delete() {
  make_repo
  add_wt alpha
  cd "$WT" || exit 2
  worktree_args "$WT"
  wt_keys 'y' --force-delete "${ARGS[@]}"
  assert_contains "alpha: you are in this worktree"
  assert_dir "$WT"
}

test_current_worktree_counts_from_a_subdirectory() {
  make_repo
  add_wt alpha
  mkdir -p "$WT/deep/er"
  cd "$WT/deep/er" || exit 2
  worktree_args "$WT"
  wt_keys 'y' --force-delete "${ARGS[@]}"
  assert_contains "you are in this worktree"
  assert_dir "$WT"
}

test_changed_file_blocks_plain_delete() {
  make_repo
  add_wt alpha
  printf 'edit\n' >>"$WT/file.txt"
  worktree_args "$WT"
  wt_keys 'y' --delete "${ARGS[@]}"
  assert_contains "alpha: it has changed files, press ctrl-f to delete it"
  assert_contains "nothing to delete"
  assert_dir "$WT"
}

test_untracked_file_blocks_plain_delete() {
  make_repo
  add_wt alpha
  printf 'new\n' >"$WT/new.txt"
  worktree_args "$WT"
  wt_keys 'y' --delete "${ARGS[@]}"
  assert_contains "it has changed files"
  assert_dir "$WT"
  [[ -f $WT/new.txt ]] || fail "the untracked file is gone"
}

test_force_delete_takes_changed_files_after_y() {
  make_repo
  add_wt alpha
  printf 'edit\n' >>"$WT/file.txt"
  printf 'new\n' >"$WT/new.txt"
  worktree_args "$WT"
  wt_keys y --force-delete "${ARGS[@]}"
  assert_contains "FORCE delete 1 worktree:"
  assert_contains "(it has changed files)"
  assert_no_dir "$WT"
}

test_force_delete_defaults_to_no() {
  make_repo
  add_wt alpha
  printf 'edit\n' >>"$WT/file.txt"
  worktree_args "$WT"
  wt_keys n --force-delete "${ARGS[@]}"
  assert_all_kept "$WT"
}

test_unheld_detached_commit_blocks_plain_delete() {
  make_repo
  add_detached_wt alpha
  commit_in "$WT"
  local head
  head=$(git -C "$WT" rev-parse HEAD)
  worktree_args "$WT"
  wt_keys 'y' --delete "${ARGS[@]}"
  assert_contains "alpha: no branch or tag holds its commit ${head:0:9}, press ctrl-f"
  assert_dir "$WT"
}

test_force_delete_takes_an_unheld_detached_commit() {
  make_repo
  add_detached_wt alpha
  commit_in "$WT"
  worktree_args "$WT"
  wt_keys y --force-delete "${ARGS[@]}"
  assert_contains "no branch or tag holds its commit"
  assert_no_dir "$WT"
}

test_detached_commit_held_by_a_branch_passes() {
  make_repo
  add_detached_wt alpha
  worktree_args "$WT"
  wt_keys y --delete "${ARGS[@]}"
  assert_no_dir "$WT"
}

test_detached_commit_held_by_a_tag_passes() {
  make_repo
  add_detached_wt alpha
  commit_in "$WT"
  must git -C "$WT" tag keep-it
  worktree_args "$WT"
  wt_keys y --delete "${ARGS[@]}"
  assert_no_dir "$WT"
  must git -C "$REPO" rev-parse --verify -q refs/tags/keep-it
}

test_ignored_files_do_not_block_and_the_warning_says_so() {
  make_repo
  add_wt alpha
  printf 'SECRET=1\n' >"$WT/.env"
  worktree_args "$WT"
  wt_keys y --delete "${ARGS[@]}"
  assert_contains "Ignored files, such as .env and node_modules, are deleted too."
  assert_no_dir "$WT"
}

test_force_delete_lists_what_is_lost() {
  make_repo
  add_wt alpha
  worktree_args "$WT"
  wt_keys n --force-delete "${ARGS[@]}"
  assert_contains "Changed files, untracked files, ignored files, and commits that no branch or tag holds are lost."
}

test_refusals_show_reasons_and_the_batch_continues() {
  local clean dirty
  make_repo
  add_wt clean
  clean=$WT
  add_wt dirty
  dirty=$WT
  printf 'edit\n' >>"$dirty/file.txt"
  worktree_args "$REPO" "$dirty" "$clean"
  wt_keys y --delete "${ARGS[@]}"
  assert_contains "These stay:"
  assert_contains "proj: it is the main checkout"
  assert_contains "dirty: it has changed files"
  assert_contains "Delete 1 worktree:"
  assert_no_dir "$clean"
  assert_dir "$dirty"
  assert_dir "$REPO"
}

test_batch_needs_one_confirmation() {
  local a b
  make_repo
  add_wt aa
  a=$WT
  add_wt bb
  b=$WT
  worktree_args "$a" "$b"
  wt_keys y --delete "${ARGS[@]}"
  assert_contains "Delete 2 worktrees:"
  assert_contains "2 of 2 done"
  assert_no_dir "$a"
  assert_no_dir "$b"
}

test_missing_directory_is_noted_and_removed() {
  make_repo
  add_wt alpha
  rm -rf "$WT"
  worktree_args "$WT"
  wt_keys y --delete "${ARGS[@]}"
  assert_contains "(its directory is missing)"
  assert_contains "remove missing worktree alpha"
  assert_not_registered "$WT"
}

test_locked_worktree_stays_after_plain_delete() {
  make_repo
  add_wt alpha
  must git -C "$REPO" worktree lock "$WT"
  worktree_args "$WT"
  wt_keys y --delete "${ARGS[@]}"
  assert_contains "alpha: it is locked."
  assert_contains "git -C $REPO worktree unlock $WT"
  assert_contains "nothing to delete"
  assert_not_contains "cannot remove"
  assert_dir "$WT"
  assert_registered "$WT"
}

test_locked_worktree_stays_after_force_delete() {
  make_repo
  add_wt alpha
  must git -C "$REPO" worktree lock "$WT"
  worktree_args "$WT"
  wt_keys y --force-delete "${ARGS[@]}"
  assert_contains "alpha: it is locked."
  assert_not_contains "FORCE delete"
  assert_not_contains "cannot remove"
  assert_dir "$WT"
  assert_registered "$WT"
}

setup_wt_with_stack() {
  make_repo
  add_wt alpha
  stack p "$WT" running
  stack p "$WT" exited
  volume p p_data
  volume p p_cache
  worktree_args "$WT"
}

test_stack_prompt_lists_the_stacks_of_the_worktree() {
  setup_wt_with_stack
  wt_keys n --delete "${ARGS[@]}"
  assert_contains "Docker stacks of these worktrees:"
  assert_contains "p  (alpha)"
  assert_contains "[y: worktrees only, s: and stacks, keep volumes, v: and stacks with volumes, N: no]"
  assert_all_kept "$WT"
}

test_stack_prompt_defaults_to_no() {
  local key
  setup_wt_with_stack
  for key in n x '\n' '\033'; do
    wt_keys "$key" --delete "${ARGS[@]}"
    assert_all_kept "$WT"
  done
}

test_stack_prompt_y_deletes_the_worktree_only() {
  setup_wt_with_stack
  wt_keys y --delete "${ARGS[@]}"
  assert_no_dir "$WT"
  assert_no_docker_changes
}

test_stack_prompt_s_keeps_the_volumes() {
  setup_wt_with_stack
  wt_keys s --delete "${ARGS[@]}"
  assert_no_dir "$WT"
  assert_log "compose -p p down"
  assert_not_log "volume rm p_data"
  assert_not_log "volume rm p_cache"
}

test_stack_prompt_v_takes_one_stack_and_its_volumes() {
  setup_wt_with_stack
  wt_keys vy --delete "${ARGS[@]}"
  assert_contains "Delete 1 stack and 2 volumes:"
  assert_no_dir "$WT"
  assert_log_order "compose -p p down" "volume rm p_data"
  assert_log_order "compose -p p down" "volume rm p_cache"
}

test_failed_worktree_delete_skips_its_stack() {
  make_repo_with_submodule
  add_wt alpha
  init_submodule_in "$WT"
  stack p "$WT" running
  worktree_args "$WT"
  wt_keys s --delete "${ARGS[@]}"
  assert_contains "✗ delete worktree alpha"
  assert_contains "- delete stack p: skipped, an earlier step failed"
  assert_contains "1 step failed, 1 skipped."
  assert_dir "$WT"
  assert_no_docker_changes
}

test_failed_step_does_not_stop_the_other_worktree() {
  local stuck free
  make_repo_with_submodule
  add_wt stuck
  stuck=$WT
  init_submodule_in "$stuck"
  add_wt free
  free=$WT
  stack stuckstack "$stuck" running
  stack freestack "$free" running
  worktree_args "$stuck" "$free"
  wt_keys s --delete "${ARGS[@]}"
  assert_contains "- delete stack stuckstack: skipped"
  assert_contains "✓ delete stack freestack"
  assert_dir "$stuck"
  assert_no_dir "$free"
  assert_log "compose -p freestack down"
  assert_not_log "compose -p stuckstack down"
}

test_stack_in_a_subdirectory_belongs_to_the_worktree() {
  make_repo
  add_wt alpha
  mkdir -p "$WT/infra/dev"
  stack p "$WT/infra/dev" running
  worktree_args "$WT"
  wt_keys n --delete "${ARGS[@]}"
  assert_contains "p  (alpha)"
}

test_a_stack_in_two_worktrees_goes_down_once() {
  local a b
  make_repo
  add_wt aa
  a=$WT
  add_wt bb
  b=$WT
  stack shared "$a" running
  stack shared "$b" running
  worktree_args "$a" "$b"
  wt_keys s --delete "${ARGS[@]}"
  assert_log_count "compose -p shared down" 1
}

test_stack_of_a_nested_worktree_belongs_to_the_inner_one() {
  local outer inner
  make_repo
  add_wt outer
  outer=$WT
  inner=$outer/nested
  must git -C "$REPO" worktree add -q -b inner "$inner"
  stack innerstack "$inner" running
  worktree_args "$outer"
  wt_keys n --force-delete "${ARGS[@]}"
  assert_not_contains "Docker stacks of these worktrees:"
  assert_contains "is inside it, delete that one first"
  assert_dir "$outer"
  worktree_args "$inner"
  wt_keys n --delete "${ARGS[@]}"
  assert_contains "innerstack  (nested)"
}

test_disk_cache_refreshes_after_stacks_are_deleted() {
  setup_wt_with_stack
  wt_keys s --delete "${ARGS[@]}"
  wait_file "$XDG_CACHE_HOME/wtree/disk-totals"
}

test_disk_cache_stays_when_only_worktrees_are_deleted() {
  setup_wt_with_stack
  wt_keys y --delete "${ARGS[@]}"
  sleep 0.3
  [[ ! -e $XDG_CACHE_HOME/wtree/disk-totals ]] || fail "the disk cache was refreshed"
  if grep -q '^system df' "$WTREE_SHIM_DIR/docker.log"; then fail "docker system df ran"; fi
}

test_a_docker_failure_does_not_stop_a_worktree_delete() {
  make_repo
  add_wt alpha
  shim_fail 'ps *'
  worktree_args "$WT"
  wt_keys y --delete "${ARGS[@]}"
  assert_no_dir "$WT"
}
