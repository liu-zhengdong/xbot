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

// The HTTP fixture supplies sequence windows; the shipped loadThread, paging,
// search and keyed transcript projection run together in a real local Chrome.
func TestTranscriptWindowBrowser(t *testing.T) {
	runTodoBrowser(t, transcriptWindowFixture, transcriptWindowDriver)
}

func TestTranscriptWindowOpeningBrowser(t *testing.T) {
	runTodoBrowser(t, transcriptWindowFixture+`historyFixture.holdTranscript=true;`, transcriptWindowOpeningDriver)
}

func TestTranscriptWindowWaitingBrowser(t *testing.T) {
	runTodoBrowser(t, transcriptWindowFixture+`historyFixture.deferTranscript=true;`, transcriptWindowWaitingDriver)
}

func TestTranscriptWindowReducedMotionBrowser(t *testing.T) {
	runTranscriptReducedMotionBrowser(t, transcriptWindowFixture+`historyFixture.deferTranscript=true;window.expectedReducedMotion=true;`, transcriptWindowWaitingDriver)
}

func TestTranscriptWindowIsolatedDeliveredBrowser(t *testing.T) {
	runTodoBrowser(t, transcriptWindowFixture+transcriptWindowIsolatedDeliveredFixture, transcriptWindowIsolatedDeliveredDriver)
}

func TestTranscriptWindowInitialFailureBrowser(t *testing.T) {
	runTodoBrowser(t, transcriptWindowFixture+`historyFixture.failTranscript=true;`, transcriptWindowInitialFailureDriver)
}

const transcriptWindowFixture = `
localStorage.removeItem('xbot-panel-open');localStorage.removeItem('xbot.side');
const historyFixture={requests:[],searches:[],version:1,failTranscript:false,holdSearch:false,releaseSearch:null,holdTranscript:false,releaseTranscript:null,deferTranscript:false,pendingTranscripts:{}};
fixture.messages[bots[0].name]=Array.from({length:420},(_,i)=>({seq:i+1,role:'user',content:'历史行 '+String(i+1).padStart(3,'0')+(i===64?' needle 历史线索':''),time:'2026-10-30T10:00:00Z'}));
fixture.messages[bots[0].name][64]={seq:65,role:'assistant',content:'',tool_calls:[{id:'delivered-history',function:{name:'send_message_to_user',arguments:JSON.stringify({text:'needle 历史线索'})}}],time:'2026-10-30T10:00:00Z'};
fixture.messages[bots[0].name][65]={seq:66,role:'tool',content:"sent to the owner's chat",tool_call_id:'delivered-history',time:'2026-10-30T10:00:00Z'};
bots[1].avatar='custom';bots[1].avatar_url='data:image/svg+xml;charset=utf-8,'+encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg" width="52" height="52"><rect width="52" height="52" fill="#777"/></svg>');
fixture.messages[bots[1].name]=[{seq:1,role:'user',content:'另一个 Bot 的独立对话',time:'2026-10-31T10:00:00Z'}];
const historyBaseFetch=window.fetch;
window.fetch=async(path,opts={})=>{
 const url=new URL(String(path),'http://fixture.local'),match=url.pathname.match(/^\/api\/bots\/([^/]+)\/transcript(\/search)?$/);
 if(!match)return historyBaseFetch(path,opts);
 const name=decodeURIComponent(match[1]),q=url.searchParams,all=fixture.messages[name]||[];
 if(match[2]){
  const query=q.get('q')||'';historyFixture.searches.push({name,query});
  if(historyFixture.holdSearch){await new Promise(r=>historyFixture.releaseSearch=r)}
  if(query==='error')return new Response(JSON.stringify({error:'scripted search failure'}),{status:500,headers:{'Content-Type':'application/json'}});
  return json({matches:query==='needle'?[{seq:65,preview:'needle 历史线索'}]:[]});
 }
 historyFixture.requests.push({name,before:Number(q.get('before')||0),after:Number(q.get('after')||0),around:Number(q.get('around')||0)});
 if(historyFixture.deferTranscript)await new Promise(resolve=>historyFixture.pendingTranscripts[name]=resolve);
 if(historyFixture.failTranscript)return new Response(JSON.stringify({error:'scripted transcript failure'}),{status:500,headers:{'Content-Type':'application/json'}});
 const limit=Number(q.get('limit')||60),total=all.length,before=Number(q.get('before')||0),after=Number(q.get('after')||0),around=Number(q.get('around')||0);
 let lo=Math.max(1,total-limit+1),hi=total;
 if(before){hi=Math.min(total,before-1);lo=Math.max(1,hi-limit+1)}
 if(after){lo=after+1;hi=Math.min(total,lo+limit-1)}
 if(around){lo=Math.max(1,around-Math.floor(limit/2));hi=Math.min(total,lo+limit-1)}
 const messages=all.filter(m=>m.seq>=lo&&m.seq<=hi),head=messages[0]?.seq||0,tail=messages.at(-1)?.seq||0;
 const tag='W/"history-'+historyFixture.version+'-'+encodeURIComponent(name)+'-'+url.search+'"';
 if(opts.headers?.['If-None-Match']===tag)return new Response(null,{status:304,headers:{ETag:tag}});
 const body=JSON.stringify({messages,window:{head_seq:head,tail_seq:tail,total,next_before:head,next_after:tail,has_more_before:head>1,has_more_after:tail<total}});
 if(historyFixture.holdTranscript){
  historyFixture.holdTranscript=false;
  const split=Math.floor(body.length/2),enc=new TextEncoder();
  return new Response(new ReadableStream({start(controller){
   controller.enqueue(enc.encode(body.slice(0,split)));
   historyFixture.releaseTranscript=()=>{controller.enqueue(enc.encode(body.slice(split)));controller.close()};
   queueMicrotask(()=>document.dispatchEvent(new Event('history-transcript-partial')));
  }}),{headers:{'Content-Type':'application/json',ETag:tag}});
 }
 return new Response(body,{headers:{'Content-Type':'application/json',ETag:tag}});
};
`

const transcriptWindowDriver = `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const waitFor=async(fn,msg)=>{for(let i=0;i<1000;i++){if(fn())return;await sleep(10)}throw Error(msg)};
const rows=()=>Array.from(document.querySelectorAll('#thread [data-transcript-key^="seq:"]')).filter(n=>n.classList.contains('msg'));
const row=seq=>$('#thread [data-transcript-key="seq:'+seq+':0"]');
const refresh=()=>document.dispatchEvent(new Event('visibilitychange'));
const search=async query=>{if($('#history-search-panel').hidden)$('#chat-search').click();const box=$('#history-search');box.value=query;box.dispatchEvent(new Event('input',{bubbles:true}));$('#history-form').requestSubmit();await sleep(30)};
(async()=>{
 await waitFor(()=>row(420)&&$('#history-older'),'tail did not load');
 assert(!row(1)&&rows().length===60,'initial load was not the bounded tail');
 assert(historyFixture.requests.some(r=>!r.before&&!r.after&&!r.around),'default tail request missing');
 const anchor=row(361),thread=$('#thread');thread.scrollTop=40;await sleep(30);
 const y=anchor.getBoundingClientRect().top;
 $('#history-older').click();await waitFor(()=>row(301),'older did not prepend');await sleep(30);
 assert(row(361)===anchor,'prepend replaced the existing anchor node');
 assert(Math.abs(anchor.getBoundingClientRect().top-y)<4,'prepend moved the reading anchor');
 assert(historyFixture.requests.some(r=>r.before===361),'older did not use exclusive head seq');
 const scrollReads=historyFixture.requests.length;thread.scrollTop=40;thread.dispatchEvent(new Event('scroll'));await sleep(30);thread.scrollTop=0;thread.dispatchEvent(new Event('scroll'));
 await waitFor(()=>historyFixture.requests.length>scrollReads&&row(241),'scrolling to top did not load older history');
 assert(historyFixture.requests.at(-1).before===301,'top scroll used the wrong before cursor');
 for(let i=0;i<4;i++){const n=historyFixture.requests.length;$('#history-older').click();await waitFor(()=>historyFixture.requests.length>n,'older request missing');await sleep(40)}
 assert(row(1)&&rows().length<=300,'oldest page did not load or DOM grew beyond window cap');
 assert($('#history-newer')&&!$('#history-newer').hidden,'trimmed newer history cannot be reached');
 $('#history-newer').click();await waitFor(()=>row(301),'newer page did not reconnect trimmed history');
 assert(rows().length<=300,'newer page exceeded DOM window cap');
 $('#chat-search').click();await waitFor(()=>$('#history-form')&&!$('#history-form').hidden,'search did not open');
 await search('needle');await waitFor(()=>$('#history-results button[data-seq="65"]'),'search hit missing');
 assert($('#history-results').textContent.includes('历史线索'),'search omitted useful preview');
 $('#history-results button[data-seq="65"]').click();await waitFor(()=>row(65)&&!row(301),'hit did not replace with nearby window');
 assert($('#history-search-panel').hidden,'hit did not close search popover');
 assert(historyFixture.requests.some(r=>r.around===65),'hit did not request around target seq');
 assert(!$('#history-older').hidden&&!$('#history-newer').hidden,'located history cannot page both ways');
 const first=row(65),top=thread.scrollTop;
 fixture.messages[bots[0].name].push({seq:421,role:'user',content:'最新追加的消息',time:'2026-10-31T11:00:00Z'});historyFixture.version++;refresh();await sleep(3300);
 assert(row(65)===first&&!row(421)&&Math.abs(thread.scrollTop-top)<4,'poll stole historical reading position');
 $('#history-newer').click();await waitFor(()=>row(96),'cannot page forward from search');
 $('#history-latest').click();await waitFor(()=>row(421)&&!row(65),'return latest did not restore tail');
 const latest=row(421);historyFixture.failTranscript=true;
 $('#history-older').click();await waitFor(()=>$('#history-error')&&!$('#history-error').hidden&&$('#history-error').textContent,'page failure was not reported');
 assert(row(421)===latest,'page failure erased loaded conversation');
 assert(!$('#thread').textContent.includes('连不上 XBot'),'page failure pretended server was offline');
 historyFixture.failTranscript=false;$('#history-older').click();await waitFor(()=>row(302),'failed page was not retryable');
 await search('none');await waitFor(()=>!$('#history-results button[data-seq]')&&$('#history-results').textContent,'empty search left stale hits');
 await search('error');await waitFor(()=>$('#history-results').textContent.includes('失败'),'search failure not distinguishable');
 historyFixture.holdSearch=true;await search('needle');await waitFor(()=>historyFixture.releaseSearch,'slow search not in flight');
 document.querySelector('[data-bot="'+bots[1].name+'"]').click();await waitFor(()=>$('#title').textContent.includes(bots[1].name)&&row(1),'second bot did not load');
 historyFixture.releaseSearch();await sleep(80);
 assert(!$('#history-results button[data-seq="65"]')&&!$('#thread').textContent.includes('历史线索'),'stale search leaked across Bot selection');
 assert(testErrors.length===0,testErrors.join('; '));
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`

const transcriptWindowInitialFailureDriver = `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const waitFor=async(fn,msg)=>{for(let i=0;i<1000;i++){if(fn())return;await sleep(10)}throw Error(msg)};
(async()=>{
 await waitFor(()=>$('.thread-loading-cover .thread-unavailable'),'initial load failure not shown inside cover');
 assert(!$('#thread .msg'),'failed opening exposed real bubbles');
 const failedFace=$('.thread-loading-cover .chat-open-avatar');
 assert(!failedFace||failedFace.getClientRects().length===0,'failure retained the playful Bot avatar');
 assert($('.thread-loading-cover').textContent.includes('对话加载失败'),'initial failure lacks honest loading message');
 assert(!$('.thread-loading-cover').textContent.includes('连不上 XBot'),'initial transcript failure pretends server offline');
 historyFixture.failTranscript=false;historyFixture.deferTranscript=true;$('.thread-loading-cover .thread-unavailable button').click();
 await waitFor(()=>historyFixture.pendingTranscripts[bots[0].name],'retry request did not start');
 assert($('.thread-loading-cover')&&!$('.thread-loading-cover .thread-unavailable'),'retry did not restore waiting cover');
 assert($('.thread-loading-cover .chat-open-avatar')?.getClientRects().length,'retry did not restore Bot avatar');
 historyFixture.deferTranscript=false;historyFixture.pendingTranscripts[bots[0].name]();
 await waitFor(()=>$('#thread [data-transcript-key="seq:420:0"]'),'initial failure retry did not load tail');
 await waitFor(()=>!$('.thread-loading-cover'),'retry retained failure cover');
 assert(!$('.thread-unavailable'),'retry retained failure shell');
 assert(testErrors.length===0,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`

const transcriptWindowOpeningDriver = `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const waitFor=async(fn,msg)=>{for(let i=0;i<1000;i++){if(fn())return;await sleep(10)}throw Error(msg)};
let partialObserved=false,partialError='',observer;
const realPacks=[],directBubbles=[];
// Pending Response streams pause Chrome's virtual clock. Inspect and finish the
// partial response in microtasks so this test does not wait on a paused timer.
document.addEventListener('history-transcript-partial',()=>{
 partialObserved=true;
 try {
  const cover=$('.thread-loading-cover');assert(cover,'opening did not immediately present loading cover');
  assert(cover.textContent.trim(),'loading cover omitted its waiting line');
  assert(cover.querySelector('.chat-open-avatar'),'opening cover omitted Bot avatar');
  const c=cover.getBoundingClientRect(),r=$('#thread').getBoundingClientRect();
  assert(c.left<=r.left+1&&c.right>=r.right-1&&c.top<=r.top+1&&c.bottom>=r.bottom-1,'loading cover did not cover the conversation area');
  assert(!$('#thread .msg'),'opening appended real bubbles before complete JSON body');
  partialObserved=true;
  observer=new MutationObserver(records=>{for(const record of records)for(const node of record.addedNodes){
   if(node.nodeType!==1)continue;
   if(node.matches('.msg'))directBubbles.push(node);
   if(node.matches('.thread-inner')&&node.querySelector('.msg'))realPacks.push(node.querySelectorAll('.msg').length);
  }});
  observer.observe($('#thread'),{childList:true,subtree:true});
 } catch(e) {partialError=e.message;document.body.dataset.testResult='failed: '+e.message}
 queueMicrotask(()=>historyFixture.releaseTranscript());
},{once:true});
(async()=>{
 await waitFor(()=>partialObserved,'partial tail response did not start');
 assert(!partialError,partialError);
 await waitFor(()=>$('#thread [data-transcript-key="seq:420:0"]'),'complete tail did not reveal');await sleep(40);
 observer.disconnect();
 await waitFor(()=>!$('.thread-loading-cover'),'complete window retained loading cover');
 assert(!$('#thread .thread-skeleton'),'success retained obsolete opening skeleton');
 assert(directBubbles.length===0&&realPacks.length===1&&realPacks[0]===60,'opening progressively appended or replaced a partial window');
 assert(testErrors.length===0,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`

// This runner uses Chrome's actual reduced-motion media preference. The app's
// scripts and styles remain unchanged; only HTTP is scripted as in runTodoBrowser.
func runTranscriptReducedMotionBrowser(t *testing.T, fixture, driver string) {
	t.Helper()
	chrome, err := exec.LookPath("/Applications/Google Chrome.app/Contents/MacOS/Google Chrome")
	if err != nil {
		t.Skip("Chrome unavailable")
	}
	dir := t.TempDir()
	err = fs.WalkDir(staticFS, "static", func(path string, d fs.DirEntry, err error) error {
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
			page = strings.Replace(page, "<head>", "<head><script>"+chatStreamFixture+fixture+"</script>", 1)
			page = strings.Replace(page, "</body>", "<script>"+driver+"</script></body>", 1)
			raw = []byte(page)
		}
		return os.WriteFile(dst, raw, 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(chrome, "--headless=new", "--disable-gpu", "--no-sandbox", "--no-first-run", "--no-default-browser-check", "--allow-file-access-from-files", "--force-prefers-reduced-motion", "--user-data-dir="+filepath.Join(t.TempDir(), "chrome"), "--virtual-time-budget=35000", "--dump-dom", (&url.URL{Scheme: "file", Path: filepath.Join(dir, "index.html")}).String()).CombinedOutput()
	if err != nil {
		t.Fatalf("Chrome: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), `data-test-result="passed"`) {
		result := string(out)
		if i := strings.Index(result, "data-test-result="); i >= 0 {
			result = result[i:min(i+1200, len(result))]
		} else if len(result) > 1500 {
			result = result[:1500]
		}
		t.Fatalf("reduced motion browser: %s", result)
	}
}

const transcriptWindowWaitingDriver = `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const waitFor=async(fn,msg)=>{for(let i=0;i<1000;i++){if(fn())return;await sleep(10)}throw Error(msg)};
(async()=>{
 await waitFor(()=>historyFixture.pendingTranscripts[bots[0].name],'opening request did not pause');
 const cover=$('.thread-loading-cover'),first=cover?.textContent;
 assert(cover&&first.trim(),'waiting cover missing line');
 const botAvatar=cover.querySelector('.chat-open-avatar');
 const av=botAvatar?.querySelector('.avatar');
 assert(av&&av.dataset.tip===bots[0].name&&av.querySelector('svg'),'waiting cover omitted the selected Bot avatar');
 const avatarStyle=getComputedStyle(botAvatar);
 assert(Math.abs(parseFloat(avatarStyle.width)-52)<0.1&&Math.abs(parseFloat(avatarStyle.height)-52)<0.1,'Bot avatar is not the approved 52px size');
 assert(!$('#thread .msg')&&!$('#thread .thread-skeleton'),'waiting exposed real or skeleton bubbles');
 const reduced=matchMedia('(prefers-reduced-motion: reduce)').matches;
 if(window.expectedReducedMotion)assert(reduced,'Chrome did not enable the reduced-motion media preference');
 if(!reduced){
  assert(cover.getAnimations({subtree:true}).length>0,'normal companion omitted its entrance animation');
  assert(botAvatar.getAnimations().length>0,'Bot avatar omitted its gentle sway');
 }
 if(reduced){
  assert(cover.getAnimations({subtree:true}).length===0,'reduced-motion cover animates');
  assert([...cover.querySelectorAll('*')].every(n=>getComputedStyle(n).animationName==='none'),'reduced-motion text has CSS animation');
 }
 let sawCharacterMotion=false;
 const start=performance.now();await waitFor(()=>{
  if([...cover.querySelectorAll('.banter-char')].some(ch=>ch.getAnimations().length>0))sawCharacterMotion=true;
  return cover.textContent!==first;
 },'long wait did not rotate banter');
 const elapsed=performance.now()-start;
 assert(elapsed>=2400&&elapsed<=3300,'banter rotation is outside the 2.5–3 second cadence');
 assert(!$('#thread .msg'),'long wait progressively appended real bubbles');
 assert(cover.querySelector('.chat-open-avatar')===botAvatar,'banter rotation replaced the Bot avatar');
 if(!reduced)assert(sawCharacterMotion,'waiting banter omitted its occasional character motion');
 if(reduced)assert(cover.getAnimations({subtree:true}).length===0,'reduced-motion banter rotation animates');
 const releaseFirst=historyFixture.pendingTranscripts[bots[0].name],oldLine=cover.textContent;
 document.querySelector('[data-bot="'+bots[1].name+'"]').click();
 await waitFor(()=>historyFixture.pendingTranscripts[bots[1].name],'switch did not request second Bot');
 const next=$('.thread-loading-cover');
 assert(next&&next.textContent.trim()&&next.textContent!==oldLine,'switch did not present fresh banter');
 const switched=next.querySelector('.chat-open-avatar .avatar');
 assert(switched?.dataset.tip===bots[1].name,'switch did not use the newly selected Bot avatar');
 assert(switched.querySelector('img')?.src===bots[1].avatar_url,'loading cover did not reuse the selected Bot uploaded avatar');
 assert(!$('#thread .msg'),'switch exposed old Bot bubbles before new tail');
 releaseFirst();await sleep(80);
 assert($('.thread-loading-cover')===next&&!$('#thread .msg'),'stale tail response removed current Bot cover');
 historyFixture.deferTranscript=false;historyFixture.pendingTranscripts[bots[1].name]();
 await waitFor(()=>$('#thread [data-transcript-key="seq:1:0"]'),'second Bot tail did not reveal');
 const leaving=$('.thread-loading-cover');
 if(reduced){
  assert(!leaving||leaving.getAnimations({subtree:true}).length===0,'reduced-motion reveal animates');
 }else{
  assert(leaving,'normal reveal skipped cover fade');
  const durations=getComputedStyle(leaving).transitionDuration.split(',').map(v=>parseFloat(v));
  assert(durations.some(n=>n>=0.28&&n<=0.36),'normal reveal lacks 280–360ms opacity fade');
 }
 await waitFor(()=>!$('.thread-loading-cover'),'ready tail retained cover');
 assert(!$('#thread').textContent.includes('历史行'),'old Bot tail leaked into new Bot');
 assert(testErrors.length===0,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`

const transcriptWindowIsolatedDeliveredFixture = `
fixture.messages[bots[0].name]=[{seq:1,role:'assistant',content:'',tool_calls:[{id:'isolated-delivery',function:{name:'send_message_to_user',arguments:JSON.stringify({text:'单行窗口里的已交付回复'})}}],delivered:['isolated-delivery'],time:'2026-10-30T10:00:00Z'}];
`

const transcriptWindowIsolatedDeliveredDriver = `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
(async()=>{
 for(let i=0;i<1000&&!$('#thread .msg.bot');i++)await sleep(10);
 assert($('#thread .msg.bot')?.textContent.includes('单行窗口里的已交付回复'),'isolated delivered call disappeared without in-window result');
 assert(testErrors.length===0,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`

func TestTranscriptWindowPeerReturnBrowser(t *testing.T) {
	runTodoBrowser(t, transcriptWindowFixture+transcriptWindowPeerReturnFixture, transcriptWindowPeerReturnDriver)
}

const transcriptWindowPeerReturnFixture = `
fixture.messages[bots[0].name][64]={seq:65,role:'user',content:'<untrusted_event source="bot:另一个Bot" sender="另一个Bot" type="message">\npeer content\n</untrusted_event>',time:'2026-10-30T10:00:00Z'};
const peerReviewFetch=window.fetch;
window.fetch=(path,opts)=>String(path).includes('/peers/')?Promise.resolve(json({messages:[{id:'peer1',from_bot:'另一个Bot',from:'另一个Bot',text:'peer-only-review-marker',ts:'2026-10-30T10:00:00Z'}]})):peerReviewFetch(path,opts);
`

const transcriptWindowPeerReturnDriver = `
const $=s=>document.querySelector(s),sleep=ms=>new Promise(r=>setTimeout(r,ms)),assert=(v,m)=>{if(!v)throw Error(m)};
const waitFor=async(fn,msg)=>{for(let i=0;i<1000;i++){if(fn())return;await sleep(10)}throw Error(msg)};
(async()=>{
 await waitFor(()=>$('#thread [data-seq="420"]'),'tail');$('#chat-search').click();$('#history-search').value='needle';$('#history-form').requestSubmit();
 await waitFor(()=>$('#history-results button[data-seq="65"]'),'hit');$('#history-results button[data-seq="65"]').click();
 await waitFor(()=>$('#thread button[data-peer]')&&$('#thread [data-seq="70"]'),'historical peer event');
 const thread=$('#thread');$('#thread [data-seq="70"]').scrollIntoView({block:'center',behavior:'instant'});await sleep(30);
 const oldTop=thread.scrollTop,oldY=$('#thread [data-seq="70"]').getBoundingClientRect().top;
 $('#thread button[data-peer]').click();await waitFor(()=>thread.textContent.includes('peer-only-review-marker'),'peer opened');
 $('#peer-close').click();
 await waitFor(()=>!thread.textContent.includes('peer-only-review-marker')&&$('#thread [data-transcript-key]'),'closing peer from historical window left peer DOM under owner chat');
 assert($('#thread [data-seq="70"]')&&!$('#thread [data-seq="420"]'),'peer close did not restore the historical window');
 assert(!$('#history-older').hidden&&!$('#history-newer').hidden&&!$('#history-latest').hidden,'peer close lost historical navigation');
 assert(Math.abs(thread.scrollTop-oldTop)<4&&Math.abs($('#thread [data-seq="70"]').getBoundingClientRect().top-oldY)<4,'peer close lost historical reading position');
 await sleep(3300);assert($('#thread [data-seq="70"]')&&!$('#thread [data-seq="420"]'),'poll after peer close stole restored history');
 assert(testErrors.length===0,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>document.body.dataset.testResult='failed: '+e.message);
`
