#!/bin/bash
# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

set -euo pipefail

# Release helper script for nomad-driver-ch

VERSION=${1:-""}

if [[ -z "$VERSION" ]]; then
    echo "Usage: $0 <version>"
    echo "Example: $0 v1.0.0"
    exit 1
fi

# Validate version format (should start with v)
if [[ ! "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+.*$ ]]; then
    echo "Error: Version should follow semantic versioning format: v1.2.3"
    exit 1
fi

echo "Preparing release $VERSION..."

# Check if we're on the main branch
CURRENT_BRANCH=$(git rev-parse --abbrev-ref HEAD)
if [[ "$CURRENT_BRANCH" != "main" ]]; then
    echo "Warning: You're not on the main branch. Current branch: $CURRENT_BRANCH"
    read -p "Continue anyway? (y/n): " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        exit 1
    fi
fi

# Check for uncommitted changes
if [[ -n $(git status --porcelain) ]]; then
    echo "Error: You have uncommitted changes. Please commit or stash them first."
    git status --short
    exit 1
fi

# Update version in version.go
echo "Updating version to $VERSION..."
sed -i.bak "s/Version = \".*\"/Version = \"${VERSION#v}\"/" version/version.go
sed -i.bak 's/VersionPrerelease = ".*"/VersionPrerelease = ""/' version/version.go
rm version/version.go.bak

# Commit version update
git add version/version.go
git commit -m "Release $VERSION"

# Create and push tag
echo "Creating and pushing tag $VERSION..."
git tag -a "$VERSION" -m "Release $VERSION"
git push origin main
git push origin "$VERSION"

echo "Release $VERSION has been tagged and pushed!"
echo "The Buildkite pipeline will automatically create the GitHub release when it detects the tag."
echo ""
echo "Monitor the build at: https://buildkite.com/your-org/nomad-driver-ch"
echo "The GitHub release will be created at: https://github.com/$(git config --get remote.origin.url | sed 's/.*github.com[:/]\(.*\)\.git/\1/')/releases"