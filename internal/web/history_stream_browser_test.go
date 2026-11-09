package web

import "testing"

func TestHistoryEarlierStreamingBrowser(t *testing.T) {
	runChatFramesBrowser(t, transcriptWindowFixture+`fixture.seq=420;`, `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const waitFor=async(fn,msg)=>{for(let i=0;i<1000;i++){if(fn())return;await sleep(10)}throw Error(msg)};
(async()=>{
 await waitFor(()=>$('#thread [data-seq="420"]')&&!$('.thread-loading-cover'),'tail did not open');
 const thread=$('#thread'),box=$('#input');
 box.textContent='长任务期间查看历史';box.dispatchEvent(new Event('input'));$('#composer').requestSubmit();
 await waitFor(()=>fixture.streams.length&&$('#thread .live'),'stream did not start');
 const stream=fixture.streams[0];say(stream,'第一条实时回复');
 await waitFor(()=>thread.textContent.includes('第一条实时回复'),'first delivery missing');
 const delivered=[...thread.querySelectorAll('.msg.bot')].find(row=>row.textContent.includes('第一条实时回复'));
 const live=$('#thread .live'),owner=$('#thread .msg.user:not([data-transcript-key])');
 thread.scrollTop=40;thread.dispatchEvent(new Event('scroll'));await sleep(100);
 const anchor=$('#thread [data-seq="361"]'),y=anchor.getBoundingClientRect().top;
 const reads=historyFixture.requests.length;
 $('#history-older').click();
 await waitFor(()=>historyFixture.requests.slice(reads).some(r=>r.before===361),'earlier click made no request during SSE');
 await waitFor(()=>$('#thread [data-seq="301"]'),'earlier page did not render during SSE');await sleep(100);
 assert(live.isConnected&&delivered.isConnected,'paging removed live SSE nodes');
 assert(owner.isConnected&&!!(owner.compareDocumentPosition(delivered)&Node.DOCUMENT_POSITION_FOLLOWING),'paging reordered pending owner and reply');
 assert(Math.abs(anchor.getBoundingClientRect().top-y)<4,'paging moved reading anchor');
 historyFixture.failTranscript=true;$('#history-older').click();
 await waitFor(()=>!$('#history-error').hidden,'paging failure has no feedback during SSE');
 assert(live.isConnected&&delivered.isConnected,'paging failure removed live replies');
 historyFixture.failTranscript=false;$('#history-older').click();
 await waitFor(()=>$('#thread [data-seq="241"]'),'paging failure cannot retry during SSE');
 say(stream,'第二条实时回复');
 await waitFor(()=>thread.textContent.includes('第二条实时回复'),'reply after paging failed');
 assert(delivered.isConnected,'second delivery lost first reply');
 fixture.messages[bots[0].name].push(...sayRows('第一条实时回复'));
 finish(stream,'第二条实时回复');historyFixture.version++;
 await waitFor(()=>!$('#thread .live')&&!$('#thread .msg.user:not([data-transcript-key])'),'settlement did not reconcile temporary rows');
 for(const text of ['第一条实时回复','第二条实时回复'])assert([...thread.querySelectorAll('.msg.bot')].filter(row=>row.textContent.includes(text)).length===1,'settlement lost or duplicated '+text);
 assert($('#thread [data-seq="241"]'),'settlement discarded loaded history');
 assert(!$('.reply-error'),'normal stream failed after paging');
 // Finish another reply while an older-page response is still in flight.
 box.textContent='翻页请求期间结束回复';box.dispatchEvent(new Event('input'));$('#composer').requestSubmit();
 await waitFor(()=>fixture.streams.length===2&&$('#thread .live'),'second stream did not start');
 const second=fixture.streams[1];say(second,'等待分页时的回复');
 await waitFor(()=>thread.textContent.includes('等待分页时的回复'),'second stream delivery missing');
 const baseFetch=window.fetch;let releaseOlder;
 window.fetch=async(path,opts)=>{const response=await baseFetch(path,opts);if(String(path).includes('/chat?before='))await new Promise(resolve=>releaseOlder=resolve);return response};
 $('#history-older').click();await waitFor(()=>releaseOlder,'older request was not held');
 finish(second,'等待分页时的回复');historyFixture.version++;
 await waitFor(()=>!$('#thread .live')&&!$('#history-older').disabled,'stream completion was blocked by older request');
 releaseOlder();await sleep(100);
 assert([...thread.querySelectorAll('.msg.bot')].filter(row=>row.textContent.includes('等待分页时的回复')).length===1,'late older response lost or duplicated completed reply');
 assert(!$('.reply-error'),'completion during paging raised reply error');
 assert(testErrors.length===0,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`)
}
