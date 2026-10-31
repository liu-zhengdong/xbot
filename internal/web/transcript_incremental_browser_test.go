package web

import "testing"

// Runs the shipped poll/load/apply seam in Chrome, not an extracted patcher.
func TestTranscriptIncrementalBrowser(t *testing.T) {
	runTodoBrowser(t, transcriptIncrementalFixture, transcriptIncrementalDriver)
}

func TestTranscriptIncrementalWithoutValidatorsBrowser(t *testing.T) {
	runTodoBrowser(t, transcriptIncrementalFixture+`useConditional=false;`, transcriptIncrementalDriver)
}

const transcriptIncrementalFixture = `
fixture.messages[bots[0].name].push(message('user','保留这条选区。'),...sayRows('第一条回复。'));
let transcriptVersion=1,notModified=0,bodyReads=0,useConditional=true;
const transcriptFetch=window.fetch;
window.fetch=async(path,opts={})=>{
 if(String(path).endsWith('/chat')){
  const tag='W/"fixture-'+transcriptVersion+'"';
  if(useConditional&&opts.headers?.['If-None-Match']===tag){notModified++;return new Response(null,{status:304,headers:{ETag:tag}})}
  const res=await transcriptFetch(path,opts);bodyReads++;
  return new Response(await res.text(),{headers:{'Content-Type':'application/json',...(useConditional?{ETag:tag}:{})}});
 }
 return transcriptFetch(path,opts);
};
`

const transcriptIncrementalDriver = `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const waitFor=async(fn,msg)=>{for(let i=0;i<1000;i++){if(fn())return;await sleep(10)}throw Error(msg)};
const refresh=()=>document.dispatchEvent(new Event('visibilitychange'));
(async()=>{
 await waitFor(()=>$('#thread .msg.user')&&$('#thread .msg.bot'),'boot');
 if(new URLSearchParams(location.search).has('shot')){document.body.dataset.testResult='passed';return}
 const inner=$('#thread .thread-inner'),user=$('#thread .msg.user'),bot=$('#thread .msg.bot');
 const range=document.createRange();range.selectNodeContents(user.querySelector('.bubble'));
 getSelection().removeAllRanges();getSelection().addRange(range);
 const selected=getSelection().toString();
 let mutations=0;const observer=new MutationObserver(records=>mutations+=records.length);
 observer.observe(inner,{childList:true,subtree:true});
 const initialBodies=bodyReads;refresh();
 await waitFor(()=>useConditional?notModified>0:bodyReads>initialBodies,'no poll response');await sleep(80);
 assert((!useConditional||bodyReads===initialBodies)&&mutations===0,'unchanged poll touched DOM or 304 read a body');
 const bodies=bodyReads;
 fixture.messages[bots[0].name].push(message('tool','hidden tool output'));transcriptVersion++;refresh();
 await waitFor(()=>bodyReads>bodies,'hidden append not loaded');await sleep(80);
 assert($('#thread .thread-inner')===inner&&$('#thread .msg.user')===user&&$('#thread .msg.bot')===bot,'hidden append rebuilt rows');
 assert(mutations===0&&getSelection().toString()===selected,'hidden tool append mutated DOM or selection');
 fixture.messages[bots[0].name].push(...sayRows('新回复。'));transcriptVersion++;refresh();
 await waitFor(()=>inner.textContent.includes('新回复。'),'visible append');
 assert($('#thread .msg.user')===user&&$('#thread .msg.bot')===bot,'visible append replaced old rows');
 assert(getSelection().toString()===selected,'append lost selection');
 fixture.messages[bots[0].name][0].content='修改后的原话。';transcriptVersion++;refresh();
 await waitFor(()=>inner.textContent.includes('修改后的原话。'),'update not patched');
 assert($('#thread .msg.user')!==user&&$('#thread .msg.bot')===bot,'update did not isolate changed row');
 fixture.messages[bots[0].name].shift();transcriptVersion++;refresh();
 await waitFor(()=>!$('#thread .msg.user'),'delete not patched');
 fixture.messages[bots[0].name]=[];transcriptVersion++;refresh();
 await waitFor(()=>$('#thread .empty'),'empty fallback');
 assert(!$('#thread .msg'),'empty retained stale rows');
 observer.disconnect();assert(testErrors.length===0,testErrors.join('; '));
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`
