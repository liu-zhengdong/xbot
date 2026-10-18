// Golden check for the vendored avatar bundle. Renders fixed seeds with
// internal/web/static/vendor/xbot-avatar.min.js and compares each SVG's
// sha256 with golden.json. A change in options.js, a dependency version or
// the bundle re-rolls every bot's face, so it must fail CI on purpose.
// Run: node scripts/avatar/golden.js            (check)
//      node scripts/avatar/golden.js --update   (only after the owner approves new faces)
'use strict';
const fs = require('fs');
const path = require('path');
const vm = require('vm');
const crypto = require('crypto');

const here = __dirname;
const bundle = path.join(here, '..', '..', 'internal', 'web', 'static', 'vendor', 'xbot-avatar.min.js');
const goldenPath = path.join(here, 'golden.json');
const SEEDS = ['diary', 'health', 'inbox', 'writer', 'coach', 'XBot', '周报', 'writer#1', 'writer#6', '5116ed70'];

const win = {};
vm.runInNewContext(fs.readFileSync(bundle, 'utf8'), { window: win });
if (!win.XBotAvatar) throw new Error('bundle did not define window.XBotAvatar');
const sha = (s) => crypto.createHash('sha256').update(s).digest('hex');
const got = {};
for (const seed of SEEDS) {
  const portrait = win.XBotAvatar.portrait(seed);
  const face = win.XBotAvatar.face(seed);
  if (/<metadata|<script|<foreignObject|\son[a-z]+=|javascript:/i.test(portrait + face)) throw new Error('unexpected markup for ' + seed);
  got[seed] = { portrait: sha(portrait), face: sha(face) };
}
if (process.argv.includes('--update')) {
  fs.writeFileSync(goldenPath, JSON.stringify(got, null, 2) + '\n');
  console.log('golden.json updated for', SEEDS.length, 'seeds');
  process.exit(0);
}
const want = JSON.parse(fs.readFileSync(goldenPath, 'utf8'));
let bad = 0;
for (const seed of SEEDS) {
  for (const k of ['portrait', 'face']) {
    if (!want[seed] || want[seed][k] !== got[seed][k]) {
      bad++;
      console.error(`avatar drift: ${seed} ${k}: want ${want[seed] && want[seed][k]} got ${got[seed][k]}`);
    }
  }
}
if (bad) {
  console.error('Every bot would get a new face. Revert the change, or get the owner to approve and run --update.');
  process.exit(1);
}
console.log('avatar golden ok:', SEEDS.length, 'seeds');
