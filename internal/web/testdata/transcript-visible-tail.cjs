const { chromium } = require('playwright');
const fs = require('node:fs');
const cfg = JSON.parse(fs.readFileSync(0, 'utf8'));
const assert = (ok, message) => { if (!ok) throw Error(message); };
(async () => {
  const browser = await chromium.launch({executablePath:'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',headless:true});
  try {
    const context = await browser.newContext({viewport:{width:1440,height:900},deviceScaleFactor:2});
    await context.addCookies([{name:cfg.cookie,value:cfg.token,url:cfg.url}]);
    await context.addInitScript(() => sessionStorage.setItem('xbot-selected-bot','History'));
    const page = await context.newPage();
    const requests = [], errors = [];
    page.on('request', req => { if(new URL(req.url()).pathname.endsWith('/transcript')) requests.push(new URL(req.url()).search); });
    page.on('pageerror', e => errors.push(e.message));
    await page.goto(cfg.url);
    await page.locator('#thread .msg.bot').first().waitFor();
    await page.locator('.thread-loading-cover').waitFor({state:'detached'});
    const shot = async stage => {
      if (!cfg.evidence) return;
      for (const theme of ['light','dark']) {
        await page.evaluate(t => { document.documentElement.dataset.theme=t; }, theme);
        await page.waitForTimeout(250);
        const dir = cfg.evidence+'/'+theme; fs.mkdirSync(dir,{recursive:true});
        await page.screenshot({path:dir+'/'+stage+'.png'});
      }
    };
    await shot('opened');
    assert(await page.locator('#thread .msg.bot').count() === 25, 'default Window did not render 25 delivered bubbles');
    assert(!requests.some(q => q.includes('before=')), 'opening relied on automatic before requests');
    await page.locator('#thread .msg.bot[data-seq="301"]').evaluate(n => { window.visibleTailAnchor = n; window.tailNodes = [...document.querySelectorAll('#thread .msg.bot')]; });
    await page.locator('#thread').evaluate(n => { n.scrollTop = 40; });
    await page.waitForTimeout(250);
    await page.evaluate(() => {
      const thread = document.querySelector('#thread');
      window.visibleTailAnchor = [...thread.querySelectorAll('.msg')].find(n => n.getBoundingClientRect().bottom > thread.getBoundingClientRect().top);
    });
    const top = await page.evaluate(() => window.visibleTailAnchor.getBoundingClientRect().top);
    await page.locator('#history-older').evaluate(n => n.click());
    await page.locator('#thread .msg.bot[data-seq="1"]').waitFor();
    await page.waitForTimeout(2100);
    await shot('older');
    assert(await page.locator('#thread .msg.bot').count() === 50, 'before Window did not add another 25 bubbles');
    assert(await page.evaluate(() => window.tailNodes.every(n => n.isConnected)), 'prepend discarded opened tail bubbles');
    assert(await page.evaluate(() => window.visibleTailAnchor === document.querySelector('#thread .msg.bot[data-seq="'+window.visibleTailAnchor.dataset.seq+'"]')), 'prepend replaced the existing delivery node');
    const deltaTop = await page.evaluate(y => window.visibleTailAnchor.getBoundingClientRect().top-y, top);
    assert(Math.abs(deltaTop)<4, 'prepend moved the reading anchor: '+deltaTop);
    assert(requests.filter(q => q.includes('before=')).length === 1 && requests.includes('?before=301'), 'caller guessed extra raw pages');
    const stats = await page.evaluate(async () => {
      const times = []; let bytes = 0;
      for (let i=0;i<40;i++) {
        const start = performance.now(), response = await fetch('/api/bots/History/transcript');
        const text = await response.text(); times.push(performance.now()-start); bytes = new TextEncoder().encode(text).length;
        const data = JSON.parse(text);
        if(data.messages.length !== 300 || data.window.head_seq !== 301) throw Error('HTTP default lost adaptive Window');
      }
      times.sort((a,b)=>a-b); return {httpP95Ms:times[37],bytes,rows:300,bubbles:25};
    });
    assert(errors.length === 0, errors.join('; '));
    console.log(JSON.stringify({...stats,deltaTop}));
  } finally { await browser.close(); }
})().catch(e => { console.error(e); process.exitCode=1; });
