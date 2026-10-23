package web

import (
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Exercise the shipped settings shell and device/pairing scripts in Chrome.
// Only HTTP responses and the clipboard are faked; no model or real gateway is used.
func TestDevicesPairBrowser(t *testing.T) {
	chrome := ""
	for _, name := range []string{"google-chrome", "chromium", "chromium-browser", "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"} {
		if p, err := exec.LookPath(name); err == nil {
			chrome = p
			break
		}
	}
	if chrome == "" {
		t.Skip("Chrome is not installed")
	}
	dir := t.TempDir()
	// An explicit evidence directory keeps the same fixture available for visual review.
	if evidence := os.Getenv("XBOT_DEVICE_EVIDENCE"); evidence != "" {
		dir = filepath.Join(evidence, "fixture")
	}
	err := fs.WalkDir(staticFS, "static", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		dst := filepath.Join(dir, strings.TrimPrefix(path, "static"))
		if d.IsDir() {
			return os.MkdirAll(dst, 0700)
		}
		data, err := staticFS.ReadFile(path)
		if err != nil {
			return err
		}
		if path == "static/index.html" {
			page := strings.ReplaceAll(string(data), `src="/`, `src="./`)
			page = strings.ReplaceAll(page, `href="/`, `href="./`)
			page = strings.Replace(page, "<head>", "<head><script>"+devicesPairFixture+"</script>", 1)
			page = strings.Replace(page, "</body>", "<script>"+devicesPairDriver+"</script></body>", 1)
			data = []byte(page)
		}
		return os.WriteFile(dst, data, 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(chrome, "--headless=new", "--disable-gpu", "--no-sandbox", "--no-first-run", "--no-default-browser-check", "--allow-file-access-from-files", "--user-data-dir="+filepath.Join(t.TempDir(), "chrome"), "--virtual-time-budget=12000", "--dump-dom", (&url.URL{Scheme: "file", Path: filepath.Join(dir, "index.html")}).String()).CombinedOutput()
	if err != nil {
		t.Fatalf("Chrome: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), `data-test-result="passed"`) {
		// Keep diagnostics small: the full app DOM contains bundled artwork.
		result := string(out)
		if i := strings.Index(result, "data-test-result="); i >= 0 {
			result = result[i:min(i+1200, len(result))]
		} else if len(result) > 1500 {
			result = result[:1500]
		}
		t.Fatalf("devices/pairing browser: %s", result)
	}
}

const devicesPairFixture = `
window.testErrors=[];
window.addEventListener('error',e=>testErrors.push(e.message));
window.addEventListener('unhandledrejection',e=>testErrors.push(String(e.reason)));
const scene=new URLSearchParams(location.search).get('scene');
localStorage.setItem('xbot-theme',new URLSearchParams(location.search).get('theme')||'light');
const fixture={calls:[],copied:'',codes:0,pendingCode:null,pendingJoin:null,delayCode:false,delayJoin:false,delayCodeDelete:false,pairing:{a:{phase:'none'},b:{phase:'none'}},devices:[]};
const peerDevice={id:'desktop',name:'台式机',label:'台式机',os:'Linux',online:true,bots:['writer','editor'],seen:new Date().toISOString(),added:'2026-10-03T00:00:00Z',bot_messaging:true};
const json=v=>new Response(JSON.stringify(v),{headers:{'Content-Type':'application/json'}});
Object.defineProperty(navigator,'clipboard',{value:{writeText:async text=>{fixture.copied=text}}});
window.fetch=async(path,opts={})=>{
 const method=opts.method||'GET',key=method+' '+path;
 fixture.calls.push({key,body:opts.body&&JSON.parse(opts.body)});
 if(path==='/api/session')return json({authed:true,owner:'owner',owner_name:'政东',version:'dev-test'});
 if(path==='/api/devices')return json({self:{name:'MacBook',os:'macOS',bots:['XBot'],gateway:'https://gateway.test'},gateway_ok:true,devices:fixture.devices,pairing:fixture.pairing});
 if(path==='/api/devices/pairing')return json({pairing:fixture.pairing});
 if(key==='POST /api/devices/code'){
  if(fixture.delayCode)await new Promise(r=>fixture.pendingCode=r);
  fixture.codes++;fixture.pairing.a={phase:'showing',code:fixture.codes%2?'2345-6789-ABCJ':'ABCD-EFGH-JKMT',expires:new Date(Date.now()+300000).toISOString()};
  return json({pairing:fixture.pairing});
 }
 if(key==='DELETE /api/devices/code'){if(fixture.delayCodeDelete)await new Promise(r=>fixture.pendingCodeDelete=r);fixture.pairing.a={phase:'none'};return json({pairing:fixture.pairing})}
 if(key==='DELETE /api/devices/join'){fixture.pairing.b={phase:'none'};return json({pairing:fixture.pairing})}
 if(key==='POST /api/devices/join'){
  if(fixture.delayJoin)await new Promise(r=>fixture.pendingJoin=r);
  fixture.pairing.b={phase:fixture.joinPhase||'confirm',name:'台式机',os:'Linux',bots:['writer']};return json({pairing:fixture.pairing});
 }
 if(key==='POST /api/devices/join/confirm'){fixture.pairing.b={phase:'waiting'};return json({pairing:fixture.pairing})}
 if(path.startsWith('/api/remote/'))return json({messages:[],device:peerDevice});
 if(path==='/api/bots/status')return json({bots:[]});
 if(path==='/api/notifications')return json({notifications:[]});
 if(path==='/api/bots')return json({bots:[]});
 if(path==='/api/groups')return json({groups:[],folders:[]});
 return json({});
};
`

const devicesPairDriver = `
const assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const $=s=>document.querySelector(s);
const waitFor=async(fn,msg)=>{for(let i=0;i<400;i++){if(fn())return;await new Promise(r=>setTimeout(r,10))}throw Error(msg)};
const input=value=>{const n=$('#g7-code');n.focus();n.value=value;n.dispatchEvent(new Event('input',{bubbles:true}))};
const clicks=key=>fixture.calls.filter(x=>x.key===key).length;
const closed=async before=>waitFor(()=>clicks('DELETE /api/devices/code')>before&&!$('#devices-modal').firstChild&&fixture.pairing.a.phase==='none'&&fixture.pairing.b.phase==='none','cancel left live pairing');
const open=async()=>{$('#settings-devices [data-act="add"]').click();await waitFor(()=>$('#g7-copy-code')&&!xbotDevices.S.busy,'pair did not generate code')};
(async()=>{
 await waitFor(()=>window.XBotSettings&&window.xbotDevices&&!document.getElementById('shell').hidden,'app did not boot');
 if(scene==='paired'||scene==='pair'||scene==='copy')fixture.devices=[peerDevice];
 await XBotSettings.open('devices');
 if(scene){if(scene==='pair'||scene==='copy'){await open();if(scene==='copy'){$('#g7-copy-code').click();await waitFor(()=>fixture.copied,'copy failed')}}document.body.dataset.testResult='passed';return}
 const devices=$('#settings-devices');
 for(const list of [[],[peerDevice]]){
  fixture.devices=list;await xbotDevices.mount();
  assert(devices.querySelectorAll('[data-act="add"]').length===1,'pair entry count');
  assert(devices.querySelector('.sethd [data-act="add"]'),'pair entry missing from title row');
  assert(!devices.querySelector('.g7-foot,.dfoot,[data-act="join-open"]'),'obsolete footer or join entry');
 }
 await open();
 assert($('#g7-code')&&$('#g7-copy-code'),'code and input must share one dialog');
 assert($('#devices-modal [data-act="join"]').disabled,'empty next enabled');
 assert($('#devices-modal').querySelectorAll('[data-act="close-pair"]').length===1,'cancel duplicated');
 $('#g7-copy-code').click();await waitFor(()=>fixture.copied,'clipboard failed');
 assert(fixture.copied===fixture.pairing.a.code,'wrong code copied');
 assert($('.g7-copy-status').textContent==='已复制','missing copy receipt');
 $('#devices-modal [data-act="gw-open"]').click();
 const codeGateway=$('#g7-gw');codeGateway.focus();codeGateway.value='https://code-gateway.test';codeGateway.setSelectionRange(8,12,'backward');codeGateway.dispatchEvent(new Event('input',{bubbles:true}));
 assert($('#g7-gw').selectionStart===8&&$('#g7-gw').selectionEnd===12&&$('#g7-gw').selectionDirection==='backward','gateway selection jumped after input');
 const old=fixture.pairing.a.code;$('#g7-renew').click();await waitFor(()=>fixture.pairing.a.code!==old&&!xbotDevices.S.busy,'renew failed');
 assert(fixture.calls.filter(x=>x.key==='POST /api/devices/code').at(-1).body.gateway==='https://code-gateway.test','code generation ignored edited gateway');
 assert($('#g7-renew').textContent.trim()===''&&$('#g7-renew').getAttribute('aria-label')==='换一个','renew must be icon only');
 input('名字@电脑');assert($('#g7-code').value==='名字@电脑','device address destroyed');
 $('#devices-modal [data-act="join"]').click();assert(clicks('POST /api/devices/join')===0,'address sent as code');
 input('23456789ABCD');$('#devices-modal [data-act="join"]').click();
 assert(clicks('POST /api/devices/join')===0,'bad checksum touched gateway');
 // Valid checksummed code, including prose, is normalized only when submitting.
 const alphabet='23456789ABCDEFGHJKMNPQRSTVWXYZ';let data='23456789ABC',sum=0,factor=2;
 for(let i=data.length-1;i>=0;i--){const v=alphabet.indexOf(data[i])*factor;sum+=Math.floor(v/alphabet.length)+v%alphabet.length;factor=3-factor}
 const code=data+alphabet[(alphabet.length-sum%alphabet.length)%alphabet.length];
 input('配对码：'+code.toLowerCase());assert(clicks('POST /api/devices/join')===0,'typing auto-submitted');
 fixture.joinPhase='wrong_or_used';
 $('#devices-modal [data-act="join"]').click();await waitFor(()=>xbotDevices.S.pairing.b.phase==='wrong_or_used','missing wrong-gateway response');
 const change=$('#devices-modal [data-act="gw-open"]');assert(change,'wrong/used code has no gateway editor');change.click();
 const gateway=$('#g7-gw');gateway.value='https://other-gateway.test';gateway.dispatchEvent(new Event('input',{bubbles:true}));
 fixture.joinPhase='confirm';$('#devices-modal [data-act="join"]').click();await waitFor(()=>xbotDevices.S.view==='confirm','next did not reach peer confirmation');
 assert(fixture.calls.filter(x=>x.key==='POST /api/devices/join').at(-1).body.gateway==='https://other-gateway.test','gateway edit did not use existing join API');
 assert(fixture.calls.find(x=>x.key==='POST /api/devices/join').body.code===code,'join API body changed');
 $('#devices-modal [data-act="confirm"]').click();await waitFor(()=>xbotDevices.S.view==='waiting','confirmation did not wait');
 fixture.pairing.b={phase:'denied'};
 await waitFor(()=>xbotDevices.S.view==='denied','refusal did not arrive');
 let deleted=clicks('DELETE /api/devices/code');document.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true}));await closed(deleted);
 assert(!$('#devices-modal').firstChild&&fixture.pairing.a.phase==='none'&&fixture.pairing.b.phase==='none','denied Escape left live pairing');
 // A code response must not discard a gateway edit started after its request.
 fixture.delayCode=true;fixture.pendingCode=null;$('#settings-devices [data-act="add"]').click();await waitFor(()=>fixture.pendingCode,'missing in-flight code');
 $('#devices-modal [data-act="gw-open"]').click();const editing=$('#g7-gw');editing.value='https://typed-during-code.test';editing.dispatchEvent(new Event('input',{bubbles:true}));
 fixture.pendingCode();await waitFor(()=>!xbotDevices.S.busy,'code did not settle');fixture.delayCode=false;
 assert($('#g7-gw')?.value==='https://typed-during-code.test'&&xbotDevices.S.gwOpen,'code response discarded new gateway edit');
 deleted=clicks('DELETE /api/devices/code');$('#devices-modal [data-act="close-pair"]').click();await closed(deleted);
 // A server request received while cancellation DELETE is pending stays closed.
 await open();fixture.delayCodeDelete=true;fixture.pendingCodeDelete=null;
 deleted=clicks('DELETE /api/devices/code');$('#devices-modal [data-act="close-pair"]').click();await waitFor(()=>fixture.pendingCodeDelete,'missing delayed delete');
 fixture.pairing.a={phase:'request',name:'台式机'};const polls=clicks('GET /api/bots/status');
 await waitFor(()=>clicks('GET /api/bots/status')>polls,'app polling did not advance');
 assert(!$('#devices-modal').firstChild,'poll reopened canceled pair while DELETE pending');
 fixture.pendingCodeDelete();await closed(deleted);fixture.delayCodeDelete=false;
 // Cancel a code POST before it returns. Late responses cannot reopen the dialog.
 fixture.delayCode=true;fixture.pendingCode=null;$('#settings-devices [data-act="add"]').click();await waitFor(()=>fixture.pendingCode,'missing delayed code request');
 deleted=clicks('DELETE /api/devices/code');$('#devices-modal [data-act="close-pair"]').click();fixture.pendingCode();await closed(deleted);fixture.delayCode=false;
 assert(!$('#devices-modal').firstChild&&fixture.pairing.a.phase==='none','late code reopened modal');
 // Likewise for a pending join request.
 await open();fixture.delayJoin=true;input(code);$('#devices-modal [data-act="join"]').click();await waitFor(()=>fixture.pendingJoin,'missing delayed join request');
 deleted=clicks('DELETE /api/devices/code');$('#devices-modal [data-act="close-pair"]').click();fixture.pendingJoin();await closed(deleted);fixture.delayJoin=false;
 assert(!$('#devices-modal').firstChild&&fixture.pairing.b.phase==='none','late join reopened modal');
 await open();deleted=clicks('DELETE /api/devices/code');document.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true}));await closed(deleted);
 assert(!$('#devices-modal').firstChild&&!$('#setpage').hidden,'Escape closed parent settings');
 XBotSettings.close();await xbotRemoteChat.open('desktop','writer');$('#remote-chat [data-rc="info"]').click();$('#remote-chat [data-rc="devices"]').click();
 await waitFor(()=>!$('#setpage').hidden&&!$('[data-spane="devices"]').hidden,'remote deep link missed settings devices');
 assert(!$('#devices-drawer'),'old device drawer returned');
 assert(testErrors.length===0,testErrors.join(';'));
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult=e.stack||String(e)});
`
