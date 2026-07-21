#!/usr/bin/env sh
set -eu

VERSION="${1:-}"
if [ -z "$VERSION" ]; then
  echo "usage: ./scripts/deploy.sh v0.1.0"
  exit 1
fi

if ! printf '%s' "$VERSION" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$'; then
  echo "version must match vX.Y.Z, got: $VERSION" >&2
  exit 1
fi

# Discovery 暂无 tag 驱动的部署 workflow，因此脚本必须自行保证版本来自当前提交的 tag。
TAG_COMMIT=$(git rev-list -n 1 "$VERSION" 2>/dev/null || true)
HEAD_COMMIT=$(git rev-parse HEAD)
if [ -z "$TAG_COMMIT" ] || [ "$TAG_COMMIT" != "$HEAD_COMMIT" ]; then
  echo "deployment requires $VERSION to exist and point at current HEAD" >&2
  exit 1
fi

fly deploy -a starcat-discovery-api --build-arg VERSION="${VERSION#v}"
