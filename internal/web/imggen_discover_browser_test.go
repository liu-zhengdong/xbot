package web

import "testing"

func TestImggenDiscoverBrowser(t *testing.T) {
	runChatStreamBrowser(t, chatStreamFixture+imggenDiscoverFixture, imggenDiscoverDriver)
}

const imggenDiscoverFixture = `
const prompt='very small simple black and white cat, '.repeat(15)+'猫😀';
const generatedPart={type:'hosted',hosted:{id:'ig_1',name:'image_generation',status:'completed',query:prompt}};
fixture.messages['2号小伙子']=[{...message('assistant','图已存到工作区。'),parts:[generatedPart]}];
fixture.generatedCalls=[];fixture.canOpen=false;fixture.generatedFail=false;fixture.openFail=false;
fixture.generatedPath='/server/xbot/work/2号小伙子/generated';
Object.defineProperty(navigator,'clipboard',{value:{writeText:async text=>{fixture.copied=text}},configurable:true});
document.execCommand=cmd=>{if(cmd==='copy'){fixture.copied=document.activeElement.value;return true}return false};
const originalFetch=window.fetch;
window.fetch=async(path,opts={})=>{
 if(path==='/api/session')return json({authed:true,owner:'owner',owner_name:'政东',version:'dev',tip:'abc1234'});
 if(String(path).endsWith('/generated')){
  fixture.generatedCalls.push(path);
  if(fixture.generatedFail)return new Response(JSON.stringify({error:'读取失败'}),{status:500});
  return json({path:fixture.generatedPath,can_open:fixture.canOpen});
 }
 if(String(path).endsWith('/open-workdir?folder=generated')){
  fixture.opened=path;
  if(fixture.openFail)return new Response(JSON.stringify({error:'打开失败'}),{status:500});
  return json({status:'ok'});
 }
 return originalFetch(path,opts);
};
`

const imggenDiscoverDriver = `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const waitFor=async(fn,msg)=>{for(let i=0;i<600;i++){if(fn())return;await new Promise(r=>setTimeout(r,10))}throw Error(msg)};
const visible=n=>!!n&&n.getClientRects().length>0;
const button=(host,text)=>[...host.querySelectorAll('button')].find(b=>b.textContent===text);
const openGenerated=async()=>{button($('#thread .hs-step'),'查看 generated/').click();await waitFor(()=>visible($('#modal')),'generated modal missing')};
(async()=>{
 await waitFor(()=>$('#thread .hs-step')&&!$('#shell').hidden,'history not ready');
 const shot=new URLSearchParams(location.search).get('shot');
 if(shot){
  if(shot==='generated')await openGenerated();
  if(shot==='desktop'){fixture.canOpen=true;await openGenerated()}
  if(shot==='prompt')button($('#thread .hs-step'),'提示词').click();
  if(shot==='about')await XBotSettings.open('about');
  document.body.dataset.testResult='passed';return;
 }
 const step=$('#thread .hs-step');
 assert(step.textContent.length<100&&!step.textContent.includes(prompt),'full prompt exposed by default');
 assert(step.textContent.includes('已生图 · 存到工作区'),'saved status missing');
 button(step,'提示词').click();assert(step.querySelector('.hs-full-prompt')?.textContent===prompt,'full prompt cannot be opened');
 button(step,'提示词').click();assert(!step.querySelector('.hs-full-prompt'),'prompt cannot be closed');
 await openGenerated();
 assert($('#modal input').readOnly&&$('#modal input').value===fixture.generatedPath,'server path is not visible');
 assert(!button($('#modal'),'打开目录'),'browser has fake desktop action');
 $('#modal-ok').click();await waitFor(()=>$('#modal').hidden,'copy did not close');
 assert(fixture.copied===fixture.generatedPath,'wrong copied path');
 assert(fixture.generatedCalls[0].includes(encodeURIComponent('2号小伙子')),'wrong bot path');
 // The persistent Bot menu works without a new generation.
 $('[data-bot="2号小伙子"]').dispatchEvent(new MouseEvent('contextmenu',{bubbles:true,cancelable:true}));
 button($('.panel-more'),'查看生成的图片').click();await waitFor(()=>visible($('#modal')),'Bot menu entry failed');$('#modal-cancel').click();
 fixture.canOpen=true;await openGenerated();
 fixture.openFail=true;button($('#modal'),'打开目录').click();await waitFor(()=>visible($('#modal-error')),'desktop failure silent');
 fixture.openFail=false;button($('#modal'),'打开目录').click();await waitFor(()=>!button($('#modal'),'打开目录').disabled,'desktop call pending');
 assert(fixture.opened.endsWith('/open-workdir?folder=generated'),'desktop did not reuse endpoint');$('#modal-cancel').click();
 fixture.generatedFail=true;button($('#thread .hs-step'),'查看 generated/').click();await new Promise(r=>setTimeout(r,30));
 assert($('#modal').hidden,'failed lookup opened fake directory');fixture.generatedFail=false;
 await XBotSettings.open('about');
 assert([...document.querySelectorAll('[data-settings-version]')].some(n=>visible(n)&&n.textContent==='dev · abc1234'),'about loses build tip');
 $('#settings-copy-version').click();await waitFor(()=>fixture.copied==='dev · abc1234','copy loses tip');XBotSettings.close();
 // Running/failed events must never claim a completed save.
 const host=document.createElement('div');document.body.append(host);
 for(const status of ['in_progress','failed']){
  XBotProviders.progress(host,{hosted:{...generatedPart.hosted,status}},()=>{});
  assert(!host.textContent.includes('存到工作区'),'unsaved status claims saved');
 }
 XBotProviders.progress(host,{hosted:{...generatedPart.hosted,status:'completed',query:''}},()=>{});
 button(host,'提示词').click();assert(host.querySelector('.hs-full-prompt').textContent===prompt,'stream update lost full prompt');host.remove();
 assert(!$('#thread img[src*="generated/"]'),'generated image leaked into chat');
 assert(testErrors.length===0,testErrors.join(';'));
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`
