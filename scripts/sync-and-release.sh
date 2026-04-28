#!/usr/bin/env bash
# sync-and-release.sh — rebase btime fork onto latest upstream and tag a release.
#
# Usage:
#   ./scripts/sync-and-release.sh           # auto-detects latest upstream tag
#   ./scripts/sync-and-release.sh v0.22.4   # explicit upstream tag
#
# Branch structure:
#   btime-proposal  — clean btime commits only (open PR to upstream)
#   btime-fork      — btime-proposal + fork infra (scripts, release workflow)
set -euo pipefail

PR_BRANCH="btime-proposal"
FORK_BRANCH="btime-fork"
UPSTREAM_REMOTE="upstream"
ORIGIN_REMOTE="origin"

git fetch "$UPSTREAM_REMOTE" --tags --quiet

UPSTREAM_TAG="${1:-}"
if [[ -z "$UPSTREAM_TAG" ]]; then
  UPSTREAM_TAG=$(git tag --list "v*" --sort=-version:refname \
    | grep -v -- "-btime" \
    | head -1)
  echo "Auto-detected latest upstream tag: $UPSTREAM_TAG"
fi

BTIME_TAG="${UPSTREAM_TAG}-btime"

if git rev-parse "$BTIME_TAG" >/dev/null 2>&1; then
  echo "Tag $BTIME_TAG already exists -- nothing to do."
  exit 0
fi

# Step 1: rebase the clean PR branch onto upstream/master
echo "Rebasing $PR_BRANCH onto $UPSTREAM_REMOTE/master..."
git checkout "$PR_BRANCH"
git rebase "$UPSTREAM_REMOTE/master"
git push "$ORIGIN_REMOTE" "$PR_BRANCH" --force-with-lease

# Step 2: rebase the fork branch onto the updated PR branch
echo "Rebasing $FORK_BRANCH onto $PR_BRANCH..."
git checkout "$FORK_BRANCH"
git rebase "$PR_BRANCH"
git push "$ORIGIN_REMOTE" "$FORK_BRANCH" --force-with-lease

# Step 3: smoke test
echo "Building to verify no compile errors..."
go build ./... >/dev/null

# Step 4: tag from fork branch and push -> triggers CI release
echo "Tagging $BTIME_TAG..."
git tag "$BTIME_TAG" -m "btime fork release based on $UPSTREAM_TAG"
git push "$ORIGIN_REMOTE" "$BTIME_TAG"

echo ""
echo "Done. CI will build and publish: $BTIME_TAG"
echo "https://github.com/jonathanMelly/kopia/actions"
