#!/usr/bin/env bash
set -euo pipefail

# Crossbuild builds and vets every desktop target. Set TMPDIR to keep go's
# temp files out of /tmp; the directory is created if missing.
if [[ -n "${TMPDIR:-}" ]]; then
	mkdir -p "$TMPDIR"
fi

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

fail=0
targets=(
	"linux/amd64"
	"darwin/arm64"
	"darwin/amd64"
	"windows/amd64"
	"windows/arm64"
)
for target in "${targets[@]}"; do
	goos="${target%/*}"
	goarch="${target#*/}"
	ok=1
	if ! CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build ./...; then
		ok=0
	fi
	if ! CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go vet ./...; then
		ok=0
	fi
	if [[ "$ok" == 1 ]]; then
		echo "OK $target"
	else
		echo "FAIL $target"
		fail=1
	fi
done
exit "$fail"
