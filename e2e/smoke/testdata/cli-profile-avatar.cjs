// Independent avatar acceptance: shipped UI, real messages/SSE, actual Shell
// CLI and persisted profile APIs. No product fetch, routes or renderer stubs.
const { chromium } = require('playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const cfg = JSON.parse(fs.readFileSync(0, 'utf8'));

(async () => {
  const browser = await chromium.launch({ executablePath: cfg.chrome, headless: true });
  try {
    const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 2, colorScheme: 'light' });
    await context.addCookies(cfg.cookies.map(c => ({ name: c.Name, value: c.Value, url: cfg.url })));
    await context.addInitScript(() => {
      sessionStorage.setItem('xbot-selected-bot', 'Worker');
      localStorage.setItem('xbot-theme', 'light');
    });
    const page = await context.newPage();
    page.setDefaultTimeout(12000);
    const errors = [], runs = [], profiles = [];
    const selectors = ['[data-bot="Worker"] .avatar', '#title .avatar'];
    page.on('pageerror', error => errors.push(error.message));
    page.on('request', request => {
      if (new URL(request.url()).pathname === '/api/bots/Worker/messages' && request.method() === 'POST') {
        runs.push({ request, finished: false, started: performance.now() });
      }
    });
    page.on('requestfinished', request => {
      const run = runs.find(r => r.request === request);
      if (run) { run.finished = true; run.ended = performance.now(); }
    });
    page.on('requestfailed', request => {
      const run = runs.find(r => r.request === request);
      if (run) run.failed = request.failure();
    });
    page.on('response', async response => {
      const pathname = new URL(response.url()).pathname;
      if (!['/api/bots', '/api/bots/Worker'].includes(pathname) || response.request().method() !== 'GET' || response.status() !== 200) return;
      try {
        const data = await response.json();
        const bot = pathname === '/api/bots' ? data.bots.find(b => b.name === 'Worker') : data;
        if (bot) profiles.push({ pathname, avatar: bot.avatar, seed: bot.avatar_seed, at: performance.now(), during: !!runs.length && !runs.at(-1).finished });
      } catch (error) { errors.push('profile response: ' + error.message); }
    });
    const wait = async (condition, message) => {
      const deadline = performance.now() + 12000;
      while (performance.now() < deadline) {
        if (await condition()) return;
        await new Promise(resolve => setTimeout(resolve, 25));
      }
      throw Error(message + '\n' + JSON.stringify({ runs: runs.map(({request,...r}) => r), profiles }));
    };
    const control = async (action, cue, method = 'GET') => {
      const response = await fetch(cfg.control + '/validation/' + action + '?cue=' + cue, { method });
      assert.equal(response.status, 200);
      return response.json();
    };
    // A changed source proves the displayed image was replaced; decoded pixels
    // below separately prove a fresh DOM node or SVG id alone cannot pass.
    const faces = () => page.evaluate(selectors => selectors.map(selector => {
      const node = document.querySelector(selector), img = node?.querySelector('img');
      return { source: img ? (img.currentSrc || img.src) : node?.querySelector('svg')?.outerHTML || '', ready: !!node && (!img || (img.complete && img.naturalWidth > 0)) };
    }), selectors);
    const pixels = () => Promise.all(selectors.map(selector => page.locator(selector).screenshot({ animations: 'disabled' })));
    const changedFaces = before => wait(async () => (await faces()).every((face, i) => face.ready && face.source && face.source !== before[i].source), 'sidebar/header avatar images did not both change');
    const checkPixels = (before, after) => after.forEach((png, i) => assert(!png.equals(before[i]), ['sidebar', 'header'][i] + ' avatar pixels did not change'));
    const send = async cue => {
      const previous = runs.length;
      await page.getByRole('tab', { name: '聊天', exact: true }).click();
      await page.locator('#input').fill(cue);
      await page.locator('#send').click();
      await wait(() => runs.length === previous + 1, 'real messages request did not start');
      return runs.at(-1);
    };
    const shot = name => page.screenshot({ path: path.join(cfg.evidence, name + '.png'), animations: 'disabled' });
    fs.mkdirSync(cfg.evidence, { recursive: true });
    await page.goto(cfg.url);
    await page.waitForFunction(() => document.querySelector('#title')?.textContent.includes('Worker'));
    await page.locator('.thread-loading-cover').waitFor({ state: 'detached' });
    await wait(async () => (await faces()).every(face => face.ready && face.source), 'uploaded avatars were not decoded');
    assert(profiles.some(profile => profile.avatar === 'custom' && profile.seed === 'Worker#original'), 'initial API did not expose the uploaded avatar');
    const initialFaces = await faces(), initialPixels = await pixels();
    await shot('avatar-uploaded');

    const long = await send('CLI_AVATAR_BROWSER_LONG');
    await wait(async () => (await control('status', 'CLI_AVATAR_BROWSER_LONG')).held, 'CLI did not return into the next held model request');
    await changedFaces(initialFaces);
    checkPixels(initialPixels, await pixels());
    assert.equal(long.finished, false, 'dual-avatar change was observed only after the long turn ended');
    await wait(() => ['/api/bots', '/api/bots/Worker'].every(pathname => profiles.some(profile => profile.pathname === pathname && profile.avatar === 'generated' && profile.seed === 'Ada#2' && profile.during)), 'real profile APIs did not expose the new seed during the stream');
    await page.locator('#thread .live').waitFor();
    await shot('avatar-during-long-turn');
    const longFaces = await faces(), longPixels = await pixels();
    await control('release', 'CLI_AVATAR_BROWSER_LONG', 'POST');
    await wait(() => long.finished, 'long turn did not finish after gate release');
    await page.locator('#thread .live').waitFor({ state: 'detached' });

    const readsBeforeShort = profiles.length;
    const short = await send('CLI_AVATAR_BROWSER_SHORT');
    await changedFaces(longFaces);
    checkPixels(longPixels, await pixels());
    const shortPainted = performance.now();
    assert.equal(short.finished, false, 'short turn updated avatar pixels only after SSE completion');
    assert(profiles.slice(readsBeforeShort).some(profile => profile.seed === 'Worker#avatar-short-29' && profile.avatar === 'generated' && profile.during), 'short turn only read avatar metadata after completion');
    await shot('avatar-during-short-turn');
    await wait(() => short.finished, 'natural short turn did not finish');
    assert(short.ended - short.started < 3000, 'short fixture allowed a full periodic polling window');
    assert(runs.every(run => !run.failed), 'a messages SSE request failed');
    assert.deepEqual(errors, []);
    console.log(JSON.stringify({ browser: browser.version(), evidence: cfg.evidence, dualAvatarPixelsChangedBeforeLongDone: true, dualAvatarPixelsChangedBeforeShortDone: true, shortPaintMs: Math.round(shortPainted - short.started), shortTurnMs: Math.round(short.ended - short.started), profiles, messageRequests: runs.length }));
    await context.close();
  } finally { await browser.close(); }
})().catch(error => { console.error(error); process.exit(1); });
