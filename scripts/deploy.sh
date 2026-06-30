#!/usr/bin/env sh
set -eu

VERSION="${1:-}"
if [ -z "$VERSION" ]; then
  echo "usage: ./scripts/deploy.sh v0.1.0"
  exit 1
fi

fly deploy -a starcat-discovery-api --build-arg VERSION="$VERSION"
