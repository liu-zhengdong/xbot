package web

import (
	"os/exec"
	"testing"
)

// Exercise the public update control, including installation failure and the
// installer route, without starting a real updater or navigating off the page.
func TestUpdateButtonContract(t *testing.T) {
	runChatStreamBrowser(t, chatStreamFixture+`
let updateOffer={state:'checking',latest:'v1.2.3'},updateInstalls=0,updateDownloads=[],updateFail=true,updateRestartPolls=0;
const updateFetch=window.fetch;
window.open=(...args)=>updateDownloads.push(args);
window.fetch=async(path,opts={})=>{
 if(path==='/api/update')return json(updateOffer);
 if(path==='/api/update/install'){updateInstalls++;return updateFail?new Response('{"error":"scripted install failure"}',{status:409,headers:{'Content-Type':'application/json'}}):json({status:'ok'})}
 if(path==='/api/session'&&updateInstalls===2)updateRestartPolls++;
 return updateFetch(path,opts);
};
`, `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const waitFor=async(fn,msg)=>{for(let i=0;i<600;i++){if(fn())return;await sleep(10)}throw Error(msg)};
const refresh=()=>document.dispatchEvent(new Event('visibilitychange'));
(async()=>{
 await waitFor(()=>!$('#shell').hidden&&$('#title').textContent.includes(bots[0].name)&&!$('.thread-loading-cover'),'boot');
 const control=$('#update');assert(control&&!control.checkVisibility(),'checking update interrupted chat');
 const inner=$('#thread .thread-inner');
 updateOffer.state='ready';refresh();await waitFor(()=>control.checkVisibility(),'ready update unreachable');
 assert(!$('#thread').contains(control),'update control entered conversation');
 const r=control.getBoundingClientRect();assert(control.contains(document.elementFromPoint(r.x+r.width/2,r.y+r.height/2)),'update click is blocked');
 control.click();await waitFor(()=>updateInstalls===1&&!control.disabled,'install did not POST or recover from failure');
 assert(control.checkVisibility()&&updateDownloads.length===0,'ready install incorrectly downloaded');
 updateOffer={state:'manual',latest:'v1.2.4',download:'https://example.test/xbot-installer'};refresh();
 await waitFor(()=>control.querySelector('.update-label')?.textContent==='下载新版','manual state did not refresh');
 control.click();await waitFor(()=>updateDownloads.length===1,'manual update did not open installer');
 assert(updateDownloads[0][0]===updateOffer.download&&updateDownloads[0][1]==='_blank'&&updateDownloads[0][2]==='noopener','wrong installer target');
 assert(updateInstalls===1,'manual update tried server install');
 updateOffer.state='current';refresh();await waitFor(()=>!control.checkVisibility(),'current update stays visible');
 assert($('#thread .thread-inner')===inner,'update refresh replaced chat');
 updateOffer.state='ready';updateFail=false;refresh();await waitFor(()=>control.checkVisibility(),'ready retry unreachable');
 control.click();await waitFor(()=>updateInstalls===2&&updateRestartPolls>0,'successful install did not poll restarted server');
 assert(control.disabled,'pending restart allows duplicate installation');
 assert(testErrors.length===0,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>document.body.dataset.testResult='failed: '+e.message);
`)
}

// A metadata-bearing release tag must recognize the version reported by the
// restarted server; a successful reply from the old process must keep polling.
func TestRestartPollNormalizesVersions(t *testing.T) {
	src := readStatic(t, "static/app.js")
	script := `const assert=require('node:assert/strict');
let pending=[],reloads=0,version='1.2.2';
const location={reload(){reloads++}},setTimeout=fn=>pending.push(fn);
const fetch=async()=>({ok:true,json:async()=>({version})});
` + "function shortVersion(v) {" + functionBody(t, src, "function shortVersion(v) {") +
		"\nfunction waitForRestart(want) {" + functionBody(t, src, "function waitForRestart(want) {") + `
(async()=>{
 waitForRestart('v1.2.3+build.7');assert.equal(pending.length,1);
 await pending.shift()();assert.equal(reloads,0);assert.equal(pending.length,1);
 version='1.2.3';await pending.shift()();assert.equal(reloads,1);assert.equal(pending.length,0);
})().catch(e=>{console.error(e);process.exitCode=1});`
	if out, err := exec.Command("node", "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
