package web

import "testing"

// Context remains reachable from its workspace tab. The running-task shortcut
// must still reveal its hidden target through the same public workspace view.
func TestContextAndTaskIndirectNavigationBrowser(t *testing.T) {
	runChatStreamBrowser(t, chatStreamFixture+`
const indirectFetch=window.fetch;
window.fetch=async(path,opts={})=>{
 if(String(path).endsWith('/context'))return json({ceiling:100000,target:50000,requests:[{prompt:30000,tools:5000,layers:[{key:'hist',tokens:20000},{key:'turn',tokens:5000}]}]});
 const res=await indirectFetch(path,opts);
 if(/^\/api\/bots\/[^/]+$/.test(String(path))&&(!opts.method||opts.method==='GET'))return json({...await res.json(),agents:[{id:'sa_indirect00000001',type:'dev',task:'间接入口任务',status:'running',current_step:'检查入口',started_at:new Date().toISOString()}]});
 return res;
};
`, `
const $=s=>document.querySelector(s),assert=(v,m)=>{if(!v)throw Error(m)};
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const waitFor=async(fn,msg)=>{for(let i=0;i<600;i++){if(fn())return;await sleep(10)}throw Error(msg)};
const tab=name=>[...document.querySelectorAll('#workspace-tabs [role=tab]')].find(n=>n.textContent===name);
const hide=name=>{
 $('#workspace-more').click();
 const item=[...document.querySelectorAll('#workspace-more-menu button')].find(n=>n.textContent===name);
 assert(item?.checkVisibility(),'visibility option unreachable');item.click();
 document.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true}));assert(!tab(name),'tab did not hide');
};
(async()=>{
 await waitFor(()=>!$('#shell').hidden&&$('#work-status button')&&!$('.thread-loading-cover'),'workspace controls missing');
 const bottomGap=()=>$('.workspace').getBoundingClientRect().bottom-$('#composer').getBoundingClientRect().bottom;
 assert(Math.abs(bottomGap()-8)<1,'composer is not at the workspace bottom: '+bottomGap());
 tab('上下文').click();
 await waitFor(()=>tab('上下文')?.getAttribute('aria-selected')==='true'&&$('#panel').checkVisibility(),'context tab navigation failed');
 assert(!$('#thread').checkVisibility(),'context left chat visible');
 assert($('#panel').innerText.includes('30%'),'context usage missing from its tab');
 tab('聊天').click();hide('子代理');
 $('#work-status button').click();
 await waitFor(()=>tab('子代理')?.getAttribute('aria-selected')==='true'&&document.getElementById(tab('子代理').getAttribute('aria-controls'))?.checkVisibility(),'busy navigation failed to reveal its hidden child tab');
 assert(document.getElementById(tab('子代理').getAttribute('aria-controls')).innerText.includes('间接入口任务'),'child destination lost running record');
 assert(testErrors.length===0,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>document.body.dataset.testResult='failed: '+e.message);
`)
}
