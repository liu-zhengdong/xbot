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
 await waitFor(()=>$('#workspace-tabs [role=tab]'),'panel');
 const inboxTab=()=>[...document.querySelectorAll('#workspace-tabs [role=tab]')].find(n=>n.textContent==='收件箱');
 inboxTab().click(); await waitFor(()=>$('.inbox-empty-title'),'empty');
 assert($('#panel').textContent.includes('Webhook、定时、其他 Bot 等叫醒时会出现在这里'),'empty hint');
 inboxItems=[{id:'1',source:'webhook:foo',time:'2026-10-27T12:00:00Z',summary:'第一行\n第二行',status:'pending'},{id:'2',source:'watch:abc',time:'2026-10-27T12:00:00Z',summary:'path: /tmp/file',status:'pending'},{id:'3',source:'timer',time:'2026-10-27T12:00:00Z',summary:'定时任务',status:'pending'},{id:'4',source:'bot:ada',time:'2026-10-27T12:00:00Z',summary:'消息',status:'pending'}];
 inboxTab().click(); await waitFor(()=>$('.inbox-row'),'list');
 assert([...document.querySelectorAll('.inbox-source')].map(x=>x.textContent).join('|')==='Webhook|文件监听|定时|其他 Bot','sources');
 $('.inbox-row-head').click(); assert($('.inbox-row').open&&$('.inbox-content').textContent.includes('第二行'),'expand');
 assert(testErrors.length===0,testErrors.join('; ')); document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`)
}
