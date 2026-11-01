// One representative shipped-UI journey. Only the upstream model and its
// control HTTP are scripted; CDP observes real bytes without replacing fetch.
const { chromium } = require('playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const cfg = JSON.parse(fs.readFileSync(0, 'utf8'));

(async () => {
  const browser = await chromium.launch({ executablePath: cfg.chrome, headless: true });
  let page;
  const runs = [], reads = [], childPosts = [], errors = [];
  const diagnostic = () => ({
    runs: runs.map(run => ({ route: run.route, ready: run.ready, finished: run.finished, failed: run.failed, profiles: events(run, 'profile') })),
    childPosts, errors,
  });
  function events(run, kind) {
    const body = Buffer.concat([run.prefix || Buffer.alloc(0), ...run.chunks]).toString('utf8');
    return body.split(/\r?\n\r?\n/).slice(0, -1).flatMap(block => {
      const lines = block.split(/\r?\n/);
      if (!lines.some(line => line.replace(/^event:\s*/, '') === kind)) return [];
      const data = lines.filter(line => line.startsWith('data:')).map(line => line.slice(5).trim()).join('\n');
      try { return [JSON.parse(data)]; } catch { return []; }
    });
  }
  const wait = async (condition, message) => {
    const deadline = performance.now() + 12000;
    while (performance.now() < deadline) {
      if (await condition()) return;
      await new Promise(resolve => setTimeout(resolve, 25));
    }
    throw Error(message + '\n' + JSON.stringify(diagnostic()));
  };
  const control = async (base, route, method = 'GET') => {
    const response = await fetch(base + route, { method });
    assert.equal(response.status, 200, 'fixture control HTTP failed');
    return response.json();
  };
  const gate = (action, key) => control(cfg.control, '/' + action + '?gate=' + key, action === 'release' ? 'POST' : 'GET');
  const model = (action, cue) => control(cfg.model, '/validation/' + action + '?cue=' + cue, action === 'release' ? 'POST' : 'GET');
  const pending = run => {
    assert.equal(run.finished, false, 'original messages SSE ended while its turn was held');
    assert.equal(run.failed, undefined, 'original messages SSE was aborted or failed');
  };
  try {
    fs.mkdirSync(cfg.evidence, { recursive: true });
    const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, colorScheme: 'light' });
    await context.addCookies(cfg.cookies.map(cookie => ({ name: cookie.Name, value: cookie.Value, url: cfg.url })));
    await context.addInitScript(() => {
      sessionStorage.setItem('xbot-selected-bot', 'Worker');
      localStorage.setItem('xbot-theme', 'light');
      localStorage.removeItem('xbot-workspace-hidden');
    });
    page = await context.newPage();
    page.setDefaultTimeout(12000);
    page.on('pageerror', error => errors.push(error.message));
    page.on('request', request => {
      const route = new URL(request.url()).pathname;
      if (request.method() === 'GET') reads.push(route);
      if (request.method() === 'POST' && /\/agents\/[^/]+\/messages$/.test(route)) childPosts.push({ route, body: request.postDataJSON() });
    });
    const cdp = await context.newCDPSession(page);
    await cdp.send('Network.enable');
    cdp.on('Network.requestWillBeSent', event => {
      const route = new URL(event.request.url).pathname;
      if (event.request.method === 'POST' && /^\/api\/bots\/[^/]+\/messages$/.test(route)) {
        runs.push({ id: event.requestId, route, chunks: [], ready: false, finished: false });
      }
    });
    cdp.on('Network.responseReceived', async event => {
      const run = runs.find(run => run.id === event.requestId);
      if (!run) return;
      try {
        assert.equal(event.response.status, 200);
        const data = await cdp.send('Network.streamResourceContent', { requestId: run.id });
        run.prefix = Buffer.from(data.bufferedData, 'base64');
        run.ready = true;
      } catch (error) { errors.push('CDP SSE observation: ' + error.message); }
    });
    cdp.on('Network.dataReceived', event => {
      const run = runs.find(run => run.id === event.requestId);
      if (run && event.data) run.chunks.push(Buffer.from(event.data, 'base64'));
    });
    cdp.on('Network.loadingFinished', event => {
      const run = runs.find(run => run.id === event.requestId);
      if (run) run.finished = true;
    });
    cdp.on('Network.loadingFailed', event => {
      const run = runs.find(run => run.id === event.requestId);
      if (run) run.failed = event.errorText;
    });
    const tab = name => page.locator('#workspace-tabs').getByRole('tab', { name, exact: true });
    const selected = async name => {
      await page.waitForFunction(name => document.querySelector('#title')?.textContent.includes(name) && sessionStorage.getItem('xbot-selected-bot') === name, name);
      await page.locator('.item.active[data-bot="' + name + '"]').waitFor();
    };
    const send = async (cue, route) => {
      const before = runs.length;
      await page.locator('#input').fill(cue);
      await page.locator('#send').click();
      await wait(() => runs.length === before + 1 && runs.at(-1).ready, 'real messages SSE observation did not start');
      assert.equal(runs.at(-1).route, route);
      return runs.at(-1);
    };
    const rootDraft = 'ROOT_DRAFT_RETAINED_DURING_RENAME';
    const childDraft = 'CLI_FULL_CHILD_FOLLOWUP';
    const assertRoot = async () => {
      assert.equal(await page.locator('#input').textContent(), rootDraft, 'rename lost the typed root draft');
      assert.equal(await page.locator('#input').evaluate(node => node === document.activeElement), true, 'rename stole root composer focus');
    };

    await page.goto(cfg.url);
    await selected('Worker');
    await page.locator('.thread-loading-cover').waitFor({ state: 'detached' });
    await page.getByText('CLI_FULL_HISTORY', { exact: false }).first().waitFor();
    // Instantiate a genuine child view before rename, then leave its draft
    // unsent while the root composer is focused. Its DOM node must survive.
    await tab('子代理').click();
    const row = page.locator('#workspace-view-agents [data-agent="' + cfg.child + '"]');
    await row.waitFor({ state: 'visible' });
    if (await row.getAttribute('role') === 'button') await row.click();
    else await row.getByRole('button', { name: '查看对话', exact: true }).click();
    const childInput = page.getByRole('textbox', { name: '给当前子代理发消息', exact: true });
    await childInput.waitFor({ state: 'visible' });
    await page.locator('.sa-records').getByText('CLI_FULL_CHILD_READY', { exact: false }).first().waitFor();
    assert(reads.some(route => route === '/api/bots/Worker/agents/' + cfg.child + '/transcript'), 'child view did not read its real public transcript');
    await childInput.fill(childDraft);
    const originalChildInput = await childInput.elementHandle();
    const originalChildView = await page.locator('.sa-child').elementHandle();
    await tab('聊天').click();

    const current = await send('CLI_FULL_CURRENT', '/api/bots/Worker/messages');
    await page.locator('#input').fill(rootDraft);
    const originalInput = await page.locator('#input').elementHandle();
    const originalLive = await page.locator('#thread .msg.live').elementHandle();
    assert(originalLive, 'original streaming row was absent');
    const top = await page.locator('#thread').evaluate(thread => {
      const range = thread.scrollHeight - thread.clientHeight;
      if (range < 400) throw Error('real delivered setup history did not provide a scrollable thread');
      thread.scrollTop = Math.min(200, range / 3);
      return thread.scrollTop;
    });
    await page.locator('#input').focus();
    await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
    await wait(async () => (await gate('status', 'CLI_FULL_CURRENT_pre')).held, 'Shell did not enter pre-rename gate');
    const chatReads = reads.filter(route => /\/bots\/(Worker|NewName)\/chat$/.test(route)).length;
    await gate('release', 'CLI_FULL_CURRENT_pre');
    await wait(async () => (await model('status', 'CLI_FULL_CURRENT')).held, 'actual CLI did not return to held model request');
    await wait(() => events(current, 'profile').some(event => event.name === 'NewName' && event.previous_name === 'Worker'), 'original SSE lacked actual name/previous_name rename event');
    pending(current);
    await selected('NewName');
    await assertRoot();
    assert.equal(await originalInput.evaluate(node => node.isConnected && node === document.querySelector('#input')), true, 'root composer was replaced');
    assert.equal(await originalLive.evaluate(node => node.isConnected && node.classList.contains('live')), true, 'rename reset the active conversation');
    assert.equal(await originalChildInput.evaluate(node => node.isConnected && node.value === 'CLI_FULL_CHILD_FOLLOWUP'), true, 'rename replaced the real child input or lost its draft');
    assert.equal(await originalChildView.evaluate(node => node.isConnected), true, 'rename disposed the real child view');
    assert(Math.abs(await page.locator('#thread').evaluate(node => node.scrollTop) - top) <= 2, 'rename moved the root scroll anchor');
    assert.equal(reads.filter(route => /\/bots\/(Worker|NewName)\/chat$/.test(route)).length, chatReads, 'rename reopened chat instead of retaining the original stream');
    assert.deepEqual(await page.evaluate(id => ({ draft: sessionStorage.getItem('xbot-child-draft:NewName:' + id), chosen: sessionStorage.getItem('xbot-child-selected:NewName'), old: sessionStorage.getItem('xbot-child-draft:Worker:' + id) }), cfg.child), { draft: childDraft, chosen: cfg.child, old: null });
    await page.screenshot({ path: path.join(cfg.evidence, 'profile-rename-original-stream.png'), animations: 'disabled' });

    await model('release', 'CLI_FULL_CURRENT');
    await wait(async () => (await gate('status', 'CLI_FULL_CURRENT_tail')).held, 'owner delivery did not reach the final model gate');
    await wait(() => events(current, 'result').some(event => event.name === 'send_message_to_user' && event.text === 'CLI_FULL_CURRENT completed'), 'subsequent delivery was absent from the same original SSE');
    await page.locator('#thread').getByText('CLI_FULL_CURRENT completed', { exact: true }).waitFor();
    pending(current);
    await selected('NewName');
    await assertRoot();
    assert(Math.abs(await page.locator('#thread').evaluate(node => node.scrollTop) - top) <= 2, 'subsequent stream pulled the retained scroll anchor');
    await gate('release', 'CLI_FULL_CURRENT_tail');
    await wait(() => current.finished, 'original renamed Bot stream did not finish');
    await page.locator('#thread .live').waitFor({ state: 'detached' });
    await tab('子代理').click();
    await childInput.waitFor({ state: 'visible' });
    assert.equal(await originalChildInput.evaluate(node => node.isConnected && node === document.querySelector('.sa-composer textarea')), true, 'reopening child after rename replaced its existing input');
    assert.equal(await childInput.inputValue(), childDraft);

    // Switch while the second Bot's Shell is held. Its original stream must
    // drain late rename/delivery events without changing the new selection.
    await page.locator('.item[data-bot="Late"]').click();
    await selected('Late');
    await page.locator('.thread-loading-cover').waitFor({ state: 'detached' });
    const late = await send('CLI_FULL_LATE', '/api/bots/Late/messages');
    await page.locator('.item[data-bot="Other"]').click();
    await selected('Other');
    await page.locator('.thread-loading-cover').waitFor({ state: 'detached' });
    const otherDraft = 'OTHER_BOT_DRAFT_KEPT';
    await page.locator('#input').fill(otherDraft);
    await wait(async () => (await gate('status', 'CLI_FULL_LATE_pre')).held, 'late Shell did not enter pre-rename gate');
    pending(late);
    await gate('release', 'CLI_FULL_LATE_pre');
    await wait(async () => (await model('status', 'CLI_FULL_LATE')).held, 'late CLI rename did not complete after switching Bot');
    await wait(() => events(late, 'profile').some(event => event.name === 'LateNew' && event.previous_name === 'Late'), 'switched-away original SSE lacked the delayed rename event');
    pending(late);
    await model('release', 'CLI_FULL_LATE');
    await wait(async () => (await gate('status', 'CLI_FULL_LATE_tail')).held, 'late owner delivery did not enter final gate');
    await wait(() => events(late, 'result').some(event => event.name === 'send_message_to_user' && event.text === 'CLI_FULL_LATE completed'), 'switched-away SSE did not receive its late response');
    pending(late);
    await selected('Other');
    assert.equal(await page.locator('#input').textContent(), otherDraft, 'late signal overwrote the other Bot draft');
    assert.equal(await page.locator('#input').evaluate(node => node === document.activeElement), true, 'late signal stole focus from the other Bot');
    assert.equal(await page.locator('#thread').getByText('CLI_FULL_LATE completed', { exact: true }).count(), 0, 'late response rendered into the other Bot');
    assert.equal(await page.locator('#thread .live').count(), 0, 'late response created a streaming row in the other Bot');
    await page.screenshot({ path: path.join(cfg.evidence, 'profile-rename-late-selection.png'), animations: 'disabled' });
    await gate('release', 'CLI_FULL_LATE_tail');
    await wait(() => late.finished, 'switched-away original stream did not drain to completion');
    await selected('Other');
    assert.equal(await page.locator('#input').textContent(), otherDraft, 'late completion overwrote the selected Bot draft');
    assert.equal(await page.locator('#input').evaluate(node => node === document.activeElement), true, 'late completion stole the selected Bot focus');
    const actualLate = await context.request.get(cfg.url + '/api/bots/LateNew');
    assert.equal(actualLate.status(), 200);
    assert.equal((await actualLate.json()).name, 'LateNew');

    await page.locator('.item[data-bot="NewName"]').click();
    await selected('NewName');
    await page.locator('.thread-loading-cover').waitFor({ state: 'detached' });
    assert.equal(await page.locator('#input').textContent(), rootDraft, 'renamed root draft key did not survive a Bot switch');
    await tab('子代理').click();
    await childInput.waitFor({ state: 'visible' });
    assert.equal(await childInput.inputValue(), childDraft, 'renamed child draft/selection key did not survive a Bot switch');
    // The preserved child input must address the same actual Local child using
    // its new Bot route, rather than merely displaying retained text.
    const childRoute = '/api/bots/NewName/agents/' + cfg.child + '/messages';
    const [childResponse] = await Promise.all([
      page.waitForResponse(response => new URL(response.url()).pathname === childRoute && response.request().method() === 'POST'),
      page.getByRole('button', { name: '发送给这个子代理', exact: true }).click(),
    ]);
    assert.equal(childResponse.status(), 200);
    assert.equal((await childResponse.json()).id, cfg.child);
    await page.locator('.sa-records').getByText('CLI_FULL_CHILD_FOLLOWED', { exact: false }).first().waitFor();
    assert.equal(childPosts.length, 1);
    assert.equal(childPosts[0].route, childRoute);
    assert.equal(childPosts[0].body.text, childDraft);
    assert.equal(runs.length, 2, 'rename or child input created an unexpected root messages request');
    assert(runs.every(run => run.finished && !run.failed), 'an original root SSE was aborted');
    assert.deepEqual(errors, []);
    console.log(JSON.stringify({ browser: browser.version(), evidence: cfg.evidence, actualRenameEvents: runs.map(run => events(run, 'profile')), originalStreamsRetained: true, rootDraftFocusScrollRetained: true, realChildInputAndDraftRetained: true, childRoute, lateSelectionIsolated: true }));
    await context.close();
  } catch (error) {
    const view = page && !page.isClosed() ? await page.evaluate(() => ({ title: document.querySelector('#title')?.textContent, selected: sessionStorage.getItem('xbot-selected-bot'), tab: document.querySelector('#workspace-tabs [aria-selected="true"]')?.textContent, rootDraft: document.querySelector('#input')?.textContent, childDraft: document.querySelector('.sa-composer textarea')?.value, scrollTop: document.querySelector('#thread')?.scrollTop })) : {};
    console.error('FULL_PROFILE_BROWSER_DIAGNOSTIC ' + JSON.stringify({ ...diagnostic(), view }));
    throw error;
  } finally { await browser.close(); }
})().catch(error => { console.error(error); process.exitCode = 1; });
