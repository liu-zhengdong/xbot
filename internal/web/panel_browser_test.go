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
 const match=String(path).match(/^\/api\/bots\/([^/]+)$/);
 if(match){
  const name=decodeURIComponent(match[1]);
  if(opts.method==='PATCH'){Object.assign(bots.find(b=>b.name===name),JSON.parse(opts.body));return json({})}
  return json({...bots.find(b=>b.name===name),persona:'You are XBot, a personal assistant. Help the person with the tools granted to you. Be concise and concrete.',model:'scripted',provider:'fake',agents:[],links:[]});
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
  assert(near(m.x,16)&&near(a.x-m.right,8),'hamburger inset or avatar gap wrong');
  assert(near(m.y+m.height/2,a.y+a.height/2)&&m.width>=32&&m.height>=32,'hamburger is not centered or touchable');
 }
 $('#info').click();
 await waitFor(()=>panel.textContent.includes('scripted'),'panel data missing');
 await sleep(250);finishAnimations();
 const opened=rect(panel),openMatrix=new DOMMatrix(getComputedStyle(panel).transform);
 assert(near(closed.width,opened.width)&&near(closed.height,opened.height),'drawer dimensions change on toggle');
 assert(near(openMatrix.m41,0)&&near(openMatrix.m42,0),'open drawer axis wrong');
 assert(getComputedStyle(panel).transitionProperty.includes('transform'),'desktop drawer skips animation');
 assert(!$('#panel-more')&&$('#panel-actions').children.length===1,'header contains extra action');
 assert(!panel.contains($('#info')),'toggle moved into panel');
 const edit=$('#panel-actions button'),avatar=$('#panel-ident .avatar');
 assert(near(rect(edit).y+rect(edit).height/2,rect(avatar).y+rect(avatar).height/2),'edit is not on identity row');
 const model=$('.binding-model'),line=model.parentElement,body=$('.tab-body');
 assert(rect(line).width>=rect(body).width-41,'model still a short card');
 assert($('.psec p').textContent.includes('personal assistant'),'persona missing');
 edit.focus();edit.click();await waitFor(()=>!$('#modal').hidden,'edit does not open');
 escape();await sleep(20);assert(document.activeElement===$('#panel-actions button'),'edit focus not restored');
 $('#panel-scrim').click();await sleep(250);finishAnimations();
 assert(!panel.classList.contains('open'),'scrim does not close drawer');
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
