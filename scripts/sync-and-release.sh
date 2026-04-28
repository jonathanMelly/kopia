#!/usr/bin/env bash
# sync-and-release.sh — sync btime branches with upstream and cut a release.
#
# Usage:
#   ./scripts/sync-and-release.sh           # auto-detects latest upstream tag
#   ./scripts/sync-and-release.sh v0.22.4   # explicit upstream tag
#
# Branch strategy:
#   btime-proposal  — btime commits only, always rebased onto upstream/master (open PR)
#   btime-fork      — btime-proposal + fork infra, always rebased onto upstream/master
#
# Release tag vX.Y.Z-btime:
#   Cherry-picks all fork commits onto the exact upstream vX.Y.Z tag so CI
#   builds the same Go/npm base as the official release (no master noise).
#   The branches themselves stay on upstream/master.
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

# Step 1: sync btime-proposal onto upstream/master (keeps PR up to date)
echo "Syncing $PR_BRANCH onto $UPSTREAM_REMOTE/master..."
git checkout "$PR_BRANCH"
git rebase "$UPSTREAM_REMOTE/master"
git push "$ORIGIN_REMOTE" "$PR_BRANCH" --force-with-lease

# Step 2: sync btime-fork onto btime-proposal (keeps fork infra up to date)
echo "Syncing $FORK_BRANCH onto $PR_BRANCH..."
git checkout "$FORK_BRANCH"
git rebase "$PR_BRANCH"
git push "$ORIGIN_REMOTE" "$FORK_BRANCH" --force-with-lease

# Step 3: build release tag on the exact upstream tag, not master.
# Cherry-pick all commits unique to btime-fork (btime + infra) onto vX.Y.Z
# so CI sees the same npm/Go base as the official release.
echo "Building release tag $BTIME_TAG on top of $UPSTREAM_TAG..."
TEMP_BRANCH="release-temp-$$"
git checkout -b "$TEMP_BRANCH" "$UPSTREAM_TAG"

FORK_COMMITS=$(git log --reverse --format="%H" "$UPSTREAM_REMOTE/master".."$FORK_BRANCH")

if ! git cherry-pick $FORK_COMMITS; then
  echo ""
  echo "=== Conflicts detected. Resolve them, then run: ==="
  echo "  git cherry-pick --continue"
  echo "  go build ./..."
  echo "  git tag $BTIME_TAG -m 'btime fork release based on $UPSTREAM_TAG'"
  echo "  git push $ORIGIN_REMOTE $BTIME_TAG"
  echo "  git checkout $FORK_BRANCH && git branch -D $TEMP_BRANCH"
  exit 1
fi

# Step 4: smoke test
echo "Building to verify no compile errors..."
go build ./... >/dev/null

# Step 5: tag and push -> triggers CI release workflow
git tag "$BTIME_TAG" -m "btime fork release based on $UPSTREAM_TAG"
git push "$ORIGIN_REMOTE" "$BTIME_TAG"

# Clean up temp branch (local only, never pushed)
git checkout "$FORK_BRANCH"
git branch -D "$TEMP_BRANCH"

echo ""
echo "Done. CI will build and publish: $BTIME_TAG"
echo "https://github.com/jonathanMelly/kopia/actions"
