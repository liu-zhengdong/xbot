// Real binary + real HTTP + shipped page. No fetch overrides or request routes.
const {chromium}=require('playwright');
const fs=require('node:fs');
const path=require('node:path');
const assert=require('node:assert/strict');
const cfg=JSON.parse(fs.readFileSync(process.argv[2],'utf8'));
const p95=xs=>xs.sort((a,b)=>a-b)[Math.ceil(xs.length*.95)-1];
const shot=async(p,theme,name)=>{const dir=path.join(cfg.shots,theme);fs.mkdirSync(dir,{recursive:true});await p.screenshot({path:path.join(dir,name+'.png')})};
(async()=>{
 const browser=await chromium.launch({executablePath:cfg.chrome,headless:true});
 try {
  for(const theme of ['light','dark']) {
   const context=await browser.newContext({viewport:{width:1440,height:1000},deviceScaleFactor:1,colorScheme:theme,serviceWorkers:'block'});
   await context.addCookies(cfg.cookies.map(c=>({name:c.Name,value:c.Value,url:cfg.url})));
   const p=await context.newPage(),errors=[],calls=[];
   p.on('pageerror',e=>errors.push(e.message));
   p.on('request',r=>{if(r.url().startsWith(cfg.url+'/api/providers'))calls.push({method:r.method(),path:new URL(r.url()).pathname})});
   await p.addInitScript(theme=>{localStorage.setItem('xbot-theme',theme);localStorage.setItem('xbot-panel-open','true')},theme);
   const api=async(method,route,data)=>{const res=await context.request.fetch(cfg.url+route,{method,data});assert.ok(res.ok(),method+' '+route+' HTTP '+res.status());return res.json()};
   try {
    await p.goto(cfg.url+'/#login-code');
    await p.locator('#shell').waitFor({state:'visible'});
    const openSettings=async()=>{await p.locator('#account').click();await p.locator('#account-settings').click();await p.locator('[data-set="providers"]').click()};
    await openSettings();
    const surface=p.locator('#settings-providers');
    await surface.locator('.provider-add').waitFor();
    const initialCards=await surface.locator('.provider-grid').last().locator('.provider-card:not(.provider-add)').count();
    await surface.locator('.provider-add').scrollIntoViewIfNeeded();await shot(p,theme,'01-grid');
    await surface.locator('.provider-add').click();
    const form=surface.locator('form.provider-custom'),base=form.locator('[name="base_url"]'),key=form.locator('[name="key"]'),title=form.locator('[name="display_name"]'),save=form.locator('button[type="submit"]');
    await form.waitFor();
    assert.equal(await form.locator('input[type="radio"]:visible').count(),0);
    await shot(p,theme,'02-form');
    const countBeforeInvalid=calls.filter(c=>c.method==='POST'&&c.path==='/api/providers/custom').length;
    await base.fill('invalid URL');await save.click();
    await base.evaluate(e=>{if(e.getAttribute('aria-invalid')!=='true')throw Error('bad URL not marked invalid')});
    await key.evaluate(e=>{if(e.getAttribute('aria-invalid')!=='true')throw Error('missing key not marked invalid')});
    assert.equal(calls.filter(c=>c.method==='POST'&&c.path==='/api/providers/custom').length,countBeforeInvalid);
    await shot(p,theme,'03-validation');
    // Losing focus calls the real infer API; no actual OpenAI request occurs.
    const infer=p.waitForResponse(r=>new URL(r.url()).pathname==='/api/providers/infer'&&r.url().includes('api.openai.com'));
    await base.fill('https://api.openai.com/v1');await title.click();await infer;
    await surface.locator('.provider-custom-protocol .provider-badge').filter({hasText:'Responses'}).waitFor();
    const catalogBeforeSave=calls.filter(c=>c.path==='/api/providers/models').length;
    assert.equal(catalogBeforeSave,0);
    const inferLocal=p.waitForResponse(r=>new URL(r.url()).pathname==='/api/providers/infer'&&r.url().includes('127.0.0.1'));
    await base.fill(cfg.base);await key.fill(cfg.key);await title.fill('本机模型站 '+theme);await inferLocal;
    await surface.locator('.provider-custom-protocol .provider-badge').filter({hasText:'Completions'}).waitFor();
    assert.ok((await form.locator('.provider-custom-note').allTextContents()).some(s=>s.includes('https')));
    const saved=p.waitForResponse(r=>r.request().method()==='POST'&&new URL(r.url()).pathname==='/api/providers/custom');
    await save.click();const savedResponse=await saved;assert.ok(savedResponse.ok());const {provider:name}=await savedResponse.json();assert.ok(name);
    await form.getByRole('checkbox',{name:'runtime-custom-browser',exact:true}).waitFor();
    // The saved provider is the true registry entry read by the grid/picker.
    let data=await api('GET','/api/providers');let row=data.providers.find(x=>x.name===name);
    assert.equal(row.base_url,cfg.base);assert.equal(row.key_configured,true);assert.equal(row.custom,true);
    assert.ok(calls.findIndex(c=>c.path==='/api/providers/models')>calls.findIndex(c=>c.method==='POST'&&c.path==='/api/providers/custom'));
    const checked=p.waitForResponse(r=>r.request().method()==='PUT'&&new URL(r.url()).pathname.endsWith('/models'));
    await form.getByRole('checkbox',{name:'runtime-custom-browser',exact:true}).check();await checked;
    const probed=p.waitForResponse(r=>r.request().method()==='PUT'&&new URL(r.url()).pathname.endsWith('/config'));
    await form.locator('.provider-custom-probe').click();assert.ok((await probed).ok());
    await form.locator('.provider-custom-probe[aria-label="测试通过，再次测试"]').waitFor();
    const keyGeometry=await form.locator('.provider-custom-keyline').evaluate(e=>{const r=e.getBoundingClientRect(),input=e.querySelector('input').getBoundingClientRect(),b=e.querySelector('button'),br=b.getBoundingClientRect();return {sameRow:Math.abs(input.y-br.y)<2,rightGap:r.right-br.right,hit:b.contains(document.elementFromPoint(br.x+br.width/2,br.y+br.height/2))}});
    assert.ok(keyGeometry.sameRow&&keyGeometry.hit&&keyGeometry.rightGap<2);
    await shot(p,theme,'04-saved-detail');
    await surface.locator('.provider-back').click();
    let card=surface.locator('.provider-card').filter({has:p.locator('strong',{hasText:'本机模型站 '+theme})});
    await card.waitFor();await card.scrollIntoViewIfNeeded();
    assert.equal(await surface.locator('.provider-grid').last().locator('.provider-card:not(.provider-add)').count(),initialCards+1);
    await card.evaluate(e=>e.blur());
    const geometry=await card.evaluate(e=>{
     const cs=getComputedStyle(e),r=e.getBoundingClientRect(),dot=e.querySelector('.sdot').getBoundingClientRect();
     const sample=document.createElement('i');sample.style.background='var(--x-fill-2)';document.body.append(sample);const fill=getComputedStyle(sample).backgroundColor;sample.remove();
     const center=document.elementFromPoint(r.x+r.width/2,r.y+r.height/2);
     return {selected:e.getAttribute('aria-current'),background:cs.backgroundColor,fill,outline:cs.outlineStyle,border:cs.borderColor,rightGap:r.right-dot.right,hit:e.contains(center),width:r.width};
    });
    assert.equal(geometry.selected,'true');assert.equal(geometry.background,geometry.fill);assert.ok(geometry.outline==='none'||geometry.outline==='');assert.ok(geometry.hit);assert.ok(geometry.rightGap<30&&geometry.rightGap>0);assert.ok(geometry.width>100);
    await shot(p,theme,'05-selected-grid');
    // Same-host registration is independent even with no display-name override.
    // Registration itself performs no network request; leave this detail closed.
    const known=await api('POST','/api/providers/custom',{base_url:'https://api.openai.com/v1',key:cfg.key});
    await p.locator('#settings-close').click();await openSettings();
    const openaiCards=surface.locator('.provider-section').last().locator('.provider-card').filter({has:p.locator('strong',{hasText:/^OpenAI$/})});
    await openaiCards.nth(1).waitFor();assert.equal(await openaiCards.count(),2);
    await api('DELETE','/api/providers/custom/'+known.provider);
    await surface.locator('.provider-card').filter({has:p.locator('strong',{hasText:'本机模型站 '+theme})}).click();
    await form.getByRole('checkbox',{name:'runtime-custom-browser',exact:true}).waitFor();
    const edited='修改后的模型站 '+theme,editedBase=cfg.base.replace(/\/v1$/,'/edited');
    await title.fill(edited);
    await form.getByRole('button',{name:'更改',exact:true}).click();assert.equal(await form.locator('input[type=radio]:visible').count(),3);
    await form.locator('input[type=radio][value=responses]').check();
    await base.fill(editedBase);
    const updated=p.waitForResponse(r=>r.request().method()==='PUT'&&new URL(r.url()).pathname.endsWith('/config'));
    await save.click();assert.ok((await updated).ok());
    await form.getByRole('checkbox',{name:'runtime-custom-browser',exact:true}).waitFor();
    data=await api('GET','/api/providers');row=data.providers.find(x=>x.name===name);assert.equal(row.base_url,editedBase);assert.equal(row.display_name,edited);assert.equal(row.protocol,'responses');
    await p.locator('#settings-close').click();
    await p.locator('#roster [data-bot="custom-browser"]').click();
    await p.locator('.binding-trigger:visible').first().click();
    const dialog=p.locator('dialog.binding-dialog');await dialog.waitFor();
    await dialog.locator('.binding-section').filter({has:p.locator('.binding-heading',{hasText:edited})}).getByRole('radio').filter({hasText:'runtime-custom-browser'}).click();
    await dialog.getByRole('button',{name:'使用此模型',exact:true}).click();await dialog.waitFor({state:'detached'});
    const bound=await api('GET','/api/bots/custom-browser');assert.equal(bound.provider,name);assert.equal(bound.model,'runtime-custom-browser');
    await shot(p,theme,'06-bound');
    const samples=[];
    for(let i=0;i<25;i++){const start=performance.now();await api('GET','/api/providers/models?provider='+encodeURIComponent(name));samples.push(performance.now()-start)}
    await openSettings();await surface.locator('.provider-card').filter({has:p.locator('strong',{hasText:edited})}).click();
    // An endpoint without /models can still use an explicitly registered id.
    const manualBase=cfg.base.replace(/\/v1$/,'/manual');await base.fill(manualBase);
    const noCatalogSaved=p.waitForResponse(r=>r.request().method()==='PUT'&&new URL(r.url()).pathname.endsWith('/config'));
    await save.click();assert.ok((await noCatalogSaved).ok());
    await form.getByRole('button',{name:'手动添加模型 id',exact:true}).click();
    await form.getByRole('textbox',{name:'手动模型 id',exact:true}).fill('manual-custom-browser');
    const manualAdded=p.waitForResponse(r=>r.request().method()==='PUT'&&new URL(r.url()).pathname.endsWith('/models'));
    await form.locator('.provider-custom-manual').getByRole('button',{name:'添加',exact:true}).click();assert.ok((await manualAdded).ok());
    await p.locator('#settings-close').click();await p.locator('.binding-trigger:visible').first().click();
    const manualDialog=p.locator('dialog.binding-dialog');await manualDialog.waitFor();
    const manualSection=manualDialog.locator('.binding-section').filter({has:p.locator('.binding-heading',{hasText:edited})});
    await manualSection.getByRole('radio').filter({hasText:'manual-custom-browser'}).click();
    await shot(p,theme,'07-manual-catalog-failure');
    await manualDialog.getByRole('button',{name:'使用此模型',exact:true}).click();await manualDialog.waitFor({state:'detached'});
    const manualBound=await api('GET','/api/bots/custom-browser');assert.equal(manualBound.provider,name);assert.equal(manualBound.model,'manual-custom-browser');
    await openSettings();await surface.locator('.provider-card').filter({has:p.locator('strong',{hasText:edited})}).click();
    await form.locator('.provider-custom-delete').getByRole('button',{name:'删除此供应商',exact:true}).click();
    const deleted=p.waitForResponse(r=>r.request().method()==='DELETE'&&new URL(r.url()).pathname.endsWith('/'+name));
    await form.locator('.provider-custom-delete').getByRole('button',{name:'删除',exact:true}).click();assert.ok((await deleted).ok());
    await surface.locator('.provider-add').waitFor();
    data=await api('GET','/api/providers');assert.ok(!data.providers.some(x=>x.name===name));
    assert.equal(await surface.locator('.provider-grid').last().locator('.provider-card:not(.provider-add)').count(),initialCards);
    assert.deepEqual(errors,[]);
    console.log(JSON.stringify({theme,catalogP95Ms:p95(samples),catalogSamples:samples.length,selectedGeometry:geometry,keyGeometry}));
   } catch(e) {await shot(p,theme,'failure');throw e}
   finally {await context.close()}
  }
  console.log('passed');
 } finally {await browser.close()}
})().catch(e=>{console.error(e.stack);process.exitCode=1});
