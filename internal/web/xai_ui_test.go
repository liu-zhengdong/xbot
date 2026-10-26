package web

import (
	"os/exec"
	"testing"
)

// TestXAIProviderLoginStates drives the xAI account-login UI: primary login
// CTA with a folded key form, cancel of a pending login, a readable failure
// with retry, and a successful re-login that pulls the catalog read-only.
func TestXAIProviderLoginStates(t *testing.T) {
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
const host=new Element('main'),calls=[],timers=[],popups=[];
let configured=false,resolveStatus,failStart=false,current=true,expired=false;
const window={open(url){assert(url==='about:blank'||url==='https://auth.example/authorize');const popup={closed:false,opener:{},location:{replace(url){popup.url=url}},close(){this.closed=true}};popups.push(popup);return popup}};
const document={documentElement:{classList:{contains:()=>false}},createElement:tag=>new Element(tag),createElementNS:(_,tag)=>new Element(tag)};
vm.runInNewContext(fs.readFileSync('static/providers.js','utf8'),{window,document,URL,WeakMap,encodeURIComponent,setTimeout:fn=>timers.push(fn)});
const walk=n=>[n,...n.children.flatMap(walk)],find=(tag,text)=>walk(host).find(n=>n.tag===tag&&n.innerText.includes(text));
const providers=()=>configured?[{name:'xai',oauth:'xai',protocol:'responses',key_configured:true,models:[]}]:[];
const api=async(path,opts={})=>{
 calls.push({path,opts});
 if(path==='/api/providers')return {providers:providers(),vendors:[{brand:'xai',title:'xAI',protocol:'chat_completions',models:['grok-4']}]};
 if(path==='/api/providers/oauth/xai/start'){if(failStart)throw Error('private-upstream-body');return {url:'https://auth.example/authorize'}}
 if(path==='/api/providers/oauth/xai/cancel')return {status:'cancelled'};
 if(path==='/api/providers/oauth/xai/status')return await new Promise(resolve=>resolveStatus=resolve);
 if(path==='/api/providers/models?provider=xai'){if(expired){const e=Error('登录凭证已失效，请重新登录');e.status=422;throw e}return {models:[{model:'grok-4',context:256000,vision:false,hosted_web_search:true}]}}
 throw Error('unexpected '+path);
};
const flush=async()=>{for(let i=0;i<8;i++)await Promise.resolve()};
const starts=()=>calls.filter(c=>c.path.endsWith('/start')).length;
(async()=>{
 await window.XBotProviders.settings({api,host,isCurrent:()=>current});
 const card=find('button','xAI');assert(card,'xAI card missing');
 await card.onclick();await flush();
 assert(find('button','登录 xAI'),'login CTA missing');
 assert(find('summary','用 API 密钥'),'folded key entry missing');
 assert(find('button','保存密钥'),'key form missing');
 // The account login leads; pending shows cancel and reopen.
 const attempt=find('button','登录 xAI').onclick();await flush();
 assert.equal(starts(),1);assert(find('button','取消')&&find('button','重新打开'),'pending controls missing');
 assert(host.innerText.includes('等待登录'),'pending note missing');
 resolveStatus({state:'pending'});await flush();assert.equal(timers.length,1,'poll sleep missing');
 // Cancelling stops the pending login; the CTA returns without a new start.
 const cancelled=find('button','取消').onclick();await flush();await cancelled;
 assert(find('button','登录 xAI'),'cancelled login cannot restart');
 assert(calls.some(c=>c.path.endsWith('/cancel')),'cancel not sent');
 assert.equal(starts(),1,'cancel started a new login');
 // A failed login keeps a readable error with cancel and re-login.
 const retry=find('button','登录 xAI').onclick();await flush();assert.equal(starts(),2);
 resolveStatus({state:'failed',error:'授权未完成，请重试'});await retry;await flush();
 assert(find('button','重新登录')&&find('button','取消'),'failed controls missing');
 assert(host.innerText.includes('授权未完成'),'failure text missing');
 // Re-login succeeds; the OAuth binding pulls its catalog read-only.
 const again=find('button','重新登录').onclick();await flush();assert.equal(starts(),3);
 configured=true;resolveStatus({state:'succeeded'});await again;await flush();
 assert(host.innerText.includes('已登录'),'logged-in status missing');
 assert(host.innerText.includes('grok-4'),'model list missing');
 assert(calls.some(c=>c.path==='/api/providers/models?provider=xai'),'oauth catalog not pulled');
 assert(!calls.some(c=>c.path.endsWith('/xai/sync')),'oauth binding hit the vendor sync route');
 assert(!host.innerText.includes('private-upstream-body'));
 // A rejected refresh surfaces as a re-login prompt, not a network error.
 expired=true;
 find('button','模型与供应商').onclick();
 await find('button','xAI').onclick();await flush();
 assert(host.innerText.includes('登录凭证已失效'),'relogin reason lost');
 assert(find('button','重新登录'),'relogin button missing');
 assert(!host.innerText.includes('检查供应商配置和网络'),'dead credential shown as a network problem');
 completed=true;
})().catch(e=>{console.error(e);process.exitCode=1});`
	if out, err := exec.Command(node, "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("xai login states: %v: %s", err, out)
	}
}

// TestXAIKeyConfiguredStillShowsLogin guards the primary path: a vendor card
// with a saved API key but no OAuth binding must still lead with「登录 xAI」
// and the folded key entry, not a failed model fetch with only a retry.
func TestXAIKeyConfiguredStillShowsLogin(t *testing.T) {
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
const walk=n=>[n,...n.children.flatMap(walk)],find=(tag,text)=>walk(host).find(n=>n.tag===tag&&n.innerText.includes(text));
// An old-style key binding: protocol chat_completions, a key configured, no OAuth.
const providers=[{name:'xai',brand:'xai',protocol:'chat_completions',api_key_env:'XAI_API_KEY',key_configured:true,models:[]}];
const api=async(path)=>{
 calls.push(path);
 if(path==='/api/providers')return {providers,vendors:[{brand:'xai',title:'xAI',protocol:'chat_completions',models:['grok-4']}]};
 if(path==='/api/providers/oauth/xai/status')return {state:'idle'};
 throw Error('unexpected '+path);
};
const flush=async()=>{for(let i=0;i<8;i++)await Promise.resolve()};
(async()=>{
 await window.XBotProviders.settings({api,host,isCurrent:()=>true});
 const card=find('button','xAI');assert(card,'xAI card missing');
 await card.onclick();await flush();
 assert(find('button','登录 xAI'),'key-configured xAI lost the login CTA');
 assert(find('summary','用 API 密钥'),'folded key entry missing');
 assert(find('button','保存密钥'),'key form missing');
 assert(host.innerText.includes('密钥已配置'),'configured status missing');
 assert(host.innerText.includes('登录 xAI 账号即可使用'),'login lead missing');
 assert(!calls.some(c=>c.endsWith('/xai/sync')||c.includes('/api/providers/models')),'key-only xAI fetched models before login');
 assert(!host.innerText.includes('拉取模型失败'),'failed fetch shown before any fetch');
 completed=true;
})().catch(e=>{console.error(e);process.exitCode=1});`
	if out, err := exec.Command(node, "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("xai key-configured login CTA: %v: %s", err, out)
	}
}
