package web

import "testing"

// Thinking shows only inside an expanded trace turn, folded, and never in the chat.
func TestTraceThinkingBrowser(t *testing.T) {
	runTodoBrowser(t, traceThinkingFixture, traceThinkingDriver)
}

const traceThinkingFixture = `
localStorage.removeItem('xbot-panel-open');localStorage.removeItem('xbot.side');
bots.forEach(b=>b.pinned=false);
const call=(id,cmd)=>({id,function:{name:'shell',arguments:JSON.stringify({command:cmd})}});
fixture.messages[bots[0].name].push(message('user','看看目录。'));
fixture.messages[bots[0].name].push({...message('assistant',''),thinking:'先列目录再决定下一步',tool_calls:[call('c1','ls')]});
fixture.messages[bots[0].name].push({...message('tool','a.txt'),name:'shell',tool_call_id:'c1'});
fixture.messages[bots[0].name].push({...message('assistant',''),thinking:'  \n ',tool_calls:[call('c2','pwd')]});
fixture.messages[bots[0].name].push({...message('tool','/x'),name:'shell',tool_call_id:'c2'});
fixture.messages[bots[0].name].push(message('assistant','目录里有 a.txt。'));
`

const traceThinkingDriver = `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const waitFor=async(fn,msg)=>{for(let i=0;i<1200;i++){if(fn())return;await sleep(10)}throw Error(msg)};
(async()=>{
 await waitFor(()=>!$('#shell').hidden&&!$('#thread .thread-skeleton')&&$('#roster .item[data-bot="'+bots[0].name+'"]'),'app did not boot');
 assert(!$('#thread').textContent.includes('先列目录'),'thinking entered conversation');
 await waitFor(()=>!$('#head-chip').disabled,'header chip stayed disabled');
 $('#head-chip').click();await waitFor(()=>$('#panel [role=tab]'),'panel did not open');
 [...document.querySelectorAll('#panel [role=tab]')].find(n=>n.textContent==='轨迹').click();
 await waitFor(()=>document.querySelectorAll('.trace-row').length===2,'trace rows missing');
 assert(!$('.trace-think'),'thinking shown before expanding');
 document.querySelectorAll('.trace-row').forEach(n=>n.click());
 await waitFor(()=>document.querySelectorAll('.trace-body').length===2,'trace did not expand');
 const bodies=[...document.querySelectorAll('.trace-body')];
 const think=bodies[0].firstElementChild;
 assert(think&&think.matches('details.trace-think')&&!think.open,'thinking not folded at top');
 assert(think.querySelector('summary').textContent==='思考','thinking title');
 assert(document.querySelectorAll('.trace-think').length===1&&!bodies[1].querySelector('.trace-think'),'blank thinking rendered');
 think.querySelector('summary').click();
 assert(think.open&&think.textContent.includes('先列目录再决定下一步'),'thinking text missing');
 assert(!$('#thread').textContent.includes('先列目录'),'thinking entered conversation');
 assert(testErrors.length===0,testErrors.join('; '));
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`
