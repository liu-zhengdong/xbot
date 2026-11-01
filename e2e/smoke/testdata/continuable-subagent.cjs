// The original binary smoke's second turn, driven through its actual serve UI.
// Only the Go fixture's upstream model is scripted. No route/fetch/DOM mocks.
const {chromium} = require('playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const cfg = JSON.parse(fs.readFileSync(0, 'utf8'));

(async () => {
  const browser = await chromium.launch({executablePath: cfg.chrome, headless: true});
  let page;
  const diagnostic = {errors: [], profiles: []};
  try {
    const context = await browser.newContext({viewport: {width: 1440, height: 900}, deviceScaleFactor: 2, colorScheme: 'light'});
    await context.addCookies(cfg.cookies.map(cookie => ({name: cookie.Name, value: cookie.Value, url: cfg.url})));
    await context.addInitScript(bot => {
      sessionStorage.setItem('xbot-selected-bot', bot);
      localStorage.setItem('xbot-theme', 'light');
      localStorage.removeItem('xbot-workspace-hidden');
    }, cfg.bot);
    page = await context.newPage();
    page.setDefaultTimeout(15000);
    const errors = diagnostic.errors, messages = [], transcriptResponses = [], artifacts = [];
    page.on('pageerror', error => errors.push(error.message));
    page.on('request', request => {
      const route = new URL(request.url()).pathname;
      if (request.method() === 'POST' && route.endsWith('/messages')) messages.push({route, body: request.postDataJSON()});
    });
    page.on('response', response => {
      const route = new URL(response.url()).pathname;
      if (route === '/api/bots/' + encodeURIComponent(cfg.bot) + '/agents/' + cfg.id + '/transcript') transcriptResponses.push(response);
      if (route === '/api/bots/' + encodeURIComponent(cfg.bot) && response.request().method() === 'GET') {
        response.json().then(data => diagnostic.profiles.push({httpStatus: response.status(), count: (data.agents || []).length, agents: (data.agents || []).map(child => ({id: child.id, status: child.status}))})).catch(() => diagnostic.profiles.push({httpStatus: response.status(), unreadable: true}));
      }
    });
    const tab = name => page.locator('#workspace-tabs').getByRole('tab', {name, exact: true});
    const panelID = async () => tab('子代理').getAttribute('aria-controls');
    await page.goto(cfg.url);
    await page.waitForFunction(bot => document.querySelector('#title')?.textContent.includes(bot), cfg.bot);
    await page.locator('.thread-loading-cover').waitFor({state: 'detached'});
    const parentDraft = 'PARENT_DRAFT_WHILE_CONTINUING_CHILD';
    await page.locator('#input').fill(parentDraft);
    await tab('子代理').click();
    const childPanel = page.locator('#' + await panelID());
    const row = childPanel.locator('[data-agent="' + cfg.id + '"]');
    await row.waitFor({state: 'visible'});
    if (await row.getAttribute('role') === 'button') await row.click();
    else await row.getByRole('button', {name: '查看对话', exact: true}).click();
    await page.getByRole('textbox', {name: '给当前子代理发消息', exact: true}).waitFor({state: 'visible'});
    await childPanel.getByText(cfg.firstConclusion, {exact: false}).first().waitFor({state: 'visible'});
    assert(transcriptResponses.length > 0, 'child UI never read its real transcript endpoint');

    // Show the original write and its paired result through the public reader.
    // The shared trace adapter and its temporary public-record fallback both
    // expose this through a visible tool row and the Record details pane.
    const toolRows = childPanel.getByRole('button').filter({hasText: /工具调用|write_file|写入文件/});
    await toolRows.first().click();
    const detail = childPanel.getByRole('complementary', {name: '记录详情', exact: true});
    await detail.waitFor({state: 'visible'});
    const raw = detail.getByRole('button', {name: '原始内容', exact: true});
    if (await raw.count()) await raw.click();
    const originalEvidence = await detail.innerText();
    assert(originalEvidence.includes('accept_write_1') && originalEvidence.includes(cfg.firstBody.trim()) && originalEvidence.includes('wrote '), 'the visible reader did not show the real first tool call with its successful result');

    const endpoint = '/api/bots/' + encodeURIComponent(cfg.bot) + '/agents/' + cfg.id + '/messages';
    const responsePending = page.waitForResponse(response => new URL(response.url()).pathname === endpoint && response.request().method() === 'POST');
    await page.getByRole('textbox', {name: '给当前子代理发消息', exact: true}).fill(cfg.feedback);
    await page.getByRole('combobox', {name: '发送方式', exact: true}).selectOption('steer');
    await page.getByRole('button', {name: '发送给这个子代理', exact: true}).click();
    const response = await responsePending;
    const receipt = await response.json();
    assert.equal(response.status(), 200);
    assert.equal(receipt.id, cfg.id, 'browser continuation changed child identity');
    assert.equal(receipt.status, 'running', 'free admission did not start the existing child');
    await childPanel.getByText(cfg.secondConclusion, {exact: false}).first().waitFor({state: 'visible'});
    assert.equal(await page.locator('#input').textContent(), parentDraft, 'child form overwrote the main Bot draft');
    assert.equal(messages.length, 1, 'browser sent duplicate feedback or a main Bot message');
    assert.equal(messages[0].route, endpoint);
    assert.equal(messages[0].body.text, cfg.feedback);
    assert.equal(messages[0].body.delivery, 'steer');

    // Read the new write too, then leave one stable desktop and narrow view.
    await toolRows.last().click();
    await detail.waitFor({state: 'visible'});
    if (await raw.count()) await raw.click();
    assert((await detail.innerText()).includes('accept_write_2'), 'second turn did not appear in the same real child reader');
    if (cfg.artifacts) {
      fs.mkdirSync(cfg.artifacts, {recursive: true});
      const desktop = path.join(cfg.artifacts, 'continuable-subagent-desktop.png');
      await page.screenshot({path: desktop, animations: 'disabled'}); artifacts.push(desktop);
    }
    await page.setViewportSize({width: 390, height: 844});
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false, 'child page overflows at 390px');
    if (cfg.artifacts) {
      const narrow = path.join(cfg.artifacts, 'continuable-subagent-390.png');
      await page.screenshot({path: narrow, animations: 'disabled'}); artifacts.push(narrow);
    }
    assert.deepEqual(errors, []);
    console.log(JSON.stringify({status: response.status(), receipt, artifacts}));
    await context.close();
  } catch (error) {
    if (page && !page.isClosed()) {
      const view = await page.evaluate(() => {
        const root = document.getElementById('workspace-view-agents');
        return {
          url: location.origin + location.pathname,
          title: document.querySelector('#title')?.textContent,
          tabs: [...document.querySelectorAll('#workspace-tabs [role=tab]')].map(tab => ({name: tab.textContent, selected: tab.getAttribute('aria-selected')})),
          rootVisible: root?.checkVisibility(),
          overviewHidden: root?.querySelector('.sa-overview')?.hidden,
          childTitles: [...(root?.querySelectorAll('h2') || [])].filter(node => node.checkVisibility()).map(node => node.textContent),
          rows: [...(root?.querySelectorAll('[data-agent]') || [])].map(node => ({id: node.dataset.agent, visible: node.checkVisibility(), role: node.getAttribute('role')})),
          visibleText: root?.innerText?.slice(0, 1200)
        };
      });
      console.error('CHILD_UI_DIAGNOSTIC ' + JSON.stringify({...diagnostic, view}));
      if (cfg.artifacts) {
        fs.mkdirSync(cfg.artifacts, {recursive: true});
        await page.screenshot({path: path.join(cfg.artifacts, 'continuable-subagent-failure.png'), animations: 'disabled'});
      }
    }
    throw error;
  } finally {
    await browser.close();
  }
})().catch(error => {console.error(error); process.exitCode = 1});
