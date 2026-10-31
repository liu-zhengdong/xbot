const { chromium } = require('playwright');
const fs = require('node:fs');
const path = require('node:path');
const cfg = JSON.parse(fs.readFileSync(0, 'utf8'));
const assert = (ok, message) => { if (!ok) throw Error(message); };
(async () => {
  const browser = await chromium.launch({executablePath:'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',headless:true});
  try {
    for (const theme of ['light','dark']) {
      const context = await browser.newContext({viewport:{width:1440,height:900},deviceScaleFactor:2,colorScheme:theme});
      await context.addCookies([{name:cfg.cookie,value:cfg.token,url:cfg.url}]);
      await context.addInitScript(() => sessionStorage.setItem('xbot-selected-bot','History'));
      const page = await context.newPage();
      const errors=[]; page.on('pageerror',error=>errors.push(error.message));
      const requests=[]; page.on('request',req=>{if(new URL(req.url()).pathname.endsWith('/transcript')) requests.push(new URL(req.url()).search)});
      let releaseTail, holdTail = true;
      const tailReady = new Promise(resolve => { releaseTail = resolve; });
      await page.route('**/api/bots/History/transcript', async route => {
        if (!holdTail) { await route.continue(); return; }
        holdTail = false;
        const response = await route.fetch();
        await tailReady;
        await route.fulfill({response});
      });
      await page.goto(cfg.url);
      await page.locator('.chat-open-avatar .avatar').waitFor();
      assert(await page.locator('#thread .msg').count()===0,'waiting cover had real bubbles');
      const shot=async name=>{if(cfg.evidence){const dir=path.join(cfg.evidence,'after',theme);fs.mkdirSync(dir,{recursive:true});await page.screenshot({path:path.join(dir,name+'.png')});}};
      await page.waitForTimeout(320);
      await shot('open-mask-idle');
      releaseTail();
      const row = seq => page.locator('#thread .msg[data-seq="'+seq+'"]');
      await row(180).waitFor();
      await page.locator('.chat-open-mask').waitFor({state:'detached'});
      assert(await page.locator('#thread .msg').count()===60,'real API did not open exactly one tail window');
      assert(await row(1).count()===0,'real API loaded the full transcript');
      await shot('tail');
      await page.locator('#thread').evaluate(node=>{node.scrollTop=40});
      await page.locator('#history-older').click();
      await row(61).waitFor();
      await shot('earlier');
      // Real wheel input verifies the browser's own scroll event, separately
      // from the virtual-time fixture's explicitly dispatched event.
      await page.locator('#thread').evaluate(node=>{node.scrollTop=40});
      const rect=await page.locator('#thread').boundingBox();
      await page.mouse.move(rect.x+rect.width/2,rect.y+100);
      await page.mouse.wheel(0,-500);
      await row(1).waitFor();
      assert(requests.includes('?before=61'),'real wheel did not fetch the preceding window');
      await page.locator('#chat-search').click();
      await page.locator('#history-search').fill('needle');
      await page.locator('#history-form').evaluate(node=>node.requestSubmit());
      const hit=page.locator('#history-results button[data-seq="25"]');
      await hit.waitFor();
      assert((await hit.textContent()).includes('历史线索'),'search result preview did not come from real stored text');
      await shot('search');
      await hit.click();
      await page.locator('#thread .history-hit-target[data-seq="25"]').waitFor();
      assert(await page.locator('#history-search-panel').isHidden(),'hit did not close search');
      assert(await row(180).count()===0,'around did not replace tail window');
      await page.waitForTimeout(200);
      await shot('hit');
      await page.locator('#history-latest').click();
      await row(180).waitFor();
      for(const width of [640,390]) {
        await page.setViewportSize({width,height:900});
        await page.locator('#chat-search').click();
        const geometry=await page.evaluate(()=>{
          const pop=document.querySelector('#history-search-panel').getBoundingClientRect();
          const button=document.querySelector('#chat-search').getBoundingClientRect();
          const chat=document.querySelector('.chat').getBoundingClientRect();
          return {fits:pop.left>=chat.left-1&&pop.right<=chat.right+1,reachable:button.left>=0&&button.right<=innerWidth,overflow:document.querySelector('#thread').scrollWidth>document.querySelector('#thread').clientWidth};
        });
        assert(geometry.fits&&geometry.reachable&&!geometry.overflow,'narrow search/column overflow at '+width);
        await shot('search-'+width);
        await page.keyboard.press('Escape');
      }
      await page.setViewportSize({width:1440,height:900});
      await page.route('**/api/bots/History/transcript', route=>route.fulfill({status:500,contentType:'application/json',body:'{"error":"scripted loading failure"}'}));
      await page.reload();
      await page.locator('.chat-open-mask .thread-unavailable').waitFor();
      assert(await page.locator('.chat-open-avatar').count()===0,'failure kept the playful avatar');
      await shot('open-mask-fail');
      assert(errors.length===0,errors.join('; '));
      console.log(theme+': real HTTP tail / older chip / wheel / FTS hit / around / latest / 640 + 390 geometry passed');
      await context.close();
    }
  } finally { await browser.close(); }
})().catch(error=>{console.error(error);process.exitCode=1});
