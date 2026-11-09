// Exercise random avatars against an isolated real xbot serve. No model calls.
const {chromium}=require('playwright');
const assert=require('node:assert/strict');
const fs=require('node:fs'),path=require('node:path');
const url=process.argv[2];
(async()=>{
 const browser=await chromium.launch({executablePath:require('../../browsertest/chrome.cjs')(),headless:true});
 try{
  const context=await browser.newContext({viewport:{width:1440,height:900},deviceScaleFactor:2});
  const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));
  const profile=async()=>{const r=await context.request.get(url+'/api/bots/Avatar');assert.equal(r.status(),200);return r.json()};
  await page.goto(url);
  await page.locator('#shell').waitFor({state:'visible'});
  await page.locator('[data-bot="Avatar"]').first().click();
  await page.waitForFunction(()=>document.querySelector('#title')?.textContent.includes('Avatar')&&document.querySelector('#title svg image'));
  const signature=selector=>page.locator(selector).first().evaluate(node=>JSON.stringify([...node.querySelectorAll('image')].map(image=>image.getAttribute('href'))));
  const open=async()=>{
   await page.locator('#head-chip').click();await page.locator('#panel-actions button').first().click();
   await page.locator('#modal-fields button[aria-label="换一个"]').waitFor({state:'visible'});
  };
  await open();
  const before=await profile(),oldFace=await signature('#title .avatar');
  const patch=page.waitForResponse(r=>r.url()===url+'/api/bots/Avatar'&&r.request().method()==='PATCH');
  await page.locator('#modal-fields button[aria-label="换一个"]').click();assert.equal((await patch).status(),200);
  await page.waitForFunction(old=>JSON.stringify([...document.querySelectorAll('#title .avatar image')].map(n=>n.getAttribute('href')))!==old,oldFace);
  const saved=await profile();assert.notEqual(saved.avatar_seed,before.avatar_seed);
  const face=await signature('#title .avatar');assert.equal(await signature('#modal-fields .avatar'),face);
  const decode=()=>page.evaluate(async()=>{await Promise.all([...new Set([...document.querySelectorAll('.avatar image')].map(n=>n.getAttribute('href')))].map(async src=>{const image=new Image();image.src=src;await image.decode()}))});
  await decode();
  await page.waitForFunction(()=>!document.querySelector('#modal-fields button[aria-label="换一个"]').disabled);
  assert(await page.locator('#modal-fields .avatar').first().evaluate(node=>{const box=node.getBoundingClientRect();for(let parent=node.parentElement;parent;parent=parent.parentElement){if(/auto|scroll|hidden/.test(getComputedStyle(parent).overflowY)){const clip=parent.getBoundingClientRect();if(box.top<clip.top||box.bottom>clip.bottom)return false}}return true}),'saved avatar is clipped by the profile scroller');
  const evidence=process.env.XBOT_AVATAR_EVIDENCE;
  const capture=async name=>{if(evidence){fs.mkdirSync(evidence,{recursive:true});await page.screenshot({path:path.join(evidence,name+'.png')})}};
  await capture('profile-light-1440');
  await page.evaluate(()=>document.documentElement.dataset.theme='dark');await capture('profile-dark-1440');
  await page.evaluate(()=>document.documentElement.dataset.theme='light');await page.setViewportSize({width:390,height:844});await capture('profile-light-390');
  const png=fs.readFileSync(path.join(__dirname,'../static/icon-192.png'));
  await page.locator('#modal-fields input[type="file"]').setInputFiles({name:'uploaded.png',mimeType:'image/png',buffer:png});
  await page.locator('#modal-fields button[aria-label="清除上传的图，用回生成的"]').waitFor({state:'visible'});
  assert.equal((await profile()).avatar_seed,saved.avatar_seed);
  await page.locator('#modal-fields button[aria-label="清除上传的图，用回生成的"]').click();
  await page.waitForFunction(expected=>JSON.stringify([...document.querySelectorAll('#modal-fields .avatar image')].map(n=>n.getAttribute('href')))===expected,face);
  await page.reload();
  await page.waitForFunction(()=>document.querySelector('#title svg image'));
  assert.equal(await signature('#title .avatar'),face);assert.equal((await profile()).avatar_seed,saved.avatar_seed);
  await decode();
  assert.deepEqual(errors,[]);
  console.log('PASS: real random save, upload/clear, reload and PNG loading');
 }finally{await browser.close()}
})().catch(e=>{console.error(e);process.exit(1)});
