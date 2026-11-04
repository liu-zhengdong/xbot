// Behavioral smoke against a running, isolated xbot serve. No model calls.
const {chromium}=require('playwright');
const assert=require('node:assert/strict');
const fs=require('node:fs'),path=require('node:path');
const url=process.argv[2];
(async()=>{
 const browser=await chromium.launch({executablePath:require('../../browsertest/chrome.cjs')(),headless:true});
 try {
  const context=await browser.newContext({viewport:{width:1440,height:900},deviceScaleFactor:2});
  const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.goto(url);await page.locator('#shell').waitFor({state:'visible'});
  const open=async()=>{await page.locator('#account').click();await page.locator('#account-settings').click();await page.locator('#settings-owner-upload').waitFor({state:'visible'})};
  await context.request.delete(url+'/api/owner/avatar');await page.reload();await page.locator('#shell').waitFor({state:'visible'});
  const me=page.locator('#me-avatar img');const original=await me.getAttribute('src');
  await open();
  const shots=process.env.OWNER_AVATAR_SHOTS;
  const capture=async(name)=>{if(shots){const dir=path.join(shots,'after',await page.locator('html').getAttribute('data-theme'));fs.mkdirSync(dir,{recursive:true});await page.screenshot({path:path.join(dir,name+'.png')})}};
  for(const theme of ['light','dark']){
   await page.locator(`.theme-option-${theme}`).click();
   assert.equal(await page.locator('html').getAttribute('data-theme'),theme);
   await capture('default');await page.locator('#settings-owner-upload').hover();await capture('hover');
  }
  const png=fs.readFileSync(path.join(__dirname,'../static/icon-192.png'));
  const times=[];
  for(let i=0;i<20;i++){
   const start=performance.now();const response=await context.request.get(url+'/api/session');assert.equal(response.status(),200);times.push(performance.now()-start);
  }
  const uploadStart=performance.now();
  await page.locator('#settings-owner-file').setInputFiles({name:'face.png',mimeType:'image/png',buffer:png});
  await page.waitForFunction(()=>document.querySelector('#me-avatar img')?.getAttribute('src').startsWith('/api/owner/avatar'));
  const uploadMs=performance.now()-uploadStart;
  assert.equal((await (await context.request.get(url+'/api/session')).json()).owner_avatar_url,'/api/owner/avatar');
  assert.deepEqual(await (await context.request.get(url+'/api/owner/avatar')).body(),png);
  for(const theme of ['light','dark']){await page.locator(`.theme-option-${theme}`).click();await capture('custom')}
  await page.reload();await page.locator('#shell').waitFor({state:'visible'});assert.equal(await me.getAttribute('src'),'/api/owner/avatar');
  await open();assert.equal(await page.locator('#settings-owner-preview img').getAttribute('src'),'/api/owner/avatar');
  // A reachable disk-write failure is tested in the scripted browser regression.
  await page.locator('#settings-owner-clear').click();
  await page.waitForFunction(src=>document.querySelector('#me-avatar img')?.getAttribute('src')===src,original);
  assert.equal((await (await context.request.get(url+'/api/session')).json()).owner_avatar_url,'');
  assert.equal((await context.request.get(url+'/api/owner/avatar')).status(),404);
  for(const theme of ['light','dark']){await page.locator(`.theme-option-${theme}`).click();await capture('cleared')}
  await page.reload();await page.locator('#shell').waitFor({state:'visible'});assert.equal(await me.getAttribute('src'),original);
  assert.deepEqual(errors,[]);
  times.sort((a,b)=>a-b);console.log(JSON.stringify({result:'PASS',sessionP95Ms:times[Math.ceil(times.length*.95)-1],uploadToMeMs:uploadMs}));
 }finally{await browser.close()}
})().catch(e=>{console.error(e);process.exit(1)});
