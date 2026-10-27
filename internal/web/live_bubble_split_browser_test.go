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
 let merged=false,leaked=false;
 new MutationObserver(()=>{if(bubbles().some(t=>t.includes('先看一下')&&t.includes('改好了')))merged=true;if(bubbles().some(t=>t.includes('草稿')))leaked=true;if($('#thread .sysline, #thread .system-line'))leaked=true}).observe($('#thread'),{subtree:true,childList:true,characterData:true});
 const box=$('#input');box.textContent='改一下配置';box.dispatchEvent(new Event('input'));$('#composer').requestSubmit();
 await waitFor(()=>fixture.streams.length&&$('#thread .live'),'send did not start');
 const s=fixture.streams[0];
 // Plain reply text never opens a bubble; only a landed send_message_to_user does.
 emit(s,'delta',{text:'草稿：我先想想。'});
 emit(s,'tool',{name:'send_message_to_user',summary:'先看一下。'});
 emit(s,'result',{name:'send_message_to_user',output:"sent to the owner's chat",text:'先看一下。'});
 await waitFor(()=>bubbles().some(t=>t.includes('先看一下')),'first message missing');
 emit(s,'tool',{name:'read_file',summary:'config.yaml'});
 emit(s,'tool',{name:'send_message_to_user',summary:'改好了。'});
 emit(s,'result',{name:'send_message_to_user',output:"sent to the owner's chat",text:'改好了。'});
 await waitFor(()=>bubbles().some(t=>t.includes('改好了')),'second message missing');
 assert(document.querySelectorAll('#thread .msg.bot:not(.live)').length===2,'one bubble per call: '+JSON.stringify(bubbles()));
 assert(document.querySelectorAll('#thread .live').length===1,'sealed bubble still live');
 const msgs=fixture.messages['2号小伙子'];
 const say=(id,text)=>({id,function:{name:'send_message_to_user',arguments:JSON.stringify({text})}});
 msgs.push({...message('assistant','草稿：我先想想。'),tool_calls:[say('c0','先看一下。')]});
 msgs.push({...message('tool',"sent to the owner's chat"),tool_call_id:'c0'});
 msgs.push({...message('assistant',''),tool_calls:[{id:'c1',function:{name:'read_file',arguments:'{}'}}]});
 msgs.push({...message('tool','ok'),tool_call_id:'c1'});
 msgs.push({...message('assistant',''),tool_calls:[say('c2','改好了。')]});
 msgs.push({...message('tool',"sent to the owner's chat"),tool_call_id:'c2'});
 msgs.push(message('assistant','草稿：收工。'));
 emit(s,'done',{text:'先看一下。\n\n改好了。'});s.controller.close();
 await waitFor(()=>!$('#thread .live'),'stream did not settle');
 assert(bubbles().length===2,'transcript split differs from live: '+JSON.stringify(bubbles()));
 assert(bubbles()[0].includes('先看一下')&&bubbles()[1].includes('改好了'),'messages out of order');
 assert(!merged,'messages merged into one bubble at some point');
 assert(!leaked,'reply text or a tool line reached the chat');
 // Idle polls with an unchanged transcript leave the thread and trace alone.
 $('#info').click();await waitFor(()=>$('#panel [role=tab]'),'panel did not open');
 [...document.querySelectorAll('#panel [role=tab]')].find(n=>n.textContent==='轨迹').click();
 await waitFor(()=>$('#panel-body .trace'),'trace missing');
 const inner=$('#thread .thread-inner'),trace=$('#panel-body .trace');
 const reads=fixture.reads;await waitFor(()=>fixture.reads>reads+1,'poll did not resume');
 assert($('#thread .thread-inner')===inner,'unchanged poll redrew the thread');
 assert($('#panel-body .trace')===trace,'unchanged poll redrew the trace');
 // History: a run from before tool-only chat keeps its reply text; a new run
 // that only wrote reply text shows no bot bubble.
 msgs.push(message('user','旧问题'));
 msgs.push({...message('assistant','旧回复'),time:'2026-01-01T00:00:00Z'});
 msgs.push(message('user','新问题'));
 msgs.push(message('assistant','新草稿'));
 await waitFor(()=>bubbles().some(t=>t.includes('旧回复')),'legacy reply text missing');
 assert(!bubbles().some(t=>t.includes('新草稿')),'new run showed bare reply text');
 assert(!$('#sidebar').textContent.includes('新草稿')&&!$('#sidebar').textContent.includes('草稿：收工'),'sidebar preview showed bare reply text');
 assert(testErrors.length===0,testErrors.join('; '));
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`
