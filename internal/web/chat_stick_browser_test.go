package web

import (
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestChatStickBrowser(t *testing.T) {
	runChatFramesBrowser(t, transcriptWindowFixture, chatStickDriver)
}

func runChatFramesBrowser(t *testing.T, fixture, driver string) {
	t.Helper()
	// ResizeObserver/rAF need real frames, not dump-dom's virtual timers.
	chrome := "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
	if _, err := os.Stat(chrome); err != nil {
		t.Skip("Chrome unavailable")
	}
	assets, err := fs.Sub(staticFS, "static")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	page := strings.Replace(string(raw), "<head>", "<head><script>"+chatStreamFixture+fixture+"</script>", 1)
	page = strings.Replace(page, "</body>", "<script>"+driver+"</script></body>", 1)
	files := http.FileServer(http.FS(assets))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, page)
			return
		}
		files.ServeHTTP(w, r)
	}))
	defer server.Close()
	out, err := exec.Command("node", filepath.Join("testdata", "chat-stick.cjs"), chrome, server.URL, filepath.Join(t.TempDir(), "chrome")).CombinedOutput()
	if err != nil {
		t.Fatalf("stick browser: %v %s", err, out)
	}
	t.Log(strings.TrimSpace(string(out)))
}

const chatStickDriver = `
const $=s=>document.querySelector(s),assert=(v,m)=>{if(!v)throw Error(m)};
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const waitFor=async(f,m)=>{for(let i=0;i<1000;i++){if(f())return;await sleep(10)}throw Error(m)};
const row=n=>$('#thread [data-seq="'+n+'"]');
const refresh=()=>document.dispatchEvent(new Event('visibilitychange'));
(async()=>{
 await waitFor(()=>row(420)&&!$('.thread-loading-cover'),'opening failed');await sleep(80);
 const thread=$('#thread'),gap=()=>thread.scrollHeight-thread.clientHeight-thread.scrollTop;
 assert(gap()<3,'opening did not stick');
 // A late media/layout expansion must not turn stick into historical reading.
 row(420).style.minHeight='900px';await sleep(100);
 assert(gap()<3,'late height growth lost stick');
 fixture.messages[bots[0].name].push({seq:421,role:'user',content:'stick append',time:'2026-10-31T11:00:00Z'});historyFixture.version++;refresh();
 await waitFor(()=>row(421),'stick poll froze after layout growth');await sleep(80);
 assert(gap()<3,'stick append did not follow');
 thread.scrollTop-=500;thread.dispatchEvent(new Event('scroll'));await sleep(50);
 const top=thread.scrollTop;
 row(421).style.minHeight='1100px';await sleep(100);
 assert(Math.abs(thread.scrollTop-top)<3,'observer pulled detached reader');
 fixture.messages[bots[0].name].push({seq:422,role:'user',content:'detached append',time:'2026-10-31T11:01:00Z'});historyFixture.version++;refresh();await sleep(3300);
 assert(Math.abs(thread.scrollTop-top)<3,'poll pulled detached reader');
 assert(!$('#history-latest').hidden,'new tail not reachable');
 $('#history-latest').click();await waitFor(()=>row(422),'latest did not reach server tail');await sleep(100);
 assert(gap()<3,'latest did not restore stick');
 thread.scrollTop=40;thread.dispatchEvent(new Event('scroll'));await sleep(50);
 const anchor=row(363),y=anchor.getBoundingClientRect().top;
 $('#history-older').click();await waitFor(()=>row(303),'prepend failed');await sleep(100);
 const anchorDelta=Math.abs(anchor.getBoundingClientRect().top-y);
 assert(anchorDelta<4,'prepend lost anchor');
 // Native scroll back to the loaded tail re-enters stick without a button.
 thread.scrollTop=thread.scrollHeight;thread.dispatchEvent(new Event('scroll'));await sleep(50);
 const box=$('#input');box.textContent='stream stick';box.dispatchEvent(new Event('input'));$('#composer').requestSubmit();
 await waitFor(()=>fixture.streams.length&&$('#thread .live'),'stream did not start');await sleep(50);
 assert(gap()<3,'optimistic/live append lost stick');
 const stream=fixture.streams[0];
 emit(stream,'result',{name:'send_message_to_user',text:'流式追加。'.repeat(200)});await sleep(100);
 assert(gap()<3,'stream append lost stick');
 thread.scrollTop-=500;thread.dispatchEvent(new Event('scroll'));await sleep(50);
 const streamTop=thread.scrollTop;
 emit(stream,'result',{name:'send_message_to_user',text:'上翻后不要拉回。'.repeat(200)});await sleep(100);
 assert(Math.abs(thread.scrollTop-streamTop)<3,'stream pulled detached reader');
 const frames=[];for(let i=0;i<20;i++){const start=performance.now();await new Promise(requestAnimationFrame);frames.push(performance.now()-start)}
 frames.sort((a,b)=>a-b);
 document.body.dataset.stickPerformance=JSON.stringify({anchorDeltaPx:anchorDelta,frameP95ms:frames[18],samples:20});
 assert(testErrors.length===0,testErrors.join('; '));
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`
