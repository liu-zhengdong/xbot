// Bundle entry: app.js has no build step, so this becomes one IIFE that sets
// window.XBotAvatar.portrait(seed) / .face(seed). Build: npm ci && npm run build.
// The SVG is rendered in the browser only; the server never stores or serves it.
import { createAvatar } from '@dicebear/core';
import * as notionists from '@dicebear/notionists';
import opts from './options.js';

const { CALM, PORTRAIT, FACE } = opts;
const make = (seed, crop) =>
  createAvatar(notionists, { seed: String(seed), ...CALM, ...crop })
    .toString()
    .replace(/<metadata[\s>][\s\S]*?<\/metadata>/, '');
window.XBotAvatar = Object.freeze({ portrait: (seed) => make(seed, PORTRAIT), face: (seed) => make(seed, FACE) });
