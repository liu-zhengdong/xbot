package web

import (
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"xbot/internal/browsertest"
)

func TestTaskFoldInPanelBrowser(t *testing.T) {
	runTodoBrowser(t, todoBrowserFixture+`
 const foldBaseFetch=window.fetch;
 window.fetch=async(path,opts={})=>{
  if(String(path).includes('/agents/sa_0123456789abcdef/transcript'))return json({id:'sa_0123456789abcdef',status:'done',turn_seq:1,continuable:false,has_more:false,entries:[{seq:1,time:'2026-11-01T08:00:00Z',message:{role:'assistant',content:[{type:'text',text:'任务结果'}]}}]});
  if(/^\/api\/bots\/[^/]+$/.test(String(path))) return json({name:bots[0].name,links:[],routines:[{id:'routine1',name:'每小时访问B站',enabled:true,cron:'0 * * * *'}],agents:[{id:'sa_0123456789abcdef',type:'dev',task:'检查任务',status:'done',summary:'任务结果'}]});
  return foldBaseFetch(path,opts);
 };
 `, `
 const $=s=>document.querySelector(s),sleep=ms=>new Promise(r=>setTimeout(r,ms));
 const waitFor=async(fn,label)=>{for(let i=0;i<1000;i++){if(fn())return;await sleep(10)}throw Error('timed out: '+label)};
 (async()=>{
  await waitFor(()=>!$('#shell').hidden && $('#title').textContent.includes(bots[0].name) && !$('.thread-loading-cover'),'boot');
  const tab=name=>[...document.querySelectorAll('#workspace-tabs [role=tab]')].find(n=>n.textContent===name);
  const view=name=>document.getElementById(tab(name).getAttribute('aria-controls'));
  await waitFor(()=>tab('任务')&&tab('子代理'),'peer tabs');
  tab('任务').click();await waitFor(()=>$('.task-routine')?.checkVisibility(),'Routine settings');
  const routine=$('.task-routine');
  tab('子代理').click();
  await waitFor(()=>view('子代理').querySelector('[data-agent="sa_0123456789abcdef"]')?.checkVisibility(),'finished child list');
  const root=view('子代理'),card=root.querySelector('[data-agent="sa_0123456789abcdef"]'),name=card.querySelector('b');
  if(!name||name.textContent!=='检查任务')throw Error('child title missing');
  const enter=card.getAttribute('role')==='button'?card:card.querySelector('button');enter.click();
  await waitFor(()=>[...root.querySelectorAll('h2')].some(n=>n.checkVisibility()&&n.textContent==='检查任务')&&root.innerText.includes('任务结果'),'child conversation and result');
  const head=[...root.querySelectorAll('h2')].find(n=>n.checkVisibility()),summary=root.querySelector('summary[aria-label="任务信息"]'),info=summary?.parentElement;
  if(!head||!summary||info.open||[...root.querySelectorAll('h2')].filter(n=>n.checkVisibility()&&n.textContent==='检查任务').length!==1)throw Error('duplicate title or initially unfolded task information');
  summary.click();if(!info.open||!info.innerText.includes('sa_0123456789abcdef')||!head.checkVisibility())throw Error('expand task information failed');
  summary.click();if(info.open||!head.checkVisibility()||head.textContent!=='检查任务')throw Error('collapse changed the conversation header');
  const back=[...root.querySelectorAll('button')].find(n=>n.checkVisibility()&&(n.getAttribute('aria-label')||n.textContent).trim()==='子代理');
  if(!back)throw Error('child list return missing');back.click();
  await waitFor(()=>card.checkVisibility(),'return to finished child list');
  if(root.querySelector('[data-agent="sa_0123456789abcdef"]')!==card||card.querySelector('b')!==name)throw Error('enter/return replaced the original child title');
  tab('任务').click();
  if(routine!==$('.task-routine')||!routine.checkVisibility()||!routine.querySelector('[role=switch]'))throw Error('routine changed');
  document.body.dataset.testResult='passed';
 })().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
 `)
}

func TestFinishedTaskFoldKeepsSameHeader(t *testing.T) {
	chrome := browsertest.Chrome(t)
	var err error
	js, _ := staticFS.ReadFile("static/app.js")
	css, _ := staticFS.ReadFile("static/app.css")
	src := string(js)
	part := func(a, b string) string {
		i, j := strings.Index(src, a), strings.Index(src, b)
		if i < 0 || j <= i {
			t.Fatal("task functions not found")
		}
		return src[i:j]
	}
	page := `<!doctype html><style>` + string(css) + `</style><body><script>
 const el=(tag,cls,text)=>{const n=document.createElement(tag);n.className=cls||'';if(text)n.textContent=text;return n};
 const icon=()=>el('span','icon'),spinIcon=icon,avatar=icon,plainText=x=>x,clip=(x,n)=>Array.from(x).slice(0,n).join(''),taskStatus=x=>x,when=x=>x,displayText=x=>x,elapsedWork=()=>'',zhError=x=>x,queuePathLayout=()=>{};
 const confirmTaskStop=()=>{},errorDetails=()=>{},controlAgent=async()=>true;
 ` + part("  function taskTypeLabel(", "  function paintGreeting(") + part("  function updateTaskNotable(", "  const routineChanges") + `
 try {
 for(const type of ['routine','dev']) {
  const ag={id:'sa_0123456789abcdef',type,task:'测试记录',status:'done',summary:'结果正文'},card=taskCard(ag);document.body.append(card);
  const head=card.querySelector('.recent-task'),name=head.querySelector('.task-name'),result=card.querySelector('.res');
  const check=(v,msg)=>{if(!v)throw Error(msg)};
  check(name && card.querySelectorAll('.task-name, .recent-name').length===1,'one title');
  check(head.getAttribute('aria-expanded')==='false' && !result.checkVisibility(),'initially folded');
  const before=head.getBoundingClientRect(),style=getComputedStyle(name),size=style.fontSize,weight=style.fontWeight;
  head.click();
  check(head===card.querySelector('.recent-task') && head.checkVisibility() && name===head.querySelector('.task-name'),'same visible header');
  check(head.getAttribute('aria-expanded')==='true' && result.checkVisibility() && result.textContent==='结果正文','expanded body');
  const after=head.getBoundingClientRect();check(before.height===after.height && getComputedStyle(name).fontSize===size && getComputedStyle(name).fontWeight===weight,'header changed');
  updateTaskCard(card,ag);check(name===head.querySelector('.task-name') && head.getAttribute('aria-expanded')==='true','poll changed header');
  head.click();check(head.getAttribute('aria-expanded')==='false' && !result.checkVisibility(),'fold again');
  card.remove();
 }
 const running={id:'sa_0123456789abcdef',type:'dev',task:'进行中的任务',status:'running'}, card=taskCard(running);document.body.append(card);
 const name=card.querySelector('.task-name');
 if(!name.closest('.ch'))throw Error('running header missing title');
 updateTaskCard(card,{...running,status:'done',summary:'完成'});
 if(card.querySelector('.recent-task .task-name')!==name || card.querySelectorAll('.task-name').length!==1)throw Error('title not reused after completion');
 document.body.dataset.result='passed';
 } catch(e) {document.body.dataset.result=e.message}
 </script>`
	dir := t.TempDir()
	path := filepath.Join(dir, "task.html")
	if err := os.WriteFile(path, []byte(page), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(chrome, "--headless=new", "--disable-gpu", "--no-sandbox", "--no-first-run", "--user-data-dir="+filepath.Join(dir, "chrome"), "--dump-dom", (&url.URL{Scheme: "file", Path: path}).String()).CombinedOutput()
	if err != nil || !strings.Contains(string(out), `data-result="passed"`) {
		t.Fatalf("browser: %v\n%s", err, out)
	}
}
