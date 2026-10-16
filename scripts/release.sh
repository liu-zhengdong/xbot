#!/usr/bin/env bash
# Build and publish one xbot release.
#
#   scripts/release.sh v0.1.0            # test, build, tag, push, release
#   DRY_RUN=1 scripts/release.sh v0.1.0  # everything except tag/push/release
#
# The tree must be clean and the full test suite green. Binaries are built with
# CGO disabled and checksummed into SHA256SUMS (sha256sum format).
set -euo pipefail

REPO=liu-zhengdong/xbot
PLATFORMS=(linux/amd64 linux/arm64 darwin/arm64)

VERSION=${1:-}
if [[ ! "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "usage: scripts/release.sh vX.Y.Z" >&2
  exit 2
fi

ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"

if [[ -n "$(git status --porcelain)" ]]; then
  echo "release: git tree is not clean" >&2
  git status --short >&2
  exit 1
fi

if git rev-parse -q --verify "refs/tags/$VERSION" >/dev/null; then
  echo "release: tag $VERSION already exists" >&2
  exit 1
fi

echo "release: running go test -count=1 ./..."
go test -count=1 ./...

DIST="dist/$VERSION"
rm -rf "$DIST"
mkdir -p "$DIST"

for platform in "${PLATFORMS[@]}"; do
  os=${platform%/*}
  arch=${platform#*/}
  out="$DIST/xbot_${os}_${arch}"
  echo "release: building $out"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
    go build -trimpath -ldflags "-s -w -X xbot/internal/version.Version=$VERSION" \
    -o "$out" ./cmd/xbot
done

(
  cd "$DIST"
  sha256sum xbot_* >SHA256SUMS
)

echo "release: artifacts in $DIST"
ls -l "$DIST"

if [[ "${DRY_RUN:-0}" == "1" ]]; then
  echo "release: DRY_RUN=1, skipping tag, push, and gh release"
  exit 0
fi

git tag -a "$VERSION" -m "xbot $VERSION"
git push origin "$VERSION"

# The token stays in the environment; never print it.
GH_TOKEN="$(gh auth token -u liu-zhengdong)" \
  gh release create "$VERSION" "$DIST"/* --repo "$REPO" --title "$VERSION" --notes "xbot $VERSION"
