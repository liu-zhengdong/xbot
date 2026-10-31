const { chromium } = require('playwright');
const fs = require('node:fs');
const cfg = JSON.parse(fs.readFileSync(0, 'utf8'));
const assert = (ok, message) => { if (!ok) throw Error(message); };
(async () => {
  const browser = await chromium.launch({executablePath:'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',headless:true});
  try {
    const context = await browser.newContext();
    await context.addCookies([{name:cfg.cookie,value:cfg.token,url:cfg.url}]);
    await context.addInitScript(() => sessionStorage.setItem('xbot-selected-bot','History'));
    const page = await context.newPage();
    const requests = [], errors = [];
    page.on('request', req => { if(new URL(req.url()).pathname.endsWith('/transcript')) requests.push(new URL(req.url()).search); });
    page.on('pageerror', e => errors.push(e.message));
    await page.goto(cfg.url);
    await page.locator('#thread .msg.bot[data-seq="111"]').waitFor();
    await page.locator('.thread-loading-cover').waitFor({state:'detached'});
    assert(await page.locator('#thread .msg.bot').count() === 25, 'default Window did not render 25 delivered bubbles');
    assert(!requests.some(q => q.includes('before=')), 'opening relied on automatic before requests');
    await page.locator('#thread .msg.bot[data-seq="111"]').evaluate(n => { window.visibleTailAnchor = n; });
    await page.locator('#thread').evaluate(n => { n.scrollTop = 40; });
    await page.locator('#history-older').evaluate(n => n.click());
    await page.locator('#thread .msg.bot[data-seq="1"]').waitFor();
    assert(await page.locator('#thread .msg.bot').count() === 50, 'before Window did not add another 25 bubbles');
    assert(await page.evaluate(() => window.visibleTailAnchor === document.querySelector('#thread .msg.bot[data-seq="111"]')), 'prepend replaced the existing delivery node');
    assert(requests.filter(q => q.includes('before=')).length === 1 && requests.includes('?before=111'), 'caller guessed extra raw pages');
    const stats = await page.evaluate(async () => {
      const times = []; let bytes = 0;
      for (let i=0;i<40;i++) {
        const start = performance.now(), response = await fetch('/api/bots/History/transcript');
        const text = await response.text(); times.push(performance.now()-start); bytes = new TextEncoder().encode(text).length;
        const data = JSON.parse(text);
        if(data.messages.length !== 110 || data.window.head_seq !== 111) throw Error('HTTP default lost adaptive Window');
      }
      times.sort((a,b)=>a-b); return {httpP95Ms:times[37],bytes,rows:110,bubbles:25};
    });
    assert(errors.length === 0, errors.join('; '));
    console.log(JSON.stringify(stats));
  } finally { await browser.close(); }
})().catch(e => { console.error(e); process.exitCode=1; });
