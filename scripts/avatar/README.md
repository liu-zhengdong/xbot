# Random component avatars

The renderer combines one body base with randomly selected hair, clothing,
expression, glasses, headwear, chest accessories, and a hand-held object.
A saved `avatar_seed` always produces the same character. The UI's “换一个”
action saves a new random seed. It does not expose a part picker.

`entry.js` owns the only ordered catalog and the stable seed algorithm.
`window.XBotAvatar.portrait(seed)` and `.face(seed)` return SVG image layers.
The app mounts these as real SVG DOM so external PNGs can load. An SVG used
as an `<img>` data URL cannot load those external images.

The owner supplied `xbot-design-assets/avatars`. To prepare the packaged assets:

```sh
python3 scripts/avatar/prepare.py /path/to/xbot-design-assets/avatars
```

Pillow is required only for asset preparation. It extracts the head occlusion
layer at the original 1024px size, then scales each PNG to 512px. Existing opaque
body and skin variants are not independent layers and are excluded.

The shipped renderer has no npm dependencies:

```sh
node scripts/avatar/build.js
node scripts/avatar/golden.js
```

CI checks that the shipped script matches `entry.js`, that fixed seeds retain
their faces, and that referenced PNGs exist within the 700 KiB pipe budget.
The Go and browser tests verify real image responses and random-avatar actions.
Only after the owner approves a new rendering scheme, update fixed outputs with
`node scripts/avatar/golden.js --update`.

PNG layers use `/api/ui/static/avatar-parts/…`, sharing authentication, ETags,
and the same device asset source with the rest of XBot. The server stores the
seed, or an uploaded photo, and does not persist composite images.
