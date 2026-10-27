package web

import (
	"os/exec"
	"testing"
)

func TestXAIKeyConfiguredShowsSubscriptionGuide(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	const script = `
const fs=require('fs'),vm=require('vm'),assert=require('node:assert/strict');
let completed=false;process.on('beforeExit',()=>{if(!completed){console.error('async test did not complete');process.exitCode=1}});
class Element {
 constructor(tag){this.tag=tag;this.children=[];this.attrs={};this.classList={add(){}};this.textContent='';}
 append(...nodes){this.children.push(...nodes)} replaceChildren(...nodes){this.children=nodes}
 setAttribute(k,v){this.attrs[k]=v} focus(){} get innerText(){return this.textContent+this.children.map(n=>n.innerText).join(' ')}
}
const host=new Element('main'),calls=[];
const window={open(){throw Error('no login window expected before OAuth')}};
const document={documentElement:{classList:{contains:()=>false}},createElement:tag=>new Element(tag),createElementNS:(_,tag)=>new Element(tag)};
vm.runInNewContext(fs.readFileSync('static/providers.js','utf8'),{window,document,URL,WeakMap,encodeURIComponent,setTimeout(){}});
const walk=n=>[n,...n.children.flatMap(walk)],find=(tag,text)=>walk(host).find(n=>n.tag===tag&&!String(n.className||'').includes('provider-subscription')&&n.innerText.includes(text));
const loginBtn=()=>walk(host).find(n=>n.tag==='button'&&n.textContent==='登录');
const cls=c=>walk(host).find(n=>String(n.className||'').split(' ').includes(c)),statusText=()=>cls('provider-status').innerText;
// An old-style key binding: protocol chat_completions, a key configured, no OAuth.
const cases=[];
// The API's default endpoint carries an empty name; a named vendor binding uses the brand.
for(const name of ['xai','']){
const providers=[{name,brand:'xai',protocol:'chat_completions',api_key_env:'XAI_API_KEY',key_configured:true,models:[]}];
const api=async(path)=>{
 calls.push(path);
 if(path==='/api/providers')return {providers,vendors:[{brand:'xai',title:'xAI',protocol:'chat_completions',models:['grok-4']}]};
 if(path==='/api/providers/oauth/xai/status')return {state:'idle'};
 throw Error('unexpected '+path);
};
const flush=async()=>{for(let i=0;i<8;i++)await Promise.resolve()};
cases.push(async()=>{
 await window.XBotProviders.settings({api,host,isCurrent:()=>true});
 const card=find('button','xAI');assert(card,'xAI card missing');
 await card.onclick();await flush();
 assert(!loginBtn(),'vendor detail exposes primary login');
 assert(find('button','去订阅'),'subscription guide missing');
 assert(find('summary','用 API 密钥'),'folded key entry missing');
 assert(find('button','保存密钥'),'key form missing');
 assert(statusText().includes('未登录'),'header status is not OAuth');
 assert(!statusText().includes('密钥已配置'),'saved key shown on the header row');
 const fold=cls('provider-key-alt');assert(!fold.open,'key fold opened by default');
 assert(fold.innerText.includes('密钥已配置 · 可用'),'saved key state missing from the fold');
 assert.equal(host.innerText.split('密钥已配置').length,2,'saved key state shown outside the fold');
 assert(host.innerText.includes('账号登录请到上方订阅'),'login guidance missing');
 assert(!calls.some(c=>c.endsWith('/xai/sync')||c.includes('/api/providers/models')),'key-only xAI fetched models before login');
 assert(!host.innerText.includes('拉取模型失败'),'failed fetch shown before any fetch');
});
}
(async()=>{for(const run of cases)await run();completed=true})().catch(e=>{console.error(e);process.exitCode=1});`
	if out, err := exec.Command(node, "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("xai key-configured subscription guide: %v: %s", err, out)
	}
}
