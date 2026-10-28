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
 vm.runInNewContext(` + string(encoded) + `,{window,document,AbortController,setTimeout:()=>0,clearTimeout:()=>{}});
 const walk=(n,out=[])=>{out.push(n);(n.children||[]).forEach(c=>walk(c,out));return out};
 const textOf=n=>walk(n).map(x=>x.textContent||'').join(' ');
 const buttons=host=>walk(host).filter(n=>n.tag==='button');
 const btn=(host,label)=>buttons(host).find(b=>b.textContent===label);
 const cardBtn=(host,title)=>walk(host).find(n=>n.className==='ap-card'&&textOf(n).includes(title)).children.find(n=>n.tag==='button');
 const rows=[
  {id:'desktop',title:'电脑操作',summary:'看屏幕、点击。',category:'local',state:'available'},
  {id:'browser',title:'浏览器',summary:'打开网页。',category:'local',state:'connected'},
  {id:'fetch',title:'网页抓取',summary:'读网页。',category:'dev',state:'connected'},
  {id:'playwright',title:'Playwright',summary:'自动化网页。',category:'dev',state:'available'},
  {id:'github',title:'GitHub',summary:'看仓库。',category:'dev',state:'connected'},
  {id:'context7',title:'Context7',summary:'查文档。',category:'dev',state:'available',card:true,fields:['API 密钥']},
  {id:'telegram',title:'Telegram',summary:'收消息。',category:'chat',state:'available',card:true},
  {id:'gmail',title:'Gmail',summary:'邮件。',category:'work',state:'soon'},
  {id:'notes',title:'notes',summary:'自定义应用',category:'custom',state:'connected',custom:true}];
 (async()=>{
  const calls=[];let connected=[], rejectKey=true;
  const api=async(path,opt)=>{calls.push([opt?.method||'GET',path,opt?.body]);
   if(path==='/api/apps'&&!opt){return {connected:rows.filter(r=>r.state==='connected').length,apps:structuredClone(rows)}}
   if(path==='/api/apps'&&opt.method==='POST'&&opt.body.id==='context7'&&rejectKey){rejectKey=false;throw Error('no')}
   if(path==='/api/apps'&&opt.method==='POST'){rows.find(r=>r.id===opt.body.id).state='connected';return {}}
   if(path.startsWith('/api/apps/')&&opt?.method==='DELETE'){rows.find(r=>r.id===decodeURIComponent(path.slice(10))).state='available';return {}}
   throw new Error('unexpected '+path)};
  const host=new Node('div');
  await window.XBotApps.mount({api,host,isCurrent:()=>true,connect:a=>connected.push(a.id)});
  // top bar: the one way back to what is connected; search wording; the chip row
  assert.ok(buttons(host).some(b=>b.textContent==='已连接 2 个'));
  assert.equal(walk(host).find(n=>n.tag==='input').placeholder,'搜索应用…');
  const chips=walk(host).filter(n=>n.className.split(' ').includes('ap-chip')).map(n=>n.textContent);
  assert.equal(chips.join(),'全部,开发,效率,通讯,更多');
  // native organs stay out of the store, the picks and the count
  for(const t of ['电脑操作','浏览器','网页抓取'])assert.ok(!textOf(host).includes(t),t+' is in the store');
  assert.ok(textOf(host).includes('为你推荐'));
  assert.ok(!textOf(host).includes('notes'),'custom apps stay out of the grid');
  // buttons by state
  // a connected card offers 移除 right in the store; nothing reads as a dead 已添加 or 已连接
  const done=cardBtn(host,'GitHub');assert.equal(done.textContent,'移除');assert.equal(done.disabled,false);
  assert.ok(!btn(host,'已添加')&&!btn(host,'已连接'));
  const soon=btn(host,'即将支持');assert.equal(soon.disabled,true);
  // chip filters
  walk(host).find(n=>n.dataset.chip==='dev').onclick();
  assert.ok(textOf(host).includes('Context7'));assert.ok(!textOf(host).includes('电脑操作'));
  // adding a plain app goes to the global route with no bot in sight
  await cardBtn(host,'Playwright').onclick();
  assert.equal(JSON.stringify(calls.filter(c=>c[0]==='POST')[0]),JSON.stringify(['POST','/api/apps',{id:'playwright'}]));
  assert.equal(cardBtn(host,'Playwright').textContent,'移除');
  const notice=walk(host).find(n=>n.attrs.role==='status');assert.equal(notice.textContent,'已添加到你的应用');notice.hidden=true;
  // The settings layer keeps blank input in place and installs on success.
  let before=calls.length;await cardBtn(host,'Context7').onclick();
  assert.equal(calls.length,before);
  let dialog=walk(host).find(n=>n.attrs.role==='dialog');assert.ok(dialog);
  let input=walk(dialog).find(n=>n.tag==='input');assert.equal(input.type,'password');
  await btn(dialog,'添加').onclick();assert.equal(calls.length,before);
  assert.ok(textOf(dialog).includes('请填写API 密钥'));
  input.value='fixture-key';await btn(dialog,'添加').onclick();
  assert.ok(walk(host).includes(dialog));assert.equal(input.value,'fixture-key');assert.ok(textOf(dialog).includes('密钥无效或连不上'));
  assert.equal(cardBtn(host,'Context7').textContent,'添加');assert.equal(notice.hidden,true);
  await btn(dialog,'添加').onclick();
  assert.equal(cardBtn(host,'Context7').textContent,'移除');assert.ok(!walk(host).includes(dialog));assert.equal(notice.hidden,false);
  // the old connect hook is never called: the store no longer hands a key app to chat
  assert.equal(connected.length,0);
  assert.ok(!textOf(host).includes('CONTEXT7'));
  assert.ok(calls.every(c=>!c[1].includes('/api/bots/')));
  // search with no match
  const search=walk(host).find(n=>n.tag==='input');search.value='zzz';search.oninput();
  assert.ok(textOf(host).includes('没有匹配的应用'));
  // connected list, remove, then the empty state
  btn(host,'已连接 4 个').onclick();
  assert.ok(textOf(host).includes('这些应用对所有 Bot 都能用'));assert.ok(textOf(host).includes('notes'));
  assert.ok(!textOf(host).includes('浏览器')&&!textOf(host).includes('网页抓取'),'native organs in the connected list');
  // removing from a store card sends the same DELETE as the connected page
  walk(host).find(n=>n.className==='setback').onclick();btn(host,'清除搜索').onclick();
  await cardBtn(host,'GitHub').onclick();
  assert.equal(JSON.stringify(calls.filter(c=>c[0]==='DELETE')[0]),JSON.stringify(['DELETE','/api/apps/github',undefined]));
  assert.equal(cardBtn(host,'GitHub').textContent,'添加');
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

// A failed add or remove keeps its reason on screen after the redraw; a real new action or a trip to the
// other page clears it. Every card and row carries its app icon, and the per-Bot limit is its own line.
func TestAppsFailureStaysAndIconsShow(t *testing.T) {
	source, err := staticFS.ReadFile("static/apps.js")
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(string(source))
	script := finishGuard + `const assert=require('node:assert/strict'),vm=require('node:vm');
 class Node{constructor(tag){this.tag=tag;this.children=[];this.dataset={};this.attrs={};this.className='';this.textContent='';this.value='';this.hidden=false;this.isConnected=true;this.innerHTML='';
  this.classList={toggle:(n,on)=>{const s=new Set(this.className.split(' ').filter(Boolean));on?s.add(n):s.delete(n);this.className=[...s].join(' ')}}}
  append(...n){this.children.push(...n)} replaceChildren(...n){this.children=n} setAttribute(k,v){this.attrs[k]=v} focus(){} addEventListener(){}}
 const window={},document={createElement:t=>new Node(t),createTextNode:t=>({textContent:t,children:[]})};
 vm.runInNewContext(` + string(encoded) + `,{window,document,AbortController,setTimeout:()=>0,clearTimeout:()=>{}});
 const walk=(n,out=[])=>{out.push(n);(n.children||[]).forEach(c=>walk(c,out));return out};
 const textOf=n=>walk(n).map(x=>x.textContent||'').join(' ');
 const buttons=host=>walk(host).filter(n=>n.tag==='button');
 const btn=(host,label)=>buttons(host).find(b=>b.textContent===label);
 const cls=(host,c)=>walk(host).filter(n=>(n.className||'').split(' ').includes(c));
 const long='看屏幕、点击、输入，帮你操作电脑上的软件。你在用电脑时它会让开，需要的时候再叫你。';
 const note='密钥按 Bot 分别保存，只有填过密钥的 Bot 能用。';
 (async()=>{
  const rows=[
   {id:'context7',title:'Context7',summary:'查文档。',category:'dev',state:'available'},
   {id:'github',title:'GitHub',summary:'看仓库。',category:'dev',state:'connected'},
   {id:'telegram',title:'Telegram',summary:long,scope:note,category:'chat',state:'connected',card:true},
   {id:'mine',title:'mine',summary:'自定义应用',category:'custom',state:'connected',custom:true}];
  let fail=null;
  const api=async(path,opt)=>{
   if(path==='/api/apps'&&!opt)return {connected:3,apps:structuredClone(rows)};
   if(fail)throw Object.assign(new Error('出错了，没有成功'),{status:400});
   return {}};
  const host=new Node('div');
  await window.XBotApps.mount({api,host,isCurrent:()=>true});
  const err=()=>walk(host).find(n=>n.attrs&&n.attrs.role==='alert'&&n.className==='sk-error');
  // icons on every store card, apart from the text; the limit is its own line
  const cards=cls(host,'ap-card');
  assert.ok(cards.length>=2);
  for(const c of cards){const ico=c.children.find(n=>n.className==='ico');assert.ok(ico&&ico.innerHTML.startsWith('<svg'),'card has an icon');assert.equal(ico.attrs['aria-hidden'],'true')}
  walk(host).find(n=>n.dataset.chip==='chat').onclick();
  const scope=cls(host,'ap-scope');assert.equal(scope.length,1);assert.equal(scope[0].textContent,note);
  assert.ok(!cls(host,'hint').some(h=>h.textContent.includes('分别保存')),'the limit is not part of the summary');
  // a failed add keeps its reason
  fail=true;walk(host).find(n=>n.dataset.chip==='dev').onclick();
  await btn(host,'添加').onclick();
  assert.equal(err().hidden,false);assert.equal(err().textContent,'出错了，没有成功');
  // going to the other page clears it
  btn(host,'已连接 3 个').onclick();
  assert.equal(err().hidden,true);
  // connected rows carry icons and the limit; the lead no longer promises every Bot
  for(const r of cls(host,'ap-row')){assert.ok(r.children.some(n=>n.className==='ico'&&n.innerHTML.startsWith('<svg')))}
  assert.equal(cls(host,'ap-scope').length,1);
  assert.ok(!textOf(host).includes('这些应用对所有 Bot 都能用'));assert.ok(textOf(host).includes('带说明的'));
  // a failed remove keeps its reason too
  await btn(host,'移除').onclick();
  assert.equal(err().hidden,false);assert.equal(err().textContent,'出错了，没有成功');
  // a new action clears the old reason first
  fail=false;await btn(host,'移除').onclick();
  assert.equal(err().hidden,true);
  finished=true;
 })().catch(e=>{console.error(e);process.exit(1)});`
	cmd := exec.Command("node", "-e", script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
