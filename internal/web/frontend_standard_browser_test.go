package web

import "testing"

// Measure the shipped DOM. Existing tests own saving, search and navigation state.
func TestFrontendStandardGeometryBrowser(t *testing.T) {
	runChatFramesBrowser(t, transcriptWindowFixture+memoryPanelFixture+skillsTitleFixture, frontendStandardGeometryDriver)
}

func TestFrontendStandardWeakActionBrowser(t *testing.T) {
	runChatFramesBrowser(t, panelFixture, `
const $=s=>document.querySelector(s),assert=(v,m)=>{if(!v)throw Error(m)};
const until=async(f,m)=>{for(let i=0;i<900;i++){if(f())return;await new Promise(r=>setTimeout(r,10))}throw Error(m)};
(async()=>{
 await until(()=>!$('#shell').hidden&&document.querySelector('[data-bot="2号小伙子"]'),'chat boot');
 document.querySelector('[data-bot="2号小伙子"]').click();await until(()=>$('#title').textContent.includes('2号小伙子'),'Bot selection');
 $('#head-chip').click();await until(()=>$('.binding-trigger'),'model entry');
 $('.binding-trigger').click();await until(()=>$('.binding-dialog'),'model chooser');
 await Promise.all($('.binding-dialog').getAnimations().map(a=>a.finished));
 const manage=[...document.querySelectorAll('.binding-dialog button')].find(b=>b.textContent.includes('管理供应商'));
 assert(manage,'manage providers action missing');
 assert(Math.abs(manage.getBoundingClientRect().height-36)<1,'weak action lost standard height');
 assert(manage.scrollWidth<=manage.clientWidth,'weak action text is clipped');
 manage.click();await until(()=>!$('#setpage').hidden&&$('#settings-providers').checkVisibility(),'manage providers action did not open settings');
 assert(testErrors.length===0,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`)
}

const frontendStandardGeometryDriver = `
const $=s=>document.querySelector(s),assert=(v,m)=>{if(!v)throw Error(m)};
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const until=async(f,m)=>{for(let i=0;i<900;i++){if(f())return;await sleep(10)}throw Error(m)};
const frame=()=>new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)));
const rect=n=>n.getBoundingClientRect();
const near=(actual,expected,label)=>assert(Math.abs(actual-expected)<1,label+': '+actual+' != '+expected);
const hit=n=>{const r=rect(n);assert(r.width>0&&r.height>0&&r.left>=-1&&r.right<=innerWidth+1&&r.top>=-1&&r.bottom<=innerHeight+1,'control outside viewport: '+n.outerHTML.slice(0,120));assert(n.contains(document.elementFromPoint(r.x+r.width/2,r.y+r.height/2)),'control is covered: '+n.outerHTML.slice(0,120))};
const tab=name=>[...document.querySelectorAll('#workspace-tabs [role=tab]')].find(n=>n.textContent.trim()===name);
(async()=>{
 const selectedWidth=Number(new URLSearchParams(location.search).get('standard-width'));
 if(!selectedWidth){
  for(const width of [1440,1100,390]){
   const target=new URL(location.href);target.searchParams.set('standard-width',String(width));
   const iframe=document.createElement('iframe');iframe.style.cssText='position:fixed;left:0;top:0;width:'+width+'px;height:900px;border:0;z-index:99999';iframe.src=target.href;document.body.append(iframe);
   await until(()=>iframe.contentDocument?.body?.dataset.testResult,'geometry driver timed out at '+width);
   assert(iframe.contentDocument.body.dataset.testResult==='passed',width+': '+iframe.contentDocument.body.dataset.testResult);iframe.remove();
  }
  document.body.dataset.testResult='passed';return;
 }
 await until(()=>$('#thread [data-seq="420"]')&&!$('.thread-loading-cover'),'chat boot');await frame();
 assert(innerWidth===selectedWidth,'incorrect test viewport');
 if(selectedWidth>=768)near(rect($('#sidebar')).width,selectedWidth<=1100?232:256,'sidebar width');
 near(rect(tab('聊天')).height,36,'tab height');
 if(selectedWidth===390){
  const text=$('#thread .bubble')||$('#thread .bot-bubble');assert(text,'chat text missing');near(parseFloat(getComputedStyle(text).fontSize),15,'phone chat font');
 }
 hit($('#workspace-more'));hit($('#input'));
 tab('记忆').click();await until(()=>$('.memory-view'),'memory');await frame();
 assert(rect($('.memory-view')).width<=960+1,'record column too wide');
 assert(document.documentElement.scrollWidth<=innerWidth,'record page overflows');
 hit($('.memory-search-input'));
 near(rect($('.memory-search')).height,36,'memory search height');
 tab('聊天').click();
 // Open from the real account entry, then measure the same settings controls.
 if(selectedWidth<768){$('#menu').click();await until(()=>rect($('#sidebar')).left>=-1,'sidebar did not open')}
 $('#account').click();$('#account-settings').click();
 if(selectedWidth<=640){await until(()=>$('.setnav').checkVisibility(),'settings directory');$('[data-set="general"]').click()}
 await until(()=>$('#settings-theme').checkVisibility(),'general settings');await frame();
 const settings=rect($('#setpage'));
 assert(settings.left>=-1&&settings.right<=innerWidth+1&&settings.top>=-1&&settings.bottom<=innerHeight+1,'settings outside viewport');
 if(selectedWidth>640){near(settings.width,820,'settings width');near(settings.height,681,'settings height');near(rect($('.setnav')).width,176,'settings navigation width')}
 const upload=$('#settings-owner-upload');hit(upload);near(rect(upload).height,36,'settings button height');near(parseFloat(getComputedStyle(upload).fontSize),14,'settings button font');
 if(selectedWidth<=640){$('#settings-back').click();await frame()}
 $('[data-set="skills"]').click();await until(()=>$('.skrow'),'skill list');await frame();
 const search=$('.sksearch'),importButton=$('.sktoolbar button');hit(search);hit(importButton);
 near(rect(search).height,36,'skill search height');near(rect(importButton).height,36,'skill import height');
 near(parseFloat(getComputedStyle(search).fontSize),14,'skill search font');near(parseFloat(getComputedStyle(importButton).fontSize),14,'skill button font');
 const row=$('.skrow');assert(rect(row).height>=64-1,'two-line skill row too short');hit(row);
 assert(document.documentElement.scrollWidth<=innerWidth&&$('.setcnt').scrollWidth<=$('.setcnt').clientWidth,'settings overflow');
 assert(testErrors.length===0,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`
