// Real serve journey: scripted model only, no route interception or clipboard mock.
const {chromium}=require('playwright');
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const cfg=JSON.parse(fs.readFileSync(0,'utf8'));
(async()=>{
 const browser=await chromium.launch({executablePath:require('../../browsertest/chrome.cjs')(),headless:true});
 try{
  const context=await browser.newContext({viewport:{width:1440,height:900},deviceScaleFactor:2,permissions:['clipboard-read','clipboard-write']});
  await context.addCookies([{name:cfg.cookie,value:cfg.token,url:cfg.url}]);
  await context.addInitScript(()=>{localStorage.setItem('xbot-theme','light');sessionStorage.setItem('xbot-selected-bot','Trace')});
  const p=await context.newPage();p.setDefaultTimeout(15000);
  const errors=[],requests=[];let traceResponses=0;
  p.on('pageerror',e=>errors.push(e.message));
  p.on('request',r=>{const u=new URL(r.url());if(u.pathname.endsWith('/transcript'))requests.push({bot:decodeURIComponent(u.pathname.split('/')[3]),before:u.searchParams.get('before'),after:u.searchParams.get('after')})});
  p.on('response',r=>{if(new URL(r.url()).pathname==='/api/bots/Trace/transcript')traceResponses++});
  const explorer=p.locator('.trace-explorer'),rows=explorer.locator('button.trace-event-row[data-event-key][data-seq]:visible');
  const detail=p.getByRole('complementary',{name:'记录详情',exact:true});
  const selected=async n=>(await n.getAttribute('aria-pressed'))==='true'||(await n.getAttribute('aria-selected'))==='true';
  const tab=name=>p.locator('nav[aria-label="详情视图"]').getByRole('button',{name,exact:true});
  const show=async row=>{await row.click();await detail.waitFor({state:'visible'})};
  const close=async()=>{await p.getByRole('button',{name:'关闭详情',exact:true}).click();await detail.waitFor({state:'hidden'})};
  const shot=async name=>{if(!cfg.evidence)return;fs.mkdirSync(cfg.evidence,{recursive:true});await p.screenshot({path:path.join(cfg.evidence,name+'.png'),animations:'disabled'})};
  const search=p.locator('input[aria-label="搜索轨迹"]');
  await p.goto(cfg.url);await p.locator('#thread .msg').last().waitFor();await p.locator('.thread-loading-cover').waitFor({state:'detached'});
  assert.equal(requests.length,0,'chat read raw transcript');
  await p.getByRole('tab',{name:'轨迹',exact:true}).click();await rows.last().waitFor();
  assert(requests.some(r=>r.bot==='Trace'),'trace did not read real transcript');
  assert.equal(await rows.filter({hasText:'已存输入 001'}).count(),0,'trace loaded whole ledger');
  assert.equal(await explorer.locator('button.trace-event-row[data-seq="103"]:visible').count(),3,'model and each call need independent entry');
  // Filtering by stable public sequence avoids assuming an internal event-key format.
  const read=explorer.locator('button.trace-event-row[data-seq="103"]').filter({hasText:'read_file'});
  await show(read);await tab('预览').click();
  assert((await detail.innerText()).includes('LANTERN'));
  assert(!(await detail.innerText()).includes('COBALT'),'wrong result attached to call');
  await shot('desktop-light-selected');await p.locator('#workspace-theme').click();await shot('desktop-dark-selected');await p.locator('#workspace-theme').click();
  await tab('原始内容').click();assert((await detail.innerText()).includes('read-one'));
  await p.getByRole('button',{name:'复制原文',exact:true}).click();
  await p.waitForFunction(()=>document.querySelector('#toast')?.textContent.includes('已复制'));
  const copied=await p.evaluate(()=>navigator.clipboard.readText());
  assert(copied.includes('read-one')&&copied.includes('LANTERN'),'actual system clipboard omitted selected public records');
  await tab('来源').click();assert((await detail.innerText()).includes('104'));
  await tab('概述').click();await close();
  const key=await read.getAttribute('data-event-key');
  const overview=p.locator('[aria-label="轨迹总览"]');
  // Result marker names are accessible even when the visual mark is only a dot.
  const related=overview.locator('button[data-event-key="'+key+'"]');
  assert(await related.count()>=2,'overview omitted call/result relationship');
  await related.last().click();await detail.waitFor({state:'visible'});
  assert(await selected(read));await close();
  for(const mode of ['轮次','调用','序号']){
   await overview.getByRole('button',{name:mode,exact:true}).click();
   assert(await overview.locator('button[data-event-key]').count()>0,'overview mode lost records: '+mode);
  }
  await search.fill('lAnTeRn');await p.waitForFunction(()=>[...document.querySelectorAll('button.trace-event-row')].filter(n=>!n.hidden).length===1);
  assert.equal(await rows.first().getAttribute('data-event-key'),key,'tool result text did not find its call');
  await search.fill('AMBER');await p.waitForFunction(()=>[...document.querySelectorAll('button.trace-event-row')].filter(n=>!n.hidden).length===1);
  await show(rows.first());await tab('预览').click();assert((await detail.innerText()).includes('AMBER'));await close();
  await search.fill('真实阅读');await p.waitForFunction(()=>[...document.querySelectorAll('button.trace-event-row')].filter(n=>!n.hidden).length===1);
  await show(rows.first());await tab('预览').click();
  await p.evaluate(()=>{
   const d=document.querySelector('aside[aria-label="记录详情"]');
   const body=[...d.querySelectorAll('pre,p')].find(n=>n.textContent.includes('SELECTABLE'));if(!body)throw Error('reading body missing');
   let scroll=body;while(scroll&&!(/auto|scroll/.test(getComputedStyle(scroll).overflowY)&&scroll.scrollHeight>scroll.clientHeight+5))scroll=scroll.parentElement;
   if(!scroll)throw Error('detail lacks independent scrolling');scroll.scrollTop=120;
   window.traceAcceptance={detail:d,body,scroll,top:scroll.scrollTop};
  });
  await search.focus();const before=traceResponses;
  const response=await p.request.post(cfg.url+'/api/bots/Trace/messages',{data:{text:'SMOKE_HELLO_2'}});
  assert.equal(response.status(),200);assert((await response.text()).includes('event: done'),'scripted owner turn did not finish');
  await p.waitForFunction(()=>document.activeElement===document.querySelector('input[aria-label="搜索轨迹"]'));
  await p.waitForFunction(()=>window.traceAcceptance.detail.isConnected&&document.querySelector('input[aria-label="搜索轨迹"]').value==='真实阅读');
  // Observe two actual conditional/incremental polls while keeping the search active.
  const deadline=Date.now()+12000;while(traceResponses<before+2&&Date.now()<deadline)await p.waitForTimeout(100);
  assert(traceResponses>=before+2,'trace polling stopped');
  assert(await search.evaluate(n=>document.activeElement===n),'poll stole search focus');assert.equal(await search.inputValue(),'真实阅读');
  assert(await p.evaluate(()=>window.traceAcceptance.detail===document.querySelector('aside[aria-label="记录详情"]')),'poll replaced reading detail');
  assert(await selected(tab('预览')),'poll reset detail tab');
  assert(await p.evaluate(()=>Math.abs(window.traceAcceptance.scroll.scrollTop-window.traceAcceptance.top)<3),'poll moved detail scroll');
  // Select actual body text, then wait two more polls without driving the view.
  await p.evaluate(()=>{
   const body=window.traceAcceptance.body,walk=document.createTreeWalker(body,NodeFilter.SHOW_TEXT);let n;
   while(walk.nextNode())if(walk.currentNode.textContent.includes('SELECTABLE')){n=walk.currentNode;break}
   const start=n.textContent.indexOf('SELECTABLE'),range=document.createRange();range.setStart(n,start);range.setEnd(n,start+10);getSelection().removeAllRanges();getSelection().addRange(range);window.traceAcceptance.text=n;
  });
  const selectedAt=traceResponses;const selectionDeadline=Date.now()+12000;while(traceResponses<selectedAt+2&&Date.now()<selectionDeadline)await p.waitForTimeout(100);
  assert(traceResponses>=selectedAt+2,'selection polls missing');
  assert(await p.evaluate(()=>getSelection().toString()==='SELECTABLE'&&getSelection().anchorNode===window.traceAcceptance.text),'poll lost body selection');
  await p.evaluate(()=>getSelection().removeAllRanges());await close();await search.fill('');
  const anchor=explorer.locator('button.trace-event-row[data-seq="75"]');await anchor.scrollIntoViewIfNeeded();
  const earlier=explorer.getByRole('button',{name:'更早的记录',exact:true});await earlier.scrollIntoViewIfNeeded();
  const y=(await anchor.boundingBox()).y;
  await earlier.click();
  await explorer.locator('button.trace-event-row[data-seq="20"]').filter({hasText:'read_file'}).waitFor();
  assert(requests.some(r=>r.before),'history did not use real before API');
  assert(Math.abs((await anchor.boundingBox()).y-y)<4,'before prepend moved reading anchor');
  const outside=explorer.locator('button.trace-event-row[data-seq="20"]').filter({hasText:'read_file'});
  await show(outside);await tab('预览').click();assert((await detail.innerText()).includes('AMBER'));await close();
  assert.equal(await explorer.locator('button.trace-event-row[data-seq="108"]').count(),0,'orphan duplicated after joining earlier call');
  await p.setViewportSize({width:390,height:844});
  const input=explorer.locator('button.trace-event-row[data-seq="101"]');await input.scrollIntoViewIfNeeded();const listY=(await input.boundingBox()).y;
  await show(input);await tab('原始内容').click();await shot('narrow-selected');
  const bounds=await detail.boundingBox(),tabs=await p.locator('#workspace-tabs').boundingBox();
  assert(bounds.y>=tabs.y+tabs.height-1&&bounds.x>=-1&&bounds.x+bounds.width<=391,'narrow detail covered page tabs or overflowed viewport');
  await p.keyboard.press('Escape');await detail.waitFor({state:'hidden'});
  assert(Math.abs((await input.boundingBox()).y-listY)<4,'narrow Escape lost list position');
  await show(input);await close();assert.equal(await p.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false,'narrow page overflow');
  await p.setViewportSize({width:1440,height:900});
  await search.fill('ORCHID');await show(rows.first());
  await p.locator('#roster [data-bot="Other"]').click();await p.waitForFunction(()=>document.querySelector('#title').textContent.includes('Other'));
  await p.locator('.thread-loading-cover').waitFor({state:'detached'});await p.getByRole('tab',{name:'轨迹',exact:true}).click();
  await explorer.waitFor();await detail.waitFor({state:'hidden'});
  assert.equal(await search.inputValue(),'','trace query leaked to other Bot');
  assert(!(await explorer.innerText()).includes('LANTERN'),'other Bot got trace body');
  assert.deepEqual(errors,[]);console.log('PASS real binary/serve transcript, linked results, clipboard readback, polling reading state, before anchor, narrow Escape/close and Bot isolation');
  await context.close();
 }finally{await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1});
