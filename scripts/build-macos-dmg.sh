#!/usr/bin/env bash
# Rebuild the macOS disk images as drag-to-Applications windows with a
# background, from the XBot.app zips scripts/package-desktop.sh made. Run on
# macOS (hdiutil); needs python3 with dmgbuild (pip install dmgbuild==1.6.7).
# Usage: scripts/build-macos-dmg.sh <dist dir> <version>
# Replaces dist/XBot-<short>-macos-<arch>.dmg for arm64 and amd64 and checks
# each image: XBot.app, the Applications link and the window settings.
set -euo pipefail
dist=$(cd "${1:?dist dir}" && pwd)
short=${2:?version}
short=${short#v}
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
work=$(mktemp -d "${RUNNER_TEMP:-${TMPDIR:-$dist}}/xbot-dmg.XXXXXX")
trap 'rm -rf "$work"' EXIT
for arch in arm64 amd64; do
	zip="$dist/XBot-$short-macos-$arch.zip"
	dmg="$dist/XBot-$short-macos-$arch.dmg"
	mkdir -p "$work/$arch"
	ditto -x -k "$zip" "$work/$arch"
	codesign --verify --strict "$work/$arch/XBot.app" || echo "build-macos-dmg: XBot.app ($arch) has no valid bundle signature" >&2
	rm -f "$dmg"
	python3 -m dmgbuild -s "$root/packaging/macos/dmg-settings.py" -D app="$work/$arch/XBot.app" XBot "$dmg"
	mnt="$work/mnt-$arch"
	mkdir -p "$mnt"
	hdiutil attach -nobrowse -readonly -mountpoint "$mnt" "$dmg" >/dev/null
	ok=1
	[[ -x "$mnt/XBot.app/Contents/MacOS/XBot" ]] || { echo "missing XBot.app in $dmg" >&2; ok=0; }
	[[ "$(readlink "$mnt/Applications")" == /Applications ]] || { echo "missing Applications link in $dmg" >&2; ok=0; }
	[[ -s "$mnt/.DS_Store" ]] || { echo "missing window settings (.DS_Store) in $dmg" >&2; ok=0; }
	ls -A "$mnt" | grep -q '^\.background' || { echo "missing background in $dmg" >&2; ok=0; }
	hdiutil detach "$mnt" >/dev/null
	[[ $ok == 1 ]] || exit 1
	echo "ok: $(basename "$dmg")"
done
