package web

import "testing"

// A turn that speaks, calls a tool, then speaks again streams into one
// bubble per segment, the same split the transcript draws after the turn.
func TestLiveBubbleSplitBrowser(t *testing.T) {
	runChatStreamBrowser(t, chatStreamFixture, liveBubbleSplitDriver)
}

const liveBubbleSplitDriver = `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const waitFor=async(fn,msg)=>{for(let i=0;i<600;i++){if(fn())return;await new Promise(r=>setTimeout(r,10))}throw Error(msg)};
const bubbles=()=>[...document.querySelectorAll('#thread .msg.bot')].map(n=>n.textContent);
(async()=>{
 await waitFor(()=>$('#title').textContent.includes('2号小伙子')&&!$('#thread .thread-skeleton'),'app did not boot');
 let merged=false;
 new MutationObserver(()=>{if(bubbles().some(t=>t.includes('先看一下')&&t.includes('改好了')))merged=true}).observe($('#thread'),{subtree:true,childList:true,characterData:true});
 const box=$('#input');box.textContent='改一下配置';box.dispatchEvent(new Event('input'));$('#composer').requestSubmit();
 await waitFor(()=>fixture.streams.length&&$('#thread .live'),'send did not start');
 const s=fixture.streams[0];
 emit(s,'delta',{text:'先看一下。'});
 await waitFor(()=>bubbles().some(t=>t.includes('先看一下')),'ack missing');
 emit(s,'tool',{name:'read_file',summary:'config.yaml'});
 emit(s,'delta',{text:'改好了。'});
 await waitFor(()=>bubbles().some(t=>t.includes('改好了')),'final missing');
 assert(bubbles().length===2,'stream did not split at the tool: '+JSON.stringify(bubbles()));
 assert(document.querySelectorAll('#thread .live').length===1,'sealed bubble still live');
 assert(bubbles()[0].includes('先看一下')&&bubbles()[1].includes('改好了'),'segments out of order');
 const msgs=fixture.messages['2号小伙子'];
 msgs.push({...message('assistant','先看一下。'),tool_calls:[{id:'c1',function:{name:'read_file',arguments:'{}'}}]});
 msgs.push({...message('tool','ok'),tool_call_id:'c1'});
 msgs.push(message('assistant','改好了。'));
 emit(s,'done',{text:'改好了。'});s.controller.close();
 await waitFor(()=>!$('#thread .live'),'stream did not settle');
 assert(bubbles().length===2,'transcript split differs from live: '+JSON.stringify(bubbles()));
 assert(!merged,'segments merged into one bubble at some point');
 // Idle polls with an unchanged transcript leave the thread and trace alone.
 $('#info').click();await waitFor(()=>$('#panel [role=tab]'),'panel did not open');
 [...document.querySelectorAll('#panel [role=tab]')].find(n=>n.textContent==='轨迹').click();
 await waitFor(()=>$('#panel-body .trace'),'trace missing');
 const inner=$('#thread .thread-inner'),trace=$('#panel-body .trace');
 const reads=fixture.reads;await waitFor(()=>fixture.reads>reads+1,'poll did not resume');
 assert($('#thread .thread-inner')===inner,'unchanged poll redrew the thread');
 assert($('#panel-body .trace')===trace,'unchanged poll redrew the trace');
 assert(testErrors.length===0,testErrors.join('; '));
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`
