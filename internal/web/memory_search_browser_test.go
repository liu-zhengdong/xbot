package web

import "testing"

func TestMemoryExperienceSearchBrowser(t *testing.T) {
	runChatFramesBrowser(t, memoryPanelFixture+memorySearchFixture, memorySearchDriver)
}

const memorySearchFixture = `
const memorySearchCalls=[];
let holdMemorySearch=false,releaseMemorySearch,failMemorySearch=false;
const memorySearchFetch=window.fetch;
window.fetch=async(path,opts={})=>{
 const route=String(path),url=new URL(route,location.href),q=url.searchParams.get('q');
 if(url.pathname.endsWith('/experiences')&&q){
  memorySearchCalls.push({q,before:url.searchParams.get('before')});
  if(holdMemorySearch){holdMemorySearch=false;await new Promise(resolve=>releaseMemorySearch=resolve)}
  if(failMemorySearch)return new Response('{"error":"search failed"}',{status:500});
  if(q==='TTS'){
   if(url.searchParams.has('before'))return json({items:[{id:11,created_at:'2026-09-02T00:00:00Z',summary:'TTS 更早的声音约定'}]});
   return json({items:[{id:13,created_at:'2026-09-04T00:00:00Z',summary:'Breeze TTS 的声音选择',focus:'稳定的 TTS 音色',segment:'用户：选第三段。'},{id:12,created_at:'2026-09-03T00:00:00Z',summary:'用克隆保持声音稳定',focus:'TTS 音色'}],next_before:12});
  }
  return json({items:[]});
 }
 return memorySearchFetch(path,opts);
};
`

const memorySearchDriver = `
const $=s=>document.querySelector(s),assert=(v,m)=>{if(!v)throw Error(m)};
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const waitFor=async(f,m)=>{for(let i=0;i<1000;i++){if(f())return;await sleep(10)}throw Error(m)};
const tab=name=>[...document.querySelectorAll('#workspace-tabs [role=tab]')].find(n=>n.textContent.trim()===name);
(async()=>{
 await waitFor(()=>tab('记忆')&&!$('.thread-loading-cover'),'boot');tab('记忆').click();
 await waitFor(()=>$('.memory-search-input')&&$('[data-experience="43"]'),'memory view');
 const field=$('.memory-search-input'),original=$('[data-experience="43"]');original.open=true;
 const type=(value,composing=false)=>{field.value=value;field.dispatchEvent(new InputEvent('input',{bubbles:true,isComposing:composing}))};
 const settled=()=>$('.memory-experience-list').getAttribute('aria-busy')==='false';
 type('TTS');await waitFor(()=>$('[data-experience="13"]')&&settled(),'old search hits absent');
 assert(memorySearchCalls[0].q==='TTS'&&memorySearchCalls[0].before===null,'first query not sent to server');
 assert(!$('.memory-view').textContent.includes('一起确认记忆面板'),'ordinary page mixed into search');
 assert($('.memory-experience time').dateTime==='2026-09-04T00:00:00Z','search date lost');
 assert($('.memory-summary mark')?.textContent==='TTS','match not marked');
 $('[data-experience="13"] summary').click();
 assert($('[data-experience="13"]').open&&$('[data-experience="13"]').textContent.includes('用户：选第三段'),'search outline missing');
 if(new URLSearchParams(location.search).get('shot')==='search'){document.body.dataset.testResult='passed';return}
 field.focus();field.setSelectionRange(1,2);
 const expanded=$('[data-experience="13"]');
 document.dispatchEvent(new Event('visibilitychange'));await sleep(250);
 assert($('.memory-search-input')===field&&document.activeElement===field&&field.value==='TTS','poll replaced search input');
 assert($('[data-experience="13"]')===expanded&&expanded.open,'poll replaced expanded search hit');
 $('.memory-earlier').click();await waitFor(()=>$('[data-experience="11"]')&&settled(),'searched older page absent');
 assert(memorySearchCalls.at(-1).q==='TTS'&&memorySearchCalls.at(-1).before==='12','search cursor/query lost');
 assert(document.querySelectorAll('.memory-experience').length===3&&!$('.memory-earlier'),'search pagination did not finish');
 const finishedCalls=memorySearchCalls.length;
 field.dispatchEvent(new KeyboardEvent('keydown',{key:'Enter',bubbles:true}));await sleep(220);
 assert(memorySearchCalls.length===finishedCalls&&!$('.memory-earlier'),'Enter restarted a finished search and rewound its cursor');
 $('.memory-search-clear').click();
 assert(field.value===''&&$('[data-experience="43"]')===original&&original.open,'clear did not restore ordinary nodes and expansion');
 // A slower old query must not overwrite a newer query.
 holdMemorySearch=true;type('旧查询');await waitFor(()=>releaseMemorySearch,'held search absent');
 const releaseOld=releaseMemorySearch;releaseMemorySearch=null;
 type('TTS');await waitFor(()=>$('[data-experience="13"]')&&settled(),'new query absent');
 releaseOld();await sleep(100);
 assert(field.value==='TTS'&&$('[data-experience="13"]'),'late query overwrote new hits');
 // Failure retains the query and offers a real retry. Empty results are distinct.
 failMemorySearch=true;type('失败');await waitFor(()=>!$('.memory-page-error').hidden&&settled(),'search failure hidden');
 assert(field.value==='失败','failure cleared query');
 failMemorySearch=false;$('.memory-page-error .memory-retry').click();
 await waitFor(()=>$('.memory-page-error').hidden&&$('.memory-empty')?.textContent.includes('没有找到')&&settled(),'retry did not show no results');
 $('.memory-empty .memory-retry').click();assert(field.value===''&&original.isConnected,'empty clear failed');
 // Chinese composition does not send incomplete words.
 const calls=memorySearchCalls.length;type('声音',true);await sleep(220);
 assert(memorySearchCalls.length===calls,'search sent during IME composition');
 field.dispatchEvent(new CompositionEvent('compositionend',{bubbles:true,data:'声音'}));
 await waitFor(()=>memorySearchCalls.length===calls+1&&settled(),'composition end did not search');
 // Replies from the previous Bot must not populate the next Bot's view.
 holdMemorySearch=true;type('切换前');await waitFor(()=>releaseMemorySearch,'held bot query absent');
 document.querySelector('[data-bot="'+bots[1].name+'"]').click();
 await waitFor(()=>$('#title').textContent.includes(bots[1].name)&&!$('.thread-loading-cover'),'second bot absent');
 releaseMemorySearch();tab('记忆').click();
 await waitFor(()=>$('.memory-search-input'),'second memory absent');
 assert($('.memory-search-input')!==field&&$('.memory-search-input').value==='','query crossed bots');
 assert(!$('.memory-search-status').checkVisibility(),'old search status crossed bots');
 assert(testErrors.length===0,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`
