#!/usr/bin/env bash
# Package the desktop app (B3) from Linux, no Apple or Windows machine needed:
#   macOS: XBot-<v>-macos-<arch>.dmg and .zip (XBot.app, ad-hoc signed, not notarized)
#   Windows: XBot-<v>-windows-<arch>-setup.exe (per-user installer, packaging/windows/xbot.nsi)
#            and XBot-<v>-windows-<arch>.zip (portable: XBot.exe opens the window; xbot-cli.exe is the CLI)
#   Linux: xbot-<v>-linux-amd64.tar.gz (needs WebKitGTK 4.1 for `xbot app`)
# Usage: scripts/package-desktop.sh <version> <out-dir>
# Tools: go, zip, genisoimage (dmg), makensis (nsis),
# go-winres (Windows icon/manifest) and rcodesign (ad-hoc signature); point
# GO_WINRES and RCODESIGN at them if they are not on PATH. Missing optional tools
# are reported and that step is skipped. Set TMPDIR to keep temp files out of /tmp.
set -euo pipefail
version=${1:?version}
out=$(mkdir -p "${2:?out dir}" && cd "$2" && pwd)
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
if compgen -G "cmd/xbot/rsrc_windows_*.syso" >/dev/null; then
	echo "package: cmd/xbot already has rsrc_windows_*.syso; remove them first" >&2
	exit 1
fi
work=$(mktemp -d "${TMPDIR:-$out}/xbot-pkg.XXXXXX")
trap 'rm -rf "$work"; rm -f cmd/xbot/rsrc_windows_amd64.syso cmd/xbot/rsrc_windows_arm64.syso' EXIT
winres=${GO_WINRES:-$(command -v go-winres || true)}
rcodesign=${RCODESIGN:-$(command -v rcodesign || true)}
short=${version#v}
# Apple wants numeric versions; build metadata after "+" stays in file names only.
numeric=${short%%[-+]*}
ldflags="-s -w -X xbot/internal/version.Version=$version"
# Icons: hand-tuned .ico and .icns (16/24/32 are pixel-snapped; never resample them).
cp packaging/icons/XBot.ico "$work/XBot.ico"
cp packaging/icons/XBot.icns "$work/XBot.icns"

for arch in arm64 amd64; do
	app="$work/mac-$arch/XBot.app"
	mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
	CGO_ENABLED=0 GOOS=darwin GOARCH=$arch go build -trimpath -ldflags "$ldflags -X main.defaultCommand=app" -o "$app/Contents/MacOS/XBot" ./cmd/xbot
	cp "$work/XBot.icns" "$app/Contents/Resources/XBot.icns"
	cat > "$app/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleName</key><string>XBot</string>
	<key>CFBundleDisplayName</key><string>XBot</string>
	<key>CFBundleIdentifier</key><string>com.github.liu-zhengdong.xbot</string>
	<key>CFBundleExecutable</key><string>XBot</string>
	<key>CFBundleIconFile</key><string>XBot</string>
	<key>CFBundlePackageType</key><string>APPL</string>
	<key>CFBundleShortVersionString</key><string>$numeric</string>
	<key>CFBundleVersion</key><string>$numeric</string>
	<key>LSMinimumSystemVersion</key><string>13.0</string>
	<key>NSHighResolutionCapable</key><true/>
	<key>LSApplicationCategoryType</key><string>public.app-category.productivity</string>
</dict>
</plist>
PLIST
	if [[ -n "$rcodesign" ]]; then
		"$rcodesign" sign "$app" >/dev/null 2>&1 || echo "package: rcodesign failed for $arch; the app keeps the linker's ad-hoc signature" >&2
	else
		echo "package: rcodesign not found; XBot.app ($arch) is not bundle-signed" >&2
	fi
	(cd "$work/mac-$arch" && zip -qry "$out/XBot-$short-macos-$arch.zip" XBot.app)
	if command -v genisoimage >/dev/null; then
		stage="$work/dmg-$arch"
		mkdir -p "$stage"
		cp -a "$app" "$stage/"
		ln -s /Applications "$stage/Applications"
		genisoimage -quiet -V XBot -D -R -no-pad -o "$out/XBot-$short-macos-$arch.dmg" "$stage"
	else
		echo "package: genisoimage not found; skipped the macOS dmg" >&2
	fi
done

for arch in amd64 arm64; do
	dir="$work/win-$arch/XBot"
	mkdir -p "$dir"
	if [[ -n "$winres" ]]; then
		"$winres" simply --arch "$arch" --out cmd/xbot/rsrc --icon "$work/XBot.ico" --manifest gui \
			--product-name XBot --file-description XBot --product-version "$numeric.0" --file-version "$numeric.0" \
			--original-filename XBot.exe
	else
		echo "package: go-winres not found; Windows binaries have no icon" >&2
	fi
	CGO_ENABLED=0 GOOS=windows GOARCH=$arch go build -trimpath -ldflags "$ldflags -X main.defaultCommand=app -H windowsgui" -o "$dir/XBot.exe" ./cmd/xbot
	CGO_ENABLED=0 GOOS=windows GOARCH=$arch go build -trimpath -ldflags "$ldflags" -o "$dir/xbot-cli.exe" ./cmd/xbot
	rm -f "cmd/xbot/rsrc_windows_$arch.syso"
	(cd "$work/win-$arch" && zip -qr "$out/XBot-$short-windows-$arch.zip" XBot)
	if command -v makensis >/dev/null; then
		makensis -V2 -DVERSION="$version" -DNUMERIC="$numeric" -DARCH="$arch" -DSRC="$dir" -DICON="$work/XBot.ico" \
			-DOUT="$out/XBot-$short-windows-$arch-setup.exe" packaging/windows/xbot.nsi
	else
		echo "package: makensis not found; skipped the Windows installer" >&2
	fi
done

lin="$work/linux/xbot-$short-linux-amd64"
mkdir -p "$lin"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$ldflags" -o "$lin/xbot" ./cmd/xbot
tar -C "$work/linux" -czf "$out/xbot-$short-linux-amd64.tar.gz" "xbot-$short-linux-amd64"

(cd "$out" && sha256sum XBot-* xbot-* > SHA256SUMS)
ls -l "$out"
