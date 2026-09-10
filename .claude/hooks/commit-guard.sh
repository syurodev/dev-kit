#!/bin/bash
#
# Stop hook: refuse to go idle on a feature branch with work left uncommitted.
#
# It deliberately does NOT commit anything itself. A hook can only invent a
# generic message, and this repo's history is expected to carry one commit per
# step with a real description — so the hook blocks and Claude writes the
# commit, rather than the other way round.
#
# Exit 2 sends stderr back to Claude as feedback and keeps the turn alive.

input=$(cat)

# The hook's own block re-triggers Stop; without this it would loop.
if [[ "$(echo "$input" | jq -r '.stop_hook_active' 2>/dev/null)" == "true" ]]; then
  exit 0
fi

# Not a git repo (or cwd moved somewhere without one): nothing to guard.
git rev-parse --git-dir >/dev/null 2>&1 || exit 0

branch=$(git branch --show-current 2>/dev/null)

# An empty branch means detached HEAD — a rebase, bisect or explicit checkout
# of a sha, where an automatic commit is the last thing anyone wants.
# main/master are the protected branches: committing straight onto them is the
# case this guard exists to stay out of.
case "$branch" in
  "" | main | master) exit 0 ;;
esac

dirty=""
git diff --quiet 2>/dev/null || dirty="unstaged changes"
git diff --cached --quiet 2>/dev/null || dirty="${dirty:+$dirty, }staged changes"
if [[ -n "$(git ls-files --others --exclude-standard 2>/dev/null)" ]]; then
  dirty="${dirty:+$dirty, }untracked files"
fi

[[ -z "$dirty" ]] && exit 0

{
  echo "Branch '$branch' is not main and has uncommitted work ($dirty)."
  echo "Commit it before finishing. Review 'git status' and 'git diff' first, then"
  echo "write a real commit message describing what changed and why — not a"
  echo "placeholder. Split unrelated changes into separate commits."
  echo "Deliberately leaving something uncommitted? Say so in your reply."
} >&2
exit 2
