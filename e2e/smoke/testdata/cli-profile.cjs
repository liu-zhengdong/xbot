// Independent acceptance against the real binary and shipped UI. No route,
// fetch, SSE, DOM rendering function or product module is stubbed here.
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
    const errors = [], requests = [], runs = [], profileReads = [];
    page.on('pageerror', error => errors.push(error.message));
    page.on('request', request => {
      const pathname = new URL(request.url()).pathname;
      requests.push({ request, pathname, at: performance.now() });
      if (pathname === '/api/bots/Worker/messages' && request.method() === 'POST') {
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
        if (bot) profileReads.push({ pathname, description: bot.description, timezone: bot.timezone, at: performance.now(), during: runs.at(-1) && !runs.at(-1).finished });
      } catch (e) { errors.push('profile response: ' + e.message); }
    });
    const wait = async (condition, message) => {
      const deadline = performance.now() + 12000;
      while (performance.now() < deadline) {
        if (await condition()) return;
        await new Promise(resolve => setTimeout(resolve, 25));
      }
      throw new Error(message + '\n' + JSON.stringify({ runs: runs.map(({request,...r}) => r), profileReads }));
    };
    const control = async (action, cue, method = 'GET') => {
      const response = await fetch(cfg.control + '/validation/' + action + '?cue=' + cue, { method });
      assert.equal(response.status, 200);
      return response.json();
    };
    const descriptionField = () => page.locator('#modal-fields label').filter({ hasText: /^简介$/ }).locator('input');
    const openEditor = async () => {
      await page.locator('#head-chip').click();
      await page.getByRole('button', { name: '编辑资料', exact: true }).click();
      await page.locator('#modal').waitFor({ state: 'visible' });
    };
    const send = async cue => {
      const previous = runs.length;
      await page.getByRole('tab', { name: '聊天', exact: true }).click();
      await page.locator('#input').fill(cue);
      await page.locator('#send').click();
      await wait(() => runs.length === previous + 1, 'real message request did not start');
      return runs.at(-1);
    };
    fs.mkdirSync(cfg.evidence, { recursive: true });
    await page.goto(cfg.url);
    await page.waitForFunction(() => document.querySelector('#title')?.textContent.includes('Worker'));
    await page.locator('.thread-loading-cover').waitFor({ state: 'detached' });
    await openEditor();
    assert.equal(await descriptionField().inputValue(), 'before browser');
    await page.locator('#modal-cancel').click();

    const long = await send('CLI_BROWSER_LONG');
    await wait(async () => (await control('status', 'CLI_BROWSER_LONG')).held, 'CLI did not reach the next held model request');
    await wait(() => profileReads.some(r => r.description === 'browser live saved' && r.during), 'saved CLI profile was not fetched while the stream remained open');
    assert.equal(long.finished, false, 'turn ended before live profile evidence');
    await page.locator('#thread .live').waitFor();
    await openEditor();
    assert.equal(await descriptionField().inputValue(), 'browser live saved', 'real profile editor still opened old data');
    assert.equal(long.finished, false, 'editor only refreshed after turn completion');
    await page.screenshot({ path: path.join(cfg.evidence, 'profile-during-turn.png'), animations: 'disabled' });
    await page.locator('#modal-cancel').click();
    await control('release', 'CLI_BROWSER_LONG', 'POST');
    await wait(() => long.finished, 'held turn did not finish after release');
    await page.getByRole('tab', { name: '聊天', exact: true }).click();
    await page.locator('#thread').getByText('CLI_BROWSER_LONG completed', { exact: true }).waitFor();
    await page.locator('#thread .live').waitFor({ state: 'detached' });

    const shortStart = profileReads.length;
    const short = await send('CLI_BROWSER_SHORT');
    await wait(() => short.finished, 'natural short turn never finished');
    const shortReads = profileReads.slice(shortStart).filter(r => r.description === 'browser short saved' && r.at >= short.started);
    assert(shortReads.some(r => r.during), 'short turn only refreshed after SSE completion (periodic polling or final reconciliation masked the missing live update)');
    assert(short.ended - short.started < 3000, 'short-turn fixture accidentally allowed a full periodic polling window');
    await page.locator('#thread .live').waitFor({ state: 'detached' });
    await openEditor();
    assert.equal(await descriptionField().inputValue(), 'browser short saved');
    await page.screenshot({ path: path.join(cfg.evidence, 'profile-after-short-turn.png'), animations: 'disabled' });
    assert.deepEqual(errors, []);
    assert(runs.every(run => !run.failed), 'messages SSE request failed');
    console.log(JSON.stringify({ browser: browser.version(), evidence: cfg.evidence, longProfileObservedBeforeSSEEnd: true, shortProfileObservedBeforeSSEEnd: true, shortTurnMs: Math.round(short.ended - short.started), profileReads, messageRequests: runs.length }));
    await context.close();
  } finally {
    await browser.close();
  }
})().catch(error => { console.error(error); process.exit(1); });
