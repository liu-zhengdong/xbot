#!/usr/bin/env bash
# Rebuild internal/web/static/fonts/NotoSansSC-subset.woff2 from the upstream
# variable font (google/fonts ofl/notosanssc, OFL): weights 400-700, every
# GB2312 character, woff2. Needs python3 with fonttools and brotli.
# Usage: scripts/subset-cjk-font.sh 'NotoSansSC[wght].ttf'
set -euo pipefail
src=${1:?path to NotoSansSC[wght].ttf}
work=$(mktemp -d "${TMPDIR:-$PWD}/cjkfont.XXXXXX")
trap 'rm -rf "$work"' EXIT
python3 - "$work/chars.txt" <<'PY'
import sys
out = set()
for hi in range(0xA1, 0xF8):
    for lo in range(0xA1, 0xFF):
        try:
            out.add(bytes([hi, lo]).decode("gb2312"))
        except UnicodeDecodeError:
            pass
open(sys.argv[1], "w", encoding="utf-8").write("".join(sorted(out)))
PY
fonttools varLib.instancer "$src" wght=400:700 -q -o "$work/vf.ttf"
pyftsubset "$work/vf.ttf" --text-file="$work/chars.txt" --flavor=woff2 \
  --layout-features='*' --no-hinting \
  --output-file="$(dirname "$0")/../internal/web/static/fonts/NotoSansSC-subset.woff2"
