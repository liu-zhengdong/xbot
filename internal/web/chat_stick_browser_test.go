package web

import (
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"xbot/internal/browsertest"
)

func TestChatStickBrowser(t *testing.T) {
	runChatFramesBrowser(t, transcriptWindowFixture, chatStickDriver)
}

func TestChatStickNearBottomReplyBrowser(t *testing.T) {
	runChatFramesBrowser(t, transcriptWindowFixture+`fixture.seq=420;`, chatStickNearBottomReplyDriver)
}

func TestChatStickAcceptedReplyBrowser(t *testing.T) {
	runChatFramesBrowser(t, transcriptWindowFixture+`fixture.seq=420;`, chatStickAcceptedReplyDriver)
}

func TestChatStickSettlementBrowser(t *testing.T) {
	runChatFramesBrowser(t, transcriptWindowFixture+`fixture.seq=420;`, chatStickSettlementDriver)
}

func TestChatStickHistoryNoticeBrowser(t *testing.T) {
	t.Setenv("XBOT_STICK_VIEWPORT", "320,844")
	runChatFramesBrowser(t, transcriptWindowFixture, chatStickHistoryNoticeDriver)
}

func TestChatStickQuoteBrowser(t *testing.T) {
	runChatFramesBrowser(t, transcriptWindowFixture+`fixture.messages[bots[0].name].at(-1).reply_to=419;`, chatStickQuoteDriver)
}

func runChatFramesBrowser(t *testing.T, fixture, driver string) {
	t.Helper()
	// ResizeObserver/rAF need real frames, not dump-dom's virtual timers.
	chrome := browsertest.Chrome(t)
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

const chatStickNearBottomReplyDriver = `
const $=s=>document.querySelector(s),assert=(v,m)=>{if(!v)throw Error(m)};
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const waitFor=async(f,m)=>{for(let i=0;i<1000;i++){if(f())return;await sleep(10)}throw Error(m)};
(async()=>{
 await waitFor(()=>$('#thread [data-seq="420"]')&&!$('.thread-loading-cover'),'opening failed');await sleep(100);
 const thread=$('#thread'),gap=()=>thread.scrollHeight-thread.clientHeight-thread.scrollTop;
 const box=$('#input');box.textContent='接近底部时接收回复';box.dispatchEvent(new Event('input'));$('#composer').requestSubmit();
 await waitFor(()=>fixture.streams.length&&$('#thread .live'),'send failed');await sleep(100);
 const stream=fixture.streams[0];
 // Deliver before the native scroll event, as a ready SSE reader can do.
 thread.scrollTop-=80;
 say(stream,'第一条回复应自动跟随。'.repeat(200));
 await sleep(100);
 assert(gap()<3,'near-bottom reply lost follow: '+gap());
 // A larger pending upward scroll must still protect historical reading.
 thread.scrollTop-=400;const top=thread.scrollTop;
 say(stream,'上翻阅读时保持位置。'.repeat(200));await sleep(100);
 assert(Math.abs(thread.scrollTop-top)<3,'reply pulled historical reader');
 finish(stream,'第一条回复应自动跟随。'.repeat(200));historyFixture.version++;
 await waitFor(()=>!$('#thread .live'),'stream did not settle');
 assert(testErrors.length===0,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`

const chatStickAcceptedReplyDriver = `
const $=s=>document.querySelector(s),assert=(v,m)=>{if(!v)throw Error(m)};
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const waitFor=async(f,m)=>{for(let i=0;i<1000;i++){if(f())return;await sleep(10)}throw Error(m)};
(async()=>{
 await waitFor(()=>$('#thread [data-seq="420"]')&&!$('.thread-loading-cover'),'opening failed');await sleep(100);
 const thread=$('#thread'),gap=()=>thread.scrollHeight-thread.clientHeight-thread.scrollTop;
 let accept;const baseFetch=window.fetch;
 window.fetch=async(path,options)=>{
  const response=await baseFetch(path,options);
  if(String(path).endsWith('/messages'))await new Promise(resolve=>accept=resolve);
  return response;
 };
 const box=$('#input');box.textContent='收到发送确认后继续跟随';box.dispatchEvent(new Event('input'));$('#composer').requestSubmit();
 await waitFor(()=>accept&&$('#thread .send-state')&&fixture.streams.length,'send failed');await sleep(100);
 assert(gap()<3,'sending state did not follow');const oldTop=thread.scrollTop,stream=fixture.streams[0];
 const observer=new MutationObserver(records=>{
  if(!records.some(r=>[...r.removedNodes].some(n=>n.nodeType===1&&n.classList.contains('send-state'))))return;
  observer.disconnect();
  // Force the status-line shrink to layout before the ready SSE response.
  document.body.dataset.acceptedShrink=String(oldTop-thread.scrollTop);
  say(stream,'发送确认后的长回复。'.repeat(200));
 });
 observer.observe(thread,{childList:true,subtree:true});accept();
 await waitFor(()=>document.body.dataset.acceptedShrink!==undefined,'sending state was not removed');await sleep(100);
 assert(Number(document.body.dataset.acceptedShrink)>3,'fixture did not shrink the scroll position');
 assert(gap()<3,'accepted reply lost follow: '+gap());
 finish(stream,'发送确认后的长回复。'.repeat(200));historyFixture.version++;
 await waitFor(()=>!$('#thread .live'),'stream did not settle');await sleep(100);
 assert(gap()<3,'accepted reply settlement lost follow');
 assert(testErrors.length===0,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`

const chatStickHistoryNoticeDriver = `
const $=s=>document.querySelector(s),assert=(v,m)=>{if(!v)throw Error(m)};
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const waitFor=async(f,m)=>{for(let i=0;i<1000;i++){if(f())return;await sleep(10)}throw Error(m)};
(async()=>{
 await waitFor(()=>$('#thread [data-seq="420"]')&&!$('.thread-loading-cover'),'opening failed');await sleep(100);
 const thread=$('#thread');thread.scrollTop-=400;thread.dispatchEvent(new Event('scroll'));await sleep(100);
 const anchor=[...thread.querySelectorAll('.msg[data-transcript-key]')].find(n=>n.getBoundingClientRect().bottom>thread.getBoundingClientRect().top);
 const y=anchor.getBoundingClientRect().top;
 fixture.messages[bots[0].name].push({seq:421,role:'user',content:'上翻时的新消息',time:'2026-10-31T11:00:00Z'});historyFixture.version++;
 document.dispatchEvent(new Event('visibilitychange'));await waitFor(()=>!$('#history-latest').hidden,'new tail notice missing');await sleep(100);
 const delta=Math.abs(anchor.getBoundingClientRect().top-y);
 assert(delta<3,'new message controls moved reading anchor: '+delta);
 assert(!$('#thread [data-seq="421"]'),'new tail replaced historical reading');
 // Retry/error controls also belong to the same stable reading transaction.
 $('#history-latest').click();await waitFor(()=>$('#thread [data-seq="421"]'),'latest failed');await sleep(100);
 thread.scrollTop-=400;thread.dispatchEvent(new Event('scroll'));await sleep(100);
 const errorAnchor=[...thread.querySelectorAll('.msg[data-transcript-key]')].find(n=>n.getBoundingClientRect().bottom>thread.getBoundingClientRect().top),errorY=errorAnchor.getBoundingClientRect().top;
 historyFixture.failTranscript=true;document.dispatchEvent(new Event('visibilitychange'));
 await waitFor(()=>!$('#history-error').hidden,'poll failure missing');await sleep(100);
 assert(Math.abs(errorAnchor.getBoundingClientRect().top-errorY)<3,'poll failure moved reading anchor');
 assert(testErrors.length===0,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`

const chatStickQuoteDriver = `
const $=s=>document.querySelector(s),assert=(v,m)=>{if(!v)throw Error(m)};
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const waitFor=async(f,m)=>{for(let i=0;i<1000;i++){if(f())return;await sleep(10)}throw Error(m)};
(async()=>{
 await waitFor(()=>$('#thread [data-seq="420"] .quote-block')&&!$('.thread-loading-cover'),'opening failed');await sleep(100);
 const thread=$('#thread'),tail=$('#thread [data-seq="420"]'),target=$('#thread [data-seq="419"]');
 // Leave room for the return control's row within the near-bottom threshold.
 tail.style.minHeight=(thread.clientHeight/2-30)+'px';await sleep(100);
 tail.querySelector('.quote-block').click();await sleep(100);
 const gap=thread.scrollHeight-thread.clientHeight-thread.scrollTop,y=target.getBoundingClientRect().top;
 assert(gap>3&&gap<120,'fixture quote did not land near bottom: '+gap);
 tail.style.minHeight=(thread.clientHeight+500)+'px';await sleep(100);
 assert(Math.abs(target.getBoundingClientRect().top-y)<3,'layout follow pulled reader away from quoted message');
 assert(testErrors.length===0,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`

const chatStickSettlementDriver = `
const $=s=>document.querySelector(s),assert=(v,m)=>{if(!v)throw Error(m)};
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const waitFor=async(f,m)=>{for(let i=0;i<1000;i++){if(f())return;await sleep(10)}throw Error(m)};
(async()=>{
 await waitFor(()=>$('#thread [data-seq="420"]')&&!$('.thread-loading-cover'),'opening failed');await sleep(100);
 const thread=$('#thread'),gap=()=>thread.scrollHeight-thread.clientHeight-thread.scrollTop;
 const send=async text=>{
  const n=fixture.streams.length,box=$('#input');box.textContent=text;box.dispatchEvent(new Event('input'));$('#composer').requestSubmit();
  await waitFor(()=>fixture.streams.length>n&&$('#thread .live'),'send failed');return fixture.streams.at(-1);
 };
 const text='收到，这是一段完整回复。'.repeat(300);
 const stream=await send('收尾后继续跟随');say(stream,text);await sleep(100);
 assert(gap()<3,'live reply did not follow');
 finish(stream,text);historyFixture.version++;
 await waitFor(()=>!$('#thread .live'),'settlement failed');await sleep(100);
 assert(gap()<3,'settlement lost bottom: '+gap());
 fixture.messages[bots[0].name].push(...sayRows('随后主动发来的消息。'.repeat(100)));historyFixture.version++;
 document.dispatchEvent(new Event('visibilitychange'));await sleep(3300);
 assert(thread.textContent.includes('随后主动发来的消息。')&&gap()<3,'poll after settlement lost bottom: '+gap());
 // Returning from history is an explicit request to follow, even if the
 // replacement tail is much shorter than the current expanded window.
 for(let i=0;i<3;i++){$('#history-older').click();await sleep(100)}
 thread.scrollTop=thread.scrollHeight;thread.dispatchEvent(new Event('scroll'));await sleep(100);
 $('#history-older').click();await sleep(100);
 thread.scrollTop-=400;thread.dispatchEvent(new Event('scroll'));await sleep(100);
 fixture.messages[bots[0].name].push(...sayRows('返回最新后这条也应跟随。'.repeat(200)));historyFixture.version++;
 document.dispatchEvent(new Event('visibilitychange'));await sleep(3300);
 assert(!$('#history-latest').hidden,'new tail missing');$('#history-latest').click();await sleep(150);
 assert(gap()<3,'shorter latest window lost bottom: '+gap());
 fixture.messages[bots[0].name].push(...sayRows('下一条主动消息。'));historyFixture.version++;
 document.dispatchEvent(new Event('visibilitychange'));await sleep(3300);
 assert(thread.textContent.includes('下一条主动消息。')&&gap()<3,'latest did not retain follow: '+gap());
 // Finishing while the reader is above the tail must still reconcile SSE
 // rows, without discarding the expanded history or moving its visible row.
 const readingRow=[...thread.querySelectorAll('.msg[data-transcript-key]')].at(-1);
 const detached=await send('上翻阅读时收工');say(detached,text);await sleep(100);
 readingRow.scrollIntoView({block:'center'});thread.dispatchEvent(new Event('scroll'));await sleep(100);
 const anchor=[...thread.querySelectorAll('.msg[data-transcript-key]')].find(n=>n.getBoundingClientRect().bottom>thread.getBoundingClientRect().top);
 assert(anchor,'detached reader has no visible persisted message');
 const y=anchor.getBoundingClientRect().top,oldest=thread.querySelector('.msg[data-transcript-key]');
 finish(detached,text);historyFixture.version++;await sleep(150);
 assert(!thread.querySelector('.msg:not([data-transcript-key])'),'detached settlement left temporary message rows');
 assert(oldest.isConnected,'detached settlement discarded loaded history');
 assert(Math.abs(anchor.getBoundingClientRect().top-y)<3,'detached settlement moved reading anchor');
 // The newly delivered long reply is also a readable location before the
 // server replaces its temporary row with the final projection.
 const liveStream=await send('阅读刚到的长回复');say(liveStream,text);await sleep(100);
 const liveText=[...thread.querySelectorAll('.bot-bubble')].at(-2);
 thread.scrollTop=thread.scrollHeight-thread.clientHeight-500;thread.dispatchEvent(new Event('scroll'));await sleep(100);
 const range=document.createRange();range.setStart(liveText.firstChild.firstChild,200);range.setEnd(liveText.firstChild.firstChild,201);
 const textY=range.getBoundingClientRect().top;
 finish(liveStream,text);historyFixture.version++;await sleep(150);
 const finalText=[...thread.querySelectorAll('.bot-bubble')].at(-1);
 const finalRange=document.createRange();finalRange.setStart(finalText.firstChild.firstChild,200);finalRange.setEnd(finalText.firstChild.firstChild,201);
 assert(Math.abs(finalRange.getBoundingClientRect().top-textY)<3,'settlement moved text inside live reply: '+(finalRange.getBoundingClientRect().top-textY));
 assert(testErrors.length===0,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`

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
