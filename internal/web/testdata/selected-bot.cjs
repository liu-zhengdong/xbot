// Shipped static page + scripted HTTP, using the same Playwright/headless-shell
// infrastructure as the other browser journeys. Never replace window.fetch or
// selection code: close real pages/contexts to exercise storage lifetimes.
const {chromium} = require('playwright');
const assert = require('node:assert/strict');
const [chrome, url, profile, scenario] = process.argv.slice(2);
const key = 'xbot-selected-bot';
const first = 'Alpha', primary = 'Home', chosen = 'Focus';
let bots = [first, primary, chosen].map(name => ({
  name, pinned: true, primary: name === primary, avatar_seed: 'glyph',
}));
let context;
const errors = [];

async function launch() {
  context = await chromium.launchPersistentContext(profile, {
    executablePath: chrome, headless: true, viewport: {width: 1000, height: 800},
    serviceWorkers: 'block',
  });
  context.setDefaultTimeout(10000);
  // Observe the browser's actual storage before app boot; do not seed or clear
  // the selected Bot, even on a newly opened page or restarted process.
  await context.addInitScript(({selectionKey, origin}) => {
    // Persistent contexts also create an about:blank page with no storage origin.
    if (location.origin !== origin) return;
    window.selectionStorageAtBoot = {
      session: sessionStorage.getItem(selectionKey),
      persistent: localStorage.getItem(selectionKey),
      windowMarker: sessionStorage.getItem('selected-bot-test-window'),
    };
  }, {selectionKey: key, origin: new URL(url).origin});
  await context.route('**/api/**', async route => {
    const pathname = new URL(route.request().url()).pathname;
    let body = {};
    if (pathname === '/api/session') body = {authed: true, owner: 'owner', owner_name: 'Owner', version: 'dev-test'};
    else if (pathname === '/api/account/me') body = {account: {email: 'owner@example.com', owner: true, official: true}};
    else if (pathname === '/api/bots') body = {bots};
    else if (pathname === '/api/groups') body = {groups: [], folders: []};
    else if (pathname === '/api/bots/status') body = {bots: []};
    else if (pathname === '/api/notes') body = {notes: []};
    else {
      const match = pathname.match(/^\/api\/bots\/([^/]+)(\/chat|\/transcript)?$/);
      if (match) {
        const name = decodeURIComponent(match[1]);
        if (!bots.some(bot => bot.name === name)) {
          await route.fulfill({status: 404, json: {error: 'not found'}});
          return;
        }
        if (match[2] === '/chat') body = {messages: [{
          id: 1, transcript_seq: 1, delivery_key: 'owner:1', kind: 'owner',
          time: '2026-11-01T00:00:00Z', text: 'conversation:' + name,
        }], has_more: false};
        else if (match[2] === '/transcript') body = {messages: []};
        else body = {name, agents: [], links: []};
      }
    }
    await route.fulfill({json: body});
  });
  context.on('page', page => page.on('pageerror', error => errors.push(error.message)));
}

async function open() {
  // newPage has no opener: it must not inherit another window's sessionStorage.
  const page = await context.newPage();
  await page.goto(url);
  return page;
}

async function selected(page, name) {
  try {
    await page.waitForFunction(name => {
      const active = document.querySelector('[data-bot].active');
      return !document.querySelector('#shell')?.hidden && active?.dataset.bot === name &&
        document.querySelector('#title')?.textContent.includes(name) &&
        document.querySelector('#thread')?.textContent.includes('conversation:' + name) &&
        !document.querySelector('.thread-loading-cover');
    }, name);
  } catch (error) {
    throw Error('expected selected Bot ' + name + ': ' + JSON.stringify(await page.evaluate(() => ({
      title: document.querySelector('#title')?.textContent,
      active: document.querySelector('[data-bot].active')?.dataset.bot,
      thread: document.querySelector('#thread')?.textContent,
      storageAtBoot: window.selectionStorageAtBoot,
    }))), {cause: error});
  }
}

async function choose(page, name) {
  await page.locator('[data-bot-menu][data-bot="' + name + '"]').click();
  await selected(page, name);
}

async function freshWindowStorage(page, remembered) {
  const storage = await page.evaluate(() => window.selectionStorageAtBoot);
  assert.equal(storage.session, null, 'new page inherited the previous session selection');
  assert.equal(storage.windowMarker, null, 'previous window sessionStorage survived');
  assert.equal(storage.persistent, remembered, 'persistent choice was not available before app boot');
}

(async () => {
  await launch();
  let page = await open();
  // Primary is deliberately not first: fallback precedence is observable.
  await selected(page, primary);
  await choose(page, chosen);
  await page.evaluate(() => sessionStorage.setItem('selected-bot-test-window', 'old-window'));

  if (scenario === 'window-preference') {
    const other = await open();
    await selected(other, chosen);
    await freshWindowStorage(other, chosen);
    await choose(other, first);
    await selected(page, chosen); // storage events must not switch another window.
    const latest = await open();
    await selected(latest, first);
    await freshWindowStorage(latest, first);
    // Existing window's session selection wins even when persistence changed.
    await page.reload();
    assert.equal((await page.evaluate(() => window.selectionStorageAtBoot)).session, chosen);
    assert.equal((await page.evaluate(() => window.selectionStorageAtBoot)).persistent, first);
    await selected(page, chosen);
  } else {
    await page.close();
    assert(page.isClosed(), 'original page did not close');
    if (scenario === 'browser-restart') {
      // The same on-disk profile and HTTP origin survive a real process exit.
      await context.close();
      context = null;
      await launch();
    } else if (scenario === 'missing-primary' || scenario === 'missing-first') {
      bots = bots.filter(bot => bot.name !== chosen);
      if (scenario === 'missing-first') bots = bots.map(bot => ({...bot, primary: false}));
    } else assert.equal(scenario, 'closed-page');
    page = await open();
    const expected = scenario === 'missing-primary' ? primary : scenario === 'missing-first' ? first : chosen;
    await selected(page, expected);
    await freshWindowStorage(page, chosen);
    assert.equal(await page.locator('[data-bot-menu][data-bot="' + chosen + '"]').count(), scenario.startsWith('missing-') ? 0 : 1);
  }
  assert.deepEqual(errors, [], 'page errors');
  console.log('passed selected Bot ' + scenario);
})().catch(error => {
  console.error(error.stack);
  process.exitCode = 1;
}).finally(async () => {
  if (context) await context.close();
});
