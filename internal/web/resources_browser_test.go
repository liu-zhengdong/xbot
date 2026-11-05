package web

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"xbot/internal/browsertest"
)

// The owner reads real host files and keeps the same conversation location.
func TestResourcesReadingBrowser(t *testing.T) {
	if !runResourcesBrowser(t, resourcesReadingDriver) {
		t.Fatal("resource open did not call the real Server OpenPath with the complete file path")
	}
}

// Sources keep their identity; a quote round trip restores the reading anchor.
func TestResourcesSourcesQuoteBrowser(t *testing.T) {
	runResourcesBrowser(t, resourcesSourcesQuoteDriver)
}

func runResourcesBrowser(t *testing.T, driver string) bool {
	t.Helper()
	chrome := browsertest.Chrome(t)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	note := filepath.Join(dir, "note")
	if err := os.Mkdir(note, 0700); err != nil {
		t.Fatal(err)
	}
	paged := filepath.Join(dir, "paged")
	if err := os.Mkdir(paged, 0700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 83; i++ {
		if err := os.WriteFile(filepath.Join(paged, fmt.Sprintf("file-%03d.txt", i)), []byte("resource page entry\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	skill := filepath.Join(note, "SKILL.md")
	var body strings.Builder
	for line := 1; line <= 180; line++ {
		fmt.Fprintf(&body, "RESOURCE-LINE-%03d 真实临时文件正文，用来检查阅读滚动。\n", line)
	}
	if err := os.WriteFile(skill, []byte(body.String()), 0600); err != nil {
		t.Fatal(err)
	}
	spaced := filepath.Join(dir, "中文 空格笔记.md")
	if err := os.WriteFile(spaced, []byte("RESOURCE-CHINESE-SPACES\n完整中文空格路径。\n"), 0600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "created-after-error.md")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	config, err := json.Marshal(map[string]string{
		"dir": dir, "note": note, "skill": skill, "spaced": spaced, "missing": missing, "home": home,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Save native fetch before chatStreamFixture replaces every API response.
	fixture := `const resourceNativeFetch=window.fetch.bind(window);` + chatStreamFixture + transcriptWindowFixture +
		`const resourcePaths=` + string(config) + `;` + resourcesBrowserFixture
	assets, err := fs.Sub(staticFS, "static")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	page := strings.Replace(string(raw), "<head>", "<head><script>"+fixture+"</script>", 1)
	page = strings.Replace(page, "</body>", "<script>"+resourcesBrowserDriver+driver+"</script></body>", 1)
	var opened atomic.Int32
	s := &Server{Root: t.TempDir(), Owner: "u1", App: true, OpenPath: func(path string) error {
		if path != skill {
			return fmt.Errorf("unexpected resource open path: %q", path)
		}
		opened.Add(1)
		return nil
	}}
	token, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	api := muxOf(s)
	files := http.FileServer(http.FS(assets))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/":
			http.SetCookie(w, &http.Cookie{Name: cookieName, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, page)
		case r.URL.Path == "/api/resources" || r.URL.Path == "/api/resources/open":
			api.ServeHTTP(w, r)
		case r.URL.Path == "/__resource-test/create" && r.Method == http.MethodPost:
			// Repair the real missing file, then let the UI retry the same API.
			if err := os.WriteFile(missing, []byte("RESOURCE-RETRY-RECOVERED\n"), 0600); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			files.ServeHTTP(w, r)
		}
	}))
	defer server.Close()
	out, err := exec.Command("node", filepath.Join("testdata", "chat-stick.cjs"), chrome, server.URL, filepath.Join(t.TempDir(), "chrome")).CombinedOutput()
	if err != nil {
		t.Fatalf("resources browser: %v %s", err, out)
	}
	t.Log(strings.TrimSpace(string(out)))
	return opened.Load() == 1
}

const resourcesBrowserFixture = `
const resourceFixture={requests:[],polls:0,copied:null,holdNext:false,release:null,delivered:0};
Object.defineProperty(navigator,'clipboard',{configurable:true,value:{writeText:async text=>{resourceFixture.copied=text}}});
const resourceMessage=[
 '资源阅读样例。',
 'Home 目录：~/',
 '泛指 SKILL.md 不猜目标。',
 '命令：'+String.fromCharCode(96)+'go test ./...'+String.fromCharCode(96),
 '完整命令：'+String.fromCharCode(96)+'cat '+resourcePaths.skill+String.fromCharCode(96),
 String.fromCharCode(96).repeat(3)+'sh\ncat '+resourcePaths.skill+'\n'+String.fromCharCode(96).repeat(3),
 '中文笔记：'+String.fromCharCode(96)+resourcePaths.spaced+String.fromCharCode(96),
 '[阅读技能文件]('+resourcePaths.skill+')',
 '行号定位：'+resourcePaths.skill+':112',
 '锚点定位：'+resourcePaths.skill+'#L112',
 '[URI文件](file://'+resourcePaths.skill+'#L112-L114)',
 '待重试：'+resourcePaths.missing,
 '资源目录：'+resourcePaths.dir+'/',
 '普通网页：[普通网页](https://example.org/plain)。来源证据。'
].join('\n\n');
fixture.messages[bots[0].name][419]={seq:420,role:'assistant',content:'',reply_to:65,time:'2026-11-04T12:00:00Z',
 parts:[{type:'citation',citation:{title:'资源契约来源',url:'https://example.org/resource-contract',start:0,end:4}}],
 tool_calls:[{id:'resource-say',function:{name:'send_message_to_user',arguments:JSON.stringify({text:resourceMessage})}}]};
fixture.messages[bots[0].name].push({seq:421,role:'tool',content:"sent to the owner's chat",tool_call_id:'resource-say'});
const resourceScriptedFetch=window.fetch;
window.fetch=async(path,options={})=>{
 const url=new URL(typeof path==='string'?path:path.url,location.href);
 if(url.pathname==='/api/resources'||url.pathname==='/api/resources/open'){
  const held=resourceFixture.holdNext;resourceFixture.holdNext=false;
  const response=await resourceNativeFetch(path,options);
  const value=await response.clone().json();
  resourceFixture.requests.push({path:url.searchParams.get('path'),line:Number(url.searchParams.get('line')||1),method:options.method||'GET',status:response.status,value});
  // Delay only delivery. The HTTP request and file read have already completed.
  if(held)await new Promise(resolve=>resourceFixture.release=resolve);
  resourceFixture.delivered++;
  return response;
 }
 const response=await resourceScriptedFetch(path,options);
 if(/\/chat$/.test(url.pathname)){
  resourceFixture.polls++;
  if(response.status===304)return response;
  const value=await response.json();
  for(const row of value.messages||[])if(row.transcript_seq===420)row.reply_to_id=65;
  return new Response(JSON.stringify(value),{status:response.status,headers:response.headers});
 }
 return response;
};
`

const resourcesBrowserDriver = `
const $=s=>document.querySelector(s),assert=(ok,message)=>{if(!ok)throw Error(message)};
const sleep=ms=>new Promise(resolve=>setTimeout(resolve,ms));
const waitFor=async(check,message)=>{for(let i=0;i<800;i++){if(check())return;await sleep(10)}throw Error(message)};
const layer=()=>$('#resource-layer');
const isOpen=()=>{const n=layer();return n&&!n.hidden&&getComputedStyle(n).display!=='none'&&getComputedStyle(n).visibility!=='hidden'};
const links=()=>[...document.querySelectorAll('#thread [data-seq="420"] .resource-link[data-path]')];
const linkTo=path=>links().find(n=>n.dataset.path===path||n.dataset.path===path+'/');
const entry=name=>[...layer().querySelectorAll('.resource-entry')].find(n=>n.textContent.trim()===name||n.textContent.trim()===name+'/');
const clickLink=async node=>{assert(node,'resource link missing');node.focus({preventScroll:true});node.click();await waitFor(isOpen,'resource layer did not open')};
const loaded=async text=>waitFor(()=>isOpen()&&layer().textContent.includes(text),'resource did not show '+text);
const closeResource=async()=>{$('#resource-close').click();await waitFor(()=>!isOpen(),'resource did not close')};
const settle=async()=>{await waitFor(()=>$('#thread [data-seq="420"] .quote-block')&&!$('.thread-loading-cover'),'resource chat did not load');await document.fonts.ready;await sleep(400)};
const checkErrors=()=>assert(testErrors.length===0,testErrors.join('; '));
`

const resourcesReadingDriver = `
(async()=>{
 await settle();
 const thread=$('#thread'),message=$('#thread [data-seq="420"]');
 const input=$('#input');input.textContent='保留未发送的资源阅读草稿';input.dispatchEvent(new Event('input'));
 assert(linkTo(resourcePaths.dir),'bare absolute directory was not linked');
 assert(linkTo('~/'),'home directory was not linked');
 assert(linkTo(resourcePaths.spaced),'backtick Chinese path with spaces was not linked whole');
 const named=links().find(n=>n.textContent==='阅读技能文件');
 assert(named&&named.dataset.path===resourcePaths.skill,'named Markdown file link lost its name or path');
 assert(message.querySelector('pre')&&!message.querySelector('pre .resource-link'),'code block missing or acquired a resource link');
 assert(links().length===8&&!links().some(n=>n.dataset.path==='SKILL.md'||n.textContent.includes('go test')||n.textContent.startsWith('cat ')),'generic filename, command, or code acquired an extra resource link');
 assert(resourceFixture.requests.length===0,'resource was read before its link was clicked');
 const sample=async action=>{
  const y=message.getBoundingClientRect().top,top=thread.scrollTop,height=thread.scrollHeight;
  let delta=0;action();const end=performance.now()+350;
  while(performance.now()<end){await new Promise(requestAnimationFrame);delta=Math.max(delta,Math.abs(message.getBoundingClientRect().top-y))}
  assert(delta<1,'resource open/close shifted message by '+delta+'px');
  assert(Math.abs(thread.scrollTop-top)<1&&thread.scrollHeight===height,'resource open/close changed conversation geometry');return delta;
 };
 const trigger=linkTo(resourcePaths.dir);trigger.focus({preventScroll:true});
 const openShift=await sample(()=>trigger.click());
 await waitFor(()=>isOpen()&&entry('note'),'real temporary directory did not list note');
 assert(resourceFixture.requests.at(-1).status===200&&resourceFixture.requests.at(-1).value.kind==='directory','directory did not use the real API');
 entry('note').focus();entry('note').click();await waitFor(()=>entry('SKILL.md'),'directory navigation did not reach note');
 assert(layer().contains(document.activeElement),'child directory navigation lost keyboard focus');
 entry('SKILL.md').focus();entry('SKILL.md').click();await loaded('RESOURCE-LINE-001');
 assert(layer().contains(document.activeElement),'child file navigation lost keyboard focus');
 assert($('.resource-file')&&$('.resource-line[data-line="1"]'),'real file body has no numbered lines');
 $('#resource-copy').click();
 await waitFor(()=>resourceFixture.copied===resourcePaths.skill&&$('#resource-copy').getAttribute('aria-label')==='已复制','copy did not write the complete file path');
 assert(!resourceFixture.requests.some(r=>r.method==='POST'),'GET or copy unexpectedly opened a host application');
 const panel=layer(),file=layer().querySelector('.resource-file');
 const scroller=[file,...function*(n){while(n&&n!==document.body){n=n.parentElement;yield n}}(file)].find(n=>n&&n.scrollHeight>n.clientHeight+40&&/auto|scroll/.test(getComputedStyle(n).overflowY));
 assert(scroller,'real file fixture must provide a scrollable reader');
 scroller.scrollTop=110;scroller.dispatchEvent(new Event('scroll'));await sleep(50);
 const fileTop=scroller.scrollTop;assert(fileTop>0,'resource reader did not scroll');
 const polls=resourceFixture.polls;
 await waitFor(()=>resourceFixture.polls>=polls+2,'two chat polling cycles did not complete');
 assert(layer()===panel&&layer().querySelector('.resource-file')===file,'chat polling replaced resource nodes');
 assert(Math.abs(scroller.scrollTop-fileTop)<1,'chat polling reset resource body scroll');
 $('#resource-back').focus();$('#resource-back').click();await waitFor(()=>entry('SKILL.md'),'back did not return to note');
 assert(layer().contains(document.activeElement),'file back navigation lost keyboard focus');
 $('#resource-back').focus();$('#resource-back').click();await waitFor(()=>entry('note'),'back did not return to the original directory');
 assert(layer().contains(document.activeElement),'directory back navigation lost keyboard focus');
 entry('paged').focus();entry('paged').click();
 const entries=()=>[...layer().querySelectorAll('.resource-entry')];
 await waitFor(()=>entries().length===40&&$('#resource-more')&&!$('#resource-more').hidden,'real directory first page did not contain 40 entries');
 assert(layer().contains(document.activeElement),'paged child navigation lost keyboard focus');
 $('#resource-more').focus();$('#resource-more').click();
 await waitFor(()=>entries().length===80&&$('#resource-more')&&!$('#resource-more').hidden,'real directory second page did not append to 80 entries');
 assert(document.activeElement===$('#resource-more'),'continuing directory pagination did not restore more-button focus');
 $('#resource-more').focus();$('#resource-more').click();
 await waitFor(()=>entries().length===83&&(!$('#resource-more')||$('#resource-more').hidden),'real directory final page did not append to 83 entries');
 assert(new Set(entries().map(n=>n.textContent.trim())).size===83,'directory pagination repeated or omitted real files');
 const directoryBody=layer().querySelector('.resource-body');
 assert(document.activeElement===directoryBody&&directoryBody.tabIndex===0,'final directory page did not focus the keyboard-readable body');
 assert(directoryBody.scrollHeight>directoryBody.clientHeight&&/auto|scroll/.test(getComputedStyle(directoryBody).overflowY),'final directory body cannot scroll');
 $('#resource-back').focus();$('#resource-back').click();await waitFor(()=>entry('note')&&entry('paged'),'paged back did not restore root directory');
 assert(layer().contains(document.activeElement),'paged back navigation lost keyboard focus');
 const closeShift=await sample(()=>document.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true,cancelable:true})));
 assert(!isOpen()&&document.activeElement===trigger,'Escape did not close and restore link focus');
 await clickLink(linkTo('~/'));
 await waitFor(()=>resourceFixture.requests.at(-1)?.path==='~/'&&resourceFixture.requests.at(-1)?.status===200,'home link did not call the real API');
 assert(resourceFixture.requests.at(-1).value.kind==='directory','home did not read a directory');
 assert(resourceFixture.requests.at(-1).value.path.replace(/\/$/,'')===resourcePaths.home.replace(/\/$/,''),'home resolved to the wrong host directory');
 await closeResource();
 await clickLink(linkTo(resourcePaths.spaced));await loaded('RESOURCE-CHINESE-SPACES');await closeResource();
 await clickLink(named);await loaded('RESOURCE-LINE-001');
 assert($('#resource-open')&&!$('#resource-open').hidden,'real OpenPath capability did not expose open action');
 $('#resource-open').click();
 await waitFor(()=>resourceFixture.requests.some(r=>r.method==='POST'&&r.status>=200&&r.status<300),'open action did not POST to the real resource API');
 if(isOpen())await closeResource();
 const uri=links().find(n=>n.textContent==='URI文件');
 assert(uri&&uri.dataset.path===resourcePaths.skill&&Number(uri.dataset.line)===112,'file URI lost its normalized path or fragment line');
 const lineLinks=links().filter(n=>n.dataset.path===resourcePaths.skill&&Number(n.dataset.line)===112);
 assert(lineLinks.length===3,'file:112, file#L112, and file URI did not all retain line 112');
 for(const node of lineLinks){
  await clickLink(node);await loaded('RESOURCE-LINE-112');
  const target=layer().querySelector('.resource-line.is-highlight[data-line="112"]');
  assert(target&&target.textContent.includes('RESOURCE-LINE-112'),'line 112 was not highlighted');
  if(node===uri)for(const line of [112,113,114]){
   const selected=layer().querySelector('.resource-line.is-highlight[data-line="'+line+'"]');
   assert(selected&&selected.textContent.includes('RESOURCE-LINE-'+line),'file URI range lost highlighted line '+line);
  }
  const bounds=target.getBoundingClientRect(),panelBounds=layer().getBoundingClientRect();
  assert(bounds.bottom>panelBounds.top&&bounds.top<panelBounds.bottom,'requested line is outside the reader');
  assert(resourceFixture.requests.at(-1).line===112,'line locator did not reach the resource API');
  input.focus();input.click();await waitFor(()=>!isOpen(),'clicking composer did not close resource');
  assert(document.activeElement===input,'closing resource stole the new composer focus');
 }
 await clickLink(linkTo(resourcePaths.missing));
 await waitFor(()=>$('#resource-retry')&&!$('#resource-retry').hidden,'missing real file did not offer retry');
 const failed=resourceFixture.requests.at(-1);assert(failed.path===resourcePaths.missing&&failed.status>=400,'missing file API was not a real failure');
 const repair=await resourceNativeFetch('/__resource-test/create',{method:'POST'});assert(repair.status===204,'could not create the real missing file');
 $('#resource-retry').click();await loaded('RESOURCE-RETRY-RECOVERED');
 assert(resourceFixture.requests.at(-1).path===resourcePaths.missing&&resourceFixture.requests.at(-1).status===200,'retry did not reread the repaired file');
 await closeResource();
 resourceFixture.holdNext=true;await clickLink(linkTo(resourcePaths.spaced));
 await waitFor(()=>resourceFixture.release,'real response was not held for bot-switch test');
 $('[data-bot="另一个Bot"]').click();
 await waitFor(()=>$('#title').textContent.includes('另一个Bot')&&!$('.thread-loading-cover'),'bot switch did not settle');
 assert(!isOpen(),'bot switch left the previous resource open');
 const delivered=resourceFixture.delivered;resourceFixture.release();
 await waitFor(()=>resourceFixture.delivered>delivered,'held real response did not finish');await sleep(100);
 assert(!isOpen()&&!$('#thread').textContent.includes('RESOURCE-CHINESE-SPACES'),'late resource response reopened the previous bot reader');
 $('[data-bot="2号小伙子"]').click();await settle();
 assert(input.textContent==='保留未发送的资源阅读草稿','resource interaction or bot switch lost the draft');
 assert(fixture.streams.length===0,'resource reading sent a model message');checkErrors();
 document.body.dataset.stickPerformance=JSON.stringify({resourceOpenShiftPx:openShift,resourceCloseShiftPx:closeShift,polls:resourceFixture.polls-polls});
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`

const resourcesSourcesQuoteDriver = `
(async()=>{
 await settle();
 const thread=$('#thread'),message=$('#thread [data-seq="420"]'),input=$('#input');
 input.textContent='引用阅读后仍保留的草稿';input.dispatchEvent(new Event('input'));
 const ordinary=[...message.querySelectorAll('a')].find(a=>a.href==='https://example.org/plain');
 assert(ordinary&&ordinary.target==='_blank','ordinary web link lost direct new-tab behavior');
 const cite=message.querySelector('.hs-cite-mark');assert(cite,'provider citation mark was not rendered');
 cite.focus({preventScroll:true});cite.click();await loaded('资源契约来源');
 const source=[...layer().querySelectorAll('a')].find(a=>a.href==='https://example.org/resource-contract');
 assert(source&&source.target==='_blank','citation source did not preserve its URL');
 assert(resourceFixture.requests.length===0,'citation source unexpectedly fetched a host resource');
 document.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true,cancelable:true}));
 await waitFor(()=>!isOpen(),'source did not close');assert(document.activeElement===cite,'source close did not return focus');
 thread.scrollTop=Math.max(100,thread.scrollTop-160);thread.dispatchEvent(new Event('scroll'));await sleep(100);
 const viewport=thread.getBoundingClientRect();
 const anchor=[...thread.querySelectorAll('.msg[data-seq]')].find(n=>n.getBoundingClientRect().bottom>viewport.top&&n.getBoundingClientRect().top<viewport.bottom);
 assert(anchor,'quote origin has no visible reading anchor');
 const seq=anchor.dataset.seq,y=anchor.getBoundingClientRect().top,top=thread.scrollTop;
 message.querySelector('.quote-block').click();
 await waitFor(()=>$('#thread [data-seq="65"]')&&$('#quote-return')&&!$('#quote-return').hidden,'quote did not locate the original message with a return action');await sleep(100);
 const original=$('#thread [data-seq="65"]'),bounds=original.getBoundingClientRect(),view=thread.getBoundingClientRect();
 assert(original.textContent.includes('needle 历史线索')&&bounds.bottom>view.top&&bounds.top<view.bottom,'quote did not reveal the original words');
 assert(historyFixture.requests.some(r=>r.around===65),'unloaded quote did not use the existing around-window API');
 $('#quote-return').click();
 await waitFor(()=>$('#thread [data-seq="420"]')&&$('#thread [data-seq="'+seq+'"]'),'quote return did not restore the previous window');await sleep(150);
 const returned=$('#thread [data-seq="'+seq+'"]');
 assert(Math.abs(thread.scrollTop-top)<1,'quote return lost original scrollTop: '+top+' -> '+thread.scrollTop);
 assert(Math.abs(returned.getBoundingClientRect().top-y)<1,'quote return lost the original reading anchor');
 assert(input.textContent==='引用阅读后仍保留的草稿'&&fixture.streams.length===0,'source/quote changed draft or sent a message');
 assert(resourceFixture.requests.length===0,'source/quote triggered a host-file read');checkErrors();
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`
