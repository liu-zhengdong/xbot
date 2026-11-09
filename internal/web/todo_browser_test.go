package web

import (
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"xbot/internal/browsertest"
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
 const check=()=>{
  assert(!$('#thread .live-status, #thread .typing-dots, #thread .working-name'),'duplicate busy status remains in chat');
  const live=$('#thread .live');
  assert(!live||live.getClientRects().length===0,'empty live row still occupies space');
  assert(!live?.querySelector('.msg-time'),'empty live row still has a timestamp');
 };
 check();
 emit(stream,'tool',{name:'shell',summary:''});
 await waitFor(()=>preview().matches('.st.run')&&preview().textContent==='','first empty description invented a sidebar label');
 emit(stream,'part',{hosted:{id:'search',name:'web_search',status:'in_progress',query:'资料'}});
 await sleep(20);check();assert(!$('#thread .live .hs-step'),'hosted progress added a busy row');
 emit(stream,'tool',{name:'shell',summary:'继续看资料\nsecret command'});
 await waitFor(()=>preview().textContent==='继续看资料','sidebar description missing');check();
 todoStatus.state='running';todoStatus.tool='shell';todoStatus.todo='旧待办';todoStatus.tool_summary='继续看资料\nsecret command';
 const reads=statusReads;
 emit(stream,'tool',{name:'shell',summary:''});todoStatus.tool_summary='';
 await waitFor(()=>statusReads>=reads+2,'empty-summary polls missing');check();
 assert(preview().textContent==='继续看资料','empty description cleared last description');
 const side=preview(),sideText=side.querySelector('span');
 emit(stream,'tool',{name:'shell',summary:'核对新资料\nprivate command'});todoStatus.tool_summary='核对新资料';
 await waitFor(()=>sideText.getAnimations().length>0,'description did not transition');
 assert(sideText.textContent==='继续看资料','description hard-switched before fade');
 await waitFor(()=>preview().textContent==='核对新资料','new description missing');
 assert(preview()===side&&side.querySelector('span')===sideText,'description replaced existing sidebar nodes');check();
 if(new URLSearchParams(location.search).has('shot')){document.body.dataset.testResult='passed';return}
 emit(stream,'result',{name:'send_message_to_user',text:'阶段结果'});
 await waitFor(()=>$('#thread').textContent.includes('阶段结果'),'message delivery missing');check();
 assert($('#thread .msg.bot:not(.live) .msg-time'),'delivered message lost its timestamp');
 assert(preview()===side,'message delivery replaced sidebar');
 emit(stream,'tool',{name:'shell',summary:'   '});todoStatus.tool_summary='';
 await waitFor(()=>statusReads>=reads+4,'sticky polls missing');check();
 assert(!/shell|secret command|旧待办/.test($('#thread .live').textContent+preview().textContent),'non-description leaked');
 todoStatus.state='idle';todoStatus.todo='';todoStatus.tool_summary='';todoStatus.last_message='查好了';
 finish(stream,'查好了');await waitFor(()=>!$('#thread .live')&&preview().textContent==='查好了','stream did not finish');
 const input=$('#input');input.textContent='新一轮';input.dispatchEvent(new Event('input'));$('#composer').requestSubmit();
 await waitFor(()=>fixture.streams.length===2&&$('#thread .live'),'new busy period missing');check();
 finish(fixture.streams.at(-1),'新一轮好了');
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`)
}

func runTodoBrowser(t *testing.T, fixture, driver string) {
	t.Helper()
	chrome := browsertest.Chrome(t)
	var err error
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

const todoBrowserDriver = traceExplorerSetup + `
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
 await waitFor(()=>$('#workspace-tabs [role=tab]'),'panel did not open');
 [...document.querySelectorAll('#workspace-tabs [role=tab]')].find(n=>n.textContent==='轨迹').click();
 await waitFor(()=>traceRows().filter(n=>n.textContent.includes('待办')).length===6,'trace missing todo events');
 if(shot==='default'){document.body.dataset.testResult='passed';return}
 const calls=traceRows().filter(n=>n.textContent.includes('待办'));
 const previews=[];
 for(const row of calls){
  await traceOpen(row);await traceTab('概述');
  previews.push(traceDetail().innerText);
  if(shot){document.body.dataset.testResult='passed';return}
  await traceClose();
 }
 assert(previews[0].includes('进度 0/3')&&!previews[0].includes('已完成 0/'),'new list reads as completed');
 assert(previews[1].includes('进度 1/3'),'partial list reads as completed');
 for(const it of todoItems)assert(previews[1].includes(it.content),'saved item absent');
 assert(previews[2].includes('已完成 3/3'),'completed receipt lost after active list archived');
 assert(previews[3].includes('来源重复'),'drop reason missing');
 assert(!previews[4].includes('不应显示为已保存'),'failed input presented as saved');
 assert(!previews[5].includes('还没写入'),'pending input presented as saved');
 for(const text of previews)assert(!/in_progress|\bpending\b/.test(text),'raw status leaked into human progress');
 await traceOpen(calls[1]);await traceTab('原始内容');
 assert(traceDetail().textContent.includes('in_progress')&&traceDetail().textContent.includes('查第二处'),'raw call unavailable in trace');
 await traceClose();await traceOpen(calls[4]);await traceTab('原始内容');
 assert(traceDetail().textContent.includes('write failed')&&traceDetail().textContent.includes('不应显示为已保存'),'failed proposal lost raw evidence');
 await traceClose();
 assert(!$('#thread').textContent.includes('in_progress')&&!$('#thread .trace-explorer'),'tool body entered conversation');
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
const working=()=>preview()?.querySelector('span');
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
 await waitFor(()=>working(),'sidebar tool missing');
 const reads=statusReads,transcriptReads=fixture.reads;
 todoStatus.state='running';todoStatus.tool='todo';todoStatus.todo=todoSummary;todoStatus.tool_summary=todoSummary;
 const started=performance.now();
 const shot=new URLSearchParams(location.search).get('shot');
 if(shot==='streaming'){await sleep(3500);document.body.dataset.testResult='passed';return}
 await waitFor(()=>statusReads>reads&&preview().matches('.st.run')&&preview().textContent===todoSummary,'streaming poll did not show running todo progress');
 document.body.dataset.progressPollMs=String(Math.round(performance.now()-started));
 assert($('#thread .live')&&fixture.reads===transcriptReads,'poll reloaded the streaming conversation');
 await waitFor(()=>working()?.textContent===todoSummary,'status poll did not update sidebar progress');
 const node=preview(),liveName=working(),stableReads=statusReads;
 await waitFor(()=>statusReads>=stableReads+2,'streaming status poll stopped');
 assert(preview()===node&&working()===liveName,'unchanged streaming poll replaced status nodes');
 todoStatus.tool='shell';emit(stream,'tool',{name:'shell',summary:todoSummary});await sleep(20);
 assert(working()?.textContent===todoSummary,'shell SSE covered readable progress');
 todoStatus.todo='进度：查第三处';todoStatus.tool_summary=todoStatus.todo;
 await waitFor(()=>preview().textContent===todoStatus.todo&&working()?.textContent===todoStatus.todo,'later progress did not reach sidebar');
 preview().click();await waitFor(()=>$('#stp li'),'running steps entry did not open');
 assert($('#stp').textContent.includes('待办')&&$('#stp').textContent.includes('正在更新待办'),'running todo step lost human label');
 assert(!/in_progress|"items"|\btodo\b/.test($('#stp').textContent),'raw todo leaked into running steps');
 if(shot==='live-steps'){document.body.dataset.testResult='passed';return}
 $('#stp .x').click();
 todoStatus.todo='';
 todoStatus.tool_summary='跑一遍测试';
 await waitFor(()=>working()?.textContent==='跑一遍测试'&&preview().textContent==='跑一遍测试','cleared progress did not fall back to the description');
 todoStatus.tool_summary='添加一条记忆';emit(stream,'tool',{name:'shell',summary:'添加一条记忆\nxbot memory add hi'});
 await waitFor(()=>working()?.textContent==='添加一条记忆','sidebar progress did not show the first summary line');
 const emptyReads=statusReads;todoStatus.tool_summary='';
 await waitFor(()=>statusReads>=emptyReads+2,'empty summary polls missing');
 assert(working()?.textContent==='添加一条记忆'&&preview().textContent==='添加一条记忆','empty summary cleared last description');
 assert(!/shell|bash|后台命令/.test(preview().textContent),'tool name shown in progress');
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
