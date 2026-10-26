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
 await context.addInitScript(t => {localStorage.setItem('xbot_account_session',t);localStorage.setItem('xbot_selected_device','mini-1');},session);
 const start=Date.now();
 await page.goto(base+'/app/');
 await page.locator('#shell').waitFor({state:'visible'});
 await page.locator('#input').waitFor({state:'visible'});
 await page.getByText('管道穿刺-紫色长颈鹿-7781',{exact:true}).first().waitFor();
 const boot=Date.now()-start;
 await page.locator('#input').fill('浏览器管道消息');
 await page.locator('#send').click();
 await page.getByText('管道测试回复',{exact:true}).first().waitFor();
 const media=await page.evaluate(()=>new Promise((resolve,reject)=>{
  const img=new Image();img.onload=()=>resolve({url:img.src,width:img.naturalWidth});img.onerror=()=>reject(new Error('owner image failed'));
  window.XBotPipe.image(img,'/api/test-image');
 }));
 assert.ok(media.url.startsWith('blob:'));assert.equal(media.width,1);
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

 assert.equal(await page.locator('meta[name=xbot-fixture]').getAttribute('content'),'dev-mini-1');
 assert.equal(await page.evaluate(()=>window.XBotPipe.tip),'dev-mini-1');
 // Native image requests, without the optional bridge image helper.
 assert.equal(await page.evaluate(()=>new Promise((resolve,reject)=>{const i=new Image();i.onload=()=>resolve(i.naturalWidth);i.onerror=reject;i.src='/api/test-image';})),1);
 assert.deepEqual(await page.evaluate(()=>cspViolations),[]);
 // Change only the device fixture, then hard reload: cloud assets are untouched.
 await page.request.post(base+'/test-advance');
 await page.reload();await page.locator('#input').waitFor({state:'visible'});
 assert.equal(await page.locator('meta[name=xbot-fixture]').getAttribute('content'),'dev-mini-2');
 // Remove init-script selection override before user-driven device switch.
 const switching = await browser.newContext({viewport:{width:1440,height:900},deviceScaleFactor:2});
 await switching.addInitScript(t=>{localStorage.setItem('xbot_account_session',t);},session);
 const switched=await switching.newPage();
 switched.on('pageerror',e=>errors.push(e.message));
 await switched.goto(base+'/#devices');
 await switched.evaluate(()=>localStorage.setItem('xbot_selected_device','mini-1'));
 await switched.goto(base+'/app/');await switched.locator('#input').waitFor({state:'visible'});
 assert.equal(await switched.locator('meta[name=xbot-fixture]').getAttribute('content'),'dev-mini-2');
 await switched.locator('#account').click();
 if(dir) { await switched.screenshot({path:path.join(dir,'device-menu-light.png')}); await switched.evaluate(()=>document.documentElement.setAttribute('data-theme','dark')); await switched.screenshot({path:path.join(dir,'device-menu-dark.png')}); }
 await switched.getByRole('combobox',{name:'选择设备'}).selectOption('laptop-2');
 await switched.waitForFunction(()=>document.querySelector('meta[name=xbot-fixture]')?.content==='dev-laptop-2' && !document.getElementById('shell').hidden);
 assert.equal(await switched.evaluate(async()=> (await(await fetch('/api/session')).json()).version),'dev-laptop-2');
 assert.deepEqual(await switched.evaluate(()=>caches.keys()),[]);
 await switched.locator('#account').click();await switched.locator('#account-about').click();
 await switched.getByText('dev-laptop-2',{exact:true}).last().waitFor();
 await switching.close();
 await context.close();
 const mobile=await browser.newContext({viewport:{width:390,height:844},deviceScaleFactor:2,isMobile:true,hasTouch:true});
 await mobile.addInitScript(t=>{localStorage.setItem('xbot_account_session',t);localStorage.setItem('xbot_selected_device','mini-1');},session);
 const phone=await mobile.newPage();phone.on('pageerror',e=>errors.push(e.message));
 await phone.goto(base+'/app/');
 await phone.locator('#input').waitFor({state:'visible'});
 await phone.getByText('管道测试回复',{exact:true}).first().waitFor();
 if(dir) { await phone.waitForTimeout(350); await phone.screenshot({path:path.join(dir,'phone-light.png')}); await phone.evaluate(()=>document.documentElement.setAttribute('data-theme','dark')); await phone.waitForTimeout(350); await phone.screenshot({path:path.join(dir,'phone-dark.png')}); }
 const account=await phone.evaluate(async()=> (await(await fetch('/api/account/me')).json()).account);
 assert.equal(account.owner,true);
 const offline=await mobile.newPage();
 await offline.route('**/account/v1/devices',r=>r.fulfill({json:{devices:[]}}));
 await offline.addInitScript(()=>localStorage.removeItem('xbot_selected_device'));
 await offline.goto(base+'/app/');
 await offline.getByText('还没有配对设备，请去电脑打开 XBot 并登录同一账号',{exact:true}).waitFor();
 await offline.getByRole('button',{name:'重试',exact:true}).waitFor();
 assert.equal(await offline.locator('#shell').count(),0);
 if(dir) { await offline.waitForTimeout(350); await offline.screenshot({path:path.join(dir,'offline-light.png')}); await offline.evaluate(()=>document.documentElement.setAttribute('data-theme','dark')); await offline.waitForTimeout(350); await offline.screenshot({path:path.join(dir,'offline-dark.png')}); }
 await offline.close();
 await phone.evaluate(()=>fetch('/api/account/logout',{method:'POST'}));
 await phone.waitForURL(base+'/#login');
 assert.equal(await phone.evaluate(()=>localStorage.getItem('xbot_account_session')),null);
 assert.deepEqual(errors,[]);
 assert.deepEqual(await phone.evaluate(()=>window.cspViolations||[]),[]);
 console.log(JSON.stringify({desktopBootMs:boot,apiSamples:timings.length,apiP95Ms:timings[18],singleSourceReload:true,deviceSwitch:true,noShellCache:true,nativeOwnerImage:true,phoneMainUI:true,scriptedMessage:true,ownerImage:true,offlineRetry:true,logout:true,pageErrors:errors.length}));
 await browser.close();
})().catch(async e=>{console.error(e);await browser?.close();process.exit(1)});
