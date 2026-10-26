const { chromium } = require('playwright');
const assert = require('node:assert/strict');
const path = require('node:path');
let browser;
(async () => {
 const base = process.argv[2];
 const dir = process.env.XBOT_PIPE_SHOTS;
 browser = await chromium.launch({executablePath:'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',headless:true});
 const context = await browser.newContext({viewport:{width:1440,height:900},deviceScaleFactor:2});
 const page = await context.newPage();
 const errors=[];
 page.on('pageerror',e=>errors.push(e.message));
 await page.goto(base);
 await context.addInitScript(() => { window.cspViolations=[];addEventListener('securitypolicyviolation',e=>window.cspViolations.push(e.violatedDirective)); });
 const session = await page.evaluate(async()=> (await (await fetch('/test-session')).json()).session);
 await context.addInitScript(t => localStorage.setItem('xbot_account_session',t),session);
 const start=Date.now();
 await page.goto(base+'/app/');
 await page.locator('#shell').waitFor({state:'visible'});
 await page.locator('#input').waitFor({state:'visible'});
 await page.getByText('管道穿刺-紫色长颈鹿-7781',{exact:true}).first().waitFor();
 const boot=Date.now()-start;
 await page.locator('#input').fill('浏览器管道消息');
 await page.locator('#send').click();
 await page.getByText('管道测试回复',{exact:true}).first().waitFor();
 const timings=await page.evaluate(async()=>{
   const times=[];
   for(let i=0;i<20;i++) { const start=performance.now();const r=await fetch('/api/bots');if(!r.ok)throw new Error('bots');await r.json();times.push(performance.now()-start); }
   return times.sort((a,b)=>a-b);
 });
 if(dir) {
   require('node:fs').mkdirSync(dir,{recursive:true});
   await page.waitForTimeout(350);
   await page.screenshot({path:path.join(dir,'desktop-light.png')});
   await page.evaluate(()=>{localStorage.setItem('xbot-theme','dark');document.documentElement.setAttribute('data-theme','dark');});
   await page.waitForTimeout(350);
   await page.screenshot({path:path.join(dir,'desktop-dark.png')});
 }
 await context.close();
 const mobile=await browser.newContext({viewport:{width:390,height:844},deviceScaleFactor:2,isMobile:true,hasTouch:true});
 await mobile.addInitScript(t=>localStorage.setItem('xbot_account_session',t),session);
 const phone=await mobile.newPage();phone.on('pageerror',e=>errors.push(e.message));
 await phone.goto(base+'/app/');
 await phone.locator('#input').waitFor({state:'visible'});
 await phone.getByText('管道测试回复',{exact:true}).first().waitFor();
 if(dir) { await phone.waitForTimeout(350); await phone.screenshot({path:path.join(dir,'phone-light.png')}); await phone.evaluate(()=>document.documentElement.setAttribute('data-theme','dark')); await phone.waitForTimeout(350); await phone.screenshot({path:path.join(dir,'phone-dark.png')}); }
 const account=await phone.evaluate(async()=> (await(await fetch('/api/account/me')).json()).account);
 assert.equal(account.owner,true);
 const offline=await mobile.newPage();
 await offline.route('**/account/v1/devices',r=>r.fulfill({json:{devices:[]}}));
 await offline.goto(base+'/app/');
 await offline.getByText('没有在线设备，请先在电脑打开 XBot 并登录同一账号',{exact:true}).waitFor();
 await offline.getByRole('button',{name:'重试',exact:true}).waitFor();
 if(dir) { await offline.waitForTimeout(350); await offline.screenshot({path:path.join(dir,'offline-light.png')}); await offline.evaluate(()=>document.documentElement.setAttribute('data-theme','dark')); await offline.waitForTimeout(350); await offline.screenshot({path:path.join(dir,'offline-dark.png')}); }
 await offline.close();
 await phone.evaluate(()=>fetch('/api/account/logout',{method:'POST'}));
 await phone.waitForURL(base+'/#login');
 assert.equal(await phone.evaluate(()=>localStorage.getItem('xbot_account_session')),null);
 assert.deepEqual(errors,[]);
 assert.deepEqual(await phone.evaluate(()=>window.cspViolations||[]),[]);
 console.log(JSON.stringify({desktopBootMs:boot,apiSamples:timings.length,apiP95Ms:timings[18],phoneMainUI:true,scriptedMessage:true,offlineRetry:true,logout:true,pageErrors:errors.length}));
 await browser.close();
})().catch(async e=>{console.error(e);await browser?.close();process.exit(1)});
