#!/bin/bash
set -euo pipefail

# Release script for herdr-auto-resume
# Usage: ./scripts/release.sh <version>
# Example: ./scripts/release.sh 0.2.0

VERSION="${1:-}"
if [[ -z "$VERSION" ]]; then
    echo "Usage: $0 <version>"
    echo "Example: $0 0.0.2"
    exit 1
fi

TAG="v$VERSION"
REPO="Wave-Consulting-Netherlands/herdr-auto-resume"

echo "==> Releasing $TAG"

# Check for uncommitted changes
if [[ -n "$(git status --porcelain)" ]]; then
    echo "Error: Working directory has uncommitted changes"
    exit 1
fi

# Check if tag already exists
if git rev-parse "$TAG" >/dev/null 2>&1; then
    echo "Error: Tag $TAG already exists"
    exit 1
fi

# Create and push tag
echo "==> Creating tag $TAG"
git tag "$TAG"
git push origin "$TAG"

# Wait for release workflow to complete
echo "==> Waiting for release workflow..."
sleep 5

RUN_ID=$(gh run list -R "$REPO" -L 1 --json databaseId -q '.[0].databaseId')
echo "==> Watching workflow run $RUN_ID"
gh run watch "$RUN_ID" -R "$REPO" --exit-status || {
    echo "Error: Release workflow failed"
    echo "Check: https://github.com/$REPO/actions"
    exit 1
}

# Download checksums
echo "==> Downloading checksums"
CHECKSUMS=$(gh release download "$TAG" -R "$REPO" -p checksums.txt -O -)

echo "==> Release $TAG complete!"
