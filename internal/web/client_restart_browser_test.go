package web

import "testing"

func TestClientRestartRetryBrowser(t *testing.T) {
	runChatFramesBrowser(t, clientRestartBrowserFixture, clientRestartRetryDriver)
}

func TestClientRestartUnavailableBrowser(t *testing.T) {
	// Desktop mode alone must not expose restart, for example on Windows.
	runChatFramesBrowser(t, clientRestartBrowserFixture+`clientRestart.canRestart=false;`, clientRestartUnavailableDriver)
}

// runChatFramesBrowser supplies chatStreamFixture first. Only session capability
// and the restart response change here; no request reaches a running desktop app.
const clientRestartBrowserFixture = `
const clientRestart={canRestart:true,sessionReads:0,requests:[],pending:[]};
const clientRestartBaseFetch=window.fetch;
window.fetch=async(path,opts={})=>{
 if(path==='/api/session'){
  clientRestart.sessionReads++;
  return json({authed:true,owner:'owner',owner_name:'政东',version:'dev-test',app:true,can_restart:clientRestart.canRestart});
 }
 if(path==='/api/app/restart'){
  clientRestart.requests.push({path,method:opts.method||'GET',body:opts.body});
  return new Promise(resolve=>clientRestart.pending.push((status,data)=>resolve(new Response(JSON.stringify(data),{status,headers:{'Content-Type':'application/json'}}))));
 }
 return clientRestartBaseFetch(path,opts);
};
`

const clientRestartBrowserHelpers = `
const $=s=>document.querySelector(s),assert=(v,m)=>{if(!v)throw Error(m)};
const visible=n=>!!n&&n.getClientRects().length>0&&getComputedStyle(n).visibility!=='hidden';
const waitFor=async(f,m)=>{for(let i=0;i<900;i++){if(f())return;await new Promise(r=>setTimeout(r,10))}throw Error(m)};
const nextFrames=()=>new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve)));
const openRestartAbout=async()=>{
 await waitFor(()=>clientRestart.sessionReads>0&&!$('#shell').hidden&&$('#title').textContent.includes(bots[0].name),'app did not boot with session capability');
 $('#account').click();$('#account-settings').click();
 await waitFor(()=>!$('#setpage').hidden,'settings did not open');
 $('[data-set="about"]').click();
 await waitFor(()=>visible($('[data-spane="about"]')),'about did not open');
};
`

const clientRestartRetryDriver = clientRestartBrowserHelpers + `
(async()=>{
 await openRestartAbout();
 const row=$('#settings-restart-row'),button=$('#settings-restart'),label=$('#settings-restart-label'),problem=$('#settings-restart-error');
 assert(visible(row)&&visible(button),'capable session has no restart entry');
 assert(row.contains(button)&&button.contains(label),'restart entry has no button label');
 assert(problem&&$('[data-spane="about"]').contains(problem),'restart error is not inline in about');
 assert(!visible(problem)&&!button.disabled,'restart entry did not start ready');
 const readyLabel=label.textContent;
 assert(readyLabel.trim(),'restart button has no ready label');
 assert(clientRestart.requests.length===0,'opening about requested a restart');
 button.click();
 await waitFor(()=>clientRestart.pending.length===1,'restart request not sent');
 const first=clientRestart.requests[0];
 assert(first.method==='POST'&&first.path==='/api/app/restart'&&(first.body==null||first.body===''),'restart did not use the argument-free POST contract');
 assert(button.disabled&&label.textContent==='正在重启…','pending restart has no disabled progress state');
 button.click();button.click();button.click();await nextFrames();
 assert(clientRestart.requests.length===1,'repeated click sent another pending restart');
 clientRestart.pending.shift()(500,{error:'desktop restart request unavailable'});
 await waitFor(()=>!button.disabled&&visible(problem)&&problem.textContent.trim(),'failed request did not show an inline error and allow retry');
 assert(label.textContent===readyLabel,'failed restart did not restore its ready label');
 assert(!$('#setpage').hidden&&visible($('[data-spane="about"]')),'restart failure left about');
 button.click();
 await waitFor(()=>clientRestart.requests.length===2&&clientRestart.pending.length===1,'retry did not send exactly one new request');
 const retry=clientRestart.requests[1];
 assert(retry.method==='POST'&&(retry.body==null||retry.body===''),'retry changed the restart request contract');
 assert(button.disabled&&label.textContent==='正在重启…','retry did not restore the progress state');
 assert(!visible(problem),'retry kept the stale failure visible');
 button.click();await nextFrames();
 assert(clientRestart.requests.length===2,'repeated retry click sent another request');
 clientRestart.pending.shift()(202,{status:'restarting'});
 await nextFrames();
 assert(button.disabled&&label.textContent==='正在重启…','accepted restart became clickable before the window closed');
 assert(!visible(problem),'accepted restart retained an error');
 button.click();await nextFrames();
 assert(clientRestart.requests.length===2,'accepted restart allowed another request');
 assert(testErrors.length===0,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`

const clientRestartUnavailableDriver = clientRestartBrowserHelpers + `
(async()=>{
 await openRestartAbout();await nextFrames();
 assert(clientRestart.sessionReads>0&&!clientRestart.canRestart,'unavailable session fixture was not read');
 assert(!visible($('#settings-restart-row'))&&!visible($('#settings-restart')),'desktop without can_restart exposed restart');
 $('#setscrim').click();
 await waitFor(()=>$('#setpage').hidden,'settings did not close');
 await openRestartAbout();await nextFrames();
 assert(!visible($('#settings-restart-row'))&&!visible($('#settings-restart')),'reopening about exposed unavailable restart');
 assert(!visible($('#settings-restart-error')),'unavailable restart showed an error');
 assert(clientRestart.requests.length===0,'unavailable session requested a restart');
 assert(testErrors.length===0,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`
