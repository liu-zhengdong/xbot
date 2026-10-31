// Run after XBOT_CHAT_EVIDENCE=<dir> go test ./internal/web -run '^TestChatImageViewerBrowser$':
// node internal/web/testdata/chat-image-viewer.cjs <dir>/fixture [screenshot-dir] [before]
// Uses trusted Chrome mouse, keyboard and CDP touch input; no real provider.
const {chromium}=require('playwright');
const {pathToFileURL}=require('node:url');
const path=require('node:path');
const fs=require('node:fs');
const assert=require('node:assert/strict');
const [fixture,shots,baseline]=process.argv.slice(2);
const url=pathToFileURL(path.join(path.resolve(fixture),'index.html')).href;
const img='#lightbox img';
const visible='#lightbox:not([hidden])';
const matrix=p=>p.locator(img).evaluate(e=>{const m=new DOMMatrix(getComputedStyle(e).transform);return {scale:m.a,x:m.e,y:m.f}});
const current=async(p,n)=>assert.ok((await p.locator(img).getAttribute('data-delivery-url')).endsWith('/'+String(n).repeat(32)+'.png'));
const loaded=p=>p.waitForFunction(()=>{const n=document.querySelector('#lightbox img');return n?.naturalWidth&&!n.hidden});
const screenshot=async(p,theme,name)=>{if(shots){const dir=path.join(shots,theme);fs.mkdirSync(dir,{recursive:true});await p.screenshot({path:path.join(dir,name+'.png')})}};
(async()=>{
 const browser=await chromium.launch({channel:'chrome',headless:true});
 try {
  for(const theme of ['light','dark']) {
   const context=await browser.newContext({viewport:{width:1440,height:900},deviceScaleFactor:2,colorScheme:theme});
   const p=await context.newPage();
   await p.goto(url+'?manual=1&theme='+theme);
   await p.waitForFunction(()=>document.body.dataset.testResult==='ready');
   await p.locator('.thread-loading-cover').waitFor({state:'detached'});
   const thumbs=p.locator('#thread .gallery .thumb');
   if(!baseline){
    for(const width of [1440,800,390]){
     await p.setViewportSize({width,height:900});
     const sizes=await p.evaluate(()=>{
      const cells=[...document.querySelectorAll('#thread .gallery .thumb')];
      const single=cells[4].getBoundingClientRect();
      const column=document.querySelector('#thread .thread-inner').getBoundingClientRect().width;
      return {column,single:single.width,height:single.height,multi:cells.slice(0,4).map(c=>({width:c.getBoundingClientRect().width,top:c.getBoundingClientRect().top}))};
     });
     assert.ok(Math.abs(sizes.single-sizes.column/3)<2,JSON.stringify(sizes));
     assert.ok(Math.abs(sizes.single/sizes.height-1.6)<.02);
     assert.ok(sizes.multi.every(c=>c.width===84));
     if(width===390)assert.ok(sizes.multi.at(-1).top>sizes.multi[0].top,'multi gallery did not wrap');
     console.log(JSON.stringify({theme,viewport:width,...sizes}));
    }
    await p.setViewportSize({width:1440,height:900});
    const singleImage=thumbs.nth(4).locator('img');
    const source=await singleImage.getAttribute('src');
    await singleImage.evaluate(e=>{e.src='data:image/svg+xml,'+encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg" width="100" height="1000"/>')});
    await p.waitForFunction(()=>document.querySelectorAll('#thread .gallery .thumb img')[4].naturalHeight===1000);
    assert.ok((await singleImage.boundingBox()).height<=320,'portrait exceeds height cap');
    assert.equal(await singleImage.evaluate(e=>getComputedStyle(e).objectFit),'contain');
    await singleImage.evaluate((e,src)=>{e.src=src},source);
    await p.waitForFunction(()=>document.querySelectorAll('#thread .gallery .thumb img')[4].naturalWidth===1600);
   }
   await screenshot(p,theme,'chat');
   await thumbs.nth(4).hover();await screenshot(p,theme,'single-hover');
   await thumbs.nth(1).click();await loaded(p);
   await screenshot(p,theme,'opened');
   if(baseline){await context.close();continue}
   await current(p,2);
   assert.equal(await p.locator(img).count(),1);
   await p.getByRole('button',{name:'下一张',exact:true}).hover();
   await screenshot(p,theme,'next-hover');
   await p.getByRole('button',{name:'下一张',exact:true}).click();await loaded(p);await current(p,3);
   await p.keyboard.press('ArrowRight');await loaded(p);await current(p,4);
   await p.keyboard.press('ArrowRight');await current(p,4);
   await p.keyboard.press('ArrowLeft');await loaded(p);await current(p,3);
   await p.locator(img).dblclick();assert.ok((await matrix(p)).scale>1);
   console.log(JSON.stringify({theme,gesture:'trusted mouse double-click',...(await matrix(p))}));
   const r=await p.locator(img).boundingBox();
   const center={x:r.x+r.width/2,y:r.y+r.height/2};
   await p.mouse.move(center.x,center.y);await p.mouse.down();await p.mouse.move(center.x+120,center.y+90,{steps:12});await p.mouse.up();
   const moved=await matrix(p);assert.ok(Math.abs(moved.x)>50&&Math.abs(moved.y)>40);
   await current(p,3);assert.ok(await p.locator(visible).count());
   await screenshot(p,theme,'zoom-drag');
   await p.keyboard.press('ArrowLeft');await loaded(p);assert.equal((await matrix(p)).scale,1);
   const fit=await p.locator(img).boundingBox();await p.mouse.move(fit.x+fit.width/2,fit.y+fit.height/2);await p.mouse.wheel(0,-250);
   await p.waitForFunction(()=>new DOMMatrix(getComputedStyle(document.querySelector('#lightbox img')).transform).a>1);
   await p.setViewportSize({width:1000,height:700});await p.waitForFunction(()=>new DOMMatrix(getComputedStyle(document.querySelector('#lightbox img')).transform).a===1);
   await p.setViewportSize({width:1440,height:900});
   for(let i=0;i<8;i++){await p.keyboard.press(i%2?'Shift+Tab':'Tab');assert.ok(await p.locator('#lightbox').evaluate(e=>e.contains(document.activeElement)))}
   const scroll=await p.locator('#thread').evaluate(e=>e.scrollTop);
   await p.keyboard.press('Escape');assert.equal(await p.locator(visible).count(),0);
   assert.ok(await thumbs.nth(1).evaluate(e=>e===document.activeElement));
   // Trusted click on the picture must keep it open, blank padding must close.
   await thumbs.nth(0).click();await loaded(p);await p.locator(img).click();assert.ok(await p.locator(visible).count());
   await p.mouse.click(5,5);assert.equal(await p.locator(visible).count(),0);
   await thumbs.nth(4).click();await loaded(p);await current(p,5);
   assert.ok(await p.locator('.image-count').isHidden());
   await p.keyboard.press('ArrowRight');await current(p,5);
   await screenshot(p,theme,'single');
   await p.getByRole('button',{name:'关闭',exact:true}).click();
   await thumbs.nth(5).click();await p.getByText('图片暂时无法显示',{exact:true}).waitFor();
   await screenshot(p,theme,'failed');await p.keyboard.press('Escape');
   // Measure DOM click to image load + two rendered frames on a warm local fixture.
   const times=await p.evaluate(async()=>{
    const out=[];const thumb=document.querySelector('#thread .gallery .thumb');
    const frame=()=>new Promise(requestAnimationFrame);
    for(let i=0;i<30;i++){
     const start=performance.now();thumb.click();const image=document.querySelector('#lightbox img');
     if(!image.complete)await new Promise(resolve=>image.addEventListener('load',resolve,{once:true}));
     await frame();await frame();out.push(performance.now()-start);
     document.querySelector('#lightbox [aria-label="关闭"]').click();
    }
    return out.sort((a,b)=>a-b);
   });
   console.log(JSON.stringify({theme,openPaintSamples:times.length,openPaintP95Ms:times[Math.ceil(times.length*.95)-1],maxMs:times.at(-1),scrollBeforeResizeClose:scroll}));
   assert.deepEqual(await p.evaluate(()=>window.testErrors),[]);
   await context.close();
  }
  if(!baseline){
   const context=await browser.newContext({viewport:{width:390,height:844},deviceScaleFactor:2,isMobile:true,hasTouch:true,reducedMotion:'reduce'});
   const p=await context.newPage();await p.goto(url+'?manual=1#login-code');await p.waitForFunction(()=>document.body.dataset.testResult==='ready');
   const thumbs=p.locator('#thread .gallery .thumb');await thumbs.nth(1).tap();await loaded(p);await current(p,2);
   const cdp=await context.newCDPSession(p);
   const touch=(type,points)=>cdp.send('Input.dispatchTouchEvent',{type,touchPoints:points.map(([id,x,y])=>({id,x,y,radiusX:2,radiusY:2,force:1}))});
   const swipe=async(from,to)=>{await touch('touchStart',[[1,from,400]]);for(let i=1;i<=10;i++)await touch('touchMove',[[1,from+(to-from)*i/10,400]]);await touch('touchEnd',[])};
   await swipe(300,80);await loaded(p);await current(p,3);
   await swipe(300,80);await loaded(p);await current(p,4);
   await swipe(300,80);await current(p,4);
   await swipe(80,300);await loaded(p);await current(p,3);
   await swipe(80,300);await loaded(p);await current(p,2);
   await swipe(80,300);await loaded(p);await current(p,1);
   await swipe(80,300);await current(p,1);
   // Pinch, then drag while zoomed; neither may change message/index or dismiss.
   await touch('touchStart',[[1,155,400],[2,235,400]]);
   for(let i=1;i<=8;i++)await touch('touchMove',[[1,155-i*10,400],[2,235+i*10,400]]);
   await touch('touchEnd',[]);
   assert.ok((await matrix(p)).scale>2);await current(p,1);
   await swipe(260,100);await current(p,1);assert.ok(Math.abs((await matrix(p)).x)>50);
   console.log(JSON.stringify({gesture:'trusted touch pinch and drag',...(await matrix(p))}));
   await screenshot(p,'mobile','pinch-drag');
   await p.getByRole('button',{name:'下一张',exact:true}).tap();await loaded(p);await current(p,2);assert.equal((await matrix(p)).scale,1);
   await p.touchscreen.tap(195,400);await p.touchscreen.tap(195,400);
   assert.ok((await matrix(p)).scale>1);
   console.log(JSON.stringify({gesture:'trusted touch double-tap',...(await matrix(p))}));
   await screenshot(p,'mobile','double-tap');
   await p.getByRole('button',{name:'下一张',exact:true}).tap();await loaded(p);await current(p,3);assert.equal((await matrix(p)).scale,1);
   await touch('touchStart',[[1,240,400]]);await touch('touchMove',[[1,100,400]]);await touch('touchCancel',[]);await current(p,3);assert.ok(await p.locator(visible).count());
   await p.touchscreen.tap(5,5);assert.equal(await p.locator(visible).count(),0);
   await thumbs.nth(0).tap();await loaded(p);
   await p.getByRole('button',{name:'关闭',exact:true}).tap();assert.equal(await p.locator(visible).count(),0);
   assert.deepEqual(await p.evaluate(()=>window.testErrors),[]);
   await context.close();console.log('Trusted mouse / keyboard / touch / reduced-motion checks PASS');
  }
 }finally{await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1});
