package web

import "testing"

func TestMemoryPanelBrowser(t *testing.T) {
	runTodoBrowser(t, memoryPanelFixture, memoryPanelDriver)
}

const memoryPanelFixture = `
localStorage.removeItem('xbot-panel-open');localStorage.removeItem('xbot.side');
const memoryData={
 person:{path:'/data/people/owner/memory/memory.md',markdown:'# 用户记忆\n\n## 索引\n关于语言与习惯\n\n## 常驻\n- (2026-10-30) 使用 **中文** 交流\n- (2026-10-29) 喜欢简洁、直接的说明',empty:false},
 bot:{path:'/data/bots/ada/memory/memory.md',markdown:'# Bot 记忆\n\n## 索引\nXBot 的工作约定\n\n## 常驻\n- (2026-10-30) 提交前运行测试\n- (2026-10-29) 先对齐设计，再开始实现',empty:false},
 experiences:{items:[{id:43,created_at:'2026-10-30T00:17:00Z',summary:'一起确认记忆面板的展示方式：常驻看整篇，经历按时间排列。',focus:'记忆面板',segment:'用户：常驻是一整份 memory.md。\nBot：按定稿重做面板。'},{id:42,created_at:'2026-10-29T08:30:00Z',summary:'核对桌面和网页的品牌图标，并完成定向回归。'}],next_before:42},
 month_log:{path:'/data/bots/ada/memory/log/2026-10.md',empty:false}
};
let memoryOlderCalls=0,memoryFailOlder=false;
const memoryBaseFetch=window.fetch;
window.fetch=async(path,opts={})=>{
 const route=String(path);
 if(route.includes('/experiences?before=')){
  memoryOlderCalls++;
  if(memoryFailOlder)return new Response('{"error":"read failed"}',{status:500});
  return json({items:[{id:1,created_at:'2026-10-01T00:00:00Z',summary:'更早的一条经历',segment:'最早的提纲'}]});
 }
 if(route.includes('/memory-files/'))return json({status:'ok'});
 const res=await memoryBaseFetch(path,opts);
 if(/^\/api\/bots\/[^/]+$/.test(route)&&(!opts.method||opts.method==='GET')){
  const data=await res.json();return json({...data,memory:structuredClone(memoryData)});
 }
 return res;
};
`

const memoryPanelDriver = `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const waitFor=async(fn,msg)=>{for(let i=0;i<1200;i++){if(fn())return;await sleep(10)}throw Error(msg)};
(async()=>{
 await waitFor(()=>!$('#shell').hidden&&$('#title').textContent.includes(bots[0].name)&&!$('#thread .thread-skeleton'),'boot');
 $('#head-chip').click();await waitFor(()=>$('#panel [role=tab]'),'panel');
 const memoryTab=()=>[...document.querySelectorAll('#panel [role=tab]')].find(n=>n.textContent==='记忆');
 memoryTab().click();await waitFor(()=>$('.memory-view'),'memory view');
 await sleep(300);
 document.getAnimations().filter(a=>a.effect?.target===$('#panel')).forEach(a=>a.finish());
 const bounds=$('#panel').getBoundingClientRect();assert(bounds.right<=innerWidth+1&&bounds.left>=-1,'memory panel outside viewport');
 assert($('.memory-view').scrollWidth<=$('.memory-view').clientWidth,'memory view overflows');
 const shot=new URLSearchParams(location.search).get('shot');
 if(shot==='default'){document.body.dataset.testResult='passed';return}
 const docs=[...document.querySelectorAll('.memory-document')];
 assert(docs.length===2,'need two whole documents');
 assert(docs[0].textContent.includes('关于你')&&docs[1].textContent.includes('关于本 Bot'),'scope order');
 assert(docs[0].querySelector('strong')?.textContent==='中文','Markdown did not use the bubble renderer');
 assert(docs[0].textContent.includes('索引')&&docs[0].textContent.includes('喜欢简洁'),'document lost content');
 assert($('.memory-experience time').dateTime===memoryData.experiences.items[0].created_at,'experience lost timestamp');
 assert(!$('.memory-experience').open,'experience starts expanded');
 $('.memory-experience summary').click();assert($('.memory-experience').open&&$('.memory-experience').textContent.includes('用户：常驻'),'segment did not expand');
 if(shot==='expanded'){document.body.dataset.testResult='passed';return}
 // An unchanged status refresh must preserve open rows, focus and text selection.
 const row=$('.memory-experience'),summary=row.querySelector('summary');summary.focus();
 document.dispatchEvent(new Event('visibilitychange'));await sleep(250);
 assert($('.memory-experience')===row&&row.open&&document.activeElement===summary,'refresh replaced the expanded row');
 memoryFailOlder=true;$('.memory-earlier').click();await waitFor(()=>$('.memory-page-error')&&!$('.memory-page-error').hidden&&!$('.memory-earlier').disabled,'pagination error hidden');
 memoryFailOlder=false;$('.memory-earlier').click();await waitFor(()=>document.querySelectorAll('.memory-experience').length===3,'older page absent');
 assert(!$('.memory-earlier')&&memoryOlderCalls===2,'pagination state lost');
 assert(row===$('.memory-experience')&&row.open,'paging rebuilt the first page');
 assert($('.memory-footer').textContent.includes('打开本月流水'),'footer missing');
 const file=$('.memory-file');assert(file.href.includes('/memory-files/person'),'file does not target person document');
 memoryData.person.empty=true;memoryData.person.markdown='';memoryData.bot.empty=true;memoryData.bot.markdown='';memoryData.experiences={items:[]};memoryData.month_log.empty=true;
 // Changing tabs uses the latest panel DTO, without a private memory cache.
 document.dispatchEvent(new Event('visibilitychange'));await sleep(250);
 [...document.querySelectorAll('#panel [role=tab]')].find(n=>n.textContent==='详情').click();memoryTab().click();
 await waitFor(()=>$('.memory-empty'),'empty state missing');
 assert(!$('.memory-experience')&&!$('.memory-file')&&!$('.memory-log'),'missing files still actionable');
 if(shot==='empty'){document.body.dataset.testResult='passed';return}
 assert(testErrors.length===0,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`
