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
	chrome, err := exec.LookPath("/Applications/Google Chrome.app/Contents/MacOS/Google Chrome")
	if err != nil {
		t.Skip("Chrome unavailable")
	}
	dir := t.TempDir()
	if evidence := os.Getenv("XBOT_TODO_EVIDENCE"); evidence != "" {
		dir = filepath.Join(evidence, "fixture")
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
			page = strings.Replace(page, "<head>", "<head><script>"+chatStreamFixture+todoBrowserFixture+"</script>", 1)
			page = strings.Replace(page, "</body>", "<script>"+todoBrowserDriver+"</script></body>", 1)
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
const todoStatus={bot:bots[0].name,state:'running',tool:'todo',todo:todoSummary,since:new Date().toISOString()};
let statusReads=0;
const addTodo=(id,items,output)=>{
 fixture.messages[bots[0].name].push({...message('assistant',''),tool_calls:[{id,function:{name:'todo',arguments:JSON.stringify({items})}}]});
 if(output!==undefined)fixture.messages[bots[0].name].push({...message('tool',output),name:'todo',tool_call_id:id});
};
fixture.messages[bots[0].name].push(message('user','帮我查三处再汇总。'));
addTodo('ok',todoItems,'todo: 3 items (1 in_progress, 1 pending, 1 done, 0 dropped)');
addTodo('closed',todoItems.map(it=>({...it,status:'done'})),'todo: 3 items (0 in_progress, 0 pending, 3 done, 0 dropped)');
addTodo('drop',[{id:'d',content:'查备用来源',status:'dropped',reason:'来源重复'}],'todo: 1 items (0 in_progress, 0 pending, 0 done, 1 dropped)');
addTodo('fail',[{id:'x',content:'不应显示为已保存',status:'done'}],'error: todo: write failed');
addTodo('wait',[{id:'y',content:'还没写入',status:'in_progress'}]);
fixture.messages[bots[0].name].push(message('assistant','第一处已查好，正在核对第二处。'));
const todoBaseFetch=window.fetch;
window.fetch=async(path,opts={})=>{
 if(path==='/api/bots/status'){statusReads++;return json({bots:[todoStatus]})}
 if(String(path).endsWith('/steps?n=20'))return json({steps:[{tool:'todo',summary:'{"items":[{"status":"in_progress"}]}',state:'done',time:new Date().toISOString()}]});
 return todoBaseFetch(path,opts);
};
`

const todoBrowserDriver = `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const waitFor=async(fn,msg)=>{for(let i=0;i<1200;i++){if(fn())return;await sleep(10)}throw Error(msg)};
(async()=>{
 const preview=()=>$('#roster .item[data-bot="'+bots[0].name+'"] .preview');
 await waitFor(()=>!$('#shell').hidden&&preview()&&!$('#thread .thread-skeleton'),'app did not boot');
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
 $('#info').click();await waitFor(()=>$('#panel [role=tab]'),'panel did not open');
 [...document.querySelectorAll('#panel [role=tab]')].find(n=>n.textContent==='轨迹').click();
 await waitFor(()=>document.querySelectorAll('.trace-row').length===5,'trace missing calls');
 if(shot==='default'){document.body.dataset.testResult='passed';return}
 const rows=[...document.querySelectorAll('.trace-row')];rows.forEach(n=>n.click());
 if(shot){document.body.dataset.testResult='passed';return}
 const calls=[...document.querySelectorAll('.trace-call')];
 const shown=n=>{const copy=n.cloneNode(true);copy.querySelectorAll('details').forEach(d=>d.remove());return copy.textContent};
 assert(calls.length===5,'missing expanded calls');
 for(const it of todoItems)assert(shown(calls[0]).includes(it.content),'saved item absent');
 assert(shown(calls[1]).includes('3/3'),'completed receipt lost after active list archived');
 assert(shown(calls[2]).includes('来源重复'),'drop reason missing');
 assert(!shown(calls[3]).includes('不应显示为已保存'),'failed input presented as saved');
 assert(!shown(calls[4]).includes('还没写入'),'pending input presented as saved');
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
