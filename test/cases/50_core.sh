# The contract of wtree core: docs/core-contract.md. One plan test for each safety rule, then apply,
# then the list. Every call goes to the shim, and the JSON is read with python3 -I.
# shellcheck shell=bash

# the JSON in $T/out goes through python, so bytes that are not UTF-8 or a bad escape fail the test.
# arg: an expression over doc (the first line), lines (all of them), item(id), choice(id), events(name)
q() {
  python3 -I -c '
import json, sys
lines = [json.loads(l) for l in open(sys.argv[1], encoding="utf-8", errors="strict") if l.strip()]
doc = lines[0] if lines else None
def item(i): return next(x for x in doc["items"] if x["id"] == i)
def choice(i): return next(x for x in doc["choices"] if x["id"] == i)
def step(i): return next(x for x in doc["steps"] if x["id"] == i)
def events(n): return [l for l in lines if l.get("event") == n]
def one(n, k, v): return next(x[n] for x in lines if x.get("event") == n and x[n][k] == v)
def flat(n, k, v): return next(x for x in lines if x.get("event") == n and x[k] == v)
value = eval(sys.argv[2])
print(value if isinstance(value, str) else json.dumps(value, sort_keys=True, ensure_ascii=False))
' "$T/out" "$1" 2>&1 || {
    cat "$T/out"
    printf 'q failed: %s\n' "$1"
    exit 1
  }
}

assert_q() {
  local got
  got=$(q "$1") || fail "q failed: $got"
  [[ $got == "$2" ]] || fail "$1: expected [$2], got [$got]"
}

# every line of the output is JSON, and a read-only command changed nothing
assert_json_only() {
  q 'len(lines)' >/dev/null || fail "not JSON"
}

assert_only_reads() {
  local other
  other=$(grep -Ev '^(ps|stats|info|inspect|volume ls|system df)( |$)' "$WTREE_SHIM_DIR/docker.log") || other=
  [[ -z $other ]] || fail "calls that are not reads: $other"
}

# for bytes that are not UTF-8: timeout can be a Rust build that refuses them in its arguments
wt_direct() {
  RC=0
  "$WTREE" "$@" >"$T/out" 2>&1 </dev/null || RC=$?
}

# args: action, then its flags and targets. Sets PLAN_ID_OF.
plan_ok() {
  wt core plan "$@"
  assert_rc 0
  PLAN_ID_OF=$(q 'doc["plan_id"]')
  assert_eq "$(q 'doc["v"]')" 1
}

# args: action, the plan id, then --choice, --phrase, and the flags and targets
apply_ok() {
  local action=$1 id=$2
  shift 2
  wt core apply "$action" --plan "$id" "$@"
}

# ---- version, usage, errors

test_core_version() {
  wt core version
  assert_rc 0
  assert_q 'doc["v"]' 1
  assert_q 'doc["contract"]' 1
  assert_q 'isinstance(doc["script"], str)' true
}

test_core_version_needs_no_repo_and_no_docker() {
  mkdir -p "$HOME/plain"
  cd "$HOME/plain" || exit 2
  wt core version
  assert_rc 0
  [[ ! -s $WTREE_SHIM_DIR/docker.log ]] || fail "version called docker"
}

test_core_usage_errors_are_json_with_exit_2() {
  local args
  make_repo
  while IFS= read -r args; do
    # shellcheck disable=SC2086
    wt core $args
    assert_rc 2
    assert_q 'doc["event"]' error
    assert_q 'doc["code"]' usage
  done <<'CASES'
nosuch
plan
plan nosuch -- x
plan delete-worktrees
plan delete-worktrees --volumes -- x
plan delete-stacks --force -- x
plan delete-stacks --repo a -- x
plan stop-stacks --dir a -- x
plan stop-stacks --phrase a -- x
apply stop-stacks -- x
list --nosuch
list --only nosuch
open
new --nosuch
CASES
}

test_core_list_outside_a_repo_is_exit_3() {
  mkdir -p "$HOME/plain"
  cd "$HOME/plain" || exit 2
  wt core list
  assert_rc 3
  assert_q 'doc["code"]' not_a_repo
}

# ---- json_str

test_core_json_str_cases() {
  local long
  printf -v long '%04096d' 7
  wt core json-str 'plain' 'a"b' 'a\b' $'a\nb' $'a\tb' $'a\rb' $'a\001b' $'a\033[31mb' $'a\177b' 'é' '日本語' '😀' "$long" ''
  assert_q 'lines[0]' plain
  assert_q 'lines[1]' 'a"b'
  assert_q 'lines[2]' 'a\b'
  assert_q 'lines[3] == "a\nb"' true
  assert_q 'lines[4] == "a\tb"' true
  assert_q 'lines[5] == "a\rb"' true
  assert_q 'lines[6] == "a\x01b"' true
  assert_q 'lines[7] == "a\x1b[31mb"' true
  assert_q 'lines[8] == "a\x7fb"' true
  assert_q 'lines[9]' 'é'
  assert_q 'lines[10]' '日本語'
  assert_q 'lines[11]' '😀'
  assert_q 'len(lines[12])' 4096
  assert_q 'lines[13]' ''
}

test_core_json_str_replaces_bytes_that_are_not_utf8() {
  local bad
  for bad in $'a\xffb' $'a\xc3' $'\xc0\xaf' $'\xed\xa0\x80' $'\xf4\x90\x80\x80' $'\xe2\x82' $'\xf0\x9f\x98'; do
    wt_direct core json-str "$bad"
    assert_rc 0
    assert_q '"�" in lines[0]' true
  done
  wt_direct core json-str $'ok\xc3\xa9\xe2\x82\xac\xf0\x9f\x98\x80'
  assert_q 'lines[0]' 'oké€😀'
}

# ---- core plan: a worktree delete

test_core_plan_main_checkout_stays_at_every_level() {
  local flag
  make_repo
  add_wt alpha
  cd "$WT" || exit 2
  for flag in '' --force; do
    plan_ok delete-worktrees $flag -- "$REPO"
    assert_q "item('worktree:$REPO')['go']" false
    assert_q "item('worktree:$REPO')['reason']['code']" main_checkout
    assert_q 'doc["nothing_to_do"]' true
    assert_q 'doc["steps"]' '[]'
  done
  assert_dir "$REPO"
}

test_core_plan_current_worktree_stays_also_from_a_subdirectory() {
  local flag
  make_repo
  add_wt alpha
  mkdir -p "$WT/deep/er"
  cd "$WT/deep/er" || exit 2
  for flag in '' --force; do
    plan_ok delete-worktrees $flag -- "$WT"
    assert_q "item('worktree:$WT')['reason']['code']" current
  done
}

test_core_plan_current_worktree_stays_from_another_repo_inside_it() {
  make_repo
  add_wt outer
  must mkdir "$WT/other"
  must git -C "$WT/other" init -q -b main
  cd "$WT/other" || exit 2
  plan_ok delete-worktrees --force --repo "$COMMON" -- "$WT"
  assert_q "item('worktree:$WT')['reason']['code']" current
}

test_core_plan_changed_file_needs_force() {
  make_repo
  add_wt alpha
  printf 'edit\n' >>"$WT/file.txt"
  plan_ok delete-worktrees -- "$WT"
  assert_q "item('worktree:$WT')['go']" false
  assert_q "item('worktree:$WT')['reason']['code']" dirty
  assert_q "item('worktree:$WT')['force_ok']" true
  plan_ok delete-worktrees --force -- "$WT"
  assert_q "item('worktree:$WT')['go']" true
  assert_q "item('worktree:$WT')['note']" 'it has changed files'
  assert_q 'doc["confirm"]["kind"]' key
  assert_q 'step("s1")["force"]' true
}

test_core_plan_untracked_file_needs_force() {
  make_repo
  add_wt alpha
  printf 'new\n' >"$WT/new.txt"
  plan_ok delete-worktrees -- "$WT"
  assert_q "item('worktree:$WT')['reason']['code']" dirty
}

test_core_plan_unheld_detached_commit_needs_force() {
  make_repo
  add_detached_wt alpha
  commit_in "$WT"
  plan_ok delete-worktrees -- "$WT"
  assert_q "item('worktree:$WT')['reason']['code']" unheld_commit
  assert_q "item('worktree:$WT')['force_ok']" true
  plan_ok delete-worktrees --force -- "$WT"
  assert_q "item('worktree:$WT')['go']" true
}

test_core_plan_a_locked_worktree_stays_also_on_force() {
  local flag
  make_repo
  add_wt alpha
  must git -C "$REPO" worktree lock --reason 'on the usb disk' "$WT"
  for flag in '' --force; do
    plan_ok delete-worktrees $flag -- "$WT"
    assert_q "item('worktree:$WT')['go']" false
    assert_q "item('worktree:$WT')['reason']['code']" locked
    assert_q "'usb disk' in item('worktree:$WT')['reason']['text']" true
    assert_q "'force_ok' in item('worktree:$WT')" false
  done
}

test_core_plan_a_worktree_inside_stays_also_on_force() {
  local flag inner
  make_repo
  add_wt outer
  inner=$WT/nested
  must git -C "$REPO" worktree add -q -b inner "$inner"
  for flag in '' --force; do
    plan_ok delete-worktrees $flag -- "$WT"
    assert_q "item('worktree:$WT')['reason']['code']" nested_worktree
  done
}

test_core_plan_a_repo_inside_needs_force_and_the_typed_name() {
  make_repo
  add_wt outer
  must mkdir -p "$WT/node_modules/dep"
  must git -C "$WT/node_modules/dep" init -q -b main
  plan_ok delete-worktrees -- "$WT"
  assert_q "item('worktree:$WT')['reason']['code']" nested_repo
  plan_ok delete-worktrees --force -- "$WT"
  assert_q "item('worktree:$WT')['go']" true
  assert_q 'doc["confirm"]["kind"]' phrase
  assert_q 'doc["confirm"]["phrases"][0]["phrase"]' outer
}

test_core_plan_a_missing_directory_removes_one_entry() {
  make_repo
  add_wt aa
  rm -rf "$WT"
  plan_ok delete-worktrees --repo "$COMMON" -- "$WT"
  assert_q "item('worktree:$WT')['go']" true
  assert_q "item('worktree:$WT')['note']" 'its directory is missing'
  assert_q 'step("s1")["kind"]' remove_missing
  assert_q 'step("s1")["force"]' false
  assert_q 'step("s1")["label"]' 'remove missing worktree aa'
}

test_core_plan_a_directory_that_git_does_not_list_stays() {
  make_repo
  mkdir -p "$HOME/plain"
  plan_ok delete-worktrees --force --repo "$COMMON" -- "$HOME/plain"
  assert_q "item('worktree:$HOME/plain')['reason']['code']" not_registered
}

test_core_plan_a_wrong_repo_stays() {
  local other
  make_repo other
  other=$COMMON
  make_repo
  add_wt alpha
  plan_ok delete-worktrees --force --repo "$other" -- "$WT"
  assert_q "item('worktree:$WT')['reason']['code']" not_registered
}

test_core_plan_a_tab_or_newline_in_the_path_stays() {
  local odd
  make_repo
  for odd in $'odd\tname' $'odd\nname'; do
    add_wt plain
    must git -C "$REPO" worktree add -q --detach "$(worktree_path "$odd")"
    plan_ok delete-worktrees --force --repo "$COMMON" -- "$(worktree_path "$odd")"
    assert_q 'doc["items"][0]["reason"]["code"]' odd_path
    assert_q 'doc["nothing_to_do"]' true
    must git -C "$REPO" worktree remove --force "$(worktree_path "$odd")"
    must git -C "$REPO" worktree remove --force "$WT"
    must git -C "$REPO" branch -qD plain
  done
}

test_core_plan_one_clean_worktree_is_one_key() {
  make_repo
  add_wt alpha
  plan_ok delete-worktrees -- "$WT"
  assert_q 'doc["action"]' delete-worktrees
  assert_q 'doc["confirm"]' '{"kind": "key", "phrases": []}'
  assert_q '[c["id"] for c in doc["choices"]]' '["worktrees"]'
  assert_q 'doc["notes"][0]["code"]' ignored_files
  assert_q 'doc["nothing_to_do"]' false
  assert_q "item('worktree:$WT')['consequences'][0]['code']" ignored_files
}

test_core_plan_does_not_change_anything() {
  make_repo
  add_wt alpha
  stack p "$WT" running
  volume p p_data
  plan_ok delete-worktrees --force -- "$WT"
  plan_ok delete-stacks --volumes -- p
  plan_ok stop-stacks -- p
  assert_only_reads
  assert_dir "$WT"
  assert_registered "$WT"
}

test_core_plan_offers_the_stacks_of_a_worktree() {
  make_repo
  add_wt alpha
  stack p "$WT" running
  volume p p_data
  plan_ok delete-worktrees -- "$WT"
  assert_q '[c["id"] for c in doc["choices"]]' '["worktrees", "worktrees+stacks", "worktrees+stacks+volumes"]'
  assert_q "item('stack:p')['offered']" true
  assert_q "choice('worktrees+stacks')['adds']" '["stack:p"]'
  assert_q "choice('worktrees')['default']" true
  assert_q "choice('worktrees+stacks+volumes')['default']" false
  assert_q "choice('worktrees+stacks+volumes')['confirm']['kind']" key
  assert_q "choice('worktrees+stacks+volumes')['consequences'][0]['code']" volumes_removed
  assert_q "choice('worktrees+stacks+volumes')['consequences'][0]['names']" '["p_data"]'
  assert_q "choice('worktrees+stacks+volumes')['consequences'][0]['severe']" true
  assert_q "choice('worktrees')['steps']" '["s1"]'
  assert_q "step(choice('worktrees+stacks')['steps'][1])['volumes']" false
  assert_q "step(choice('worktrees+stacks+volumes')['steps'][1])['volumes']" true
  assert_q "step(choice('worktrees+stacks')['steps'][1])['after']" '["s1"]'
}

test_core_plan_volumes_of_two_stacks_from_a_worktree_need_the_phrase() {
  make_repo
  add_wt alpha
  stack pa "$WT" running
  stack pb "$WT" running
  plan_ok delete-worktrees -- "$WT"
  assert_q "choice('worktrees+stacks+volumes')['confirm']['phrases'][0]['phrase']" '2 stacks'
  assert_q "choice('worktrees+stacks')['confirm']['kind']" key
}

test_core_plan_volumes_of_a_project_outside_the_selection_need_the_phrase() {
  local alpha beta
  make_repo
  add_wt alpha
  alpha=$WT
  add_wt beta
  beta=$WT
  stack shared "$alpha" running
  stack shared "$beta" running
  plan_ok delete-worktrees -- "$alpha"
  assert_q "choice('worktrees+stacks+volumes')['confirm']['phrases'][0]['phrase']" shared
  assert_q "[c['code'] for c in item('stack:shared')['consequences']]" '["containers_removed", "runs_elsewhere"]'
  # with both worktrees selected, no directory is outside
  plan_ok delete-worktrees -- "$alpha" "$beta"
  assert_q "choice('worktrees+stacks+volumes')['confirm']['kind']" key
}

test_core_plan_a_protected_stack_inside_a_worktree_needs_its_name() {
  local inner
  make_repo
  add_wt alpha
  inner=$WT/node_modules/inner
  must mkdir -p "$inner"
  must git -C "$inner" init -q -b main
  stack inner "$inner" running
  plan_ok delete-worktrees --force -- "$WT"
  assert_q "[p['phrase'] for p in choice('worktrees+stacks+volumes')['confirm']['phrases']]" '["alpha", "inner"]'
  assert_q "[p['phrase'] for p in choice('worktrees')['confirm']['phrases']]" '["alpha"]'
}

test_core_plan_a_docker_that_fails_leaves_the_worktree_and_says_so() {
  make_repo
  add_wt alpha
  stack p "$WT" running
  shim_fail 'ps *'
  plan_ok delete-worktrees -- "$WT"
  assert_q "item('worktree:$WT')['go']" true
  assert_q '[c["id"] for c in doc["choices"]]' '["worktrees"]'
  assert_q 'doc["notes"][0]["code"]' docker_unavailable
  assert_q '"cannot see the stacks" in doc["notes"][0]["text"]' true
}

test_core_plan_volumes_stay_out_of_reach_if_docker_cannot_count_them() {
  make_repo
  add_wt alpha
  stack p "$WT" running
  volume p p_data
  shim_fail 'volume ls -q *'
  plan_ok delete-worktrees -- "$WT"
  assert_q "choice('worktrees+stacks+volumes')['available']" false
  assert_q "'nothing is deleted' in choice('worktrees+stacks+volumes')['reason']" true
  assert_q "choice('worktrees+stacks')['available']" true
}

# ---- core plan: a stack delete

setup_core_two_linked() {
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

test_core_plan_keep_volumes_is_one_key_also_for_a_main_checkout() {
  make_repo
  stack mainstack "$REPO" running
  volume mainstack mainstack_data
  plan_ok delete-stacks -- mainstack
  assert_q 'doc["confirm"]["kind"]' key
  assert_q 'step("s1")["volumes"]' false
  assert_q 'doc["notes"][0]["code"]' volumes_stay
}

test_core_plan_volumes_of_one_linked_stack_are_one_key() {
  make_repo
  add_wt alpha
  stack p "$WT" running
  volume p p_data
  plan_ok delete-stacks --volumes -- p
  assert_q 'doc["confirm"]["kind"]' key
  assert_q "choice('stacks')['consequences'][0]['names']" '["p_data"]'
  assert_q "choice('stacks+worktrees')['adds']" "[\"worktree:$WT\"]"
  assert_q "step(choice('stacks+worktrees')['steps'][-1])['after']" '["s1"]'
}

test_core_plan_volumes_of_two_stacks_need_the_count_from_either_list() {
  setup_core_two_linked
  plan_ok delete-stacks --volumes -- pa pb
  assert_q "choice('stacks')['confirm']['phrases'][0]['phrase']" '2 stacks'
  assert_q "choice('stacks+worktrees')['confirm']['phrases'][0]['phrase']" '2 stacks w'
  plan_ok delete-worktrees -- "$WT_A" "$WT_B"
  assert_q "choice('worktrees+stacks+volumes')['confirm']['phrases'][0]['phrase']" '2 stacks'
}

test_core_plan_protected_stacks_need_the_name_and_clear_the_offers() {
  make_repo
  add_wt alpha
  stack linked "$WT" running
  stack mainstack "$REPO" running
  plan_ok delete-stacks --volumes -- mainstack
  assert_q 'doc["confirm"]["phrases"][0]["phrase"]' mainstack
  plan_ok delete-stacks --volumes -- linked mainstack
  assert_q 'doc["confirm"]["phrases"][0]["phrase"]' '2 stacks'
  assert_q '[c["id"] for c in doc["choices"]]' '["stacks"]'
  assert_q "item('worktree:$WT')['go']" false
  assert_q "item('worktree:$WT')['reason']['code']" protected_stack
}

expect_core_protected() {
  plan_ok delete-stacks --volumes -- "$1"
  assert_q 'doc["confirm"]["kind"]' phrase
  assert_q 'doc["confirm"]["phrases"][0]["phrase"]' "$1"
}

test_core_plan_protected_kinds() {
  make_repo
  mkdir -p "$REPO/infra" "$HOME/plain"
  stack subdir "$REPO/infra" running
  stack plain "$HOME/plain" running
  stack gone "$HOME/nowhere" running
  stack nodir "" running
  volume only only_data
  must ln -s "$REPO" "$HOME/link"
  stack linked "$HOME/link" running
  add_wt alpha
  stack shared "$REPO" running
  stack shared "$WT" running
  for name in subdir plain gone nodir only linked shared; do
    expect_core_protected "$name"
  done
}

test_core_plan_the_volume_phrase_has_no_volumes_needed() {
  make_repo
  stack mainstack "$REPO" running
  plan_ok delete-stacks --volumes -- mainstack
  assert_q "choice('stacks')['consequences'][0]['count']" 0
  assert_q 'doc["confirm"]["kind"]' phrase
}

test_core_plan_volumes_stop_when_docker_cannot_say_where_a_stack_runs() {
  make_repo
  add_wt alpha
  stack p "$WT" running
  shim_fail 'ps *'
  plan_ok delete-stacks --volumes -- p
  assert_q 'doc["nothing_to_do"]' true
  assert_q 'doc["choices"]' '[]'
  assert_q "item('stack:p')['go']" false
  assert_q "item('stack:p')['reason']['code']" docker_unavailable
  plan_ok delete-stacks -- p
  assert_q 'doc["nothing_to_do"]' false
  assert_q '[c["id"] for c in doc["choices"]]' '["stacks"]'
  assert_q '"all the worktrees stay" in doc["notes"][0]["text"]' true
}

test_core_plan_volumes_stop_when_docker_cannot_count_them() {
  make_repo
  add_wt alpha
  stack p "$WT" running
  shim_hang 'volume ls -q *'
  WT_TIMEOUT=12
  plan_ok delete-stacks --volumes -- p
  assert_q 'doc["nothing_to_do"]' true
  assert_q '"Docker did not answer" in doc["notes"][0]["text"]' true
}

expect_core_not_offered() {
  plan_ok delete-stacks "${@:3}" -- "$1"
  assert_q "item('worktree:$2')['go']" false
  assert_q "item('worktree:$2')['offered']" true
  assert_q '[c["id"] for c in doc["choices"]]' '["stacks"]'
}

test_core_plan_a_dirty_worktree_is_not_offered() {
  make_repo
  add_wt alpha
  stack p "$WT" running
  printf 'edit\n' >>"$WT/file.txt"
  expect_core_not_offered p "$WT"
  assert_q "item('worktree:$WT')['reason']['code']" dirty
}

test_core_plan_a_locked_worktree_is_not_offered() {
  make_repo
  add_wt alpha
  stack p "$WT" running
  must git -C "$REPO" worktree lock "$WT"
  expect_core_not_offered p "$WT"
  assert_q "item('worktree:$WT')['reason']['code']" locked
}

test_core_plan_a_worktree_with_a_worktree_inside_is_not_offered() {
  make_repo
  add_wt outer
  must git -C "$REPO" worktree add -q -b inner "$WT/nested"
  stack p "$WT" running
  expect_core_not_offered p "$WT"
  assert_q "item('worktree:$WT')['reason']['code']" nested_worktree
}

test_core_plan_a_worktree_with_a_repo_inside_is_not_offered() {
  make_repo
  add_wt outer
  must mkdir -p "$WT/node_modules/dep"
  must git -C "$WT/node_modules/dep" init -q -b main
  stack p "$WT" running
  expect_core_not_offered p "$WT"
  assert_q "item('worktree:$WT')['reason']['code']" nested_repo
}

test_core_plan_a_worktree_stays_if_an_unselected_stack_runs_from_it() {
  make_repo
  add_wt alpha
  stack one "$WT" running
  stack two "$WT" running
  expect_core_not_offered one "$WT"
  assert_q "item('worktree:$WT')['reason']['code']" other_stack
  plan_ok delete-stacks -- one two
  assert_q "item('worktree:$WT')['go']" true
  assert_q "step(choice('stacks+worktrees')['steps'][-1])['after']" '["s1", "s2"]'
}

test_core_plan_the_current_worktree_and_the_main_checkout_are_not_offered() {
  make_repo
  add_wt alpha
  stack p "$WT" running
  stack m "$REPO" running
  cd "$WT" || exit 2
  plan_ok delete-stacks -- p m
  assert_q '[c["id"] for c in doc["choices"]]' '["stacks"]'
  assert_q "[i['id'] for i in doc['items'] if i['kind'] == 'worktree']" '[]'
}

test_core_plan_a_stale_directory_gets_no_offer() {
  make_repo
  add_wt alpha
  stack p "$WT" running
  must git -C "$REPO" worktree add -q -b other "$(worktree_path other)"
  plan_ok delete-stacks --dir "$(worktree_path other)" -- p
  assert_q '[c["id"] for c in doc["choices"]]' '["stacks"]'
}

test_core_plan_without_a_dir_looks_at_all_directories_of_the_project() {
  local alpha beta
  make_repo
  add_wt alpha
  alpha=$WT
  add_wt beta
  beta=$WT
  stack shared "$alpha" running
  stack shared "$beta" running
  plan_ok delete-stacks -- shared
  assert_q "sorted(choice('stacks+worktrees')['adds'])" "[\"worktree:$alpha\", \"worktree:$beta\"]"
}

test_core_plan_a_target_twice_counts_once() {
  make_repo
  add_wt alpha
  stack p "$WT" running
  plan_ok delete-stacks --volumes -- p p
  assert_q 'len(choice("stacks")["steps"])' 1
  assert_q "step(choice('stacks+worktrees')['steps'][-1])['after']" '["s1"]'
  assert_q 'doc["confirm"]["kind"]' key
}

test_core_plan_empty_names_are_ignored() {
  make_repo
  plan_ok delete-stacks -- ''
  assert_q 'doc["nothing_to_do"]' true
}

# ---- core plan: stop and open

test_core_plan_stop_one_stack_needs_no_confirmation() {
  make_repo
  add_wt alpha
  stack p "$WT" running
  plan_ok stop-stacks -- p
  assert_q 'doc["confirm"]["kind"]' none
  assert_q 'step("s1")["kind"]' stack_stop
  assert_q 'step("s1")["label"]' 'stop stack p'
  assert_only_reads
}

test_core_plan_stop_two_stacks_needs_one_key() {
  setup_core_two_linked
  plan_ok stop-stacks -- pa pb
  assert_q 'doc["confirm"]["kind"]' key
  assert_q 'len(doc["steps"])' 2
}

test_core_plan_open() {
  local a b
  make_repo
  add_wt aa
  a=$WT
  add_wt bb
  b=$WT
  plan_ok open -- "$a"
  assert_q 'doc["confirm"]["kind"]' none
  plan_ok open -- "$a" "$b"
  assert_q 'doc["confirm"]["kind"]' key
  plan_ok open -- "$HOME/nowhere"
  assert_q "item('dir:$HOME/nowhere')['reason']['code']" no_directory
  assert_q 'doc["nothing_to_do"]' true
}

# ---- core plan: the plan_id

test_core_plan_id_is_the_same_for_the_same_state() {
  make_repo
  add_wt alpha
  stack p "$WT" running
  volume p p_data
  plan_ok delete-worktrees -- "$WT"
  local first=$PLAN_ID_OF
  plan_ok delete-worktrees -- "$WT"
  assert_eq "$PLAN_ID_OF" "$first"
  [[ $first =~ ^[0-9a-f]{16}$ ]] || fail "not a short hash: $first"
}

test_core_plan_id_changes_with_the_flags_and_the_directory() {
  local plain force
  make_repo
  add_wt alpha
  plan_ok delete-worktrees -- "$WT"
  plain=$PLAN_ID_OF
  plan_ok delete-worktrees --force -- "$WT"
  force=$PLAN_ID_OF
  [[ $plain != "$force" ]] || fail "the same id with and without force"
  plan_ok delete-worktrees --cwd "$HOME" --repo "$COMMON" -- "$WT"
  [[ $PLAN_ID_OF != "$plain" ]] || fail "the same id from another directory"
}

# ---- core apply: refusals. Each proves that nothing ran.

assert_core_refused() {
  assert_rc 4
  assert_q 'lines[-1]["event"]' refused
  assert_q 'lines[-1]["code"]' "$1"
  assert_no_docker_changes
}

test_core_apply_plan_stale_when_a_file_is_created() {
  make_repo
  add_wt alpha
  plan_ok delete-worktrees -- "$WT"
  printf 'new\n' >"$WT/new.txt"
  apply_ok delete-worktrees "$PLAN_ID_OF" --choice worktrees -- "$WT"
  assert_core_refused plan_stale
  assert_dir "$WT"
  assert_q "lines[-1]['plan']['items'][0]['reason']['code']" dirty
}

test_core_apply_plan_stale_when_a_lock_comes() {
  make_repo
  add_wt alpha
  plan_ok delete-worktrees --force -- "$WT"
  must git -C "$REPO" worktree lock "$WT"
  apply_ok delete-worktrees "$PLAN_ID_OF" --choice worktrees --force -- "$WT"
  assert_core_refused plan_stale
  assert_dir "$WT"
}

test_core_apply_plan_stale_when_a_stack_gains_a_directory() {
  make_repo
  add_wt alpha
  stack p "$WT" running
  volume p p_data
  plan_ok delete-stacks --volumes -- p
  stack p "$REPO" running
  apply_ok delete-stacks "$PLAN_ID_OF" --choice stacks --volumes -- p
  assert_core_refused plan_stale
  assert_not_log "volume rm p_data"
  assert_q "lines[-1]['plan']['confirm']['phrases'][0]['phrase']" p
}

test_core_apply_plan_stale_when_a_volume_comes() {
  make_repo
  add_wt alpha
  stack p "$WT" running
  volume p p_data
  plan_ok delete-stacks --volumes -- p
  volume p p_more
  apply_ok delete-stacks "$PLAN_ID_OF" --choice stacks --volumes -- p
  assert_core_refused plan_stale
}

test_core_apply_plan_stale_when_the_user_moves_into_the_worktree() {
  make_repo
  add_wt alpha
  plan_ok delete-worktrees -- "$WT"
  cd "$WT" || exit 2
  apply_ok delete-worktrees "$PLAN_ID_OF" --choice worktrees -- "$WT"
  assert_core_refused plan_stale
  assert_dir "$WT"
}

test_core_apply_plan_stale_for_a_made_up_id() {
  make_repo
  add_wt alpha
  apply_ok delete-worktrees 0000000000000000 --choice worktrees -- "$WT"
  assert_core_refused plan_stale
  assert_dir "$WT"
}

test_core_apply_refuses_near_misses_of_the_phrase() {
  local answer
  setup_core_two_linked
  plan_ok delete-stacks --volumes -- pa pb
  for answer in y yes '2 stack' '2 STACKS' stacks 2 '3 stacks' '2 stacks x' '2  stacks' ' 2 stacks' '2 stacks ' '2 stacks.' '2 stacksw' '2 stacks w' pa ''; do
    apply_ok delete-stacks "$PLAN_ID_OF" --choice stacks --phrase "$answer" --volumes -- pa pb
    assert_core_refused phrase_mismatch
  done
  apply_ok delete-stacks "$PLAN_ID_OF" --choice 'stacks+worktrees' --phrase '2 stacks' --volumes -- pa pb
  assert_core_refused phrase_mismatch
  assert_dir "$WT_A"
  assert_not_log "volume rm pa_data"
}

test_core_apply_refuses_a_missing_phrase_and_extra_phrases() {
  setup_core_two_linked
  plan_ok delete-stacks --volumes -- pa pb
  apply_ok delete-stacks "$PLAN_ID_OF" --choice stacks --volumes -- pa pb
  assert_core_refused phrase_mismatch
  apply_ok delete-stacks "$PLAN_ID_OF" --choice stacks --phrase '2 stacks' --phrase '2 stacks' --volumes -- pa pb
  assert_core_refused phrase_mismatch
}

test_core_apply_two_phrases_come_in_order() {
  local inner
  make_repo
  add_wt alpha
  inner=$WT/node_modules/inner
  must mkdir -p "$inner"
  must git -C "$inner" init -q -b main
  stack inner "$inner" running
  plan_ok delete-worktrees --force -- "$WT"
  apply_ok delete-worktrees "$PLAN_ID_OF" --choice 'worktrees+stacks+volumes' --phrase inner --phrase alpha --force -- "$WT"
  assert_core_refused phrase_mismatch
  apply_ok delete-worktrees "$PLAN_ID_OF" --choice 'worktrees+stacks+volumes' --phrase alpha --force -- "$WT"
  assert_core_refused phrase_mismatch
  assert_dir "$WT"
}

test_core_apply_a_key_plan_needs_a_choice() {
  make_repo
  add_wt alpha
  plan_ok delete-worktrees -- "$WT"
  apply_ok delete-worktrees "$PLAN_ID_OF" -- "$WT"
  assert_core_refused no_choice
  assert_dir "$WT"
}

test_core_apply_refuses_an_unknown_choice() {
  make_repo
  add_wt alpha
  plan_ok delete-worktrees -- "$WT"
  apply_ok delete-worktrees "$PLAN_ID_OF" --choice 'worktrees+stacks+volumes' -- "$WT"
  assert_core_refused bad_choice
  assert_dir "$WT"
}

test_core_apply_refuses_a_choice_that_is_not_available() {
  make_repo
  add_wt alpha
  stack p "$WT" running
  volume p p_data
  shim_fail 'volume ls -q *'
  plan_ok delete-worktrees -- "$WT"
  apply_ok delete-worktrees "$PLAN_ID_OF" --choice 'worktrees+stacks+volumes' -- "$WT"
  assert_core_refused choice_unavailable
  assert_dir "$WT"
}

test_core_apply_refuses_a_plan_with_nothing_to_do() {
  make_repo
  plan_ok delete-worktrees -- "$REPO"
  apply_ok delete-worktrees "$PLAN_ID_OF" --choice worktrees -- "$REPO"
  assert_core_refused nothing_to_do
}

# ---- core apply: running

test_core_apply_one_worktree_events() {
  make_repo
  add_wt alpha
  plan_ok delete-worktrees -- "$WT"
  apply_ok delete-worktrees "$PLAN_ID_OF" --choice worktrees -- "$WT"
  assert_rc 0
  assert_no_dir "$WT"
  assert_not_registered "$WT"
  assert_q 'lines[0]["event"]' start
  assert_q 'lines[0]["steps"]' 1
  assert_q '[(e["id"], e["state"]) for e in events("step")]' '[["s1", "running"], ["s1", "ok"]]'
  assert_q 'events("step")[1]["ms"] >= 0' true
  assert_q 'lines[-1]["event"]' end
  assert_q '[lines[-1][k] for k in ("ok", "failed", "skipped", "disk_refresh", "cancelled")]' '[1, 0, 0, false, false]'
}

test_core_apply_never_reads_the_terminal() {
  local alpha
  make_repo
  add_wt alpha
  alpha=$WT
  stack p "$alpha" running
  volume p p_data
  plan_ok delete-stacks --volumes -- p
  # in a pty, with no answers: a prompt would show and wait
  wt_keys '' core apply delete-stacks --plan "$PLAN_ID_OF" --choice stacks --volumes -- p
  assert_rc 0
  assert_not_contains 'y/N'
  assert_not_contains 'Press a key'
  assert_not_contains 'Type "'
  assert_log "volume rm p_data"
}

test_core_apply_runs_without_a_terminal_and_stops_a_stack() {
  setup_core_two_linked
  plan_ok stop-stacks -- pa
  apply_ok stop-stacks "$PLAN_ID_OF" -- pa
  assert_rc 0
  assert_log "compose -p pa stop"
  assert_not_log "compose -p pb stop"
  assert_not_log "compose -p pa down"
}

test_core_apply_stop_of_two_stacks_needs_the_choice() {
  setup_core_two_linked
  plan_ok stop-stacks -- pa pb
  apply_ok stop-stacks "$PLAN_ID_OF" -- pa pb
  assert_core_refused no_choice
  apply_ok stop-stacks "$PLAN_ID_OF" --choice stacks -- pa pb
  assert_rc 0
  assert_log "compose -p pa stop"
  assert_log "compose -p pb stop"
}

test_core_apply_order_on_the_worktree_side() {
  make_repo
  add_wt alpha
  stack p "$WT" running
  volume p p_data
  volume p p_cache
  plan_ok delete-worktrees -- "$WT"
  apply_ok delete-worktrees "$PLAN_ID_OF" --choice 'worktrees+stacks+volumes' -- "$WT"
  assert_rc 0
  assert_no_dir "$WT"
  assert_log_order "volume ls -q --filter label=com.docker.compose.project=p" "compose -p p down"
  assert_log_order "compose -p p down" "volume rm p_data"
  assert_log_order "volume rm p_data" "volume rm p_cache"
  assert_q '[e["id"] for e in events("step") if e["state"] == "ok"]' "[\"s1\", \"s3\"]"
  assert_q 'lines[-1]["disk_refresh"]' true
}

test_core_apply_order_on_the_stack_side() {
  make_repo
  add_wt alpha
  stack one "$WT" running
  stack two "$WT" running
  volume two two_data
  plan_ok delete-stacks --volumes -- one two
  apply_ok delete-stacks "$PLAN_ID_OF" --choice 'stacks+worktrees' --phrase '2 stacks w' --volumes -- one two
  assert_rc 0
  assert_log_order "compose -p one down" "compose -p two down"
  assert_log_order "compose -p two down" "volume rm two_data"
  assert_no_dir "$WT"
  assert_q '[e["id"] for e in events("step") if e["state"] == "ok"]' '["s1", "s2", "s3"]'
}

test_core_apply_a_failed_stack_skips_its_worktree() {
  make_repo
  add_wt alpha
  stack one "$WT" running
  stack two "$WT" running
  shim_fail 'compose -p two down' 'the daemon said no'
  plan_ok delete-stacks -- one two
  apply_ok delete-stacks "$PLAN_ID_OF" --choice 'stacks+worktrees' -- one two
  assert_rc 1
  assert_dir "$WT"
  assert_registered "$WT"
  assert_q '[e["state"] for e in events("step") if e["id"] == "s2"]' '["running", "failed"]'
  assert_q 'flat("step", "state", "failed")["tail"]' '["the daemon said no"]'
  assert_q 'flat("step", "state", "skipped")["id"]' s3
  assert_q 'flat("step", "state", "skipped")["because"]' s2
  assert_q '[lines[-1][k] for k in ("ok", "failed", "skipped")]' '[1, 1, 1]'
}

test_core_apply_a_failed_worktree_skips_its_stack_and_keeps_the_others() {
  local stuck free
  make_repo_with_submodule
  add_wt stuck
  stuck=$WT
  init_submodule_in "$stuck"
  add_wt free
  free=$WT
  stack stuckstack "$stuck" running
  stack freestack "$free" running
  plan_ok delete-worktrees -- "$stuck" "$free"
  apply_ok delete-worktrees "$PLAN_ID_OF" --choice 'worktrees+stacks' -- "$stuck" "$free"
  assert_rc 1
  assert_dir "$stuck"
  assert_no_dir "$free"
  assert_log "compose -p freestack down"
  assert_not_log "compose -p stuckstack down"
  assert_q '[lines[-1][k] for k in ("ok", "failed", "skipped")]' '[2, 1, 1]'
}

test_core_apply_a_failed_down_keeps_the_volumes() {
  make_repo
  add_wt alpha
  stack p "$WT" running
  volume p p_data
  shim_fail 'compose -p p down'
  plan_ok delete-stacks --volumes -- p
  apply_ok delete-stacks "$PLAN_ID_OF" --choice stacks --volumes -- p
  assert_rc 1
  assert_not_log "volume rm p_data"
}

test_core_apply_keeps_only_the_last_eight_lines_in_the_tail_and_streams_all() {
  make_repo
  add_wt alpha
  stack p "$WT" running
  plan_ok stop-stacks -- p
  mkdir -p "$T/wrap"
  cat >"$T/wrap/docker" <<WRAP
#!/usr/bin/env bash
[[ \$1 == ps ]] && exec "$TEST_ROOT/shim/docker" "\$@"
for i in \$(seq 1 12); do printf 'line-%d-end\n' "\$i"; done
exit 1
WRAP
  chmod +x "$T/wrap/docker"
  export PATH=$T/wrap:$PATH
  apply_ok stop-stacks "$PLAN_ID_OF" -- p
  assert_rc 1
  assert_q 'len(events("output"))' 12
  assert_q 'flat("step", "state", "failed")["tail"][0]' line-5-end
  assert_q 'len(flat("step", "state", "failed")["tail"])' 8
}

# a guard at execution: the state changes while an earlier step takes long

test_core_apply_a_lock_during_an_earlier_step_keeps_the_worktree() {
  make_repo
  add_wt alpha
  stack p "$WT" running
  plan_ok delete-stacks -- p
  shim_delay 'compose -p p down' 1.5
  (
    wait_log_count '^compose -p p down' 1
    git -C "$REPO" worktree lock "$WT"
  ) >/dev/null 2>&1 &
  apply_ok delete-stacks "$PLAN_ID_OF" --choice 'stacks+worktrees' -- p
  assert_rc 1
  assert_dir "$WT"
  assert_registered "$WT"
  assert_q '"changed after the plan" in flat("step", "state", "failed")["tail"][0]' true
}

test_core_apply_a_change_during_an_earlier_step_keeps_the_worktree() {
  make_repo
  add_wt alpha
  stack p "$WT" running
  plan_ok delete-stacks -- p
  shim_delay 'compose -p p down' 1.5
  (
    wait_log_count '^compose -p p down' 1
    printf 'new\n' >"$WT/new.txt"
  ) >/dev/null 2>&1 &
  apply_ok delete-stacks "$PLAN_ID_OF" --choice 'stacks+worktrees' -- p
  assert_rc 1
  [[ -f $WT/new.txt ]] || fail "the new file is gone"
}

test_core_apply_a_stack_that_comes_during_an_earlier_step_keeps_the_worktree() {
  make_repo
  add_wt alpha
  stack p "$WT" running
  plan_ok delete-stacks -- p
  shim_delay 'compose -p p down' 1.5
  (
    wait_log_count '^compose -p p down' 1
    stack newcomer "$WT" running
  ) >/dev/null 2>&1 &
  apply_ok delete-stacks "$PLAN_ID_OF" --choice 'stacks+worktrees' -- p
  assert_rc 1
  assert_dir "$WT"
  assert_q '"newcomer" in flat("step", "state", "failed")["tail"][0]' true
}

test_core_apply_a_volume_that_comes_during_an_earlier_step_stops_that_stack() {
  setup_core_two_linked
  plan_ok delete-stacks --volumes -- pa pb
  shim_delay 'compose -p pa down' 1.5
  (
    wait_log_count '^compose -p pa down' 1
    volume pb pb_new
  ) >/dev/null 2>&1 &
  apply_ok delete-stacks "$PLAN_ID_OF" --choice stacks --phrase '2 stacks' --volumes -- pa pb
  assert_rc 1
  assert_log "volume rm pa_data"
  assert_not_log "compose -p pb down"
  assert_not_log "volume rm pb_data"
}

test_core_apply_a_directory_that_comes_during_an_earlier_step_stops_that_stack() {
  setup_core_two_linked
  plan_ok delete-stacks -- pa pb
  shim_delay 'compose -p pa down' 1.5
  (
    wait_log_count '^compose -p pa down' 1
    stack pb "$REPO" running
  ) >/dev/null 2>&1 &
  apply_ok delete-stacks "$PLAN_ID_OF" --choice stacks -- pa pb
  assert_rc 1
  assert_not_log "compose -p pb down"
}

# the same guard under the fzf UI: the prompt can wait for minutes
test_fzf_ui_a_lock_during_an_earlier_step_keeps_the_worktree() {
  make_repo
  add_wt alpha
  stack p "$WT" running
  shim_delay 'compose -p p down' 1.5
  (
    wait_log_count '^compose -p p down' 1
    git -C "$REPO" worktree lock "$WT"
  ) >/dev/null 2>&1 &
  wt_keys w --stack-delete p "$WT"
  assert_contains "✗ delete worktree alpha"
  assert_contains "It changed after the plan"
  assert_dir "$WT"
}

test_core_apply_a_term_stops_before_the_next_step() {
  setup_core_two_linked
  plan_ok delete-stacks -- pa pb
  shim_delay 'compose -p pa down' 1.5
  "$WTREE" core apply delete-stacks --plan "$PLAN_ID_OF" --choice stacks -- pa pb >"$T/out" 2>&1 </dev/null &
  local job=$!
  wait_log_count '^compose -p pa down' 1
  kill -TERM "$job"
  RC=0
  wait "$job" || RC=$?
  assert_rc 1
  assert_log "compose -p pa down"
  assert_not_log "compose -p pb down"
  assert_q 'flat("step", "state", "skipped")["because"]' cancelled
  assert_q 'lines[-1]["cancelled"]' true
  assert_q 'lines[-1]["ok"]' 1
}

# ---- the fzf UI and the core take one path

test_fzf_ui_and_core_see_the_same_refusal() {
  make_repo
  add_wt alpha
  printf 'edit\n' >>"$WT/file.txt"
  plan_ok delete-worktrees -- "$WT"
  assert_q "item('worktree:$WT')['reason']['text']" 'it has changed files'
  worktree_args "$WT"
  wt_keys y --delete "${ARGS[@]}"
  assert_contains "alpha: it has changed files, press ctrl-f to delete it"
}

# one implementation: the prompts and the steps go through plan_*, and only worktree_guard calls these
test_only_worktree_guard_reads_the_risks_of_a_worktree() {
  OUT=$(grep -n 'worktree_risk "\|nested_repos "\|lock_note "' "$WTREE" | grep -v '^[0-9]*:worktree_risk() \|^[0-9]*:nested_repos() ') || OUT=
  assert_eq "$(printf '%s\n' "$OUT" | wc -l | tr -d ' ')" 3
  assert_eq "$(grep -c 'read -r KEY\|read -r ANSWER\|read -r -n1' "$WTREE")" 3
}

# ---- core open and core new

test_core_open_streams_one_step_for_each_directory() {
  local a
  make_repo
  add_wt aa
  a=$WT
  wt core open -- "$a" "$HOME/nowhere"
  assert_rc 1
  assert_q 'lines[0]["steps"]' 2
  assert_q '[e["state"] for e in events("step") if e["id"] == "s1"]' '["running", "ok"]'
  assert_q '[e["state"] for e in events("step") if e["id"] == "s2"]' '["running", "failed"]'
  assert_q '"no tab support" in events("output")[0]["line"]' true
}

test_core_new_dry_run_changes_nothing() {
  make_repo
  wt core new --dry-run
  assert_rc 0
  assert_q 'doc["event"]' plan_new
  assert_q 'doc["head"]' "$(git -C "$REPO" rev-parse HEAD)"
  assert_eq "$(git -C "$REPO" worktree list --porcelain | grep -c '^worktree ')" 1
}

test_core_new_makes_a_detached_worktree_and_opens_it() {
  local path
  make_repo
  wt core new
  assert_rc 0
  assert_q 'doc["event"]' created
  path=$(q 'doc["path"]')
  assert_dir "$path"
  assert_registered "$path"
  assert_q 'events("step")[-1]["state"]' ok
}

test_core_new_uses_the_head_of_this_worktree() {
  make_repo
  add_wt alpha
  commit_in "$WT"
  cd "$WT" || exit 2
  wt core new --no-open
  assert_rc 0
  assert_q 'doc["head"]' "$(git -C "$WT" rev-parse HEAD)"
  assert_eq "$(git -C "$(q 'doc["path"]')" rev-parse HEAD)" "$(git -C "$WT" rev-parse HEAD)"
  assert_q 'len(lines)' 1
}

test_core_new_with_a_name_and_a_repo() {
  make_repo
  mkdir -p "$HOME/plain"
  cd "$HOME/plain" || exit 2
  wt core new --repo "$COMMON" --name feature-1 --no-open
  assert_rc 0
  assert_q 'doc["path"]' "$HOME/code/worktrees/feature-1"
  wt core new --repo "$COMMON" --name feature-1 --no-open
  assert_rc 4
  assert_q 'doc["code"]' name_taken
  local name
  for name in ../up a/b .hidden -x 'a b'; do
    wt core new --repo "$COMMON" --name "$name" --no-open
    assert_rc 4
    assert_q 'doc["code"]' bad_name
  done
}

test_core_new_outside_a_repo() {
  mkdir -p "$HOME/plain"
  cd "$HOME/plain" || exit 2
  wt core new
  assert_rc 3
  assert_q 'doc["code"]' not_a_repo
}

# ---- core list: the shape

setup_core_list() {
  make_repo
  add_wt alpha
  stack p "$WT" running 2.5 200MiB
  stack p "$WT" exited
  volume p p_data
}

test_core_list_document() {
  setup_core_list
  wt core list --no-refresh
  assert_rc 0
  assert_q 'doc["v"]' 1
  assert_q 'sorted(doc.keys())' '["cleanup", "cleanup_totals", "disk_totals", "engine", "repos", "stack_age", "stack_disk", "stack_stats", "stacks", "v", "warnings", "worktree_git", "worktrees"]'
  assert_q 'len(lines)' 1
  assert_q '[r["name"] for r in doc["repos"]]' '["proj"]'
  assert_q '[r["default_branch"] for r in doc["repos"]]' '["main"]'
  assert_q '[w["path"] for w in doc["worktrees"]]' "[\"$REPO\", \"$WT\"]"
  assert_q 'doc["worktrees"][0]["main"]' true
  assert_q 'doc["worktrees"][0]["current"]' true
  assert_q 'doc["worktrees"][1]["main"]' false
  assert_q 'doc["worktrees"][1]["branch"]' alpha
  assert_q 'doc["worktrees"][1]["locked"]' null
  assert_q 'doc["stacks"][0]["project"]' p
  assert_q 'doc["stacks"][0]["worktree"]' "$WT"
  assert_q 'doc["stacks"][0]["states"]' '{"exited": 1, "running": 1}'
  assert_q 'doc["stacks"][0]["volumes"]' 1
  assert_q 'doc["engine"]["ncpu"]' 8
  assert_q 'doc["warnings"]' '[]'
  assert_only_reads
}

test_core_list_stream_stages_come_in_order() {
  setup_core_list
  wt core list --stream --no-refresh
  assert_rc 0
  assert_q '[(e["stage"], e.get("done", False)) for e in events("stage")]' '[["structure", false], ["structure", true], ["git", false], ["git", true], ["stats", false], ["stats", true], ["disk", false], ["disk", true], ["cleanup", false], ["cleanup", true]]'
  assert_q 'lines[0]["event"]' stage
  assert_q 'lines[-1]["event"]' end
  assert_q '[e["event"] for e in lines[1:4]]' '["repo", "worktree", "worktree"]'
  assert_q 'all(e["v"] == 1 for e in lines)' true
  assert_q 'all("ms" in e for e in events("stage") if e.get("done"))' true
  assert_only_reads
}

test_core_list_the_names_come_before_docker_answers() {
  local tries=0
  setup_core_list
  shim_delay 'ps *' 3
  "$WTREE" core list --stream --no-refresh >"$T/stream" 2>&1 </dev/null &
  local job=$!
  until grep -q '"event":"worktree".*alpha' "$T/stream" 2>/dev/null; do
    tries=$((tries + 1))
    ((tries < 40)) || fail "no worktree within 2 seconds"
    sleep 0.05
  done
  if grep -q '"event":"stack"' "$T/stream"; then fail "the stacks came first"; fi
  wait "$job"
  grep -q '"event":"stack"' "$T/stream" || fail "the stacks never came"
}

test_core_list_only_gives_the_stages_that_were_asked() {
  setup_core_list
  wt core list --stream --only stats --no-refresh
  assert_rc 0
  assert_q '[e["stage"] for e in events("stage")]' '["stats", "stats"]'
  assert_q 'events("worktree")' '[]'
  assert_q 'flat("stack_stats", "project", "p")["cpu_pct"]' 2.5
  wt core list --stream --only git --no-refresh
  assert_q 'len(events("worktree_git"))' 2
  assert_q 'events("stack")' '[]'
}

test_core_list_cwd_chooses_the_repo() {
  setup_core_list
  mkdir -p "$HOME/plain"
  cd "$HOME/plain" || exit 2
  wt core list --cwd "$WT" --no-refresh
  assert_rc 0
  assert_q 'doc["worktrees"][1]["current"]' true
  wt core list --cwd "$HOME/nowhere"
  assert_rc 3
}

test_core_list_never_changes_git_state() {
  local before after
  setup_core_list
  printf 'edit\n' >>"$WT/file.txt"
  before=$(stat -c %Y "$(git -C "$WT" rev-parse --git-path index)" 2>/dev/null)
  sleep 1.1
  touch "$WT/file.txt"
  wt core list --no-refresh
  after=$(stat -c %Y "$(git -C "$WT" rev-parse --git-path index)" 2>/dev/null)
  assert_eq "$after" "$before" "the list wrote the index"
}

# ---- core list: failures

test_core_list_a_hung_docker_gives_warnings_and_a_usable_list() {
  local started=$SECONDS
  setup_core_list
  shim_hang 'ps *'
  shim_hang 'volume ls *'
  shim_hang 'stats *'
  WT_TIMEOUT=20
  wt core list --stream --no-refresh
  assert_rc 0
  ((SECONDS - started < 12)) || fail "it took $((SECONDS - started)) seconds"
  assert_q 'flat("warning", "code", "docker_timeout")["code"]' docker_timeout
  assert_q 'len(events("worktree"))' 2
  assert_q 'len(events("worktree_git"))' 2
  assert_q 'events("stack")' '[]'
  assert_q 'lines[-1]["event"]' end
}

test_core_list_a_docker_that_ignores_term_gives_a_warning() {
  setup_core_list
  shim_wedge 'ps *' 15
  WT_TIMEOUT=20
  wt core list --stream --no-refresh --only structure
  assert_rc 0
  assert_q 'flat("warning", "code", "docker_timeout")["code"]' docker_timeout
}

test_core_list_a_stopped_docker_gives_a_warning() {
  setup_core_list
  shim_fail 'ps *' 'cannot connect'
  wt core list --no-refresh
  assert_rc 0
  assert_q '[w["code"] for w in doc["warnings"]]' '["docker_down"]'
  assert_q 'len(doc["worktrees"])' 2
  assert_q 'doc["engine"]' null
}

test_core_list_no_docker_on_the_host_gives_a_warning() {
  local tools=$T/nodocker tool
  setup_core_list
  must mkdir "$tools"
  for tool in "$WTREE_TEST_SCRATCH"/tools/*; do must ln -s "$tool" "$tools/"; done
  PATH=$tools wt core list --no-refresh
  assert_rc 0
  assert_q '[w["code"] for w in doc["warnings"]]' '["docker_missing"]'
  assert_q 'len(doc["worktrees"])' 2
}

test_core_list_a_failed_stats_leaves_the_stacks() {
  setup_core_list
  shim_fail 'stats *'
  wt core list --no-refresh
  assert_rc 0
  assert_q '[w["code"] for w in doc["warnings"]]' '["stats_unavailable"]'
  assert_q 'doc["stack_stats"]' '[]'
  assert_q 'doc["engine"]["cpu_pct"]' null
  assert_q 'doc["engine"]["ncpu"]' 8
  assert_q 'len(doc["stacks"])' 1
}

test_core_list_a_failed_info_or_inspect_or_volume_read_gives_a_warning() {
  setup_core_list
  shim_fail 'info *'
  shim_fail 'inspect *'
  shim_fail 'volume ls *'
  wt core list --no-refresh
  assert_rc 0
  assert_q 'sorted(w["code"] for w in doc["warnings"])' '["info_unavailable", "inspect_unavailable", "volumes_unavailable"]'
  assert_q 'doc["stacks"][0]["volumes"]' null
  assert_q 'doc["engine"]["ncpu"]' null
  assert_q 'doc["stack_age"]' '[]'
}

# ---- core list: the facts of a worktree

test_core_list_git_facts_of_a_changed_worktree() {
  make_repo
  add_wt alpha
  printf 'edit\n' >>"$WT/file.txt"
  printf 'new\n' >"$WT/new.txt"
  wt core list --stream --no-refresh --only git
  assert_q 'flat("worktree_git", "path", "'"$WT"'")["changed"]' 2
  assert_q 'flat("worktree_git", "path", "'"$WT"'")["untracked"]' 1
  assert_q 'flat("worktree_git", "path", "'"$REPO"'")["changed"]' 0
  assert_q 'flat("worktree_git", "path", "'"$WT"'")["last_commit_subject"]' init
  assert_q 'flat("worktree_git", "path", "'"$WT"'")["last_commit_at"] > 0' true
  assert_q 'flat("worktree_git", "path", "'"$WT"'")["index_mtime"] > 0' true
  assert_q 'flat("worktree_git", "path", "'"$WT"'")["head_held"]' true
}

test_core_list_ahead_and_behind_and_gone() {
  local clone
  make_repo
  clone=$HOME/code/clone
  must git clone -q "$REPO" "$clone"
  must git -C "$clone" worktree add -q -b topic "$HOME/code/topic" origin/main
  must git -C "$HOME/code/topic" branch -q --set-upstream-to=origin/main
  commit_in "$HOME/code/topic"
  printf 'x\n' >"$REPO/other.txt"
  must git -C "$REPO" add -A
  must git -C "$REPO" commit -qm other
  must git -C "$clone" fetch -q
  cd "$clone" || exit 2
  wt core list --no-refresh --stream --only git
  local path=$HOME/code/topic
  assert_q 'flat("worktree_git", "path", "'"$path"'")["upstream"]' origin/main
  assert_q 'flat("worktree_git", "path", "'"$path"'")["ahead"]' 1
  assert_q 'flat("worktree_git", "path", "'"$path"'")["behind"]' 1
  assert_q 'flat("worktree_git", "path", "'"$path"'")["upstream_gone"]' false
  must git -C "$REPO" branch -q gone-branch
  must git -C "$clone" fetch -q
  must git -C "$clone" worktree add -q -b gone-local "$HOME/code/gl" origin/gone-branch
  must git -C "$HOME/code/gl" branch -q --set-upstream-to=origin/gone-branch
  must git -C "$REPO" branch -qD gone-branch
  must git -C "$clone" fetch -q --prune
  wt core list --no-refresh --stream --only git
  assert_q 'flat("worktree_git", "path", "'"$HOME/code/gl"'")["upstream_gone"]' true
  assert_q 'flat("worktree_git", "path", "'"$HOME/code/gl"'")["ahead"]' null
}

test_core_list_merged_into_the_default_branch() {
  local merged open
  make_repo
  add_wt merged
  merged=$WT
  add_wt open
  open=$WT
  commit_in "$open"
  wt core list --no-refresh --stream --only git
  assert_q 'flat("worktree_git", "path", "'"$merged"'")["merged_into"]' main
  assert_q 'flat("worktree_git", "path", "'"$open"'")["merged_into"]' null
  assert_q 'flat("worktree_git", "path", "'"$REPO"'")["merged_into"]' null
}

test_core_list_uses_origin_head_for_the_default_branch() {
  local clone
  make_repo
  must git -C "$REPO" branch -q trunk
  must git -C "$REPO" symbolic-ref HEAD refs/heads/trunk
  clone=$HOME/code/clone
  must git clone -q "$REPO" "$clone"
  cd "$clone" || exit 2
  wt core list --no-refresh --stream --only structure
  assert_q 'events("repo")[0]["repo"]["default_branch"]' trunk
}

test_core_list_detached_held_and_unheld() {
  local held unheld
  make_repo
  add_detached_wt held
  held=$WT
  add_detached_wt unheld
  unheld=$WT
  commit_in "$unheld"
  wt core list --no-refresh --stream --only git
  assert_q 'flat("worktree_git", "path", "'"$held"'")["head_held"]' true
  assert_q 'flat("worktree_git", "path", "'"$unheld"'")["head_held"]' false
}

test_core_list_a_locked_and_a_missing_worktree() {
  local gone
  make_repo
  add_wt locked
  must git -C "$REPO" worktree lock --reason 'on the usb disk' "$WT"
  local plain_lock=$(worktree_path plainlock)
  must git -C "$REPO" worktree add -q -b plainlock "$plain_lock"
  must git -C "$REPO" worktree lock "$plain_lock"
  add_wt gone
  gone=$WT
  rm -rf "$gone"
  wt core list --no-refresh --stream --only structure,git
  assert_q 'one("worktree", "path", "'"$(worktree_path locked)"'")["locked"]' 'on the usb disk'
  assert_q 'one("worktree", "path", "'"$plain_lock"'")["locked"]' ''
  assert_q 'one("worktree", "path", "'"$gone"'")["missing"]' true
  assert_q 'flat("worktree_git", "path", "'"$gone"'")["missing"]' true
}

test_core_list_a_slow_worktree_does_not_hold_the_list_up() {
  local real=$(command -v git) started=$SECONDS
  make_repo
  add_wt alpha
  must mkdir "$T/slowgit"
  cat >"$T/slowgit/git" <<SCRIPT
#!/bin/bash
for arg in "\$@"; do [[ \$arg != status ]] || sleep 30; done
exec "$real" "\$@"
SCRIPT
  must chmod +x "$T/slowgit/git"
  export PATH=$T/slowgit:$PATH
  WT_TIMEOUT=25
  wt core list --no-refresh --stream --only git
  assert_rc 0
  ((SECONDS - started < 20)) || fail "it took $((SECONDS - started)) seconds"
  assert_q 'flat("worktree_git", "path", "'"$WT"'")["timed_out"]' true
}

# ---- core list: the facts of a stack

test_core_list_containers_have_service_state_health_and_ports() {
  make_repo
  add_wt alpha
  stack p "$WT" running
  stack p "$WT" exited
  container_meta id0 api 'Up 2 hours (healthy)' '0.0.0.0:8080->80/tcp, [::]:8080->80/tcp, 0.0.0.0:5555->5555/tcp, 9000/tcp' '' '' p-api-1
  container_meta id1 db 'Exited (1) 3 days ago' '' '2026-01-01T00:00:00.000000000Z' '2026-02-01T00:00:00.000000000Z' p-db-1
  wt core list --no-refresh --stream --only structure
  assert_q 'events("stack")[0]["stack"]["containers"][0]["service"]' api
  assert_q 'events("stack")[0]["stack"]["containers"][0]["name"]' p-api-1
  assert_q 'events("stack")[0]["stack"]["containers"][0]["state"]' running
  assert_q 'events("stack")[0]["stack"]["containers"][0]["health"]' healthy
  assert_q 'events("stack")[0]["stack"]["containers"][0]["published"]' '["8080", "5555"]'
  assert_q 'events("stack")[0]["stack"]["containers"][1]["health"]' null
  assert_q 'events("stack")[0]["stack"]["containers"][1]["published"]' '[]'
}

test_core_list_stats_of_a_stack_and_its_containers() {
  make_repo
  add_wt alpha
  stack p "$WT" running 2.5 200MiB
  stack p "$WT" running 1.5 1GiB
  stack other "$REPO" running 1.0 100MiB
  wt core list --no-refresh --stream --only stats
  assert_q 'flat("stack_stats", "project", "p")["cpu_pct"]' 4.0
  assert_q 'flat("stack_stats", "project", "p")["mem_bytes"]' 1283457024
  assert_q 'len(flat("stack_stats", "project", "p")["containers"])' 2
  assert_q 'events("engine")[0]["cpu_pct"]' 5.0
  assert_q 'events("engine")[0]["mem_total"]' 8589934592
}

test_core_list_age_of_a_stack_comes_from_its_containers() {
  make_repo
  add_wt alpha
  stack old "$WT" exited
  stack old "$WT" exited
  stack fresh "$WT" exited
  stack up "$WT" running
  container_meta id0 a 'Exited (0)' '' '2020-01-01T00:00:00.000000000Z' '2020-01-02T00:00:00.000000000Z'
  container_meta id1 b 'Exited (0)' '' '2020-01-01T00:00:00.000000000Z' '2020-01-03T00:00:00.000000000Z'
  wt core list --no-refresh --stream --only stats
  assert_q 'flat("stack_age", "project", "old")["last_finished_at"]' 1578009600
  assert_q 'flat("stack_age", "project", "old")["last_started_at"]' 1577836800
  assert_q 'flat("stack_age", "project", "old")["stale"]' true
  assert_q 'flat("stack_age", "project", "old")["running"]' false
  assert_q 'flat("stack_age", "project", "fresh")["stale"]' false
  assert_q 'flat("stack_age", "project", "up")["running"]' true
  assert_q 'flat("stack_age", "project", "up")["last_finished_at"]' null
}

test_core_list_scope_of_a_repo_and_of_the_machine() {
  local mine other
  make_repo other
  other=$REPO
  stack otherstack "$other" running
  volume stray stray_data
  make_repo mine
  mine=$REPO
  stack minestack "$mine" running
  volume mine-old mine-old_data
  wt core list --no-refresh --stream --only structure
  assert_q 'sorted(e["stack"]["project"] for e in events("stack"))' '["mine-old", "minestack"]'
  assert_q 'one("stack", "project", "mine-old")["kind"]' volumes
  assert_q 'one("stack", "project", "mine-old")["dir"]' null
  wt core list --global --no-refresh --stream --only structure
  assert_q 'sorted(e["stack"]["project"] for e in events("stack"))' '["mine-old", "minestack", "otherstack", "stray"]'
  assert_q 'sorted(e["repo"]["name"] for e in events("repo"))' '["mine", "other"]'
  assert_only_reads
}

test_core_list_global_finds_a_repo_outside_the_roots_through_its_stack() {
  local outside=$T/outside
  mkdir -p "$outside"
  must git -C "$outside" init -q -b main
  must git -C "$outside" -c user.name=x -c user.email=x@example.invalid commit -q --allow-empty -m init
  stack far "$outside" running
  wt core list --global --no-refresh --stream --only structure
  assert_q 'one("stack", "project", "far")["worktree"]' "$outside"
  assert_q '[e["repo"]["name"] for e in events("repo")]' '["outside"]'
}

test_core_list_global_a_slow_first_scan_goes_on_behind_the_list() {
  local real=$(command -v find) started=$SECONDS
  must mkdir "$T/slowfind"
  cat >"$T/slowfind/find" <<SCRIPT
#!/bin/bash
sleep 8
exec "$real" "\$@"
SCRIPT
  must chmod +x "$T/slowfind/find"
  make_repo
  export PATH=$T/slowfind:$PATH
  wt core list --global --no-refresh --stream --only structure
  assert_rc 0
  ((SECONDS - started < 7)) || fail "it took $((SECONDS - started)) seconds"
  assert_q 'flat("warning", "code", "repo_scan_running")["code"]' repo_scan_running
  assert_q '[e["repo"]["name"] for e in events("repo")]' '["proj"]'
}

# ---- core list: the disk

setup_core_disk_cache() {
  mkdir -p "$XDG_CACHE_HOME/wtree"
  printf 'p\t3000000000\nother\t5\n' >"$XDG_CACHE_HOME/wtree/disk-projects"
  printf 'Images\t10.5GB\nContainers\t1.2GB\nLocal Volumes\t5GB\nBuild Cache\t200MB\n' >"$XDG_CACHE_HOME/wtree/disk-totals"
}

test_core_list_reads_the_disk_from_the_cache() {
  setup_core_list
  setup_core_disk_cache
  wt core list --stream --only structure,disk --no-refresh
  assert_q 'flat("stack_disk", "project", "p")["bytes"]' 3000000000
  assert_q 'flat("stack_disk", "project", "p")["age_s"] >= 0' true
  assert_q '[e["project"] for e in events("stack_disk")]' '["p"]'
  assert_q 'events("disk_totals")[0]["images"]' 10500000000
  assert_q 'events("disk_totals")[0]["volumes"]' 5000000000
  assert_q 'events("disk_totals")[0]["refreshing"]' false
  if grep -q '^system df' "$WTREE_SHIM_DIR/docker.log"; then fail "docker system df ran"; fi
}

test_core_list_refreshes_an_old_disk_cache_behind_the_list() {
  setup_core_list
  wt core list --stream --only disk
  assert_rc 0
  assert_q 'events("disk_totals")[0]["images"]' null
  wait_file "$XDG_CACHE_HOME/wtree/disk-totals"
}

test_core_list_no_refresh_starts_no_scan() {
  setup_core_list
  wt core list --no-refresh
  sleep 0.3
  [[ ! -e $XDG_CACHE_HOME/wtree/disk-totals ]] || fail "the disk cache was refreshed"
  if grep -q '^system df' "$WTREE_SHIM_DIR/docker.log"; then fail "docker system df ran"; fi
}

# ---- core list: the cleanup tiers

# args: id, tier, code of the first reason
assert_cleanup() {
  assert_q "flat('cleanup', 'id', '$1')['tier']" "$2"
  assert_q "flat('cleanup', 'id', '$1')['reasons'][0]['code']" "$3"
}

assert_no_cleanup_row() {
  assert_q "[e for e in events('cleanup') if e['id'] == '$1']" '[]'
}

test_core_cleanup_never() {
  local locked current
  make_repo
  add_wt locked
  locked=$WT
  must git -C "$REPO" worktree lock "$locked"
  add_wt current
  current=$WT
  cd "$current" || exit 2
  wt core list --stream --no-refresh
  assert_cleanup "worktree:$REPO" never main_checkout
  assert_cleanup "worktree:$current" never current
  assert_cleanup "worktree:$locked" never locked
}

test_core_cleanup_safe_merged_with_no_stack() {
  make_repo
  add_wt alpha
  wt core list --stream --no-refresh
  assert_cleanup "worktree:$WT" safe merged
  assert_q "'main' in flat('cleanup', 'id', 'worktree:$WT')['reasons'][0]['text']" true
}

test_core_cleanup_a_worktree_with_its_own_commits_has_no_row() {
  make_repo
  add_wt alpha
  commit_in "$WT"
  wt core list --stream --no-refresh
  assert_no_cleanup_row "worktree:$WT"
}

test_core_cleanup_needs_a_look_for_a_changed_file() {
  make_repo
  add_wt alpha
  printf 'edit\n' >>"$WT/file.txt"
  wt core list --stream --no-refresh
  assert_cleanup "worktree:$WT" needs_look dirty
}

test_core_cleanup_needs_a_look_for_an_unheld_commit() {
  make_repo
  add_detached_wt alpha
  commit_in "$WT"
  wt core list --stream --no-refresh
  assert_cleanup "worktree:$WT" needs_look unheld_commit
}

test_core_cleanup_needs_a_look_for_a_running_stack_on_a_merged_branch() {
  make_repo
  add_wt alpha
  stack p "$WT" running
  wt core list --stream --no-refresh
  assert_cleanup "worktree:$WT" needs_look merged_running
}

test_core_cleanup_a_merged_worktree_with_old_stacks_is_safe_and_counts_their_disk() {
  make_repo
  add_wt alpha
  stack p "$WT" exited
  container_meta id0 a 'Exited (0)' '' '2020-01-01T00:00:00.000000000Z' '2020-01-02T00:00:00.000000000Z'
  mkdir -p "$XDG_CACHE_HOME/wtree"
  printf 'p\t1000\n' >"$XDG_CACHE_HOME/wtree/disk-projects"
  printf 'Images\t1GB\n' >"$XDG_CACHE_HOME/wtree/disk-totals"
  wt core list --stream --no-refresh
  assert_cleanup "worktree:$WT" safe merged
  assert_q "flat('cleanup', 'id', 'worktree:$WT')['bytes']" 1000
  assert_q "flat('cleanup', 'id', 'worktree:$WT')['stacks']" '["p"]'
  assert_q "events('cleanup_totals')[0]['safe']" 1000
  assert_no_cleanup_row "stack:p"
}

test_core_cleanup_a_merged_worktree_with_a_recent_stack_needs_a_look() {
  make_repo
  add_wt alpha
  stack p "$WT" exited
  wt core list --stream --no-refresh
  assert_cleanup "worktree:$WT" needs_look merged_recent_stack
}

test_core_cleanup_a_merged_worktree_with_volumes_needs_a_look() {
  make_repo
  add_wt alpha
  stack p "$WT" exited
  volume p p_data
  container_meta id0 a 'Exited (0)' '' '2020-01-01T00:00:00.000000000Z' '2020-01-02T00:00:00.000000000Z'
  wt core list --stream --no-refresh
  assert_q "flat('cleanup', 'id', 'worktree:$WT')['tier']" needs_look
  assert_q "flat('cleanup', 'id', 'worktree:$WT')['reasons'][1]['code']" volumes
}

test_core_cleanup_volumes_only_needs_a_look() {
  make_repo proj
  volume proj-old proj-old_data
  wt core list --stream --no-refresh
  assert_cleanup "stack:proj-old" needs_look volumes_only
}

test_core_cleanup_a_stack_whose_worktree_is_gone() {
  make_repo proj
  stack other-thing "$HOME/code/proj-spike" exited
  stack proj-named "$HOME/code/worktrees/spike" exited
  stack proj-vols "$HOME/code/worktrees/proj-vols" exited
  volume proj-vols proj-vols_data
  wt core list --stream --no-refresh
  assert_cleanup "stack:other-thing" needs_look worktree_gone_unnamed
  assert_cleanup "stack:proj-named" safe worktree_gone
  assert_cleanup "stack:proj-vols" needs_look gone_volumes
}

test_core_cleanup_a_stale_stack_of_a_live_worktree_needs_a_look() {
  make_repo
  add_wt alpha
  commit_in "$WT"
  stack p "$WT" exited
  container_meta id0 a 'Exited (0)' '' '2020-01-01T00:00:00.000000000Z' '2020-01-02T00:00:00.000000000Z'
  wt core list --stream --no-refresh
  assert_cleanup "stack:p" needs_look stale
  assert_no_cleanup_row "worktree:$WT"
}

test_core_cleanup_totals_split_by_tier() {
  local safe_wt
  make_repo proj
  add_wt safe
  safe_wt=$WT
  volume proj-old proj-old_data
  stack mainstack "$REPO" running
  mkdir -p "$XDG_CACHE_HOME/wtree"
  printf 'proj-old\t7\nmainstack\t11\n' >"$XDG_CACHE_HOME/wtree/disk-projects"
  printf 'Images\t1GB\n' >"$XDG_CACHE_HOME/wtree/disk-totals"
  wt core list --stream --no-refresh
  assert_q "events('cleanup_totals')[0]['needs_look']" 7
  assert_q "events('cleanup_totals')[0]['never']" 11
  assert_q "events('cleanup_totals')[0]['safe']" 0
  assert_q "events('cleanup_totals')[0]['unknown']" true
  assert_q "flat('cleanup', 'id', 'worktree:$safe_wt')['bytes']" null
}

test_core_cleanup_safe_really_is_not_refused() {
  make_repo
  add_wt alpha
  wt core list --stream --no-refresh
  assert_cleanup "worktree:$WT" safe merged
  plan_ok delete-worktrees -- "$WT"
  assert_q "item('worktree:$WT')['go']" true
}

# ---- odd names

test_core_odd_paths_are_valid_json_and_keep_their_names() {
  local dq sq uni
  make_repo
  dq=$(worktree_path 'with"quote')
  sq=$(worktree_path "with'single")
  uni=$(worktree_path 'ünï-日本-😀')
  must git -C "$REPO" worktree add -q --detach "$dq"
  must git -C "$REPO" worktree add -q --detach "$sq"
  must git -C "$REPO" worktree add -q --detach "$uni"
  stack quoted "$dq" running
  must git -C "$REPO" worktree lock --reason "$(printf 'bad\033[31m "red"')" "$uni"
  wt core list --no-refresh
  assert_rc 0
  assert_q '[w["path"].split("/")[-1] for w in doc["worktrees"]][1:]' '["with\"quote", "with'"'"'single", "ünï-日本-😀"]'
  assert_q 'any(w["odd"] for w in doc["worktrees"])' false
  assert_q 'doc["stacks"][0]["worktree"].endswith("with\"quote")' true
  assert_q 'doc["worktrees"][3]["locked"] == "bad\x1b[31m \"red\""' true
  wt core list --stream --no-refresh
  assert_rc 0
  assert_json_only
}

test_core_a_tab_or_newline_in_a_path_is_marked_odd() {
  local tabbed newline
  make_repo
  tabbed=$(worktree_path $'with\ttab')
  newline=$(worktree_path $'with\nnewline')
  must git -C "$REPO" worktree add -q --detach "$tabbed"
  must git -C "$REPO" worktree add -q --detach "$newline"
  wt core list --no-refresh
  assert_rc 0
  assert_q "[w['path'] for w in doc['worktrees'] if w['odd']] == ['''$tabbed''', '''$newline''']" true
  assert_q "flat('cleanup', 'id', 'x') if False else [c['reasons'][0]['code'] for c in doc['cleanup'] if '\t' in c['id'] or '\n' in c['id']]" '["odd_path", "odd_path"]'
}

test_core_a_path_that_is_not_utf8_is_marked_odd_and_cannot_come_back() {
  local bad lossy
  make_repo
  bad=$(worktree_path $'bad\xffname')
  must git -C "$REPO" worktree add -q --detach "$bad"
  wt core list --no-refresh
  assert_rc 0
  lossy=$(q '[w["path"] for w in doc["worktrees"] if w["odd"]][0]')
  [[ $lossy == *$'\xef\xbf\xbd'* ]] || fail "not a replacement character: $lossy"
  # what the TUI can send back names no worktree, so it takes nothing
  plan_ok delete-worktrees --force --repo "$COMMON" -- "$lossy"
  assert_q 'doc["items"][0]["reason"]["code"]' not_registered
  assert_dir "$bad"
  # the raw bytes are refused too: the plan could not show the real target, and the output stays valid
  wt_direct core plan delete-worktrees --force --repo "$COMMON" -- "$bad"
  assert_rc 0
  assert_q 'doc["items"][0]["go"]' false
  assert_q 'doc["items"][0]["reason"]["code"]' odd_path
  assert_q 'doc["nothing_to_do"]' true
  wt_direct core apply delete-worktrees --force --plan "$(q 'doc["plan_id"]')" --choice worktrees --repo "$COMMON" -- "$bad"
  assert_rc 4
  assert_dir "$bad"
}

test_core_odd_commit_subjects_and_branches() {
  make_repo
  add_wt 'feat/"x"'
  must git -C "$REPO" worktree list
  printf 'x\n' >>"$WT/file.txt"
  must git -C "$WT" commit -qam "$(printf 'subject "with" \\ back\tslash é \xff')"
  wt core list --no-refresh
  assert_rc 0
  assert_q 'doc["worktrees"][1]["branch"]' 'feat/"x"'
  assert_q 'doc["worktree_git"][1]["last_commit_subject"].startswith("subject \"with\" \\ back\tslash é")' true
}

test_core_odd_stack_names_and_directories() {
  make_repo
  mkdir -p "$REPO/in\"fra" "$REPO"/$'ta\tb'
  stack 'we"ird&name' "$REPO/in\"fra" running
  stack tabdir "$REPO/ta<TAB>b" running
  volume 'we"ird&name' weird_data
  wt core list --no-refresh
  assert_rc 0
  assert_q 'sorted(s["project"] for s in doc["stacks"])' '["tabdir", "we\"ird&name"]'
  assert_q 'next(s for s in doc["stacks"] if s["project"] == "tabdir")["dir"].endswith("ta\tb")' true
  plan_ok delete-stacks --volumes -- 'we"ird&name'
  assert_q 'doc["confirm"]["phrases"][0]["phrase"]' 'we"ird&name'
  apply_ok delete-stacks "$PLAN_ID_OF" --choice stacks --phrase 'we"ird&name' --volumes -- 'we"ird&name'
  assert_rc 0
  assert_log 'volume rm weird_data'
}

test_core_plan_and_apply_with_a_quote_in_the_path() {
  local dq
  make_repo
  dq=$(worktree_path 'with"quote')
  must git -C "$REPO" worktree add -q --detach "$dq"
  plan_ok delete-worktrees -- "$dq"
  assert_q 'doc["items"][0]["label"]' 'with"quote'
  apply_ok delete-worktrees "$PLAN_ID_OF" --choice worktrees -- "$dq"
  assert_rc 0
  assert_no_dir "$dq"
}

test_core_a_failed_step_with_binary_output_stays_valid_json() {
  make_repo
  add_wt alpha
  stack p "$WT" running
  plan_ok stop-stacks -- p
  shim_fail 'compose -p p stop' "$(printf 'bad \xff\033[31m "output"\tend')"
  apply_ok stop-stacks "$PLAN_ID_OF" -- p
  assert_rc 1
  assert_q 'len(flat("step", "state", "failed")["tail"])' 1
}

test_core_plan_says_that_only_volumes_are_left() {
  make_repo
  add_wt alpha
  stack live "$WT" running
  volume live live_data
  volume lonely lonely_data
  plan_ok delete-stacks -- live lonely
  assert_eq "$(q 'item("stack:lonely").get("only_volumes")')" true
  assert_eq "$(q 'item("stack:live").get("only_volumes")')" null
  # containers come: the plan says so, and the list of a while ago does not count
  stack lonely "$WT" running
  plan_ok delete-stacks -- lonely
  assert_eq "$(q 'item("stack:lonely").get("only_volumes")')" null
}

test_core_plan_does_not_say_only_volumes_when_docker_did_not_answer() {
  make_repo
  volume lonely lonely_data
  shim_fail 'ps *'
  plan_ok delete-stacks -- lonely
  assert_eq "$(q 'item("stack:lonely").get("only_volumes")')" null
}

# ---- containers that compose did not start

# A container run from an image that compose built carries the project label, and compose down
# and stop pass it by. Found on a real host: the stack stayed in the list after each delete,
# and the core said ok.
stray_stack() {
  make_repo
  add_wt alpha
  stack p "$WT" exited
  stack p "$WT" "${1:-exited}"
  printf 'id1\n' >"$WTREE_SHIM_DIR/strays"
}

test_core_delete_stack_removes_a_container_compose_did_not_start() {
  stray_stack
  plan_ok delete-stacks -- p
  apply_ok delete-stacks "$PLAN_ID_OF" --choice stacks -- p
  assert_rc 0
  assert_log "compose -p p down"
  assert_log "rm -f id1"
  if grep -q "^p	" "$WTREE_SHIM_DIR/containers"; then fail "a container of p is left"; fi
  assert_q 'lines[-1]["ok"]' 1
}

test_core_delete_stack_fails_when_a_stray_container_stays() {
  stray_stack
  shim_fail 'rm -f *' 'cannot remove'
  plan_ok delete-stacks -- p
  apply_ok delete-stacks "$PLAN_ID_OF" --choice stacks -- p
  assert_q 'lines[-1]["ok"]' 0
  assert_q 'lines[-1]["failed"]' 1
}

test_core_stop_stack_stops_a_running_container_compose_did_not_start() {
  stray_stack running
  plan_ok stop-stacks -- p
  apply_ok stop-stacks "$PLAN_ID_OF" -- p
  assert_rc 0
  assert_log "compose -p p stop"
  assert_log "stop id1"
  if grep -q "running" "$WTREE_SHIM_DIR/containers"; then fail "a container of p still runs"; fi
}

test_core_delete_stack_without_strays_calls_no_rm() {
  make_repo
  add_wt alpha
  stack p "$WT" exited
  plan_ok delete-stacks -- p
  apply_ok delete-stacks "$PLAN_ID_OF" --choice stacks -- p
  assert_rc 0
  assert_not_log "rm -f id0"
  assert_q 'lines[-1]["ok"]' 1
}
