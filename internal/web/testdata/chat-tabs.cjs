// Independent acceptance: real serve APIs and shipped UI, no route stubs.
const {chromium}=require('playwright');
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const cfg=JSON.parse(fs.readFileSync(0,'utf8'));
(async()=>{
 const browser=await chromium.launch({executablePath:require('../../browsertest/chrome.cjs')(),headless:true});
 try{
 for(const theme of ['light','dark']){
  const context=await browser.newContext({viewport:{width:1440,height:900},deviceScaleFactor:2,colorScheme:theme});
  await context.addCookies([{name:cfg.cookie,value:cfg.token,url:cfg.url}]);
  await context.addInitScript(theme=>{localStorage.setItem('xbot-theme',theme);sessionStorage.setItem('xbot-selected-bot','Workspace')},theme);
  const p=await context.newPage();p.setDefaultTimeout(15000);
  const errors=[];p.on('pageerror',e=>errors.push(e.message));
  const requests=[];p.on('request',r=>requests.push(new URL(r.url()).pathname));
  const tab=name=>p.locator('#workspace-tabs').getByRole('tab',{name,exact:true});
  const shot=async name=>{if(!cfg.evidence)return;const dir=path.join(cfg.evidence,'after',theme);fs.mkdirSync(dir,{recursive:true});await p.screenshot({path:path.join(dir,name+'.png'),animations:'disabled'})};
  await p.goto(cfg.url);await p.locator('#thread .msg').last().waitFor();
  await p.locator('.thread-loading-cover').waitFor({state:'detached'});
  assert.equal(await tab('聊天').getAttribute('aria-selected'),'true');
  assert.equal(requests.filter(r=>r.endsWith('/transcript')).length,0,'chat opened raw trace');
  await shot('chat');
  await p.locator('#input').fill('真实入口切页后保留的草稿');
  await p.locator('#thread').evaluate(n=>{n.scrollTop=100});
  await p.waitForTimeout(100);
  const scroll=await p.locator('#thread').evaluate(n=>n.scrollTop);
  await p.locator('#thread .msg').last().evaluate(n=>window.acceptanceChatRow=n);
  await tab('记忆').click();
  await p.locator('.memory-preview').getByText(/Bot 记忆验收标记/).waitFor();
  assert.equal(await p.locator('.memory-document').count(),1,'person document is still shown');
  assert(!(await p.locator('.memory-view').innerText()).includes('用户记忆验收标记'));
  assert.equal(await p.locator('.memory-markdown').isVisible(),false,'whole memory starts expanded');
  await p.locator('.memory-expand').click();assert(await p.locator('.memory-markdown').isVisible());
  await p.locator('.memory-expand').click();
  const docBox=await p.locator('.memory-document').boundingBox(),expBox=await p.locator('.memory-experiences').boundingBox();
  assert(expBox.y>docBox.y+docBox.height&&Math.abs(expBox.width-docBox.width)<2,'not one column');
  assert.equal(await p.getByText('真实经历验收标记',{exact:true}).count(),0,'fixture did not leave old experience outside first page');
  await shot('memory');
  const search=p.getByRole('searchbox',{name:'搜索经历',exact:true});
  await search.fill('真实经历验收标记');
  await p.getByText('真实经历验收标记',{exact:true}).click();
  await p.locator('.memory-turn-text').filter({hasText:'来自真实存储的可展开提纲'}).waitFor();
  await shot('memory-old-search');
  await search.fill('tTs');
  await p.waitForFunction(()=>document.querySelector('.memory-experience-list').getAttribute('aria-busy')==='false'&&document.querySelectorAll('.memory-experience').length===40);
  assert.equal(await p.locator('.memory-summary mark').count(),40,'search match highlighting missing');
  await p.locator('.memory-experience summary').first().click();await shot('memory-search');
  await p.locator('.memory-earlier').click();
  await p.waitForFunction(()=>document.querySelectorAll('.memory-experience').length===45&&!document.querySelector('.memory-earlier'));
  await search.fill('查无此事');await p.getByText('没有找到相关经历',{exact:false}).waitFor();
  await shot('memory-search-empty');
  await p.getByRole('button',{name:'清除搜索',exact:true}).first().click();
  await p.getByText('最近工作进展 41',{exact:true}).waitFor();
  assert.equal(await search.inputValue(),'');
  await tab('轨迹').click();await p.locator('#panel-body').waitFor();
  await p.waitForFunction(()=>document.querySelector('#panel-body').innerText.trim().length>0);
  assert(requests.some(r=>r.endsWith('/transcript')),'trace did not read real ledger');await shot('trace');
  await tab('聊天').click();
  assert.equal(await p.locator('#input').innerText(),'真实入口切页后保留的草稿');
  assert(await p.evaluate(()=>window.acceptanceChatRow.isConnected),'chat was rebuilt');
  assert(Math.abs((await p.locator('#thread').evaluate(n=>n.scrollTop))-scroll)<2,'scroll changed: '+scroll+' -> '+await p.locator('#thread').evaluate(n=>n.scrollTop));
  await p.locator('#workspace-more').click();await shot('more');
  const option=p.locator('#workspace-more-menu').getByRole('button',{name:/收件箱/});
  await option.click();assert.equal(await tab('收件箱').count(),0);
  if(await p.locator('#workspace-more').getAttribute('aria-expanded')!=='true')await p.locator('#workspace-more').click();
  await option.click();await p.keyboard.press('Escape');
  assert.equal(await tab('收件箱').count(),1);
  // Send via actual HTTP/SSE, immediately read memory, then return to the reply.
  const reply='切页期间的实际回复 '+theme.toUpperCase();
  await p.locator('#input').fill('TOP_TABS_'+theme.toUpperCase());await p.locator('#send').click();
  await tab('记忆').click();await p.locator('.memory-preview').getByText(/Bot 记忆验收标记/).waitFor();
  try{await p.waitForFunction(()=>document.querySelector('#thread').textContent.includes('切页期间的实际回复 '+document.documentElement.dataset.theme.toUpperCase()))}
  catch(e){await shot('stream-failure');console.log(JSON.stringify(await p.evaluate(()=>({text:document.querySelector('#thread').textContent,input:document.querySelector('#input').innerText,errors:document.querySelector('#toast').textContent})),null,2));throw e}
  await tab('聊天').click();
  assert.equal(await p.locator('#thread').getByText(reply,{exact:true}).count(),1);
  await shot('chat-reply');
  const timings=[];
  for(let i=0;i<10;i++){
   const start=performance.now();await tab(i%2?'聊天':'记忆').click();
   await p.evaluate(()=>new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r))));
   timings.push(performance.now()-start);
  }
  timings.sort((a,b)=>a-b);
  console.log(JSON.stringify({theme,tabSwitchP95Ms:Math.round(timings[9]),samples:timings.length}));
  await p.getByRole('tab',{name:'轨迹',exact:true}).click();
  const traceRow=p.locator('button.trace-event-row[data-event-key]').first();
  await traceRow.waitFor();await traceRow.click();
   await p.getByRole('complementary',{name:'记录详情',exact:true}).waitFor();await shot('trace-detail');
   await p.getByRole('button',{name:'关闭详情',exact:true}).click();
  for(const name of ['任务','上下文','收件箱']){
   await p.getByRole('tab',{name,exact:true}).click();await shot(name);
  }
  await p.locator('#head-chip').click();await shot('profile');
  await p.getByRole('button',{name:'编辑资料',exact:true}).click();
  await p.locator('#modal').waitFor({state:'visible'});await shot('profile-edit');
  await p.locator('#modal-cancel').click();
  await p.getByRole('tab',{name:'聊天',exact:true}).click();
  await p.getByRole('tab',{name:'记忆',exact:true}).hover();await shot('hover');
  for(const platform of ['mac','win']){
   await p.evaluate(platform=>{
    document.documentElement.classList.remove('os-mac','os-win');
    document.documentElement.classList.add('in-app','os-'+platform);
    document.documentElement.style.setProperty('--mygo-titlebar-height','32px');
   },platform);
   await shot('shell-css-'+platform);
  }
  await p.evaluate(()=>{
   document.documentElement.classList.remove('in-app','os-mac','os-win');
   document.documentElement.style.removeProperty('--mygo-titlebar-height');
  });
  for(const width of [768,390]){
   await p.setViewportSize({width,height:900});
   await p.waitForFunction(()=>innerWidth>=768||document.querySelector('#sidebar').getBoundingClientRect().right<=1);
   await p.locator('#workspace-more').click();
   const menuBox=await p.locator('#workspace-more-menu').boundingBox();
   assert(menuBox.x>=0&&menuBox.x+menuBox.width<=width,'more menu outside viewport');
   await shot('more-'+width);await p.keyboard.press('Escape');
   for(const name of ['聊天','记忆','轨迹']){
    await tab(name).click();
    assert.equal(await p.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false,'page overflow '+width+' '+name);
    const bounds=await tab(name).boundingBox();assert(bounds.x>=-1&&bounds.x+bounds.width<=width+1,'active tab unreachable');
    await shot(name+'-'+width);
   }
  }
  assert.deepEqual(errors,[]);await context.close();
 }
 }finally{await browser.close()}
})().catch(e=>{console.error(e);process.exit(1)});
