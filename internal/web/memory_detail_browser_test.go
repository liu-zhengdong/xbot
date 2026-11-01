package web

import "testing"

func TestMemoryDetailReadingBrowser(t *testing.T) {
	runChatFramesBrowser(t, memoryPanelFixture+memoryDetailFixture, memoryDetailDriver)
}

const memoryDetailFixture = `
const noisyDigest='user: 工具 sh (sh_a508abb4) 已结束: done exit 0 cwd: /Users/tester/.xbot-dev/work/小鲸/longmao 0\n/Users/tester/Library/Application S\n[truncated]';
const mixedDigest='user: 先问清真实目的再继续。\nassistant: 已确认订阅入口。 [tools: shell]\nuser: 工具 sh (sh_background) 已结束: done exit 0 cwd: /Users/tester/private\n[truncated]\nassistant: 下一步核对域名状态。\n… 4 more lines';
memoryData.experiences={items:[
 {id:43,created_at:'2026-10-31T14:48:00Z',summary:'抓 SNI 能定位域名，但订阅路径还需要进一步确认',segment:noisyDigest},
 {id:42,created_at:'2026-10-31T14:34:00Z',summary:'确认订阅入口和下一步',focus:'先明确目的，再继续排查',segment:mixedDigest}
]};
const detailBaseFetch=window.fetch;
window.fetch=async(path,opts={})=>{
 const url=new URL(String(path),location.href);
 if(url.pathname.endsWith('/experiences')&&url.searchParams.has('q'))return json({items:[structuredClone(memoryData.experiences.items[0])]});
 return detailBaseFetch(path,opts);
};
`

const memoryDetailDriver = `
const $=s=>document.querySelector(s),assert=(v,m)=>{if(!v)throw Error(m)};
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const waitFor=async(f,m)=>{for(let i=0;i<1000;i++){if(f())return;await sleep(10)}throw Error(m)};
(async()=>{
 await waitFor(()=>$('#workspace-tab-library')&&!$('.thread-loading-cover'),'boot');$('#workspace-tab-library').click();
 await waitFor(()=>$('[data-experience="43"]'),'memory');
 const noisy=$('[data-experience="43"]');noisy.querySelector('summary').click();
 const source=noisy.querySelector('.memory-raw');
 assert(source&&!source.open,'technical source should start folded');
 assert(!noisy.querySelector('.memory-dialogue'),'tool completion was passed off as dialogue');
 assert(!noisy.querySelector('.memory-source').checkVisibility(),'tool output is still visible by default');
 assert(source.querySelector('pre').textContent===noisyDigest,'original digest was modified or lost');
 source.querySelector('summary').click();
 assert(source.open&&source.querySelector('pre').checkVisibility(),'original digest cannot be opened');
 assert(source.querySelector('pre').textContent.includes('[truncated]'),'source no longer retains truncation marker');
 const next=$('[data-experience="42"]');next.querySelector('summary').click();
 const speakers=[...next.querySelectorAll('.memory-speaker')].map(n=>n.textContent);
 assert(JSON.stringify(speakers)===JSON.stringify(['你',bots[0].name,bots[0].name]),'human roles are not readable');
 const readable=next.querySelector('.memory-dialogue');
 assert(readable.textContent.includes('先问清真实目的')&&readable.textContent.includes('下一步核对域名状态'),'human words lost');
 assert(!/sh_background|\/Users\/|truncated|tools:|more lines/.test(readable.textContent),'technical text leaked into reading projection');
 assert(next.querySelector('.memory-source').textContent===mixedDigest,'mixed source lost text');
 const held=next.querySelector('.memory-raw');held.open=true;
 document.dispatchEvent(new Event('visibilitychange'));await sleep(250);
 assert(held===next.querySelector('.memory-raw')&&held.open,'refresh collapsed original source');
 const input=$('.memory-search-input');input.value='sh_a508abb4';input.dispatchEvent(new InputEvent('input',{bubbles:true}));
 await waitFor(()=>$('.memory-experience-list').getAttribute('aria-busy')==='false'&&$('.memory-source mark'),'raw-only search match absent');
 const hit=$('.memory-experience');hit.open=true;hit.querySelector('.memory-raw summary').click();
 assert(hit.querySelector('.memory-source mark').textContent==='sh_a508abb4','raw search highlighting lost');
 assert(hit.querySelector('.memory-source').textContent===noisyDigest,'highlighting changed source text');
 assert(testErrors.length===0,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`
