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
			result = result[i:min(i+4000, len(result))]
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
const fixture={calls:[],copied:'',codes:0,pendingCode:null,delayCode:false,delayCodeDelete:false,
 account:{email:'zh***@example.com',owner:true,hosted:false,gateway:'xbot.babelgo.cn',official:true,gateway_ok:true,unbound:false},
 pairing:{a:{phase:'none'},b:{phase:'none'}},devices:[],accountDevices:[{id:'self',name:'MacBook',online:true,self:true,bots:['XBot']},{id:'desktop',name:'台式机',online:true,self:false,bots:['writer','editor']}]};
const peerDevice={id:'desktop',name:'台式机',label:'台式机',os:'Linux',online:true,bots:['writer','editor'],seen:new Date().toISOString(),added:'2026-10-03T00:00:00Z',bot_messaging:true};
const json=v=>new Response(JSON.stringify(v),{headers:{'Content-Type':'application/json'}});
Object.defineProperty(navigator,'clipboard',{value:{writeText:async text=>{fixture.copied=text}}});
window.fetch=async(path,opts={})=>{
 const method=opts.method||'GET',key=method+' '+path;
 fixture.calls.push({key,body:opts.body&&JSON.parse(opts.body)});
 if(path==='/api/account/me')return json({account:fixture.account});
 if(path==='/api/account/claim'){fixture.account=Object.assign({},fixture.account,{owner:true,unbound:false});return json({account:fixture.account,bound:true})}
 if(path==='/api/account/devices')return json({devices:fixture.accountDevices});
 if(path==='/api/session')return json({authed:true,owner:'owner',owner_name:scene==='remote'?'政东':'政东',version:'dev-test',remote:scene==='remote'});
 if(path==='/api/devices')return json({self:{name:'MacBook',os:'macOS',bots:['XBot'],gateway:'https://gateway.test'},gateway_ok:true,devices:fixture.devices,pairing:fixture.pairing});
 if(path==='/api/devices/pairing')return json({pairing:fixture.pairing});
 if(key==='POST /api/devices/code'){
  if(fixture.delayCode)await new Promise(r=>fixture.pendingCode=r);
  fixture.codes++;fixture.pairing.a={phase:'showing',code:fixture.codes%2?'2345-6789-ABCJ':'ABCD-EFGH-JKMT',expires:new Date(Date.now()+300000).toISOString()};
  return json({pairing:fixture.pairing});
 }
 if(key==='DELETE /api/devices/code'){if(fixture.delayCodeDelete)await new Promise(r=>fixture.pendingCodeDelete=r);fixture.pairing.a={phase:'none'};return json({pairing:fixture.pairing})}
 if(key==='DELETE /api/devices/join'){fixture.pairing.b={phase:'none'};return json({pairing:fixture.pairing})}
 if(key==='POST /api/devices/request'){fixture.pairing.a={phase:'done',name:'台式机'};return json({pairing:fixture.pairing})}
 if(/^DELETE \/api\/devices\//.test(path)){const id=path.split('/').pop();fixture.devices=fixture.devices.filter(d=>d.id!==id);return json({status:'ok'})}
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
const clicks=key=>fixture.calls.filter(x=>x.key===key).length;
const closed=async before=>waitFor(()=>clicks('DELETE /api/devices/code')>before&&!$('#devices-modal').firstChild&&fixture.pairing.a.phase==='none'&&fixture.pairing.b.phase==='none','cancel left live pairing');
const open=async()=>{$('#settings-devices [data-act="add"]').click();await waitFor(()=>$('#g7-copy-code')&&!xbotDevices.S.busy,'add did not generate code')};
(async()=>{
 await waitFor(()=>window.XBotSettings&&window.xbotDevices&&!document.getElementById('shell').hidden,'app did not boot');
 if(scene==='remote'){
  // 官网入口：底栏写账号设备数，不写本机名；侧栏照旧是真实设备组。
  await waitFor(()=>document.getElementById('me-device').textContent==='已绑定 2 台设备','remote foot did not name the account devices');
  assert(clicks('GET /api/account/devices')>0,'remote foot did not read the account devices');
  document.body.dataset.testResult='passed';return;
 }
 if(scene==='claim'){
  // 开机认领：本机未绑定时给「把这台电脑绑到账号？」，绑定是真 POST。
  await waitFor(()=>$('#devices-modal [data-act="claim"]'),'boot claim offer missing');
  assert($('#devices-modal').textContent.includes('把这台电脑绑到账号？'),'claim copy');
  $('#devices-modal [data-act="claim-dismiss"]').click();
  await waitFor(()=>!$('#devices-modal').firstChild,'dismiss did not close');
  await xbotDevices.offerClaim();
  assert(!$('#devices-modal').firstChild,'dismissed offer came back');
  sessionStorage.removeItem('xbot-claim-dismissed');
  await xbotDevices.offerClaim();
  await waitFor(()=>$('#devices-modal [data-act="claim"]'),'offer did not return');
  $('#devices-modal [data-act="claim"]').click();
  await waitFor(()=>clicks('POST /api/account/claim')===1,'claim did not POST');
  await waitFor(()=>!$('#devices-modal').firstChild,'claim did not close');
  assert(fixture.account.unbound===false,'claim did not bind');
  await XBotSettings.open('devices');
  await waitFor(()=>!$('[data-spane="devices"]').hidden,'settings devices pane');
  assert(!$('#settings-devices .g7-claim'),'claim banner stayed after binding');
  document.body.dataset.testResult='passed';return;
 }
 if(scene==='request'){
  // 对方输入本机短码后：确认卡 → 允许 → 成功。
  fixture.pairing.a={phase:'request',name:'台式机',os:'Linux',bots:['writer'],expires:new Date(Date.now()+120000).toISOString()};
  await XBotSettings.open('devices');
  await waitFor(()=>xbotDevices.S.view==='request','poll did not surface the request');
  $('#devices-modal [data-act="decide"][data-d="allow"]').click();
  await waitFor(()=>xbotDevices.S.view==='success-a','allow did not succeed');
  assert($('#devices-modal').textContent.includes('已和 台式机 绑定'),'success copy');
  assert(clicks('POST /api/devices/request')===1&&fixture.calls.find(x=>x.key==='POST /api/devices/request').body.decision==='allow','allow body');
  document.body.dataset.testResult='passed';return;
 }
 if(scene==='unpair'){
  fixture.devices=[peerDevice];
  await XBotSettings.open('devices');
  await waitFor(()=>$('#settings-devices .g7-dev-card'),'device card missing');
  $('#settings-devices [data-act="row-menu"][data-id="desktop"]').click();
  const row=$('#devices-modal [data-act="ask-unpair"]')||document.querySelector('.g7-row-menu [data-act="ask-unpair"]');
  assert(row&&row.textContent.includes('解除绑定'),'unbind entry');
  row.click();
  await waitFor(()=>xbotDevices.S.view==='unpair','unbind confirm missing');
  $('#devices-modal [data-act="unpair"]').click();
  await waitFor(()=>clicks('DELETE /api/devices/desktop')===1,'unbind did not DELETE');
  await waitFor(()=>!$('#settings-devices .g7-dev-card')&&document.getElementById('settings-devices').textContent.includes('还没有设备'),'unbound device stayed in the list');
  await waitFor(()=>document.getElementById('remote-roster').hidden,'unbind left the sidebar group');
  document.body.dataset.testResult='passed';return;
 }
 // 默认场景：设置·设备与添加设备嵌层。
 await XBotSettings.open('devices');
 await waitFor(()=>!$('[data-spane="devices"]').hidden,'settings devices pane');
 const devices=$('#settings-devices');
 assert(devices.querySelectorAll('[data-act="add"]').length===1,'add entry count');
 const add=devices.querySelector('.sethd [data-act="add"]');
 assert(add&&add.textContent.includes('添加设备'),'add entry must be 添加设备 in the title row');
 assert(!devices.querySelector('.g7-foot,.dfoot,[data-act="join-open"]'),'obsolete footer or join entry');
 assert(!devices.querySelector('#g7-code')&&devices.querySelector('[data-act="self-gw-edit"]').textContent.trim()==='','join input or text gateway button');
 assert(devices.querySelector('[data-act="self-gw-edit"]').getAttribute('aria-label')==='改网关地址','gateway pencil label');
 assert(!devices.querySelector('.g7-claim'),'claim banner without an unbound account');
 // 空态与侧栏。
 assert(devices.textContent.includes('还没有设备'),'empty copy');
 fixture.devices=[peerDevice];await xbotDevices.mount();
 await waitFor(()=>document.querySelector('.g7-side-head'),'sidebar group missing');
 assert(document.querySelector('.g7-side-head').textContent.includes('台式机'),'sidebar group name');
 // 添加设备：说明为主、短码次要；复制、换一个、完成都真清理。
 await open();
 assert($('#devices-modal').textContent.includes('把这台电脑绑到账号'),'add lead copy');
 assert($('#devices-modal').textContent.includes('也可以让对方输入下方短码'),'short code is secondary');
 assert(!$('#g7-code'),'join input must stay gone');
 assert($('#devices-modal [data-act="close-pair"]').textContent.includes('完成'),'finish button');
 $('#g7-copy-code').click();await waitFor(()=>fixture.copied,'clipboard failed');
 assert(fixture.copied===fixture.pairing.a.code,'wrong code copied');
 assert($('.g7-copy-status').textContent==='已复制','missing copy receipt');
 const old=fixture.pairing.a.code;$('#g7-renew').click();await waitFor(()=>fixture.pairing.a.code!==old&&!xbotDevices.S.busy,'renew failed');
 let deleted=clicks('DELETE /api/devices/code');
 $('#devices-modal [data-act="close-pair"]').click();
 await closed(deleted);
 // 迟到的码不能重开弹窗；后台的 DELETE 完成前也不重开。
 fixture.delayCode=true;fixture.pendingCode=null;$('#settings-devices [data-act="add"]').click();await waitFor(()=>fixture.pendingCode,'missing delayed code request');
 deleted=clicks('DELETE /api/devices/code');$('#devices-modal [data-act="close-pair"]').click();fixture.pendingCode();await closed(deleted);fixture.delayCode=false;
 assert(!$('#devices-modal').firstChild&&fixture.pairing.a.phase==='none','late code reopened modal');
 fixture.delayCodeDelete=true;fixture.pendingCodeDelete=null;
 $('#settings-devices [data-act="add"]').click();await waitFor(()=>$('#g7-copy-code')&&!xbotDevices.S.busy,'reopen add');
 deleted=clicks('DELETE /api/devices/code');$('#devices-modal [data-act="close-pair"]').click();await waitFor(()=>fixture.pendingCodeDelete,'missing delayed delete');
 fixture.pairing.a={phase:'request',name:'台式机'};const polls=clicks('GET /api/bots/status');
 await waitFor(()=>clicks('GET /api/bots/status')>polls,'app polling did not advance');
 assert(!$('#devices-modal').firstChild,'poll reopened canceled pair while DELETE pending');
 fixture.pendingCodeDelete();await closed(deleted);fixture.delayCodeDelete=false;
 // 问候下状态只写「在忙」。
 assert(!document.getElementById('greeting').textContent.includes('刚做完'),'greeting keeps 刚做完');
 assert(!document.getElementById('greeting').textContent.includes('有什么事'),'greeting idle line');
 assert(testErrors.length===0,testErrors.join(';'));
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult="view="+xbotDevices.S.view+" modal="+document.getElementById("devices-modal").innerHTML.slice(0,150)+" ERR "+String(e).slice(0,150)});
`
