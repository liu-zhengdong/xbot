"""Prepare the user's modular avatar assets. Requires Pillow.

Usage: python3 scripts/avatar/prepare.py /path/to/xbot-design-assets/avatars
"""
from pathlib import Path
import sys
from PIL import Image, ImageFilter

source = Path(sys.argv[1]) / "modular"
target = Path(__file__).resolve().parents[2] / "internal/web/static/avatar-parts"
base = Image.open(source / "base/baseplate-bald.png").convert("RGBA")
if base.size != (1024, 1024):
    raise ValueError("The head matte requires the original 1024 by 1024 base.")


def save(image, name):
    path = target / name
    path.parent.mkdir(parents=True, exist_ok=True)
    image.resize((512, 512), Image.Resampling.LANCZOS).save(path, optimize=True)


save(base, "base/bald.png")
mask = Image.new("L", base.size)
mask.putdata([
    255 if i // 1024 < 800 and r > 1.15 * g and g > 1.10 * b and r > 100 else 0
    for i, (r, g, b, _) in enumerate(base.getdata())
])
head = base.copy()
head.putalpha(mask.filter(ImageFilter.GaussianBlur(0.35)))
save(head, "base/head.png")
for slot in ("tops", "face", "hair", "accessories", "props"):
    for path in sorted((source / "parts" / slot).glob("*.png")):
        image = Image.open(path).convert("RGBA")
        if image.getchannel("A").getbbox():
            save(image, slot + "/" + path.name)
files = list(target.rglob("*.png"))
print(f"Prepared {len(files)} layers; total {sum(p.stat().st_size for p in files)} bytes; largest {max(p.stat().st_size for p in files)} bytes")
