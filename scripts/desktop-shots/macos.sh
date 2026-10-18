#!/usr/bin/env bash
# Screenshots of the real XBot window (WKWebView + traffic lights) on a macOS
# CI runner. Never run on a user's Mac: it flips the system appearance.
#   macos.sh <XBot binary> <seed binary> <out dir>
set -euo pipefail
bin=$1 seed=$2 out=$(mkdir -p "$3" && cd "$3" && pwd)
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
work="${RUNNER_TEMP:?}/xbot-shots-mac"
rm -rf "$work"; mkdir -p "$work"
swiftc -O -o "$work/macwin" "$here/macwin.swift"
app="$work/XBot.app"
mkdir -p "$app/Contents/MacOS"
cp "$bin" "$app/Contents/MacOS/XBot"
cat > "$app/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
<key>CFBundleName</key><string>XBot</string>
<key>CFBundleIdentifier</key><string>com.github.liu-zhengdong.xbot.shots</string>
<key>CFBundleExecutable</key><string>XBot</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>NSHighResolutionCapable</key><true/>
</dict></plist>
PLIST
log="$out/mac-log.txt"; : > "$log"
say() { echo "$(date +%T) $*" | tee -a "$log"; }
say "screen: $(system_profiler SPDisplaysDataType 2>/dev/null | grep -E 'Resolution|Retina|UI Looks' | tr -s ' ' | tr '\n' ';')"

appearance() { # light|dark
  local dark=false; [ "$1" = dark ] && dark=true
  osascript -e "tell application \"System Events\" to tell appearance preferences to set dark mode to $dark" 2>>"$log" ||
    { if $dark; then defaults write -g AppleInterfaceStyle Dark; else defaults delete -g AppleInterfaceStyle 2>/dev/null || true; fi; }
}

port=8790
run_case() { # mode theme extras...
  local mode=$1 theme=$2; shift 2
  port=$((port + 1))
  local root="$work/root-$mode-$theme"
  "$seed" "$root" "$mode" | tee -a "$log"
  echo '{"main":{"x":120,"y":80,"width":1024,"height":672}}' > "$root/app-window.json"
  appearance "$theme"
  "$app/Contents/MacOS/XBot" --root "$root" app --port "$port" >>"$log" 2>&1 &
  local pid=$! win=""
  for _ in $(seq 1 120); do sleep 0.5; win=$("$work/macwin" find "$pid" 2>/dev/null || true); [ -n "$win" ] && break; done
  if [ -z "$win" ]; then say "no XBot window ($mode $theme)"; kill "$pid" 2>/dev/null || true; return 1; fi
  for _ in $(seq 1 60); do curl -fsS "http://127.0.0.1:$port/" >/dev/null 2>&1 && break; sleep 0.5; done
  sleep 6 # page, fonts and first transcript render
  read -r id x y w h <<<"$("$work/macwin" find "$pid")"
  say "case $mode $theme window $id at $x,$y ${w}x$h"
  osascript -e "tell application \"System Events\" to set frontmost of (first process whose unix id is $pid) to true" 2>>"$log" || true
  "$work/macwin" move 5 5; sleep 1
  screencapture -x -o -l "$id" "$out/mac-$mode-$theme.png"
  for e in "$@"; do
    case $e in
    hover) # traffic lights at {16,14}: close centre about 22,20 from the window's corner
      "$work/macwin" move $((x + 22)) $((y + 20)); sleep 0.8
      screencapture -x -o -l "$id" "$out/mac-$mode-$theme-hover.png"
      "$work/macwin" move 5 5 ;;
    unfocused)
      osascript -e 'tell application "Finder" to activate' 2>>"$log" || open -a Finder; sleep 1.5
      screencapture -x -o -l "$id" "$out/mac-$mode-$theme-unfocused.png" ;;
    esac
  done
  kill "$pid" 2>/dev/null || true; sleep 2
}

run_case rich light hover unfocused || true
run_case rich dark hover unfocused || true
run_case empty light || true
appearance light
for f in "$out"/mac-*.png; do say "$(basename "$f") $(sips -g pixelWidth -g pixelHeight "$f" | tail -2 | awk '{print $2}' | tr '\n' 'x')"; done
