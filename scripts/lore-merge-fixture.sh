#!/bin/sh
set -eu

repository_url=$1
worktree=$2
identity=fixture
lore=/usr/local/bin/lore

rm -rf "$worktree"
mkdir -p "$worktree/src"

run_lore() {
    "$lore" --repository "$worktree" --identity "$identity" "$@"
}

run_lore repository create "$repository_url"
printf '%s\n' '# Lore merge fixture' >"$worktree/README.md"
printf '%s\n' 'package fixture' >"$worktree/src/main.go"
run_lore status --scan .
run_lore stage --scan .
run_lore commit 'initial fixture'
run_lore push main

printf '%s\n' 'second fixture revision' >>"$worktree/README.md"
run_lore status --scan .
run_lore stage --scan .
run_lore commit 'second fixture revision'
run_lore push main

run_lore branch create feature-clean
run_lore branch switch feature-clean --reset
printf '%s\n' 'clean source' >"$worktree/clean-source.txt"
run_lore status --scan .
run_lore stage --scan .
run_lore commit 'clean feature'
run_lore push feature-clean

run_lore branch switch main --reset
run_lore branch create conflict-target
run_lore branch create feature-conflict
run_lore branch create recovery-target
run_lore branch create recovery-source
run_lore branch create race-target
run_lore branch create race-source

run_lore branch switch race-target --reset
run_lore push race-target

run_lore branch switch race-source --reset
printf '%s\n' 'race source' >"$worktree/race-source.txt"
run_lore status --scan .
run_lore stage --scan .
run_lore commit 'race source'
run_lore push race-source

run_lore branch switch recovery-target --reset
printf '%s\n' 'recovery target' >"$worktree/recovery.txt"
run_lore status --scan .
run_lore stage --scan .
run_lore commit 'recovery target'
run_lore push recovery-target

run_lore branch switch recovery-source --reset
printf '%s\n' 'recovery source' >"$worktree/recovery.txt"
run_lore status --scan .
run_lore stage --scan .
run_lore commit 'recovery source'
run_lore push recovery-source

run_lore branch switch conflict-target --reset
printf '%s\n' 'target value' >"$worktree/conflict.txt"
run_lore status --scan .
run_lore stage --scan .
run_lore commit 'target conflict'
run_lore push conflict-target

run_lore branch switch feature-conflict --reset
printf '%s\n' 'source value' >"$worktree/conflict.txt"
run_lore status --scan .
run_lore stage --scan .
run_lore commit 'source conflict'
run_lore push feature-conflict
