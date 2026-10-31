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

// Exercise the shipped app. Only HTTP and the model stream are scripted.
func TestTodoBrowser(t *testing.T) {
	runTodoBrowser(t, todoBrowserFixture, todoBrowserDriver)
}

func TestTodoStreamingBrowser(t *testing.T) {
	runTodoBrowser(t, todoBrowserFixture+todoStreamingFixture, todoStreamingDriver)
}

func TestTodoLiveToolsBrowser(t *testing.T) {
	runTodoBrowser(t, todoBrowserFixture+todoStreamingFixture, todoLiveToolsDriver)
}

func TestBusyLineBrowser(t *testing.T) {
	runTodoBrowser(t, todoBrowserFixture+todoStreamingFixture, todoLiveSetup+`
(async()=>{
 const stream=await start();
 const check=(label)=>{
  const row=$('#thread .live .live-status'), dots=row?.querySelector('.typing-dots'), name=row?.querySelector('.working-name');
  assert(row&&dots&&row.querySelectorAll('.typing-dots i').length===3,'busy line missing three dots');
  assert(document.querySelectorAll('#thread .live .typing-dots').length===1,'duplicate dots');
  assert((name?.textContent||'')===label,'wrong busy label');
  assert(['flex','inline-flex'].includes(getComputedStyle(row).display),'busy line is not inline flex');
  if(name){const a=dots.getBoundingClientRect(),b=name.getBoundingClientRect();assert(b.left>=a.right&&Math.abs((a.top+a.bottom-b.top-b.bottom)/2)<2,'label is not beside dots');}
 };
 check('');
 emit(stream,'part',{hosted:{id:'search',name:'web_search',status:'in_progress',query:'资料'}});
 await sleep(20);assert(!$('#thread .live .hs-step'),'hosted progress added a second busy row');
 emit(stream,'tool',{name:'shell',summary:'继续看资料\nsecret command'});
 await waitFor(()=>working()?.textContent==='继续看资料','description missing');check('继续看资料');
 todoStatus.state='running';todoStatus.tool='shell';todoStatus.todo='旧待办';todoStatus.tool_summary='继续看资料\nsecret command';
 await waitFor(()=>preview().textContent==='继续看资料','sidebar description missing');
 emit(stream,'tool',{name:'shell',summary:''});todoStatus.tool_summary='';
 await waitFor(()=>!working()&&preview().textContent==='在处理','empty description leaked another label');check('');
 assert(!/shell|secret command|旧待办/.test($('#thread .live').textContent+preview().textContent),'non-description leaked');
 finish(stream,'查好了');await waitFor(()=>!$('#thread .live'),'stream did not finish');
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`)
}

func runTodoBrowser(t *testing.T, fixture, driver string) {
	t.Helper()
	chrome, err := exec.LookPath("/Applications/Google Chrome.app/Contents/MacOS/Google Chrome")
	if err != nil {
		t.Skip("Chrome unavailable")
	}
	dir := t.TempDir()
	if evidence := os.Getenv("XBOT_TODO_EVIDENCE"); evidence != "" {
		dir = filepath.Join(evidence, t.Name(), "fixture")
	}
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
	out, err := exec.Command(chrome, "--headless=new", "--disable-gpu", "--no-sandbox", "--no-first-run", "--no-default-browser-check", "--allow-file-access-from-files", "--user-data-dir="+filepath.Join(t.TempDir(), "chrome"), "--virtual-time-budget=35000", "--dump-dom", (&url.URL{Scheme: "file", Path: filepath.Join(dir, "index.html")}).String()).CombinedOutput()
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
		t.Fatalf("todo browser: %s", result)
	}
}

const todoBrowserFixture = `
localStorage.removeItem('xbot-panel-open');localStorage.removeItem('xbot.side');
bots.forEach(b=>b.pinned=false);
const todoItems=[{id:'a',content:'查第一处',status:'done'},{id:'b',content:'查第二处',status:'in_progress'},{id:'c',content:'查第三处',status:'pending'}];
const todoSummary='进度：查第二处 · 还剩 1 项待办';
const todoStatus={bot:bots[0].name,state:'running',tool:'todo',todo:todoSummary,tool_summary:todoSummary,since:new Date().toISOString()};
let statusReads=0;
const addTodo=(id,items,output)=>{
 fixture.messages[bots[0].name].push({...message('assistant',''),tool_calls:[{id,function:{name:'todo',arguments:JSON.stringify({items})}}]});
 if(output!==undefined)fixture.messages[bots[0].name].push({...message('tool',output),name:'todo',tool_call_id:id});
};
fixture.messages[bots[0].name].push(message('user','帮我查三处再汇总。'));
addTodo('created',todoItems.map(it=>({...it,status:'pending'})),'todo: 3 items (0 in_progress, 3 pending, 0 done, 0 dropped)');
addTodo('ok',todoItems,'todo: 3 items (1 in_progress, 1 pending, 1 done, 0 dropped)');
addTodo('closed',todoItems.map(it=>({...it,status:'done'})),'todo: 3 items (0 in_progress, 0 pending, 3 done, 0 dropped)');
addTodo('drop',[{id:'d',content:'查备用来源',status:'dropped',reason:'来源重复'}],'todo: 1 items (0 in_progress, 0 pending, 0 done, 1 dropped)');
addTodo('fail',[{id:'x',content:'不应显示为已保存',status:'done'}],'error: todo: write failed');
addTodo('wait',[{id:'y',content:'还没写入',status:'in_progress'}]);
fixture.messages[bots[0].name].push(message('assistant','第一处已查好，正在核对第二处。'));
const todoBaseFetch=window.fetch;
window.fetch=async(path,opts={})=>{
 if(path==='/api/bots/status'){statusReads++;return json({bots:[todoStatus]})}
 if(String(path).endsWith('/steps?n=20'))return json({steps:[{tool:'todo',summary:'{"items":[{"status":"in_progress"}]}',state:'running',time:new Date().toISOString()}]});
 return todoBaseFetch(path,opts);
};
`

const todoBrowserDriver = `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const waitFor=async(fn,msg)=>{for(let i=0;i<1200;i++){if(fn())return;await sleep(10)}throw Error(msg)};
(async()=>{
 const preview=()=>$('#roster .item[data-bot="'+bots[0].name+'"] .preview');
 await waitFor(()=>!$('#shell').hidden&&preview()&&!$('.thread-loading-cover'),'app did not boot');
 const shot=new URLSearchParams(location.search).get('shot');
 if(!shot){
  await waitFor(()=>preview().textContent===todoSummary,'sidebar did not consume status summary');
  await waitFor(()=>statusReads>=2,'first settled status poll missing');
  const node=preview(),reads=statusReads;
  await waitFor(()=>statusReads>=reads+2,'two status polls missing');
  assert(preview()===node,'unchanged poll replaced preview');
 }
 preview().click();await waitFor(()=>$('#stp')&&!$('#stp').hidden&&$('#stp li'),'steps did not open');
 if(!shot){assert(!/in_progress|"items"|\btodo\b/.test($('#stp').textContent),'raw todo leaked into steps');}
 if(shot==='steps'){document.body.dataset.testResult='passed';return}
 $('#stp .x').click();
 $('#head-chip').click();await waitFor(()=>$('#panel [role=tab]'),'panel did not open');
 [...document.querySelectorAll('#panel [role=tab]')].find(n=>n.textContent==='轨迹').click();
 await waitFor(()=>document.querySelectorAll('.trace-row').length===6,'trace missing calls');
 if(shot==='default'){document.body.dataset.testResult='passed';return}
 const rows=[...document.querySelectorAll('.trace-row')];rows.forEach(n=>n.click());
 if(shot){document.body.dataset.testResult='passed';return}
 const calls=[...document.querySelectorAll('.trace-call')];
 const shown=n=>{const copy=n.cloneNode(true);copy.querySelectorAll('details').forEach(d=>d.remove());return copy.textContent};
 assert(calls.length===6,'missing expanded calls');
 assert(shown(calls[0]).includes('进度 0/3')&&!shown(calls[0]).includes('已完成 0/'),'new list reads as completed');
 assert(shown(calls[1]).includes('进度 1/3'),'partial list reads as completed');
 for(const it of todoItems)assert(shown(calls[1]).includes(it.content),'saved item absent');
 assert(shown(calls[2]).includes('已完成 3/3'),'completed receipt lost after active list archived');
 assert(shown(calls[3]).includes('来源重复'),'drop reason missing');
 assert(!shown(calls[4]).includes('不应显示为已保存'),'failed input presented as saved');
 assert(!shown(calls[5]).includes('还没写入'),'pending input presented as saved');
 for(const c of calls)assert(!/in_progress|\bpending\b|\btodo\b/.test(shown(c)),'raw status leaked');
 const raw=calls[0].querySelector('details');assert(raw&&!raw.open,'raw call must start folded');raw.querySelector('summary').click();
 assert(raw.open&&raw.textContent.includes('in_progress'),'raw evidence unavailable in trace');
 assert(!$('#thread').textContent.includes('in_progress')&&!$('#thread .trace-call'),'tool body entered conversation');
 todoStatus.state='idle';todoStatus.todo='待办：还有 2 项';
 await waitFor(()=>preview().textContent===todoStatus.todo,'idle pending summary lost');
 todoStatus.todo='';todoStatus.last_message='已汇总三处';
 await waitFor(()=>preview().textContent===todoStatus.last_message,'archived summary did not clear');
 assert(testErrors.length===0,testErrors.join('; '));
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`

const todoStreamingFixture = `
todoStatus.state='idle';todoStatus.todo='';todoStatus.tool='';todoStatus.last_message='上一条闲聊';
`

const todoLiveSetup = `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const waitFor=async(fn,msg)=>{for(let i=0;i<900;i++){if(fn())return;await sleep(10)}throw Error(msg)};
const preview=()=>$('#roster .item[data-bot="'+bots[0].name+'"] .preview');
const working=()=>$('#thread .live .working-name');
const start=async()=>{
 await waitFor(()=>!$('#shell').hidden&&$('#title').textContent.includes(bots[0].name)&&preview()?.textContent==='上一条闲聊'&&!$('.thread-loading-cover'),'app did not boot');
 const input=$('#input');input.textContent='继续查三处';input.dispatchEvent(new Event('input'));$('#composer').requestSubmit();
 await waitFor(()=>fixture.streams.length&&$('#thread .live'),'stream did not start');
 return fixture.streams.at(-1);
};
`

const todoStreamingDriver = todoLiveSetup + `
(async()=>{
 const stream=await start();
 emit(stream,'tool',{name:'todo',summary:todoSummary});
 await waitFor(()=>working(),'live tool missing');
 const reads=statusReads,transcriptReads=fixture.reads;
 todoStatus.state='running';todoStatus.tool='todo';todoStatus.todo=todoSummary;todoStatus.tool_summary=todoSummary;
 const started=performance.now();
 const shot=new URLSearchParams(location.search).get('shot');
 if(shot==='streaming'){await sleep(3500);document.body.dataset.testResult='passed';return}
 await waitFor(()=>statusReads>reads&&preview().matches('.st.run')&&preview().textContent===todoSummary,'streaming poll did not show running todo progress');
 document.body.dataset.progressPollMs=String(Math.round(performance.now()-started));
 assert($('#thread .live')&&fixture.reads===transcriptReads,'poll reloaded the streaming conversation');
 await waitFor(()=>working()?.textContent===todoSummary,'status poll did not update live progress');
 const node=preview(),liveName=working(),stableReads=statusReads;
 await waitFor(()=>statusReads>=stableReads+2,'streaming status poll stopped');
 assert(preview()===node&&working()===liveName,'unchanged streaming poll replaced status nodes');
 todoStatus.tool='shell';emit(stream,'tool',{name:'shell',summary:todoSummary});await sleep(20);
 assert(working()?.textContent===todoSummary,'shell SSE covered readable progress');
 todoStatus.todo='进度：查第三处';todoStatus.tool_summary=todoStatus.todo;
 await waitFor(()=>preview().textContent===todoStatus.todo&&working()?.textContent===todoStatus.todo,'later progress did not reach sidebar and live');
 preview().click();await waitFor(()=>$('#stp li'),'running steps entry did not open');
 assert($('#stp').textContent.includes('待办')&&$('#stp').textContent.includes('正在更新待办'),'running todo step lost human label');
 assert(!/in_progress|"items"|\btodo\b/.test($('#stp').textContent),'raw todo leaked into running steps');
 if(shot==='live-steps'){document.body.dataset.testResult='passed';return}
 $('#stp .x').click();
 todoStatus.todo='';
 todoStatus.tool_summary='跑一遍测试';
 await waitFor(()=>working()?.textContent==='跑一遍测试'&&preview().textContent==='跑一遍测试','cleared progress did not fall back to the description');
 todoStatus.tool_summary='添加一条记忆';emit(stream,'tool',{name:'shell',summary:'添加一条记忆\nxbot memory add hi'});
 await waitFor(()=>working()?.textContent==='添加一条记忆','live progress did not show the first summary line');
 todoStatus.tool_summary='';await waitFor(()=>!working()&&$('#thread .live .live-status')&&preview().textContent==='在处理','no description did not leave the dot alone');
 assert(!/shell|bash|后台命令/.test($('#thread .live .live-status').textContent+preview().textContent),'tool name shown in progress');
 todoStatus.state='idle';todoStatus.tool='';todoStatus.last_message='查好了';finish(stream,'查好了');
 await waitFor(()=>!$('#thread .live')&&$('#thread').textContent.includes('查好了'),'stream did not finish');
 assert(testErrors.length===0,testErrors.join('; '));
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`

const todoLiveToolsDriver = todoLiveSetup + `
(async()=>{
 const stream=await start();
 for(const [tool,summary,label] of [['todo','更新清单','更新清单'],['shell','添加一条记忆\nxbot memory add hi','添加一条记忆']]){
  emit(stream,'tool',{name:tool,summary:summary});
  await waitFor(()=>working()?.textContent===label,'raw '+tool+' leaked into live status');
  todoStatus.state='running';todoStatus.tool=tool;todoStatus.tool_summary=summary.split('\n')[0];
  await waitFor(()=>preview().matches('.st.run')&&preview().textContent===label,'description missing in sidebar');
  assert(!/\btodo\b|\bshell\b|xbot memory/.test(preview().textContent+working().textContent),'raw tool leaked into progress');
 }
 finish(stream,'查好了');
 await waitFor(()=>!$('#thread .live'),'stream did not finish');
 assert(testErrors.length===0,testErrors.join('; '));
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`
