package web

import "testing"

func TestInboxBrowser(t *testing.T) {
	runTodoBrowser(t, todoBrowserFixture+`
let inboxItems=[];
const inboxFetch=window.fetch;
window.fetch=async(path,opts={})=>String(path).endsWith('/inbox') ? json({pending_count:inboxItems.length,items:inboxItems}) : inboxFetch(path,opts);
`, `
const $=s=>document.querySelector(s), assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const waitFor=async(fn,msg)=>{for(let i=0;i<1200;i++){if(fn())return;await sleep(10)}throw Error(msg)};
(async()=>{
 await waitFor(()=>!$('#shell').hidden&&$('#roster .item[data-bot="'+bots[0].name+'"] .preview')&&!$('.thread-loading-cover'),'boot');
 $('#roster .item[data-bot="'+bots[0].name+'"]').click();
 $('#head-chip').click(); await waitFor(()=>$('#panel [role=tab]'),'panel');
 const tabs=[...document.querySelectorAll('#panel [role=tab]')];
 assert(tabs.map(x=>x.textContent).join('·')==='详情·记忆·任务·轨迹·上下文·收件箱','tab order');
 tabs[5].click(); await waitFor(()=>$('.inbox-empty-title'),'empty');
 assert($('#panel').textContent.includes('Webhook、定时、其他 Bot 等叫醒时会出现在这里'),'empty hint');
 inboxItems=[{id:'1',source:'webhook:foo',time:'2026-10-27T12:00:00Z',summary:'第一行\n第二行',status:'pending'},{id:'2',source:'watch:abc',time:'2026-10-27T12:00:00Z',summary:'path: /tmp/file',status:'pending'},{id:'3',source:'timer',time:'2026-10-27T12:00:00Z',summary:'定时任务',status:'pending'},{id:'4',source:'bot:ada',time:'2026-10-27T12:00:00Z',summary:'消息',status:'pending'}];
 tabs[5].click(); await waitFor(()=>$('.inbox-row'),'list');
 assert([...document.querySelectorAll('.inbox-source')].map(x=>x.textContent).join('|')==='Webhook|文件监听|定时|其他 Bot','sources');
 $('.inbox-row-head').click(); assert($('.inbox-row').open&&$('.inbox-content').textContent.includes('第二行'),'expand');
 $('#panel-tabs').style.width='240px'; assert($('#panel-tabs').scrollWidth>$('#panel-tabs').clientWidth,'tabs should scroll');
 $('#panel [role=tab][aria-selected=true]').click(); await waitFor(()=>$('#panel-tabs').scrollLeft>0,'selected tab not visible');
 assert(testErrors.length===0,testErrors.join('; ')); document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`)
}
