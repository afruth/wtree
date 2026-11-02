# Which stacks and volumes wtree --stack-stop, --stack-delete and --stack-purge take.
# shellcheck shell=bash

# sets WT and one stack p in it, with two volumes
setup_linked_stack() {
  make_repo
  add_wt alpha
  stack p "$WT" running
  volume p p_data
  volume p p_cache
}

setup_two_linked_stacks() {
  make_repo
  add_wt aa
  WT_A=$WT
  add_wt bb
  WT_B=$WT
  stack pa "$WT_A" running
  stack pb "$WT_B" running
  volume pa pa_data
  volume pb pb_data
}

test_stop_one_stack_needs_no_confirmation() {
  setup_linked_stack
  wt_keys '' --stack-stop p
  assert_rc 0
  assert_log "compose -p p stop"
  assert_contains "✓ stop stack p"
  assert_not_contains "Continue?"
  assert_not_log "compose -p p down"
}

test_stop_several_stacks_needs_one_confirmation() {
  setup_two_linked_stacks
  wt_keys y --stack-stop pa pb
  assert_contains "Stop 2 stacks:"
  assert_log "compose -p pa stop"
  assert_log "compose -p pb stop"
}

test_stop_several_stacks_defaults_to_no() {
  local key
  setup_two_linked_stacks
  for key in n x '\n'; do
    wt_keys "$key" --stack-stop pa pb
    assert_no_docker_changes
  done
}

test_stop_continues_after_a_failure_and_shows_it() {
  setup_two_linked_stacks
  shim_fail 'compose -p pa stop' 'the daemon said no'
  wt_keys y --stack-stop pa pb
  assert_contains "✗ stop stack pa"
  assert_contains "    the daemon said no"
  assert_contains "✓ stop stack pb"
  assert_contains "1 step failed, 0 skipped."
  assert_log "compose -p pb stop"
}

test_steps_show_only_the_last_eight_lines_of_a_failure() {
  setup_linked_stack
  # a wrapper around the shim, because a failure of the shim has one line
  mkdir -p "$T/wrap"
  cat >"$T/wrap/docker" <<WRAP
#!/usr/bin/env bash
"$TEST_ROOT/shim/docker" "\$@" >/dev/null 2>&1
for i in \$(seq 1 12); do printf 'line-%d-end\n' "\$i"; done
exit 1
WRAP
  chmod +x "$T/wrap/docker"
  export PATH=$T/wrap:$PATH
  wt_keys '' --stack-stop p
  assert_contains "✗ stop stack p"
  assert_not_contains "line-4-end"
  assert_contains "line-5-end"
  assert_contains "line-12-end"
}

test_delete_stack_keeps_the_volumes() {
  setup_linked_stack
  wt_keys y --stack-delete p "$WT"
  assert_contains "Delete the containers and networks of 1 stack. The volumes stay:"
  assert_log "compose -p p down"
  assert_not_log "volume rm p_data"
  assert_dir "$WT"
}

test_delete_stack_defaults_to_no() {
  local key
  setup_linked_stack
  for key in n x '\n' '\033'; do
    wt_keys "$key" --stack-delete p "$WT"
    assert_no_docker_changes
    assert_dir "$WT"
  done
}

test_delete_stack_without_a_terminal_deletes_nothing() {
  setup_linked_stack
  wt --stack-delete p "$WT"
  assert_no_docker_changes
  wt --stack-purge p "$WT"
  assert_no_docker_changes
}

test_purge_removes_volumes_by_label_one_at_a_time() {
  setup_linked_stack
  volume other other_data
  wt_keys y --stack-purge p "$WT"
  assert_contains "Delete 1 stack and 2 volumes:"
  assert_contains "p  (2 volumes)"
  assert_contains "The data in the volumes is lost."
  assert_log "volume ls -q --filter label=com.docker.compose.project=p"
  assert_log_order "compose -p p down" "volume rm p_data"
  assert_log_order "compose -p p down" "volume rm p_cache"
  assert_not_log "volume rm other_data"
  assert_not_log "compose -p p down -v"
}

test_purge_of_a_stack_with_no_volumes_works() {
  make_repo
  add_wt alpha
  stack p "$WT" running
  wt_keys y --stack-purge p "$WT"
  assert_contains "Delete 1 stack and 0 volumes:"
  assert_log "compose -p p down"
}

test_compose_runs_from_root_without_compose_file() {
  setup_linked_stack
  printf 'services: {}\n' >"$WT/compose.yaml"
  cd "$WT" || exit 2
  export COMPOSE_FILE=$WT/compose.yaml
  wt_keys y --stack-stop p
  wt_keys y --stack-delete p "$WT"
  local count=0 dir file call
  while IFS=$'\t' read -r dir file call; do
    [[ $call == compose* ]] || continue
    count=$((count + 1))
    assert_eq "$dir" "/" "the directory of: $call"
    assert_eq "$file" "<unset>" "COMPOSE_FILE of: $call"
  done <"$WTREE_SHIM_DIR/calls.ctx"
  assert_eq "$count" 2 "compose calls"
}

test_purge_of_several_stacks_needs_the_typed_phrase() {
  setup_two_linked_stacks
  wt_keys '2 stacks\n' --stack-purge pa pb "$WT_A" "$WT_B"
  assert_contains 'Type "2 stacks" to delete, or "2 stacks w" to delete the worktrees too:'
  assert_log "volume rm pa_data"
  assert_log "volume rm pb_data"
  assert_dir "$WT_A"
  assert_dir "$WT_B"
}

test_purge_of_several_stacks_refuses_a_near_match() {
  local answer
  setup_two_linked_stacks
  for answer in y yes '2 stack' '2 STACKS' stacks 2 '3 stacks' '2 stacks x' '2  stacks' '2 stacksw' pa; do
    wt_keys "$answer\\n" --stack-purge pa pb "$WT_A" "$WT_B"
    assert_rc 0
    assert_no_docker_changes
    assert_dir "$WT_A"
    assert_dir "$WT_B"
  done
}

test_purge_of_several_stacks_refuses_an_empty_answer_and_eof() {
  setup_two_linked_stacks
  wt_keys '\n' --stack-purge pa pb "$WT_A" "$WT_B"
  assert_no_docker_changes
  wt --stack-purge pa pb "$WT_A" "$WT_B"
  assert_no_docker_changes
}

test_typed_phrase_with_w_also_deletes_the_worktrees() {
  setup_two_linked_stacks
  wt_keys '2 stacks w\n' --stack-purge pa pb "$WT_A" "$WT_B"
  assert_log "volume rm pa_data"
  assert_no_dir "$WT_A"
  assert_no_dir "$WT_B"
}

test_protected_stack_of_a_main_checkout_needs_its_name() {
  make_repo
  stack mainstack "$REPO" running
  volume mainstack mainstack_data
  wt_keys 'y\n' --stack-purge mainstack "$REPO"
  assert_contains "These stacks belong to a main checkout, to no repo, or to a directory that is gone:"
  assert_contains 'Type "mainstack" to delete:'
  assert_no_docker_changes
  wt_keys 'mainstack\n' --stack-purge mainstack "$REPO"
  assert_log "volume rm mainstack_data"
}

test_protected_stack_refuses_a_near_match() {
  local answer
  make_repo
  stack mainstack "$REPO" running
  volume mainstack mainstack_data
  for answer in mainstac MAINSTACK 'mainstack w' mainstack2 '1 stack'; do
    wt_keys "$answer\\n" --stack-purge mainstack "$REPO"
    assert_no_docker_changes
  done
}

test_protected_stack_in_a_subdirectory_of_main() {
  make_repo
  mkdir -p "$REPO/infra"
  stack mainstack "$REPO/infra" running
  wt_keys '\n' --stack-purge mainstack "$REPO/infra"
  assert_contains 'Type "mainstack" to delete:'
  assert_no_docker_changes
}

test_protected_stack_in_a_directory_with_no_repo() {
  mkdir -p "$HOME/plain"
  stack plainstack "$HOME/plain" running
  volume plainstack plain_data
  make_repo
  wt_keys '\n' --stack-purge plainstack "$HOME/plain"
  assert_contains 'Type "plainstack" to delete:'
  assert_no_docker_changes
}

test_stack_with_a_missing_directory_is_protected() {
  make_repo
  stack gone "$HOME/nowhere" running
  volume gone gone_data
  wt_keys 'y\n' --stack-purge gone "$HOME/nowhere"
  assert_contains 'Type "gone" to delete:'
  assert_no_docker_changes
  wt_keys 'gone\n' --stack-purge gone "$HOME/nowhere"
  assert_log "volume rm gone_data"
}

test_stack_with_no_containers_is_protected() {
  make_repo
  volume only only_data
  wt_keys 'y\n' --stack-purge only ""
  assert_contains 'Type "only" to delete:'
  assert_no_docker_changes
  wt_keys 'only\n' --stack-purge only ""
  assert_log "volume rm only_data"
}

test_linked_stack_alone_needs_one_key() {
  setup_linked_stack
  wt_keys y --stack-purge p "$WT"
  assert_not_contains "Type"
  assert_log "volume rm p_data"
}

test_protected_stack_in_a_batch_clears_the_worktree_offers() {
  make_repo
  add_wt alpha
  stack linked "$WT" running
  stack mainstack "$REPO" running
  wt_keys '2 stacks w\n' --stack-purge linked mainstack "$WT" "$REPO"
  assert_contains "All the worktrees stay."
  assert_contains 'Type "2 stacks" to delete: '
  assert_not_contains "or \"2 stacks w\""
  assert_no_docker_changes
  assert_dir "$WT"
  wt_keys '2 stacks\n' --stack-purge linked mainstack "$WT" "$REPO"
  assert_log "compose -p linked down"
  assert_log "compose -p mainstack down"
  assert_dir "$WT"
}

test_keep_volumes_never_asks_for_a_phrase() {
  make_repo
  stack mainstack "$REPO" running
  volume mainstack mainstack_data
  wt_keys y --stack-delete mainstack "$REPO"
  assert_not_contains "Type"
  assert_log "compose -p mainstack down"
  assert_not_log "volume rm mainstack_data"
}

test_stack_offers_its_clean_worktree() {
  setup_linked_stack
  wt_keys n --stack-delete p "$WT"
  assert_contains "Worktrees of these stacks:"
  assert_contains "~/code/worktrees/alpha"
  assert_contains "Deleting a worktree also deletes its ignored files"
  assert_contains "[y: stacks only, w: and worktrees, N: no]"
}

test_stack_prompt_y_keeps_the_worktree() {
  setup_linked_stack
  wt_keys y --stack-delete p "$WT"
  assert_log "compose -p p down"
  assert_dir "$WT"
}

test_stack_prompt_w_deletes_the_worktree_after_the_stack() {
  setup_linked_stack
  wt_keys w --stack-delete p "$WT"
  assert_log "compose -p p down"
  assert_no_dir "$WT"
  assert_not_registered "$WT"
}

test_worktree_of_two_stacks_goes_after_both_and_only_once() {
  make_repo
  add_wt alpha
  stack one "$WT" running
  stack two "$WT" running
  wt_keys w --stack-delete one two "$WT" "$WT"
  assert_log "compose -p one down"
  assert_log "compose -p two down"
  assert_contains "✓ delete worktree alpha"
  assert_contains "3 of 3 done"
  assert_no_dir "$WT"
}

test_dirty_worktree_is_not_offered() {
  setup_linked_stack
  printf 'edit\n' >>"$WT/file.txt"
  wt_keys w --stack-delete p "$WT"
  assert_contains "These worktrees stay:"
  assert_contains "alpha: it has changed files"
  assert_not_contains "Worktrees of these stacks:"
  assert_dir "$WT"
  assert_no_docker_changes
}

test_unheld_detached_worktree_is_not_offered() {
  make_repo
  add_detached_wt alpha
  commit_in "$WT"
  stack p "$WT" running
  wt_keys y --stack-delete p "$WT"
  assert_contains "alpha: no branch or tag holds its commit"
  assert_log "compose -p p down"
  assert_dir "$WT"
}

test_current_worktree_is_not_offered() {
  setup_linked_stack
  cd "$WT" || exit 2
  wt_keys n --stack-delete p "$WT"
  assert_not_contains "Worktrees of these stacks:"
  assert_contains "Continue?"
}

test_main_checkout_is_not_offered() {
  make_repo
  stack mainstack "$REPO" running
  wt_keys n --stack-delete mainstack "$REPO"
  assert_not_contains "Worktrees of these stacks:"
  assert_contains "Continue?"
}

# git refuses a plain remove of a worktree with a submodule, and --force takes it. No risk check sees
# this, so only a real force would remove it.
test_stack_side_delete_never_forces() {
  make_repo_with_submodule
  add_wt alpha
  init_submodule_in "$WT"
  stack p "$WT" running
  wt_keys w --stack-delete p "$WT"
  assert_contains "✗ delete worktree alpha"
  assert_dir "$WT"
  assert_registered "$WT"
}

test_disk_cache_refreshes_after_a_stack_delete() {
  setup_linked_stack
  wt_keys y --stack-delete p "$WT"
  wait_file "$XDG_CACHE_HOME/wtree/disk-totals"
  wait_file "$XDG_CACHE_HOME/wtree/disk-projects"
  assert_eq "$(head -n 1 "$XDG_CACHE_HOME/wtree/disk-totals")" "Images	10.5GB"
  # two volumes of 1.5GB. The container is gone by then, so its 500MB does not count
  assert_eq "$(cat "$XDG_CACHE_HOME/wtree/disk-projects")" "p	3000000000"
}

test_a_fresh_lock_skips_the_disk_refresh() {
  setup_linked_stack
  mkdir -p "$XDG_CACHE_HOME/wtree/lock"
  wt_keys y --stack-delete p "$WT"
  sleep 0.3
  if grep -q '^system df' "$WTREE_SHIM_DIR/docker.log"; then fail "docker system df ran"; fi
  assert_dir "$XDG_CACHE_HOME/wtree/lock"
}

test_a_stale_lock_is_replaced() {
  setup_linked_stack
  mkdir -p "$XDG_CACHE_HOME/wtree/lock"
  touch -d '20 minutes ago' "$XDG_CACHE_HOME/wtree/lock"
  wt_keys y --stack-delete p "$WT"
  wait_file "$XDG_CACHE_HOME/wtree/disk-totals"
}

test_disk_refresh_leaves_no_temp_file_or_lock() {
  setup_linked_stack
  wt_keys y --stack-delete p "$WT"
  wait_file "$XDG_CACHE_HOME/wtree/disk-totals"
  local tries=0
  while [[ -d $XDG_CACHE_HOME/wtree/lock ]] && ((tries < 100)); do
    tries=$((tries + 1))
    sleep 0.05
  done
  assert_no_dir "$XDG_CACHE_HOME/wtree/lock"
  OUT=$(ls "$XDG_CACHE_HOME/wtree")
  assert_not_contains "tmp."
}

test_empty_project_names_are_ignored() {
  make_repo
  wt_keys y --stack-delete "" ""
  assert_rc 0
  assert_no_docker_changes
}
