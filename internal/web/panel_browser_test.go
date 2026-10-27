package web

import (
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Exercise the shipped page and computed geometry, including a nonzero safe
// area. The iframe gives Chrome an exact viewport even below its window minimum.
func TestBotPanelBrowser(t *testing.T) {
	chrome, err := exec.LookPath("/Applications/Google Chrome.app/Contents/MacOS/Google Chrome")
	if err != nil {
		t.Skip("Chrome unavailable")
	}
	for _, width := range []int{390, 820, 1440} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			dir := t.TempDir()
			err := fs.WalkDir(staticFS, "static", func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				dst := filepath.Join(dir, strings.TrimPrefix(path, "static"))
				if d.IsDir() {
					return os.MkdirAll(dst, 0700)
				}
				raw, err := staticFS.ReadFile(path)
				if err != nil {
					return err
				}
				if path == "static/index.html" {
					page := strings.ReplaceAll(string(raw), `src="/`, `src="./`)
					page = strings.ReplaceAll(page, `href="/`, `href="./`)
					page = strings.Replace(page, "<head>", "<head><script>"+chatStreamFixture+panelFixture+"</script>", 1)
					page = strings.Replace(page, "</body>", "<script>"+panelDriver+"</script></body>", 1)
					raw = []byte(page)
				}
				if strings.HasSuffix(path, ".css") {
					// Emulate a notched screen without replacing the sizing rules.
					raw = []byte(strings.ReplaceAll(string(raw), "env(safe-area-inset-top)", "44px"))
				}
				return os.WriteFile(dst, raw, 0600)
			})
			if err != nil {
				t.Fatal(err)
			}
			outer := fmt.Sprintf(`<body><iframe src="index.html#login-code" style="border:0;width:%dpx;height:900px"></iframe></body>`, width)
			if err := os.WriteFile(filepath.Join(dir, "test.html"), []byte(outer), 0600); err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command(chrome, "--headless=new", "--disable-gpu", "--no-sandbox", "--no-first-run", "--no-default-browser-check", "--allow-file-access-from-files", "--user-data-dir="+filepath.Join(dir, "chrome"), "--window-size=1600,1000", "--virtual-time-budget=15000", "--dump-dom", (&url.URL{Scheme: "file", Path: filepath.Join(dir, "test.html")}).String()).CombinedOutput()
			if err != nil {
				t.Fatalf("Chrome: %v", err)
			}
			if !strings.Contains(string(out), `data-test-result="passed"`) {
				result := string(out)
				if i := strings.Index(result, "<body"); i >= 0 {
					result = result[i:]
				}
				t.Fatalf("panel browser: %.1600s", result)
			}
		})
	}
}

const panelFixture = `
localStorage.removeItem('xbot-panel-open');
localStorage.removeItem('xbot.side');
bots[1].pinned=false;
const baseFetch=window.fetch;
fixture.calls=[];
window.fetch=async(path,opts={})=>{
 fixture.calls.push([String(path),opts]);
 if(path==='/api/devices')return json({self:{name:'本机'},gateway_ok:true,devices:[{id:'remote',label:'台式机',bots:['远端Bot'],online:true}]});
 if(path==='/api/groups')return json({groups:[{id:'group',name:'讨论组',members:[]}],folders:[{name:'项目',count:2,bots:bots.map(b=>b.name)}]});
 if(String(path).endsWith('/context'))return json({ceiling:128000,target:64000,requests:[]});
 if(String(path).startsWith('/api/models'))return json({models:[{provider:'fake',model:'scripted',protocol:'chat_completions'},{provider:'deep',brand:'deepseek',model:'deep-pro',protocol:'responses',hosted_web_search:true}]});
 if(path==='/api/providers'){
  if(fixture.providers==='fail')return new Response('{"error":"boom"}',{status:500,headers:{'Content-Type':'application/json'}});
  if(fixture.providers==='none')return json({providers:[]});
  return json({providers:[{name:'xai-key',brand:'xai',protocol:'chat_completions',key_configured:false,models:[]},{name:'fake',protocol:'chat_completions',key_configured:true,models:[{model:'scripted',available:true,context:128000},{model:'stale-local',available:true,context:128000}]},{name:'deep',brand:'deepseek',protocol:'responses',key_configured:true,models:[]}]});
 }
 // The live catalog differs from the local registry on purpose: stale-local is gone, fresh-new is new.
 if(String(path).startsWith('/api/providers/models?')){
  const name=new URL(path,location.href).searchParams.get('provider');
  if(fixture.catalogFail===name){return new Response('{"error":"拉取失败"}',{status:422,headers:{'Content-Type':'application/json'}})}
  if(name==='fake')return json({models:[{model:'scripted',available:true,context:128000},{model:'fresh-new',available:true,context:64000}]});
  if(name==='deep')return json({models:[{model:'deep-pro',available:true,hosted_web_search:true,vision:true,context:500000}]});
 }
 if(String(path).endsWith('/binding')){fixture.bound=JSON.parse(opts.body);return json(fixture.bound)}
 const match=String(path).match(/^\/api\/bots\/([^/]+)$/);
 if(match){
  const name=decodeURIComponent(match[1]);
  if(opts.method==='PATCH'){Object.assign(bots.find(b=>b.name===name),JSON.parse(opts.body));return json({})}
  return json({...bots.find(b=>b.name===name),persona:'You are XBot, a personal assistant. Help the person with the tools granted to you. Be concise and concrete.',model:fixture.bound?.model||'scripted',provider:fixture.bound?.provider||'fake',agents:[],links:[]});
 }
 return baseFetch(path,opts);
};
Object.defineProperty(navigator,'clipboard',{value:{writeText:async text=>{fixture.copied=text}},configurable:true});
`

const panelDriver = `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const waitFor=async(fn,msg)=>{for(let i=0;i<600;i++){if(fn())return;await sleep(10)}throw Error(msg)};
const rect=n=>n.getBoundingClientRect(),near=(a,b)=>Math.abs(a-b)<1;
const finishAnimations=()=>{document.getAnimations().forEach(a=>{if(a.effect.getTiming().iterations!==Infinity)a.finish()})};
const escape=()=>document.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true}));
const row=name=>document.querySelector('[data-bot="'+name+'"]');
const context=name=>row(name).dispatchEvent(new MouseEvent('contextmenu',{bubbles:true,cancelable:true}));
const item=text=>[...document.querySelectorAll('.panel-more button')].find(n=>n.textContent.includes(text));
const pointer=(type,x=20)=>row('另一个Bot').dispatchEvent(new PointerEvent(type,{bubbles:true,pointerType:'touch',isPrimary:true,pointerId:1,clientX:x,clientY:100}));
(async()=>{
 await waitFor(()=>!$('#shell').hidden&&row('2号小伙子'),'app did not boot');
 row('2号小伙子').click();
 await waitFor(()=>$('#title').textContent.includes('2号小伙子'),'Bot did not open');
 const panel=$('#panel');
 // A closed panel has the same dimensions and horizontal axis as an open one.
 const closed=rect(panel),matrix=new DOMMatrix(getComputedStyle(panel).transform);
 assert(matrix.m41>0&&near(matrix.m42,0),'closed drawer moves vertically or does not slide offscreen');
 if(innerWidth<768){
  const m=rect($('#menu')),a=rect($('#title .avatar')),top=rect($('.top'));
  assert(near(top.height,96)&&near(m.y+m.height/2,top.y+44+26),'safe area compresses the content band');
  const c=rect($('#head-chip')),bar=rect($('.top-inner'));
  assert(near(m.x,16),'hamburger inset wrong');
  assert(Math.abs((c.x-m.right-8)-(bar.right-c.right))<2,'chip is not centred between hamburger and edge');
  assert(near(m.y+m.height/2,a.y+a.height/2)&&m.width>=32&&m.height>=32,'hamburger is not centered or touchable');
 }
 const chip=$('#head-chip');
 assert(!$('#info')&&!$('.ptog'),'separate panel toggle still present');
 assert(!chip.disabled&&chip.contains($('#title .avatar'))&&chip.getAttribute('aria-expanded')==='false','chip is not the closed toggle');
 chip.click();
 assert(chip.classList.contains('on')&&chip.getAttribute('aria-expanded')==='true'&&panel.classList.contains('open'),'chip does not open panel');
 assert($('#setpage').hidden,'chip opened settings');
 await waitFor(()=>panel.textContent.includes('scripted'),'panel data missing');
 await sleep(250);finishAnimations();
 const opened=rect(panel),openMatrix=new DOMMatrix(getComputedStyle(panel).transform);
 assert(near(closed.width,opened.width)&&near(closed.height,opened.height),'drawer dimensions change on toggle');
 assert(near(openMatrix.m41,0)&&near(openMatrix.m42,0),'open drawer axis wrong');
 assert(getComputedStyle(panel).transitionProperty.includes('transform'),'desktop drawer skips animation');
 assert(!$('#panel-more')&&$('#panel-actions').children.length===1,'header contains extra action');
 assert(!panel.contains($('#head-chip')),'toggle moved into panel');
 const edit=$('#panel-actions button'),avatar=$('#panel-ident .avatar');
 assert(near(rect(edit).y+rect(edit).height/2,rect(avatar).y+rect(avatar).height/2),'edit is not on identity row');
 const model=$('.tab-body .binding-trigger'),body=$('.tab-body'),face=n=>[n.querySelector('.bt-model').textContent,n.querySelector('.bt-sub').textContent].join('|');
 assert(rect(model).width>=rect(body).width-41,'model still a short card');
 assert(face(model)==='scripted|fake · Completions','details model is not model over provider · protocol');
 assert($('.psec p').textContent.includes('personal assistant'),'persona missing');
 edit.focus();edit.click();await waitFor(()=>!$('#modal').hidden,'edit does not open');
 // The drawer shares the face; rebinding there repaints both faces and leaves the drawer open.
 const drawerModel=$('#modal .binding-trigger');
 assert(drawerModel&&face(drawerModel)==='scripted|fake · Completions','drawer lacks the model face');
 // Empty and failed lists say so in a flat line instead of a blank picker.
 const pick=async()=>{drawerModel.click();await waitFor(()=>$('.binding-dialog')&&!$('.binding-dialog').textContent.includes('正在拉模型'),'picker does not open')};
 const dlgButton=text=>[...$('.binding-dialog').querySelectorAll('button')].find(b=>b.textContent===text);
 fixture.providers='none';await pick();
 assert($('.binding-flat').textContent.includes('还没有可用的供应商')&&dlgButton('管理供应商')&&$('.binding-search').hidden,'no-provider state missing');
 dlgButton('取消').click();
 fixture.providers='fail';await pick();
 assert($('.binding-flat').textContent.includes('模型名单暂时拉不下来'),'list failure state missing');
 // A vendor whose live list fails says so in its own group, without falling back to the local registry.
 fixture.catalogFail='deep';fixture.providers='';dlgButton('重试').click();await waitFor(()=>$('.binding-option'),'retry does not reload');
 const catalogCalls=name=>fixture.calls.filter(([p])=>p==='/api/providers/models?provider='+name).length;
 const deepGroup=()=>[...$('.binding-dialog').querySelectorAll('.binding-section')].find(n=>n._title==='DeepSeek');
 await waitFor(()=>deepGroup().textContent.includes('模型名单暂时拉不下来'),'vendor list failure not shown');
 assert(![...document.querySelectorAll('.binding-option')].some(b=>b.textContent.includes('stale-local')),'picker shows the local registry instead of the live list');
 assert([...document.querySelectorAll('.binding-option')].some(b=>b.textContent.includes('fresh-new')),'new vendor model missing from picker');
 const before=catalogCalls('deep'),listCalls=fixture.calls.filter(([p])=>p==='/api/providers').length;
 fixture.catalogFail='';[...deepGroup().querySelectorAll('button')].find(b=>b.textContent==='重试').click();
 await waitFor(()=>deepGroup().querySelector('.binding-option'),'vendor retry does not refetch the live list');
 assert(catalogCalls('deep')===before+1&&fixture.calls.filter(([p])=>p==='/api/providers').length===listCalls,'vendor retry did not hit the live list');
 const dlg=$('.binding-dialog'),sub=$('.binding-sub').textContent,heads=[...dlg.querySelectorAll('.binding-heading')].map(n=>n.textContent);
 assert($('.binding-dialog h2').textContent==='更换模型','picker title');
 assert(!sub.includes(' / ')&&sub.endsWith('当前 fake · scripted'),'picker subtitle is slash soup');
 assert(heads.includes('xAI · Completions')&&!heads.some(h=>h.includes('xai-key')),'group header uses a raw id');
 assert([...dlg.querySelectorAll('.binding-empty')].some(n=>n.textContent==='配好密钥后再选模型'),'keyless vendor pretends to have no models');
 const option=text=>[...dlg.querySelectorAll('.binding-option')].find(b=>b.textContent.includes(text)),cap=text=>option(text).querySelector('.binding-cap').textContent;
 assert(!dlg.querySelector('.provider-badge')&&cap('scripted')==='不看图 · 本机搜索 · 128K'&&cap('fresh-new')==='不看图 · 本机搜索 · 64K'&&cap('deep-pro')==='可看图 · 厂商搜索 · 500K','row is not one quiet capability line');
 const use=dlgButton('使用此模型');
 assert(use.disabled&&option('scripted').getAttribute('aria-checked')==='true'&&!$('.binding-note').textContent,'current binding is confirmable');
 const search=$('.binding-search');
 search.value='nothing';search.dispatchEvent(new Event('input'));assert(dlg.textContent.includes('没有匹配的模型'),'no-match state missing');
 search.value='pro';search.dispatchEvent(new Event('input'));assert(!option('deep-pro').hidden&&option('scripted').hidden&&!dlg.textContent.includes('没有匹配的模型'),'search does not filter');
 option('deep-pro').click();
 assert(!use.disabled&&$('.binding-note').textContent==='下一轮改用厂商搜索。旧对话还能看。','consequence note is not plain');
 assert(!/web_search|hosted|互斥|绑定/.test(dlg.textContent),'engineering words in picker');
 assert(!fixture.bound,'row click committed');
 $('.binding-dialog').dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true}));
 assert(!$('#modal').hidden&&panel.classList.contains('open'),'Escape in picker closed the layers below');
 // Esc (the dialog's cancel), ✕ and 取消 all drop the staged row; reopening starts from the current binding.
 $('.binding-dialog').dispatchEvent(new Event('cancel',{cancelable:true}));
 assert(!$('.binding-dialog')&&!fixture.bound&&!$('#modal').hidden,'Esc did not close only the picker');
 await pick();await waitFor(()=>document.querySelectorAll('.binding-option').length===3,'picker rows missing');
 [...document.querySelectorAll('.binding-option')].find(b=>b.textContent.includes('fresh-new')).click();
 $('.binding-close').click();assert(!$('.binding-dialog')&&!fixture.bound,'close button did not discard');
 await pick();await waitFor(()=>document.querySelectorAll('.binding-option').length===3,'picker rows missing');
 [...document.querySelectorAll('.binding-option')].find(b=>b.textContent.includes('fresh-new')).click();
 dlgButton('取消').click();assert(!$('.binding-dialog')&&!fixture.bound&&face(drawerModel)==='scripted|fake · Completions','cancel did not discard');
 await pick();await waitFor(()=>document.querySelectorAll('.binding-option').length===3,'picker rows missing');const pickRow=text=>[...document.querySelectorAll('.binding-option')].find(b=>b.textContent.includes(text));
 assert(pickRow('scripted').getAttribute('aria-checked')==='true'&&pickRow('deep-pro').getAttribute('aria-checked')==='false','reopen kept staged row');
 pickRow('deep-pro').click();
 dlgButton('使用此模型').click();
 await waitFor(()=>!$('.binding-dialog'),'picker does not close');
 assert(face(drawerModel)==='deep-pro|DeepSeek · Responses · 厂商搜索','drawer face not repainted with the vendor display name');
 assert(fixture.bound.provider==='deep'&&fixture.bound.model==='deep-pro','binding not saved');
 await waitFor(()=>$('.tab-body .binding-trigger .bt-model')?.textContent==='deep-pro','details face not refreshed');
 assert(face($('.tab-body .binding-trigger'))==='deep-pro|DeepSeek · Responses · 厂商搜索','details face shows the raw provider id');
 assert(!$('#modal').hidden,'rebind closed the drawer');
 escape();await sleep(20);assert(document.activeElement===$('#panel-actions button'),'edit focus not restored');
 $('#panel-scrim').click();await sleep(250);finishAnimations();
 assert(!panel.classList.contains('open'),'scrim does not close drawer');
 assert(!chip.classList.contains('on')&&chip.getAttribute('aria-expanded')==='false','chip keeps open state after scrim');
 chip.click();assert(panel.classList.contains('open'),'chip does not reopen');
 // Hit-test like a real pointer: the open scrim must not swallow the chip.
 await sleep(250);finishAnimations();
 const cr=rect(chip),hit=document.elementFromPoint(cr.x+cr.width/2,cr.y+cr.height/2);
 assert(chip.contains(hit)||panel.contains(hit),'scrim covers the chip');
 (chip.contains(hit)?hit:chip).click();await sleep(250);finishAnimations();
 assert(!panel.classList.contains('open')&&!chip.classList.contains('on'),'chip does not close panel');
 const closing=new DOMMatrix(getComputedStyle(panel).transform);
 assert(closing.m41>0&&near(closing.m42,0),'drawer exits downwards');
 // Right-click an unselected Bot: every action belongs to that Bot.
 context('另一个Bot');assert(item('复制 Bot ID'),'row menu missing');
 assert($('#title').textContent.includes('2号小伙子'),'context menu changed conversation');
 item('复制 Bot ID').click();await sleep(20);assert(fixture.copied==='另一个Bot','copied the selected Bot instead of target');
 context('另一个Bot');item('移到分组').click();assert($('.menu-sub:not([hidden])'),'move submenu missing');
 context('2号小伙子');assert(!$('.menu-sub:not([hidden])')&&document.querySelectorAll('.panel-more').length===1,'old menu/submenu leaked');
 assert(item('取消置顶'),'pinned tile does not use its own state');
 escape();assert(!$('.panel-more'),'Escape did not close menu');
 context('另一个Bot');document.body.click();assert(!$('.panel-more'),'outside click did not close menu');
 // A remote row can share a local name; it must not expose local destructive actions.
 const remote=document.createElement('button');remote.dataset.bot='另一个Bot';$('#sidebar').append(remote);
 remote.dispatchEvent(new MouseEvent('contextmenu',{bubbles:true,cancelable:true}));
 assert(!$('.panel-more'),'remote row opened local Bot menu');remote.remove();
 // Movement/cancel/short taps never open a long-press menu.
 pointer('pointerdown');pointer('pointermove',40);await sleep(550);assert(!$('.panel-more'),'scroll opened a menu');
 pointer('pointerdown');pointer('pointercancel');await sleep(550);assert(!$('.panel-more'),'cancel opened a menu');
 pointer('pointerdown');pointer('pointerup');await sleep(550);assert(!$('.panel-more'),'short tap opened a menu');
 pointer('pointerdown');await sleep(1600);pointer('pointerup');row('另一个Bot').click();
 assert($('.panel-more')&&$('#title').textContent.includes('2号小伙子'),'long press selected the Bot or lost menu');
 escape();context('另一个Bot');item('置顶').click();
 await waitFor(()=>fixture.calls.some(([p,o])=>p.includes(encodeURIComponent('另一个Bot'))&&p.endsWith('/pin')&&o.method==='POST'),'pin hit wrong Bot');
 if(innerWidth>=768){
  $('#collapse').click();
  assert(near(rect($('#sidebar')).width,72),'rail width changed');
  for(const head of document.querySelectorAll('.folder-head,.group-label,.g7-side-head')){
   const style=getComputedStyle(head);
   assert(near(rect(head).height,10)&&style.backgroundColor==='rgba(0, 0, 0, 0)'&&parseFloat(style.borderRadius)===0,'rail header is a gray block');
   assert([...head.children].every(n=>getComputedStyle(n).display==='none'),'rail header has leftover controls');
  }
 }
 assert(testErrors.length===0,testErrors.join('; '));
 parent.document.body.dataset.testResult='passed';
})().catch(e=>{parent.document.body.dataset.testResult='failed: '+e.message+'; '+testErrors.join('; ')});
`
