# The findings of the review of the core contract. Each test states the right behaviour, and each failed
# on the code before the fix. The helpers q, plan_ok and apply_ok are in 50_core.sh.
# shellcheck shell=bash

# ---- helpers

# a prompt waits, the fixture changes, and then a key answers the prompt. MUTATE and KEYS_AFTER say how.
feed_mutating() {
  local job=$1 tries=0
  until grep -aq 'Continue?\|N: no\]' "$T/out" 2>/dev/null; do
    tries=$((tries + 1))
    ((tries < 400)) || return 0
    sleep 0.02
  done
  eval "$MUTATE"
  printf '%s' "$KEYS_AFTER"
  while kill -0 "$job" 2>/dev/null; do
    printf '\n'
    sleep 0.2
  done
}

wt_mutate() {
  local cmd job
  printf -v cmd '%q ' "$WTREE" "$@"
  cat >"$T/pty.sh" <<EOF
stty cols 120 rows 40
$cmd
status=\$?
for _ in \$(seq 1 40); do [ -d "$XDG_CACHE_HOME/wtree/lock" ] || break; sleep 0.05; done
exit \$status
EOF
  rm -f "$T/answers"
  mkfifo "$T/answers"
  : >"$T/out"
  timeout -k 1 20 script -qfec "bash $T/pty.sh" /dev/null <"$T/answers" >"$T/out" 2>&1 &
  job=$!
  feed_mutating "$job" >"$T/answers"
  RC=0
  wait "$job" || RC=$?
  OUT=$(tr '\0' '\n' <"$T/out" | strip_ansi)
}

drop_project() {
  awk -F'\t' -v p="$1" '$1 != p' "$WTREE_SHIM_DIR/containers" >"$T/containers.new"
  must cp "$T/containers.new" "$WTREE_SHIM_DIR/containers"
}

cleanup_row() {
  q "flat('cleanup', 'id', '$1')$2"
}

# a git on PATH that hangs for each call on one directory
git_hangs_on() {
  local real
  real=$(command -v git)
  must mkdir -p "$T/bin"
  cat >"$T/bin/git" <<EOF
#!$(command -v bash)
case " \$* " in *" -C $1 "*${2:-}*) exec -a "hung-git-$$" sleep 40 ;; esac
exec "$real" "\$@"
EOF
  must chmod +x "$T/bin/git"
  export PATH=$T/bin:$PATH
}

# moves ~/code after the worktree is registered, so git lists a path that goes through a symlink
relocate_code_behind_a_symlink() {
  must mv "$HOME/code" "$HOME/data"
  must ln -s data "$HOME/code"
}

# ---- what a stack waits for

test_review_a_stack_in_two_selected_worktrees_waits_for_both() {
  make_repo_with_submodule
  add_wt a
  local a=$WT
  add_wt b
  local b=$WT
  init_submodule_in "$b"
  stack p "$a" running
  stack p "$b" running
  volume p p_data
  plan_ok delete-worktrees -- "$a" "$b"
  assert_q "step(choice('worktrees+stacks+volumes')['steps'][-1])['after']" '["s1", "s2"]'
  apply_ok delete-worktrees "$PLAN_ID_OF" --choice 'worktrees+stacks+volumes' -- "$a" "$b"
  assert_rc 1
  assert_dir "$b"
  assert_not_log 'compose -p p down'
  assert_not_log 'volume rm p_data'
  [[ -f $(worktree_path b)/file.txt ]] || fail "the worktree that failed lost its files"
}

test_review_a_selected_stack_whose_down_failed_keeps_each_of_its_worktrees() {
  make_repo
  add_wt a
  local a=$WT
  add_wt b
  local b=$WT
  stack p "$a" running
  stack q "$a" running
  stack q "$b" running
  shim_fail 'compose -p q down'
  plan_ok delete-stacks --dir "$a" --dir "$b" -- p q
  apply_ok delete-stacks "$PLAN_ID_OF" --choice 'stacks+worktrees' --dir "$a" --dir "$b" -- p q
  assert_rc 1
  assert_dir "$a"
  assert_dir "$b"
  # s1 p, s2 q fails. Both worktrees wait for q.
  assert_q "[e['because'] for e in events('step') if e['state'] == 'skipped']" '["s2", "s2"]'
}

test_review_the_same_through_the_fzf_keys() {
  make_repo
  add_wt a
  local a=$WT
  add_wt b
  local b=$WT
  stack p "$a" running
  stack q "$a" running
  stack q "$b" running
  shim_fail 'compose -p q down'
  wt_keys w --stack-delete p q "$a" "$b"
  assert_dir "$a"
  assert_dir "$b"
}

test_review_a_step_refuses_a_stack_that_is_still_in_the_worktree() {
  # the step itself asks, also when the plan has no dependency that could stop it
  make_repo
  add_wt a
  local a=$WT
  stack p "$a" running
  MUTATE='stack late "$a" running'
  KEYS_AFTER=w
  wt_mutate --stack-delete p "$a"
  assert_contains 'The stack late has containers there now'
  assert_dir "$a"
}

test_review_a_stack_that_started_during_the_prompt_keeps_the_worktree() {
  make_repo
  add_wt a
  local a=$WT
  MUTATE='stack late "$a" running; volume late late_data'
  KEYS_AFTER=y
  wt_mutate --delete "$a" "$COMMON"
  assert_contains 'The stack late has containers there now'
  assert_dir "$a"
  assert_not_log 'compose -p late down'
}

test_review_a_known_stack_does_not_stop_the_worktree_side() {
  make_repo
  add_wt a
  local a=$WT
  stack p "$a" running
  plan_ok delete-worktrees -- "$a"
  apply_ok delete-worktrees "$PLAN_ID_OF" --choice worktrees -- "$a"
  assert_rc 0
  assert_no_dir "$a"
}

test_review_volumes_go_after_the_worktree_on_the_worktree_side() {
  # the step of the worktree took the directory, and that does not make the stack protected
  make_repo
  add_wt a
  local a=$WT
  add_wt b
  local b=$WT
  stack p "$a" running
  stack p "$b" running
  volume p p_data
  plan_ok delete-worktrees -- "$a"
  assert_q "[x['phrase'] for x in choice('worktrees+stacks+volumes')['confirm']['phrases']]" '["p"]'
  apply_ok delete-worktrees "$PLAN_ID_OF" --choice 'worktrees+stacks+volumes' --phrase p -- "$a"
  assert_rc 0
  assert_no_dir "$a"
  assert_log 'volume rm p_data'
}

test_review_a_directory_that_goes_during_the_prompt_keeps_the_volumes() {
  make_repo
  add_wt a
  local a=$WT
  stack p "$a" running
  volume p p_data
  MUTATE='git -C "$REPO" worktree remove "$a"'
  KEYS_AFTER=y
  wt_mutate --stack-purge p "$a"
  assert_contains 'has no linked worktree now'
  assert_not_log 'compose -p p down'
  assert_not_log 'volume rm p_data'
}

test_review_because_names_the_first_step_that_failed() {
  make_repo
  add_wt alpha
  stack p1 "$WT" exited
  stack p2 "$WT" exited
  shim_fail 'compose -p p1 down'
  shim_fail 'compose -p p2 down'
  plan_ok delete-stacks -- p1 p2
  apply_ok delete-stacks "$PLAN_ID_OF" --choice stacks+worktrees -- p1 p2
  assert_rc 1
  assert_q "[e['because'] for e in events('step') if e['state'] == 'skipped']" '["s1"]'
}

# ---- what a plain delete must not take

test_review_a_status_config_does_not_hide_an_untracked_file() {
  make_repo
  must git -C "$REPO" config status.showUntrackedFiles no
  add_wt a
  printf 'only copy\n' >"$WT/notes.txt"
  plan_ok delete-worktrees -- "$WT"
  assert_q "item('worktree:$WT')['reason']['code']" dirty
  apply_ok delete-worktrees "$PLAN_ID_OF" --choice worktrees -- "$WT"
  assert_rc 4
  [[ -f $WT/notes.txt ]] || fail "the untracked file is gone"
}

test_review_assume_unchanged_does_not_hide_an_edit() {
  make_repo
  add_wt a
  must git -C "$WT" update-index --assume-unchanged file.txt
  printf 'edited\n' >>"$WT/file.txt"
  plan_ok delete-worktrees -- "$WT"
  assert_q "item('worktree:$WT')['reason']['code']" dirty
  assert_q "item('worktree:$WT')['force_ok']" true
  apply_ok delete-worktrees "$PLAN_ID_OF" --choice worktrees -- "$WT"
  assert_rc 4
  assert_dir "$WT"
}

test_review_skip_worktree_does_not_hide_a_file_on_the_disk() {
  make_repo
  add_wt a
  must git -C "$WT" update-index --skip-worktree file.txt
  printf 'edited\n' >>"$WT/file.txt"
  plan_ok delete-worktrees -- "$WT"
  assert_q "item('worktree:$WT')['reason']['code']" dirty
}

test_review_a_sparse_file_that_is_not_on_the_disk_holds_nothing() {
  make_repo
  add_wt a
  must git -C "$WT" update-index --skip-worktree file.txt
  must rm "$WT/file.txt"
  plan_ok delete-worktrees -- "$WT"
  assert_q "item('worktree:$WT')['go']" true
}

test_review_the_list_sees_what_the_plan_sees() {
  make_repo
  must git -C "$REPO" config status.showUntrackedFiles no
  add_wt a
  printf 'only copy\n' >"$WT/notes.txt"
  add_wt b
  must git -C "$WT" update-index --assume-unchanged file.txt
  wt core list --no-refresh --stream
  assert_q "flat('worktree_git', 'path', '$(worktree_path a)')['untracked']" 1
  assert_q "flat('worktree_git', 'path', '$(worktree_path b)')['changed']" 1
  assert_eq "$(cleanup_row "worktree:$(worktree_path a)" "['tier']")" needs_look
  assert_eq "$(cleanup_row "worktree:$(worktree_path b)" "['reasons'][0]['code']")" dirty
}

test_review_a_missing_detached_worktree_keeps_its_only_commit() {
  local head
  make_repo
  add_detached_wt a
  commit_in "$WT"
  head=$(git -C "$WT" rev-parse HEAD)
  must rm -rf "$WT"
  plan_ok delete-worktrees -- "$WT"
  assert_q "item('worktree:$WT')['go']" false
  assert_q "item('worktree:$WT')['reason']['code']" unheld_commit
  assert_q "item('worktree:$WT')['force_ok']" true
  apply_ok delete-worktrees "$PLAN_ID_OF" --choice worktrees -- "$WT"
  assert_rc 4
  assert_registered "$WT"
  # the force level says what it costs, and it still takes only this entry
  plan_ok delete-worktrees --force -- "$WT"
  assert_q "item('worktree:$WT')['go']" true
  assert_q "step(item('worktree:$WT')['steps'][0])['kind']" remove_missing
  assert_q "[c['code'] for c in item('worktree:$WT')['consequences']]" '["missing_dir", "force_note"]'
  assert_q "item('worktree:$WT')['consequences'][1]['severe']" true
  apply_ok delete-worktrees "$PLAN_ID_OF" --force --choice worktrees -- "$WT"
  assert_rc 0
  assert_not_registered "$WT"
  [[ -n $head ]] || broken "no head"
}

test_review_a_missing_worktree_that_a_branch_holds_is_not_refused() {
  make_repo
  add_wt a
  must rm -rf "$WT"
  plan_ok delete-worktrees -- "$WT"
  assert_q "item('worktree:$WT')['go']" true
  assert_q "item('worktree:$WT')['note']" 'its directory is missing'
  apply_ok delete-worktrees "$PLAN_ID_OF" --choice worktrees -- "$WT"
  assert_rc 0
  assert_not_registered "$WT"
}

test_review_a_stash_does_not_hold_a_detached_commit() {
  make_repo
  add_detached_wt a
  commit_in "$WT"
  printf 'x\n' >>"$WT/file.txt"
  must git -C "$WT" stash -q
  plan_ok delete-worktrees -- "$WT"
  assert_q "item('worktree:$WT')['go']" false
  assert_q "item('worktree:$WT')['reason']['code']" unheld_commit
  wt core list --no-refresh --stream --only git
  assert_q "flat('worktree_git', 'path', '$WT')['head_held']" false
}

test_review_a_tag_and_a_remote_branch_still_hold_a_detached_commit() {
  make_repo
  add_detached_wt a
  commit_in "$WT"
  must git -C "$REPO" update-ref refs/remotes/origin/saved "$(git -C "$WT" rev-parse HEAD)"
  plan_ok delete-worktrees -- "$WT"
  assert_q "item('worktree:$WT')['go']" true
}

# ---- repos inside a worktree

test_review_an_ignored_bare_repo_inside_a_worktree_stays() {
  make_repo
  printf 'backup.git/\n' >>"$REPO/.gitignore"
  must git -C "$REPO" commit -qam ignore
  add_wt a
  must git init -q --bare "$WT/backup.git"
  plan_ok delete-worktrees -- "$WT"
  assert_q "item('worktree:$WT')['reason']['code']" nested_repo
  apply_ok delete-worktrees "$PLAN_ID_OF" --choice worktrees -- "$WT"
  assert_rc 4
  assert_dir "$WT/backup.git"
}

test_review_an_embedded_repo_added_as_a_gitlink_needs_the_phrase() {
  make_repo
  add_wt a
  must git -C "$WT" init -q -b main inner
  printf 'x\n' >"$WT/inner/f"
  must git -C "$WT/inner" add -A
  must git -C "$WT/inner" commit -q -m only-copy
  must git -C "$WT" add inner
  must git -C "$WT" commit -q -m gitlink
  plan_ok delete-worktrees -- "$WT"
  assert_q "item('worktree:$WT')['reason']['code']" nested_repo
  plan_ok delete-worktrees --force -- "$WT"
  assert_q "choice('worktrees')['confirm']['kind']" phrase
  apply_ok delete-worktrees "$PLAN_ID_OF" --force --choice worktrees -- "$WT"
  assert_rc 4
  assert_dir "$WT/inner/.git"
}

test_review_a_real_submodule_needs_no_phrase() {
  make_repo_with_submodule
  add_wt a
  init_submodule_in "$WT"
  plan_ok delete-worktrees --force -- "$WT"
  assert_q "item('worktree:$WT')['go']" true
  assert_q "choice('worktrees')['confirm']['kind']" key
}

test_review_a_file_named_head_is_not_a_repo() {
  make_repo
  printf 'sub/\n' >>"$REPO/.gitignore"
  must git -C "$REPO" commit -qam ignore
  add_wt a
  must mkdir -p "$WT/sub/objects"
  printf 'x\n' >"$WT/sub/HEAD"
  plan_ok delete-worktrees -- "$WT"
  assert_q "item('worktree:$WT')['go']" true
}

test_review_each_worktree_with_a_repo_inside_has_its_own_phrase() {
  make_repo
  add_wt a
  local a=$WT
  add_wt b
  local b=$WT
  must git -C "$a" init -q n
  must git -C "$b" init -q n
  plan_ok delete-worktrees --force -- "$a" "$b"
  assert_q "[p['phrase'] for p in choice('worktrees')['confirm']['phrases']]" '["a", "b"]'
  apply_ok delete-worktrees "$PLAN_ID_OF" --force --choice worktrees --phrase a -- "$a" "$b"
  assert_rc 4
  assert_dir "$a"
  apply_ok delete-worktrees "$PLAN_ID_OF" --force --choice worktrees --phrase a --phrase b -- "$a" "$b"
  assert_rc 0
  assert_no_dir "$a"
  assert_no_dir "$b"
}

test_review_the_fzf_prompt_asks_each_phrase() {
  make_repo
  add_wt a
  local a=$WT
  add_wt b
  local b=$WT
  must git -C "$a" init -q n
  must git -C "$b" init -q n
  wt_keys 'a\nb\n' --force-delete "$a" "$b" "$COMMON" "$COMMON"
  assert_contains 'Type "a" to delete'
  assert_contains 'Type "b" to delete'
  assert_no_dir "$a"
  assert_no_dir "$b"
}

test_review_the_fzf_prompt_stops_on_a_wrong_second_phrase() {
  make_repo
  add_wt a
  local a=$WT
  add_wt b
  local b=$WT
  must git -C "$a" init -q n
  must git -C "$b" init -q n
  wt_keys 'a\nnope\n' --force-delete "$a" "$b" "$COMMON" "$COMMON"
  assert_dir "$a"
  assert_dir "$b"
}

# ---- what the plan_id covers

test_review_a_stack_that_leaves_makes_the_plan_stale() {
  make_repo
  add_wt a
  local a=$WT
  add_wt b
  local b=$WT
  stack p "$a" running
  stack q "$a" running
  stack r "$b" running
  plan_ok delete-stacks -- p r
  assert_q "item('worktree:$a')['reason']['code']" other_stack
  assert_q "choice('stacks+worktrees')['adds']" "[\"worktree:$b\"]"
  drop_project q
  apply_ok delete-stacks "$PLAN_ID_OF" --choice 'stacks+worktrees' -- p r
  assert_rc 4
  assert_q 'doc["code"]' plan_stale
  assert_q "doc['plan']['choices'][1]['adds']" "[\"worktree:$a\", \"worktree:$b\"]"
  assert_dir "$a"
  assert_dir "$b"
  assert_no_docker_changes
}

test_review_a_stack_that_arrives_makes_the_plan_stale() {
  make_repo
  add_wt a
  local a=$WT
  add_wt b
  local b=$WT
  stack p "$a" exited
  stack r "$b" exited
  plan_ok delete-stacks -- p r
  stack other "$b" running
  apply_ok delete-stacks "$PLAN_ID_OF" --choice 'stacks+worktrees' -- p r
  assert_rc 4
  assert_q 'doc["code"]' plan_stale
  assert_dir "$a"
  assert_dir "$b"
  assert_no_docker_changes
}

test_review_a_stack_that_arrives_at_the_only_offer_is_stale_and_not_a_bad_choice() {
  make_repo
  add_wt a
  stack p "$WT" exited
  plan_ok delete-stacks -- p
  stack other "$WT" running
  apply_ok delete-stacks "$PLAN_ID_OF" --choice 'stacks+worktrees' -- p
  assert_rc 4
  assert_q 'doc["code"]' plan_stale
  assert_q "doc['plan']['items'][1]['reason']['code']" other_stack
}

test_review_more_changes_make_a_force_plan_stale() {
  make_repo
  add_wt a
  printf 'small\n' >"$WT/scratch.txt"
  plan_ok delete-worktrees --force -- "$WT"
  printf 'big\n' >"$WT/thesis.txt"
  apply_ok delete-worktrees "$PLAN_ID_OF" --force --choice worktrees -- "$WT"
  assert_rc 4
  assert_q 'doc["code"]' plan_stale
  [[ -f $WT/thesis.txt ]] || fail "the new file is gone"
}

test_review_a_further_edit_of_a_changed_file_makes_a_force_plan_stale() {
  make_repo
  add_wt a
  printf 'one\n' >>"$WT/file.txt"
  plan_ok delete-worktrees --force -- "$WT"
  printf 'two\n' >>"$WT/file.txt"
  wt core plan delete-worktrees --force -- "$WT"
  [[ $(q 'doc["plan_id"]') != "$PLAN_ID_OF" ]] || fail "the plan_id is the same after a further edit"
}

test_review_a_second_repo_inside_makes_a_force_plan_stale() {
  make_repo
  add_wt a
  # ignored, so that only the scan for repos can see the second one
  printf 'n1/\nn2/\n' >"$WT/.git-exclude"
  must git -C "$WT" config --local core.excludesFile "$WT/.git-exclude"
  must git -C "$WT" init -q n1
  printf '.git-exclude\n' >>"$WT/.git-exclude"
  plan_ok delete-worktrees --force -- "$WT"
  must git -C "$WT" init -q n2
  wt core plan delete-worktrees --force -- "$WT"
  [[ $(q 'doc["plan_id"]') != "$PLAN_ID_OF" ]] || fail "the plan_id is the same after a second repo"
  assert_q "'and 1 more' in item('worktree:$WT')['note']" true
}

test_review_the_same_state_gives_the_same_plan_id() {
  make_repo
  add_wt a
  printf 'x\n' >"$WT/untracked.txt"
  printf 'y\n' >>"$WT/file.txt"
  must git -C "$WT" init -q n1
  plan_ok delete-worktrees --force -- "$WT"
  local first=$PLAN_ID_OF
  plan_ok delete-worktrees --force -- "$WT"
  assert_eq "$PLAN_ID_OF" "$first"
  apply_ok delete-worktrees "$first" --force --choice worktrees --phrase a -- "$WT"
  assert_rc 0
  assert_no_dir "$WT"
}

test_review_directories_that_swap_between_stacks_change_the_plan_id() {
  make_repo
  must mkdir -p "$T/d/a" "$T/d/c" "$T/d/x" "$T/d/y"
  stack p "$T/d/a" running
  stack p "$T/d/x" running
  stack q "$T/d/c" running
  stack q "$T/d/y" running
  plan_ok delete-stacks -- p q
  : >"$WTREE_SHIM_DIR/containers"
  stack p "$T/d/a" running
  stack p "$T/d/y" running
  stack q "$T/d/c" running
  stack q "$T/d/x" running
  wt core plan delete-stacks -- p q
  [[ $(q 'doc["plan_id"]') != "$PLAN_ID_OF" ]] || fail "the plan_id is the same after the directories swapped"
}

test_review_a_stack_that_stops_still_does_not_make_a_plan_stale() {
  make_repo
  add_wt a
  stack p "$WT" running
  plan_ok delete-stacks -- p
  awk -F'\t' -v OFS='\t' '{ $3 = "exited"; print }' "$WTREE_SHIM_DIR/containers" >"$T/c"
  must cp "$T/c" "$WTREE_SHIM_DIR/containers"
  apply_ok delete-stacks "$PLAN_ID_OF" --choice stacks -- p
  assert_rc 0
}

# ---- targets

test_review_a_repeated_target_is_one_item_and_one_step() {
  make_repo
  add_wt a
  plan_ok delete-worktrees -- "$WT" "$WT"
  assert_q "[i['id'] for i in doc['items']]" "[\"worktree:$WT\"]"
  assert_q 'doc["steps"][0]["id"]' s1
  assert_q 'len(doc["steps"])' 1
  apply_ok delete-worktrees "$PLAN_ID_OF" --choice worktrees -- "$WT" "$WT"
  assert_rc 0
  assert_no_dir "$WT"
}

test_review_a_stack_side_offer_with_an_odd_path_stays() {
  local bad
  make_repo
  bad=$(worktree_path $'bad\xffname')
  must git -C "$REPO" worktree add -q --detach "$bad"
  stack p "$bad" exited
  wt_direct core plan delete-stacks -- p
  assert_rc 0
  assert_q '[i["reason"]["code"] for i in doc["items"] if i["kind"] == "worktree"]' '["odd_path"]'
  assert_q '[c["id"] for c in doc["choices"]]' '["stacks"]'
}

# ---- a registered path that goes through a symlink

test_review_the_current_worktree_guard_sees_through_a_symlink() {
  make_repo
  add_wt a
  local a=$WT
  relocate_code_behind_a_symlink
  cd "$HOME/data/worktrees/a" || exit 2
  printf 'mine\n' >keep.txt
  plan_ok delete-worktrees --force -- "$a"
  assert_q "item('worktree:$a')['reason']['code']" current
  apply_ok delete-worktrees "$PLAN_ID_OF" --choice worktrees --force -- "$a"
  assert_rc 4
  [[ -f $HOME/data/worktrees/a/keep.txt ]] || fail "the worktree of the caller was deleted"
}

test_review_a_clean_worktree_behind_a_symlink_is_readable_and_has_its_stacks() {
  make_repo
  add_wt a
  local a=$WT
  relocate_code_behind_a_symlink
  stack p "$a" running
  volume p p_data
  cd "$HOME" || exit 2
  plan_ok delete-worktrees --repo "$HOME/data/proj/.git" -- "$a"
  assert_q "item('worktree:$a')['go']" true
  assert_q "'stack:p' in [i['id'] for i in doc['items']]" true
  cd "$HOME/data/proj" || exit 2
  wt core list --no-refresh --stream
  assert_q "one('stack', 'project', 'p')['worktree']" "$a"
  cd "$HOME/data/worktrees/a" || exit 2
  wt core list --no-refresh --stream
  assert_q "one('worktree', 'path', '$a')['current']" true
}

# ---- the tiers of the cleanup view

test_review_cleanup_is_not_safe_when_the_volume_read_fails() {
  make_repo
  stack proj-gone "$HOME/code/worktrees/was-here" exited
  volume proj-gone precious
  add_wt merged
  stack proj-merged "$WT" exited
  container_meta id1 app 'Exited (0) 3 weeks ago' '' 2026-01-01T00:00:00.000000000Z 2026-01-02T00:00:00.000000000Z
  volume proj-merged data
  shim_fail 'volume ls *'
  wt core list --no-refresh --stream
  assert_rc 0
  assert_eq "$(cleanup_row stack:proj-gone "['tier']")" needs_look
  assert_eq "$(cleanup_row stack:proj-gone "['reasons'][0]['code']")" volumes_unknown
  assert_eq "$(cleanup_row "worktree:$WT" "['tier']")" needs_look
  assert_eq "$(cleanup_row "worktree:$WT" "['reasons'][-1]['code']")" volumes_unknown
}

test_review_cleanup_is_not_safe_when_docker_ps_fails() {
  make_repo
  add_wt merged
  stack proj-merged "$WT" running
  volume proj-merged data
  shim_fail 'ps *'
  wt core list --no-refresh --stream
  assert_rc 0
  assert_eq "$(cleanup_row "worktree:$WT" "['tier']")" needs_look
  assert_eq "$(cleanup_row "worktree:$WT" "['reasons'][0]['code']")" stacks_unknown
}

test_review_cleanup_is_not_safe_when_docker_ps_hangs() {
  make_repo
  add_wt merged
  shim_hang 'ps *' 30
  wt core list --no-refresh --stream
  assert_eq "$(cleanup_row "worktree:$WT" "['reasons'][0]['code']")" stacks_unknown
}

test_review_cleanup_without_docker_on_the_host_can_still_be_safe() {
  # no docker means no stacks, and that is known
  make_repo
  add_wt merged
  local keep=$PATH dir
  must mkdir -p "$T/nodocker"
  for dir in ${PATH//:/ }; do
    for tool in "$dir"/*; do
      [[ ${tool##*/} == docker ]] || ln -sf "$tool" "$T/nodocker/" 2>/dev/null
    done
  done
  PATH=$T/nodocker wt core list --no-refresh --stream
  PATH=$keep
  assert_eq "$(cleanup_row "worktree:$WT" "['tier']")" safe
}

test_review_a_project_in_two_directories_counts_for_both_in_any_order() {
  local order
  for order in main-last main-first; do
    make_repo
    add_wt "merged-$order"
    : >"$WTREE_SHIM_DIR/containers"
    if [[ $order == main-last ]]; then
      stack p "$WT" running
      stack p "$REPO" running
    else
      stack p "$REPO" running
      stack p "$WT" running
    fi
    wt core list --no-refresh --stream
    assert_eq "$(cleanup_row "worktree:$WT" "['tier']")" needs_look "$order"
    assert_eq "$(cleanup_row "worktree:$WT" "['reasons'][0]['code']")" merged_running "$order"
    assert_eq "$(cleanup_row "worktree:$WT" "['stacks']")" '["p"]' "$order"
    assert_q "one('stack', 'project', 'p')['worktree']" "$REPO"
    assert_q "one('stack', 'project', 'p')['worktrees']" "[\"$REPO\", \"$WT\"]"
    must git -C "$REPO" worktree remove --force "$WT"
    must rm -rf "$REPO"
  done
}

test_review_a_stack_in_two_rows_counts_its_disk_once() {
  make_repo
  add_wt one
  local one=$WT
  add_wt two
  local two=$WT
  stack p "$one" running
  stack p "$two" running
  must mkdir -p "$XDG_CACHE_HOME/wtree"
  printf 'p\t1000\n' >"$XDG_CACHE_HOME/wtree/disk-projects"
  printf 'Images\t1GB\n' >"$XDG_CACHE_HOME/wtree/disk-totals"
  wt core list --no-refresh --stream
  assert_q "sorted(x['bytes'] or 0 for x in events('cleanup'))" '[0, 0, 1000]'
}

# ---- facts of the list

test_review_a_locked_worktree_whose_directory_is_gone_is_missing() {
  make_repo
  add_wt usb
  local usb=$WT
  add_wt plain
  local plain=$WT
  must git -C "$REPO" worktree lock --reason usb "$usb"
  must rm -rf "$usb" "$plain"
  wt core list --no-refresh --stream
  assert_q "one('worktree', 'path', '$usb')['missing']" true
  assert_q "one('worktree', 'path', '$usb')['locked']" usb
  assert_q "one('worktree', 'path', '$plain')['missing']" true
  assert_q "one('worktree', 'path', '$REPO')['missing']" false
}

test_review_a_worktree_git_cannot_read_has_no_counts() {
  make_repo
  add_wt broken
  printf 'gitdir: /nonexistent\n' >"$WT/.git"
  wt core list --no-refresh --stream --only git
  assert_q "flat('worktree_git', 'path', '$WT')['unreadable']" true
  assert_q "flat('worktree_git', 'path', '$WT')['changed']" null
  assert_q "flat('worktree_git', 'path', '$WT')['untracked']" null
  assert_q "flat('worktree_git', 'path', '$WT')['head_held']" null
  assert_q "flat('worktree_git', 'path', '$REPO')['changed']" 0
}

test_review_only_takes_only_known_stages() {
  make_repo
  wt core list --only nosuch,git --no-refresh --stream
  assert_rc 2
  assert_q 'doc["code"]' usage
  wt core list --only git, --no-refresh --stream
  assert_rc 2
  wt core list --only git,cleanup --no-refresh --stream
  assert_rc 0
}

# ---- time limits and signals

test_review_a_git_that_hangs_on_a_stack_directory_cannot_hang_a_list() {
  make_repo
  add_wt alpha
  must mkdir -p "$HOME/deadmount"
  stack proj-dead "$HOME/deadmount" running
  git_hangs_on "$HOME/deadmount"
  WT_TIMEOUT=25 wt core list --stream --no-refresh --only structure
  assert_rc 0
  assert_q 'lines[-1]["event"]' end
}

test_review_a_git_that_hangs_on_a_stack_directory_cannot_hang_a_list_of_the_machine() {
  make_repo
  must mkdir -p "$HOME/deadmount"
  stack proj-dead "$HOME/deadmount" running
  git_hangs_on "$HOME/deadmount"
  WT_TIMEOUT=25 wt core list --global --stream --no-refresh --only structure
  assert_rc 0
  assert_q 'lines[-1]["event"]' end
}

test_review_a_git_that_hangs_on_a_stack_directory_cannot_hang_a_plan() {
  make_repo
  must mkdir -p "$HOME/deadmount"
  stack proj-dead "$HOME/deadmount" running
  git_hangs_on "$HOME/deadmount"
  WT_TIMEOUT=25 wt core plan delete-stacks -- proj-dead
  assert_rc 0
  assert_q 'doc["items"][0]["id"]' stack:proj-dead
  # and with volumes it cannot say that the stack has a worktree, so it is protected
  volume proj-dead data
  WT_TIMEOUT=25 wt core plan delete-stacks --volumes -- proj-dead
  assert_rc 0
  assert_q "choice('stacks')['confirm']['kind']" phrase
}

test_review_a_killed_list_lets_go_of_its_output_at_once() {
  local pid start line elapsed
  make_repo
  add_wt alpha
  stack proj-alpha "$WT" running
  shim_wedge 'ps *' 6
  shim_wedge 'volume ls *' 6
  export TMPDIR=$T/tmp
  must mkdir -p "$TMPDIR"
  must mkfifo "$T/f"
  "$WTREE" core list --stream --no-refresh >"$T/f" 2>"$T/err" &
  pid=$!
  exec 7<"$T/f"
  IFS= read -r line <&7
  sleep 0.5
  start=$SECONDS
  kill -TERM "$pid"
  wait "$pid" || true
  cat <&7 >/dev/null
  elapsed=$((SECONDS - start))
  exec 7<&-
  ((elapsed < 2)) || fail "the end of the output came after $elapsed s"
  sleep 1
  [[ ! -s $T/err ]] || fail "stderr after the end: $(cat "$T/err")"
  [[ -z $(ls -A "$TMPDIR") ]] || fail "the temp directory stayed"
}

test_review_a_git_that_ran_out_of_time_does_not_stay_behind() {
  local rc=0 left
  make_repo
  add_wt alpha
  git_hangs_on "$WT" status
  "$WTREE" core list --stream --no-refresh --only git >"$T/out" 2>&1 </dev/null || rc=$?
  assert_eq "$rc" 0
  assert_q "flat('worktree_git', 'path', '$WT')['timed_out']" true
  assert_q "flat('worktree_git', 'path', '$REPO')['timed_out']" false
  sleep 1.5
  left=$(ps -eo args | grep -F "hung-git-$$" | grep -v grep) || left=
  [[ -z $left ]] || fail "the git that hung is still there: $left"
}

test_review_a_closed_pipe_leaves_no_file_and_refreshes_the_disk() {
  local rc tries=0
  make_repo
  add_wt alpha
  local a=$WT
  add_wt beta
  stack proj-alpha "$a" exited
  stack proj-beta "$WT" exited
  shim_delay 'compose -p proj-alpha down' 1
  plan_ok delete-stacks -- proj-alpha proj-beta
  export TMPDIR=$T/tmp
  must mkdir -p "$TMPDIR"
  # head reads the start and the running line of the first step, and goes while the step runs
  { "$WTREE" core apply delete-stacks --plan "$PLAN_ID_OF" --choice stacks -- proj-alpha proj-beta 2>"$T/err"; } | head -n 2 >/dev/null
  until grep -q '^system df' "$WTREE_SHIM_DIR/docker.log" || ((tries > 60)); do
    sleep 0.1
    tries=$((tries + 1))
  done
  assert_log 'compose -p proj-alpha down'
  assert_not_log 'compose -p proj-beta down'
  grep -q '^system df' "$WTREE_SHIM_DIR/docker.log" || fail "no scan of the disk after a stack went"
  [[ -z $(ls -A "$TMPDIR") ]] || fail "a temp file stayed: $(ls -A "$TMPDIR")"
}

test_review_a_hup_leaves_no_file() {
  local pid rc=0 tries=0
  make_repo
  add_wt alpha
  stack proj-alpha "$WT" running
  plan_ok stop-stacks -- proj-alpha
  shim_delay 'compose -p proj-alpha stop' 1
  export TMPDIR=$T/tmp
  must mkdir -p "$TMPDIR"
  "$WTREE" core apply stop-stacks --plan "$PLAN_ID_OF" -- proj-alpha >"$T/o" 2>"$T/err" </dev/null &
  pid=$!
  until grep -q '"running"' "$T/o" 2>/dev/null || ((tries > 100)); do
    sleep 0.05
    tries=$((tries + 1))
  done
  kill -HUP "$pid"
  wait "$pid" || rc=$?
  assert_eq "$rc" 129
  [[ -z $(ls -A "$TMPDIR") ]] || fail "a temp file stayed: $(ls -A "$TMPDIR")"
  sleep 1.2
}

test_review_core_new_reports_a_directory_it_cannot_make() {
  make_repo
  printf x >"$HOME/code/worktrees"
  wt core new --no-open
  assert_rc 1
  assert_q 'lines[-1]["event"]' error
  assert_q 'lines[-1]["code"]' internal
}

test_review_core_list_reports_a_temp_directory_it_cannot_make() {
  local rc=0
  make_repo
  TMPDIR=/nonexistent/x "$WTREE" core list --no-refresh >"$T/out" 2>"$T/err" </dev/null || rc=$?
  assert_eq "$rc" 1
  assert_q 'lines[-1]["event"]' error
  assert_q 'lines[-1]["code"]' internal
}

test_review_core_apply_reports_a_temp_file_it_cannot_make_and_runs_nothing() {
  local rc=0
  make_repo
  add_wt alpha
  stack proj-alpha "$WT" running
  plan_ok stop-stacks -- proj-alpha
  TMPDIR=/nonexistent/x "$WTREE" core apply stop-stacks --plan "$PLAN_ID_OF" -- proj-alpha >"$T/out" 2>"$T/err" </dev/null || rc=$?
  assert_eq "$rc" 1
  assert_q 'lines[-1]["event"]' error
  assert_no_docker_changes
}
