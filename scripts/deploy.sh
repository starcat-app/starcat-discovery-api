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

# Discovery 的正式发布必须来自当前提交上的 tag；独立业务仓库不再部署 Fly App。
# Starcat 官方生产环境统一由 starcat-api 聚合仓从六个 main 分支构建。
TAG_COMMIT=$(git rev-list -n 1 "$VERSION" 2>/dev/null || true)
HEAD_COMMIT=$(git rev-parse HEAD)
if [ -z "$TAG_COMMIT" ] || [ "$TAG_COMMIT" != "$HEAD_COMMIT" ]; then
  echo "deployment requires $VERSION to exist and point at current HEAD" >&2
  exit 1
fi

git push origin "$VERSION"
