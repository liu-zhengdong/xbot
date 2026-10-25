package web

import (
	"encoding/json"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// The store script against a fake DOM and API: chips, search, the empty list, soon cards that cannot be
// added, key connectors that go to chat, and calls that never name a Bot.
func TestAppsStoreBehaviour(t *testing.T) {
	source, err := staticFS.ReadFile("static/apps.js")
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(string(source))
	script := finishGuard + `const assert=require('node:assert/strict'),vm=require('node:vm');
 const all=[];
 class Node{constructor(tag){this.tag=tag;this.children=[];this.dataset={};this.attrs={};this.className='';this.textContent='';this.value='';this.hidden=false;this.isConnected=true;all.push(this);
  this.classList={toggle:(n,on)=>{const s=new Set(this.className.split(' ').filter(Boolean));on?s.add(n):s.delete(n);this.className=[...s].join(' ')}}}
  append(...n){this.children.push(...n)} replaceChildren(...n){this.children=n} setAttribute(k,v){this.attrs[k]=v} focus(){} addEventListener(){}}
 const window={},document={createElement:t=>new Node(t),createTextNode:t=>({textContent:t,children:[]})};
 vm.runInNewContext(` + string(encoded) + `,{window,document});
 const walk=(n,out=[])=>{out.push(n);(n.children||[]).forEach(c=>walk(c,out));return out};
 const textOf=n=>walk(n).map(x=>x.textContent||'').join(' ');
 const buttons=host=>walk(host).filter(n=>n.tag==='button');
 const btn=(host,label)=>buttons(host).find(b=>b.textContent===label);
 const rows=[
  {id:'desktop',title:'电脑操作',summary:'看屏幕、点击。',category:'local',state:'available'},
  {id:'browser',title:'浏览器',summary:'打开网页。',category:'local',state:'connected'},
  {id:'context7',title:'Context7',summary:'查文档。',category:'dev',state:'available'},
  {id:'telegram',title:'Telegram',summary:'收消息。',category:'chat',state:'available',card:true},
  {id:'gmail',title:'Gmail',summary:'邮件。',category:'work',state:'soon'},
  {id:'notes',title:'notes',summary:'自定义应用',category:'custom',state:'connected',custom:true}];
 (async()=>{
  const calls=[];let connected=[];
  const api=async(path,opt)=>{calls.push([opt?.method||'GET',path,opt?.body]);
   if(path==='/api/apps'&&!opt){return {connected:rows.filter(r=>r.state==='connected').length,apps:structuredClone(rows)}}
   if(path==='/api/apps'&&opt.method==='POST'){rows.find(r=>r.id===opt.body.id).state='connected';return {}}
   if(path.startsWith('/api/apps/')&&opt?.method==='DELETE'){rows.find(r=>r.id===decodeURIComponent(path.slice(10))).state='available';return {}}
   throw new Error('unexpected '+path)};
  const host=new Node('div');
  await window.XBotApps.mount({api,host,isCurrent:()=>true,connect:a=>connected.push(a.id)});
  // top bar: the one way back to what is connected; search wording; the chip row
  assert.ok(buttons(host).some(b=>b.textContent==='已连接 2 个'));
  assert.equal(walk(host).find(n=>n.tag==='input').placeholder,'搜索应用…');
  const chips=walk(host).filter(n=>n.className.split(' ').includes('ap-chip')).map(n=>n.textContent);
  assert.equal(chips.join(),'全部,本机,开发,效率,通讯,更多');
  assert.ok(textOf(host).includes('为你推荐'));
  assert.ok(!textOf(host).includes('notes'),'custom apps stay out of the grid');
  // buttons by state
  assert.ok(btn(host,'已连接'));
  const soon=btn(host,'即将支持');assert.equal(soon.disabled,true);
  // chip filters
  walk(host).find(n=>n.dataset.chip==='dev').onclick();
  assert.ok(textOf(host).includes('Context7'));assert.ok(!textOf(host).includes('电脑操作'));
  // adding a plain app goes to the global route with no bot in sight
  await btn(host,'添加').onclick();
  assert.equal(JSON.stringify(calls.filter(c=>c[0]==='POST')[0]),JSON.stringify(['POST','/api/apps',{id:'context7'}]));
  assert.ok(buttons(host).some(b=>b.textContent==='已添加'));
  assert.ok(calls.every(c=>!c[1].includes('/api/bots/')),'store calls must not name a Bot');
  // a key connector hands off to chat instead of posting
  walk(host).find(n=>n.dataset.chip==='chat').onclick();
  const before=calls.length;await btn(host,'连接').onclick();
  assert.equal(connected.join(),'telegram');assert.equal(calls.length,before);
  // search with no match
  const search=walk(host).find(n=>n.tag==='input');search.value='zzz';search.oninput();
  assert.ok(textOf(host).includes('没有匹配的应用'));
  // connected list, remove, then the empty state
  btn(host,'已连接 3 个').onclick();
  assert.ok(textOf(host).includes('这些应用对所有 Bot 都能用'));assert.ok(textOf(host).includes('notes'));
  for(const r of rows)if(r.state==='connected'&&!r.custom)r.state='available';
  rows.find(r=>r.id==='notes').state='available';
  await window.XBotApps.mount({api,host,isCurrent:()=>true});
  btn(host,'已连接 0 个').onclick();
  assert.ok(textOf(host).includes('还没有连接应用'));
  assert.ok(!/先选|当前 Bot|先新建/.test(textOf(host)));
  assert.ok(btn(host,'去商店看看'));
  finished=true;
 })().catch(e=>{console.error(e);process.exit(1)});`
	cmd := exec.Command("node", "-e", script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

// What the owner reads in the store carries no engineering words, and the old per-Bot wording is gone.
func TestAppsSurfaceHasNoEngineeringWords(t *testing.T) {
	js, _ := staticFS.ReadFile("static/apps.js")
	html, _ := staticFS.ReadFile("static/index.html")
	text := string(html)
	i := strings.Index(text, `data-spane="apps"`)
	pane := text[i : i+120]
	if strings.Contains(pane, "settings-channels") || strings.Contains(text, "settings-apps-bot") || strings.Contains(text, "settings-add-connector") {
		t.Fatal("per-Bot controls are back in the apps pane")
	}
	var shown []string
	for _, m := range regexp.MustCompile(`"([^"\\\n]*[一-鿿][^"\\\n]*)"`).FindAllStringSubmatch(string(js), -1) {
		if !strings.Contains(m[1], "=>") && !strings.Contains(m[1], "/") {
			shown = append(shown, m[1])
		}
	}
	banned := regexp.MustCompile(`(?i)MCP|stdio|\benv\b|npx|uvx|\bcommand\b|\bargs\b|transport|mcp\.json|JSON|CLI|桌面|路径|Webhook|文件监听|技能`)
	for _, s := range shown {
		if banned.MatchString(s) {
			t.Fatalf("engineering word in UI text %q", s)
		}
	}
	if len(shown) < 10 {
		t.Fatalf("found only %d strings; the scan is broken", len(shown))
	}
}
