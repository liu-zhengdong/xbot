package web

import (
	"os/exec"
	"strings"
	"testing"
)

func TestRoutineRunsHideOnlyTheirSilentCompletion(t *testing.T) {
	raw, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	a := strings.Index(s, "  function routineRuns(")
	b := strings.Index(s, "  function taskAvatar(")
	script := `const assert=require('node:assert/strict');
 const routineOf=m=>m.routine?{label:m.routine}:null,unwrap=text=>({untrusted:text.startsWith('event:')});
 ` + s[a:b] + `
 const rows=[{seq:1,role:'user',routine:'新闻',content:'event:timer',branch:'wake'},
 {seq:2,role:'assistant',content:'另一个回合的回答'},
 {seq:3,role:'assistant',content:'SILENT',branch:'wake'},
 {seq:4,role:'user',routine:'提醒',content:'event:timer'},
 {seq:5,role:'assistant',content:'现在该出发了'}];
 const runs=routineRuns(rows);assert.deepEqual([...runs.hidden],[0,2]);assert.equal(runs.recent.length,2);assert.equal(runs.recent[0].summary,'没什么要说的');
 assert.equal(runs.recent[1].summary,'现在该出发了');
 const unfinished=routineRuns(rows.slice(0,1));assert.equal(unfinished.hidden.size,0);assert.equal(unfinished.recent.length,0);
 const owner=routineRuns([{role:'user',routine:'提醒',content:'event:timer'},{role:'user',content:'我的新问题'},{role:'assistant',content:'SILENT'}]);assert.equal(owner.hidden.size,0);
 `
	if out, err := exec.Command("node", "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestSlowToolNoticeRequiresAnOwnerInterjection(t *testing.T) {
	raw, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	a := strings.Index(s, "  function elapsedWork(")
	b := strings.Index(s, "  function routineRuns(")
	c := strings.Index(s, "  function slowToolNotice(")
	d := strings.Index(s, "  function updateWorkTimes(")
	script := `const assert=require('node:assert/strict'),unwrap=text=>({untrusted:text.startsWith('event:')});
 ` + s[a:b] + s[c:d] + `
 const current={tool:'shell',tool_summary:'git status --short',since:'2026-10-21T08:00:00Z'},now=Date.parse('2026-10-21T08:00:06Z');
 const initial={role:'user',content:'检查项目',time:'2026-10-21T07:59:00Z'};
 assert.equal(slowToolNotice(current,[initial],[],now),'');
 assert.equal(slowToolNotice(current,[{...initial,content:'event:timer',time:'2026-10-21T08:00:05Z'}],[],now),'');
 const aside={...initial,content:'顺便问一句',time:'2026-10-21T08:00:04Z'};
 assert.match(slowToolNotice(current,[initial,aside],[],now),/已 6 秒/);
 assert.match(slowToolNotice(current,[initial],[{dataset:{time:aside.time}}],now),/你的话会接在它后面/);
 assert.equal(slowToolNotice(current,[aside],[],Date.parse('2026-10-21T08:00:01Z')),'');
 assert.equal(slowToolNotice({},[aside],[],now),'');
 `
	if out, err := exec.Command("node", "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestTaskCardUsesTitleAndKeepsOriginalErrorAcrossPolls(t *testing.T) {
	raw, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	part := func(start, end string) string {
		a, b := strings.Index(s, start), strings.Index(s, end)
		if a < 0 || b <= a {
			t.Fatal("task rendering functions not found")
		}
		return s[a:b]
	}
	script := `const assert=require('node:assert/strict');
 const node=()=>({dataset:{},hidden:false,textContent:'',children:[],classList:{toggle(){},add(){},contains(){return true}},setAttribute(){},addEventListener(t,fn){this[t]=fn},append(...xs){this.children.push(...xs)},replaceChildren(...xs){this.children=xs},querySelector(sel){return this.children.find(x=>sel==='button'&&x.tag==='button')||null},get childElementCount(){return this.children.length},firstElementChild:{replaceWith(){}}});
 const el=(tag,cls,text)=>Object.assign(node(),{tag,textContent:text||''}), icon=()=>node(),spinIcon=icon;
 const document={createTextNode:text=>({textContent:text})};
 const taskStatus=x=>x,when=x=>x,plainText=x=>x,displayText=x=>x,elapsedWork=()=>'',zhError=()=> '请求超时';
 const elements=new Map([['.task-error',node()]]),card=node();card.querySelector=sel=>{if(sel==='.res'&&elements.get('.task-type')?.className?.split(' ').includes('res'))return elements.get('.task-type');if(!elements.has(sel))elements.set(sel,node());return elements.get(sel)};
 ` + part("  function clip(", "  function when(") + part("  function taskTypeLabel(", "  function paintGreeting(") + part("  function errorDetails(", "  function visibleError(") + part("  function updateTaskCard(", "  const routineChanges") + `
 const ag={id:'sa_0123456789abcdef',type:'dev',task:'核对页面\n下一行是具体步骤',status:'failed',summary:'provider: context deadline exceeded',steps:3};
 updateTaskCard(card,ag);
 assert.equal(elements.get('.task-name').textContent,'核对页面');assert.equal(elements.get('.recent-name').textContent,'核对页面');
 assert.equal(elements.get('.task-id').textContent,ag.id);
 assert.equal(taskTitle({...ag,task:'',type:'research'}),'调研');assert.equal(taskTitle({...ag,task:''}),'开发');
 assert.equal(taskStatusIcon({status:'cancelled'}),'ban');assert.equal(taskStatusIcon({status:'done'}),'circle-check');assert.equal(taskStatusIcon({status:'failed'}),'x');
 assert.equal(taskTitle({id:ag.id}), '子代理');assert.equal(taskTitle({...ag,task:ag.id}), '开发');
 assert.equal(taskTitle({...ag,task:'😀'.repeat(25)+'\n这行不要显示'}),'😀'.repeat(24)+'…');
 updateTaskCard(card,{...ag,task:''});assert.equal(elements.get('.task-name').textContent,'开发');assert.equal(elements.get('.recent-name').textContent,'开发');
 updateTaskCard(card,ag);
 const detail=elements.get('.task-error');assert.equal(detail.hidden,false);assert.equal(detail.children.length,2);assert.equal(detail.children[1].textContent,ag.summary);
 detail.children[0].click();assert.equal(detail.children[1].hidden,false);
 const prior=detail.children[1];updateTaskCard(card,ag);assert.equal(detail.children[1],prior);assert.equal(prior.hidden,false);
 updateTaskCard(card,{...ag,status:'running'});assert.equal(detail.hidden,true);assert.equal(detail.children.length,0);
 updateTaskCard(card,{...ag,type:'research',status:'done',summary:'调研完成'});assert.equal(elements.get('.task-type').textContent,'调研');assert.equal(elements.get('.res').textContent,'调研完成');
 `
	if out, err := exec.Command("node", "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestTaskGuideOpensOnDemandAndClosesOnEscapeOrSend(t *testing.T) {
	raw, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	a, b := strings.Index(s, "  function bindTaskEditor("), strings.Index(s, "  function updateTaskCard(")
	script := `const assert=require('node:assert/strict');
 const handlers=()=>({attrs:{},setAttribute(k,v){this.attrs[k]=v},addEventListener(k,f){this[k]=f},focus(){this.focused=true}});
 const guide=handlers(),field={value:'',focus(){this.focused=true}},send={disabled:false},editor=handlers();editor.hidden=true;editor.querySelector=s=>s==='input'?field:send;
 const card={querySelector:s=>s==='.steer-editor'?editor:guide};let ok=true,calls=[];const controlAgent=async(...args)=>{calls.push(args);return ok};
 ` + s[a:b] + `
 (async()=>{bindTaskEditor(card,'sa_0123456789abcdef');assert.equal(editor.hidden,true);
 guide.click();assert.equal(editor.hidden,false);assert.equal(field.focused,true);field.value='先运行测试';
 let stopped=false;editor.keydown({key:'Escape',preventDefault(){},stopPropagation(){stopped=true}});assert.equal(stopped,true);assert.equal(editor.hidden,true);assert.equal(field.value,'先运行测试');
 guide.click();ok=false;await editor.submit({preventDefault(){}});assert.equal(editor.hidden,false);assert.equal(field.value,'先运行测试');
 ok=true;await editor.submit({preventDefault(){}});assert.equal(editor.hidden,true);assert.equal(field.value,'');assert.deepEqual(calls[1],['sa_0123456789abcdef','steer','先运行测试']);
 })().catch(e=>{console.error(e);process.exit(1)});`
	if out, err := exec.Command("node", "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestRoutineFailureClearsOnRetryAndFades(t *testing.T) {
	raw, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	a, b := strings.Index(s, "  const routineChanges"), strings.Index(s, "  function syncTaskView(")
	script := `const assert=require('node:assert/strict');
 const timers=new Map();let next=0,fail=true;const setTimeout=fn=>{timers.set(++next,fn);return next},clearTimeout=id=>timers.delete(id);
 function el(tag,cls,text=''){return {tag,cls,textContent:text,hidden:false,dataset:{},style:{},children:[],classList:{toggle(){}},setAttribute(){},append(...xs){this.children.push(...xs)},addEventListener(t,f){this[t]=f},getAnimations(){return []},animate(){return {finished:Promise.resolve()}},querySelector(sel){const match=n=>n.cls?.split(' ').includes(sel.slice(1))?n:n.children?.map(match).find(Boolean);return this.children.map(match).find(Boolean)}}}
 const icon=()=>el('svg','icon'),when=x=>x,cronText=()=> '每天 08:00';
 const api=async(path,opts)=>{if(fail)throw Error('offline');return {enabled:opts.body.enabled}};
 ` + s[a:b] + `
 (async()=>{const c=routineCard({name:'ada'},{id:'morning',enabled:true,cron:'0 8 * * *'}),toggle=c.querySelector('.tog'),notice=c.querySelector('.routine-error');
 await toggle.click();assert.equal(c.routine.enabled,true);assert.equal(notice.hidden,false);assert.match(notice.textContent,/恢复为开启/);
 fail=false;await toggle.click();assert.equal(c.routine.enabled,false);assert.equal(notice.hidden,true,'successful retry must clear the previous failure');assert.equal(timers.size,0);
 fail=true;await toggle.click();assert.equal(c.routine.enabled,false);assert.match(notice.textContent,/恢复为关闭/);
 [...timers.values()].at(-1)();await Promise.resolve();assert.equal(notice.hidden,true,'failure feedback fades away');
 })().catch(e=>{console.error(e);process.exit(1)});`
	if out, err := exec.Command("node", "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
