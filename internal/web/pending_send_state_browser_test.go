package web

import "testing"

func TestPendingSendExactSettlementBrowser(t *testing.T) {
	runChatFramesBrowser(t, pendingStateFixture, pendingStateDriver)
}

const pendingStateFixture = `
const pendingRequests=[];
const pendingBaseFetch=window.fetch;
window.fetch=async(path,opts={})=>{
 if(String(path).endsWith('/messages')&&opts.method==='POST'){
  const body=JSON.parse(opts.body);pendingRequests.push(body);
  if(body.text==='独立排队')return json({status:'queued',event_id:body.client_id});
 }
 return pendingBaseFetch(path,opts);
};
`

const pendingStateDriver = `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const waitFor=async(f,m)=>{for(let i=0;i<800;i++){if(f())return;await sleep(10)}throw Error(m)};
const submit=text=>{$('#input').textContent=text;$('#input').dispatchEvent(new Event('input'));$('#composer').requestSubmit()};
const pending=()=>[...document.querySelectorAll('#thread .send-state')];
const land=request=>fixture.messages[bots[0].name].push({...message('user',request.text),event_id:request.client_id});
const poll=async()=>{const reads=fixture.reads;document.dispatchEvent(new Event('visibilitychange'));await waitFor(()=>fixture.reads>reads,'poll did not run');await sleep(40)};
(async()=>{
 await waitFor(()=>!$('#shell').hidden&&$('#title').textContent.includes(bots[0].name)&&!$('.thread-loading-cover'),'boot');
 submit('独立排队');await waitFor(()=>pendingRequests.length===1&&pending().length===1&&!$('#thread .live'),'first queue');
 submit('独立排队');await waitFor(()=>pendingRequests.length===2&&pending().length===2&&!$('#thread .live'),'second queue');
 const [first,second]=pendingRequests;
 assert(/^web_[0-9a-f]{32}$/.test(first.client_id)&&first.client_id!==second.client_id,'new submissions did not get independent IDs');
 land({text:first.text,client_id:'web_'+'f'.repeat(32)});await poll();
 assert(pending().length===2,'unrelated same-text history settled a queue');
 land(second);await poll();assert(pending().length===1,'exact second identity did not settle exactly one queue');
 land(first);await poll();assert(pending().length===0,'exact first identity did not settle');
 submit('已落盘但回复失败');await waitFor(()=>fixture.streams.length===1,'error stream');
 finish(fixture.streams[0],'','scripted reply failure');
 await waitFor(()=>$('#thread .reply-error')&&!$('#thread .live'),'reply error did not settle');
 assert(!$('#thread .send-failed'),'recorded input was shown as undelivered');
 submit('流提前结束');await waitFor(()=>fixture.streams.length===2,'EOF stream');
 const eofRequest=pendingRequests.at(-1),source=fixture.messages[bots[0].name];source.splice(source.findIndex(m=>m.event_id===eofRequest.client_id),1);
 fixture.streams[1].controller.close();
 await waitFor(()=>$('#thread .send-failed'),'premature EOF did not retain uncertain input');
 const attempts=pendingRequests.length;await poll();assert(pendingRequests.length===attempts,'uncertain input automatically resent');
 land(eofRequest);await poll();assert(!$('#thread .send-failed'),'late exact event did not settle uncertain send');
 assert(testErrors.length===0,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`

func TestPendingVoiceRetryIdentityBrowser(t *testing.T) {
	runChatFramesBrowser(t, voiceBrowserFixture+pendingVoiceFixture, pendingVoiceDriver)
}

const pendingVoiceFixture = `
const retryRequests=[];const retryVoiceFetch=window.fetch;
window.fetch=async(path,opts={})=>{
 if(String(path).endsWith('/messages')&&opts.method==='POST'){
  const body=JSON.parse(opts.body);retryRequests.push(body);
  if(retryRequests.length===1)return new Response('{}',{status:400,headers:{'Content-Type':'application/json'}});
  fixture.messages[bots[0].name].push({...message('user',body.text),audios:body.audios,event_id:body.client_id});
  return json({status:'queued',event_id:body.client_id});
 }
 return retryVoiceFetch(path,opts);
};
`
const pendingVoiceDriver = `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const waitFor=async(f,m)=>{for(let i=0;i<800;i++){if(f())return;await new Promise(r=>setTimeout(r,10))}throw Error(m)};
(async()=>{
 await waitFor(()=>!$('#shell').hidden&&$('#title').textContent.includes(bots[0].name)&&!$('.thread-loading-cover'),'boot');
 $('#mic').click();await waitFor(()=>voiceStreams.length===1,'recording');$('#rec-send').click();
 await waitFor(()=>$('#thread .send-failed button'),'voice failure');const retry=$('#thread .send-failed button');retry.click();
 await waitFor(()=>retryRequests.length===2&&!$('#thread .send-state'),'voice retry did not settle');
 assert(retryRequests[0].client_id===retryRequests[1].client_id&&/^web_[0-9a-f]{32}$/.test(retryRequests[0].client_id),'retry created a new identity');
 assert(JSON.stringify(retryRequests[0].audios)===JSON.stringify(retryRequests[1].audios),'retry changed audio');
 retry.click();await new Promise(r=>setTimeout(r,50));assert(retryRequests.length===2,'recorded retry sent again');
 assert(testErrors.length===0,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`

func TestPendingGroupReceiptBrowser(t *testing.T) {
	runChatFramesBrowser(t, pendingGroupFixture, pendingGroupDriver)
}

const pendingGroupFixture = `
const groupRequests=[],groupLines=[];const pendingGroupFetch=window.fetch;
window.fetch=async(path,opts={})=>{
 if(path==='/api/groups')return json({groups:[{id:'g1',name:'发送测试群',members:bots.map(b=>b.name)}],folders:[]});
 if(path==='/api/groups/g1')return json({group:{id:'g1',name:'发送测试群'},messages:groupLines});
 if(path==='/api/groups/g1/messages'){
  const body=JSON.parse(opts.body);groupRequests.push(body);
  if(groupRequests.length===1)return new Response('{}',{status:400,headers:{'Content-Type':'application/json'}});
  groupLines.push({id:'group-message',sender:'owner',kind:'person',text:body.text,time:new Date().toISOString()});return json({status:'sent'});
 }
 return pendingGroupFetch(path,opts);
};
`
const pendingGroupDriver = `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const waitFor=async(f,m)=>{for(let i=0;i<800;i++){if(f())return;await new Promise(r=>setTimeout(r,10))}throw Error(m)};
const submit=()=>{$('#input').textContent='同一句群消息';$('#input').dispatchEvent(new Event('input'));$('#composer').requestSubmit()};
(async()=>{
 await waitFor(()=>!$('#shell').hidden&&$('#title').textContent.includes(bots[0].name)&&!$('.thread-loading-cover'),'boot');
 const group=[...document.querySelectorAll('#roster button')].find(n=>n.textContent.includes('发送测试群'));assert(group,'group missing');group.click();
 await waitFor(()=>$('#title').textContent.includes('发送测试群')&&!$('.thread-loading-cover'),'group did not open');
 submit();await waitFor(()=>$('#thread .send-failed'),'group failure missing');
 submit();await waitFor(()=>groupRequests.length===2&&$('#thread .msg[data-sender]')&&!$('#thread .optimistic'),'group success did not settle');
 assert(document.querySelectorAll('#thread .send-failed').length===1,'group success settled another failed record');
 assert(groupRequests.every(r=>!r.client_id),'group got unsupported Bot identity field');
 assert(testErrors.length===0,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`
