package web

import "testing"

// Thinking is opt-in inside an event detail and never enters the chat.
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
let thinkingTraceReads=0;
const thinkingBaseFetch=window.fetch;
window.fetch=async(path,opts={})=>{
 const u=new URL(String(path),'http://fixture.local');
 if(!u.pathname.endsWith('/transcript'))return thinkingBaseFetch(path,opts);
 thinkingTraceReads++;
 const name=decodeURIComponent(u.pathname.split('/')[3]),all=fixture.messages[name]||[],after=Number(u.searchParams.get('after')||0),messages=all.filter(m=>m.seq>after);
 const head=messages[0]?.seq||0,tail=messages.at(-1)?.seq||0,total=all.at(-1)?.seq||0;
 return json({messages,window:{head_seq:head,tail_seq:tail,total,next_before:head,next_after:tail,has_more_before:false,has_more_after:false}});
};
`

const traceThinkingDriver = traceExplorerSetup + `
(async()=>{
 await openTrace();
 assert(!$('#thread').textContent.includes('先列目录'),'thinking entered conversation');
 const model=traceRows().find(n=>Number(n.dataset.seq)===2&&!n.textContent.includes('shell'));
 await traceOpen(model);await traceTab('预览');
 const thinking=()=>[...traceDetail().querySelectorAll('details')].find(n=>n.querySelector('summary')?.textContent==='思考');
 let think=thinking();assert(think&&!think.open,'thinking must start folded in detail');
 think.querySelector('summary').click();await sleep(20);
 assert(think.open&&think.textContent.includes('先列目录再决定下一步'),'thinking text missing');
 // A changed poll must preserve the expanded choice and selected thinking text.
 const text=think.querySelector('pre')?.firstChild;assert(text,'thinking text not selectable');
 const range=document.createRange();range.selectNodeContents(text);getSelection().removeAllRanges();getSelection().addRange(range);
 const reads=thinkingTraceReads;fixture.messages[bots[0].name].push(message('user','思考详情打开期间的追加'));
 await waitFor(()=>thinkingTraceReads>=reads+2,'two thinking polls missing');
 assert(thinking()?.open&&getSelection().toString()==='先列目录再决定下一步','poll reset thinking or selection');
 getSelection().removeAllRanges();await traceClose();await traceOpen(model);await traceTab('预览');
 assert(thinking()?.open,'reopening lost deliberate thinking choice');
 thinking().querySelector('summary').click();await sleep(20);assert(!thinking().open,'thinking cannot be folded again');
 await traceClose();
 const blank=traceRows().find(n=>Number(n.dataset.seq)===4&&!n.textContent.includes('shell'));
 // A tool-only assistant with blank reasoning need not have a separate model row.
 await traceOpen(blank||traceRows().find(n=>Number(n.dataset.seq)===4));await traceTab('预览');
 assert(!thinking(),'blank thinking rendered');await traceClose();
 assert(!$('#thread').textContent.includes('先列目录'),'thinking entered conversation');
 assert(testErrors.length===0,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`
