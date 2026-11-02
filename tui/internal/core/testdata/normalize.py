"""Writes the recordings of guarded.sh to the testdata directory, in a stable form. See README.md."""
import json
import os
import re
import sys

source, target = sys.argv[1], sys.argv[2]
PLAN = re.compile(r'"plan_id":"([0-9a-f]+)"')
FIXED = re.compile(r'"(ms|age_s|index_mtime|last_started_at|last_finished_at)":\d+')
CONSTANT = {"ms": 7, "age_s": 60, "index_mtime": 1767323045, "last_started_at": 1767225600, "last_finished_at": 1767312000}


def fixed(match):
    return f'"{match.group(1)}":{CONSTANT[match.group(1)]}'


def normalize(text):
    ids = {}

    def plan(match):
        new = ids.setdefault(match.group(1), f"{len(ids) + 1:016x}")
        return f'"plan_id":"{new}"'

    lines = [FIXED.sub(fixed, PLAN.sub(plan, line)) for line in text.splitlines()]
    # the git stage runs in parallel, so only the order of its lines can differ
    out, block = [], []
    for line in lines:
        if '"event":"worktree_git"' in line:
            block.append(line)
            continue
        out += sorted(block, key=lambda l: json.loads(l)["path"])
        block = []
        out.append(line)
    out += sorted(block, key=lambda l: json.loads(l)["path"])
    return "".join(line + "\n" for line in out)


codes = []
for name in sorted(os.listdir(source)):
    path = os.path.join(source, name)
    if name.endswith(".rc"):
        with open(path, encoding="ascii") as f:
            codes.append((name[:-3], f.read().strip()))
        continue
    with open(path, encoding="utf-8", errors="strict") as f:
        text = normalize(f.read())
    with open(os.path.join(target, name), "w", encoding="utf-8") as f:
        f.write(text)

with open(os.path.join(target, "exit-codes.tsv"), "w", encoding="ascii") as f:
    for name, code in codes:
        f.write(f"{name}\t{code}\n")
print(f"recorded {len(codes)} fixtures")
