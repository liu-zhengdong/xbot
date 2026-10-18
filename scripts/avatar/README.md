# Bot portrait bundle

Builds `internal/web/static/vendor/xbot-avatar.min.js`: DiceBear core and
Notionists, both pinned to 9.4.2, plus the frozen part whitelist in
`options.js`. It sets `window.XBotAvatar.portrait(seed)` (head and shoulders,
24px and up) and `.face(seed)` (face only, 16px). Portraits are drawn in the
browser only; the server never stores or serves a generated SVG.

```sh
cd scripts/avatar
npm ci && npm run build   # rebuilds the vendored bundle byte for byte
node golden.js            # fixed seeds must hash to golden.json
```

**Frozen.** DiceBear draws parts from the option lists, so editing `options.js`
or bumping a version gives every bot a new face. `TestAvatarBundleAndWhitelistAreFrozen`
and the `avatar` CI job fail on any drift. Change it only with the owner's
approval: rebuild, `node golden.js --update`, and update the hashes in
`internal/web/avatar_vendor_test.go` in the same PR. Do not use
`@dicebear/collection`: it pulls in styles under other licenses.

Licenses: code MIT (Florian Körner), Notionists art CC0 1.0 (Zoish). See
`internal/web/static/vendor/LICENSES.txt`.
