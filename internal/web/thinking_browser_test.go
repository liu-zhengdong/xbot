package web

import "testing"

// Exercise the shipped bubble renderer with the public stream/history shape.
// Backend privacy (including vendor payload removal) is covered separately by
// TestThinkingToolTurnReplaysReasoningAndHidesIt and the real-process smoke.
func TestThinkingBubbleBrowser(t *testing.T) {
	runPageInChrome(t, `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const waitFor=async(fn,msg)=>{for(let i=0;i<600;i++){if(fn())return;await new Promise(r=>setTimeout(r,10))}throw Error(msg)};
(async()=>{
 await waitFor(()=>!$('#shell').hidden&&$('#title').textContent.includes('2号小伙子'),'app did not boot');
 const box=$('#input');box.textContent='读两个文件';box.dispatchEvent(new Event('input'));$('#composer').requestSubmit();
 await waitFor(()=>fixture.streams.length&&$('#thread .live'),'stream did not start');
 const s=fixture.streams.at(-1);
 // The server has no reasoning delta event; only ordinary answer text reaches here.
 say(s,'文件已读完。');
 await waitFor(()=>$('#thread').textContent.includes('文件已读完。'),'answer missing');
 assert(!$('#thread').textContent.includes('PRIVATE-THOUGHT'),'private thought in live bubble');
 finish(s,'文件已读完。');
 // Defensive rendering: even an unexpected reasoning part stays opaque.
 fixture.messages[s.bot].at(-1).parts=[{type:'reasoning',text:'PRIVATE-THOUGHT',vendor_raw:{reasoning_content:'PRIVATE-THOUGHT'}}];
 await waitFor(()=>!$('#thread .live'),'stream did not settle');
 const reads=fixture.reads;await waitFor(()=>fixture.reads>reads,'history was not reloaded');
 assert($('#thread').textContent.includes('文件已读完。'),'answer missing from history');
 assert(!$('#thread').textContent.includes('PRIVATE-THOUGHT')&&!$('#thread').textContent.includes('[thinking]'),'private thought in persisted bubble');
 assert(testErrors.length===0,testErrors.join('; '));
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`)
}
