package web

import "testing"

func TestReplyPendingQuoteBrowser(t *testing.T) {
	runChatFramesBrowser(t, replySendFixture+`const checkPendingQuote=true;`, replySendDriver)
}

func TestReplyComposerReceiptBrowser(t *testing.T) {
	runChatFramesBrowser(t, replySendFixture+`const checkPendingQuote=false;`, replySendDriver)
}

const replySendFixture = `
fixture.messages[bots[0].name]=[message('user','先确认发布时间。'),...sayRows('明天下午三点发布。')];
const replyRequests=[];
const replyBaseFetch=window.fetch;
window.fetch=async(path,opts={})=>{
 if(String(path).endsWith('/device-tasks'))return json({tasks:[]});
 if(String(path).endsWith('/messages')&&opts.method==='POST'){
  const request={body:JSON.parse(opts.body)};replyRequests.push(request);
  await new Promise(resolve=>request.accept=resolve);
  if(request.reject)return new Response(JSON.stringify({error:'scripted send failure'}),{status:400,headers:{'Content-Type':'application/json'}});
  if(request.queued)return json({status:'queued',event_id:request.body.client_id});
  const response=await replyBaseFetch(path,opts);
  fixture.messages[bots[0].name].at(-1).reply_to=request.body.reply_to;
  return response;
 }
 return replyBaseFetch(path,opts);
};
`

const replySendDriver = `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const waitFor=async(f,m)=>{for(let i=0;i<800;i++){if(f())return;await new Promise(r=>setTimeout(r,10))}throw Error(m)};
const submit=text=>{$('#input').textContent=text;$('#input').dispatchEvent(new Event('input'));$('#composer').requestSubmit()};
const quote=()=>{
 const bubble=$('#thread .msg.bot .bot-bubble'),box=bubble.getBoundingClientRect();
 bubble.dispatchEvent(new MouseEvent('contextmenu',{bubbles:true,cancelable:true,clientX:box.left+10,clientY:box.top+10}));
 const reply=[...document.querySelectorAll('.message-menu [role="menuitem"]')].find(n=>n.textContent==='回复');
 assert(reply,'right-click reply is missing');reply.click();
 assert(!$('#quote-bar').hidden&&$('#quote-bar-text').textContent.includes('明天下午三点发布'),'reply did not arm');
};
(async()=>{
 await waitFor(()=>!$('#shell').hidden&&$('#thread .msg.bot .bot-bubble')&&!$('.thread-loading-cover'),'boot');
 quote();submit('收到，我来准备。');
 await waitFor(()=>replyRequests.length===1&&replyRequests[0].accept,'request did not start');
 const first=replyRequests[0],pending=$('#thread .msg.user.optimistic');
 if(checkPendingQuote)assert(pending?.querySelector('.quote-block')?.textContent.includes('明天下午三点发布'),'pending reply has no quoted message');
 assert(first.body.reply_to>0,'reply target missing from request');first.accept();
 await waitFor(()=>fixture.streams.length===1&&!$('#thread .send-state'),'acceptance missing');
 assert($('#quote-bar').hidden,'accepted reply left composer quoting until stream completion');
 assert($('#input').dataset.placeholder!=='回复…','accepted reply kept reply placeholder');
 assert($('#thread .live'),'fixture finished before checking acceptance');
 assert(pending.querySelectorAll('.quote-block').length===1,'accepted reply must keep exactly one quote');
 pending.querySelector('.quote-block').click();assert($('#thread .quote-hit'),'pending quote cannot locate original');
 await waitFor(()=>!$('#quote-return').hidden,'quote return missing');$('#quote-return').click();
 quote();
 finish(fixture.streams[0],'好的，明天见。');
 await waitFor(()=>!$('#thread .live')&&$('#thread .msg.user[data-seq] .quote-block'),'reply did not settle');
 assert(!$('#quote-bar').hidden,'old stream completion cleared a newly armed quote of the same message');
 assert($('#thread .msg.user[data-seq] .quote-block').textContent.includes('明天下午三点发布'),'recorded reply lost quote');
 submit('失败后保留回复目标。');await waitFor(()=>replyRequests.length===2&&replyRequests[1].accept,'failure request missing');
 replyRequests[1].reject=true;replyRequests[1].accept();
 await waitFor(()=>$('#thread .send-failed'),'failure feedback missing');
 assert(!$('#quote-bar').hidden&&$('#input').textContent==='失败后保留回复目标。','rejected send lost reply draft');
 submit('排队后继续写。');await waitFor(()=>replyRequests.length===3&&replyRequests[2].accept,'queued request missing');
 replyRequests[2].queued=true;replyRequests[2].accept();
 await waitFor(()=>[...document.querySelectorAll('#thread .send-state')].some(n=>n.textContent==='已排队'),'queue receipt missing');
 assert($('#quote-bar').hidden,'queued reply left composer quoting');
 quote();submit('再确认一次。');await waitFor(()=>replyRequests.length===4&&replyRequests[3].accept,'replacement request missing');
 quote();replyRequests[3].accept();
 await waitFor(()=>fixture.streams.length===2&&!$('#thread .optimistic'),'replacement acceptance missing');
 assert(!$('#quote-bar').hidden,'acceptance cleared a quote reselected during the request');
 finish(fixture.streams[1],'确认了。');await waitFor(()=>!$('#thread .live'),'replacement stream did not settle');
 assert(!$('#quote-bar').hidden,'settlement cleared the replacement quote');$('#quote-clear').click();
 assert(testErrors.length===0,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`
