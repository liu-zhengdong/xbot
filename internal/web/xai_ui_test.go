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
let configured=false,resolveStatus,failStart=false,current=true,expired=false,cancelFail=false,logoutFail=false;
const window={open(url){assert(url==='about:blank'||url==='https://auth.example/authorize');const popup={closed:false,opener:{},location:{replace(url){popup.url=url}},close(){this.closed=true}};popups.push(popup);return popup}};
const document={documentElement:{classList:{contains:()=>false}},createElement:tag=>new Element(tag),createElementNS:(_,tag)=>new Element(tag)};
vm.runInNewContext(fs.readFileSync('static/providers.js','utf8'),{window,document,URL,WeakMap,encodeURIComponent,setTimeout:fn=>timers.push(fn)});
const walk=n=>[n,...n.children.flatMap(walk)],find=(tag,text)=>walk(host).find(n=>n.tag===tag&&n.innerText.includes(text));
const cls=c=>walk(host).find(n=>String(n.className||'').split(' ').includes(c)),statusText=()=>cls('provider-status').innerText;
const providers=()=>configured?[{name:'xai',oauth:'xai',protocol:'responses',key_configured:true,models:[]}]:[];
const api=async(path,opts={})=>{
 calls.push({path,opts});
 if(path==='/api/providers')return {providers:providers(),vendors:[{brand:'xai',title:'xAI',protocol:'chat_completions',models:['grok-4']}]};
 if(path==='/api/providers/oauth/xai/start'){if(failStart)throw Error('private-upstream-body');return {url:'https://auth.example/authorize'}}
 if(path==='/api/providers/oauth/xai/cancel'){if(cancelFail){const e=Error('暂时无法取消');e.status=409;throw e}return {status:'cancelled'}}
 if(path==='/api/providers/oauth/xai/status')return await new Promise(resolve=>resolveStatus=resolve);
 if(path==='/api/providers/oauth/xai/logout'){if(logoutFail){const e=Error('无法删除授权凭证，请重试');e.status=500;throw e}configured=false;return {status:'logged_out'}}
 if(path==='/api/providers/models?provider=xai'){if(expired){const e=Error('登录凭证已失效，请重新登录');e.status=422;throw e}return {models:[{model:'grok-4',context:256000,vision:false,hosted_web_search:true}]}}
 throw Error('unexpected '+path);
};
const flush=async()=>{for(let i=0;i<8;i++)await Promise.resolve()};
const starts=()=>calls.filter(c=>c.path.endsWith('/start')).length;
(async()=>{
 await window.XBotProviders.settings({api,host,isCurrent:()=>current});
 const card=find('button','xAI');assert(card,'xAI card missing');
 await card.onclick();await flush();
 // Idle: the header row reflects OAuth only; the key form is folded away.
 assert(find('button','登录 xAI'),'login CTA missing');
 assert(find('button','登录 xAI').className.includes('provider-btn-pri'),'login CTA is not the primary button');
 assert(statusText().includes('未登录'),'idle status missing');
 assert(!host.innerText.includes('密钥已配置'),'unconfigured key shown as configured');
 assert(!host.innerText.includes('登录未完成'),'idle shows a failure');
 assert(find('summary','用 API 密钥')&&!cls('provider-key-alt').open,'folded key entry missing');
 assert(find('button','保存密钥'),'key form missing');
 // The account login leads; pending shows a disabled wait button, cancel and reopen.
 const attempt=find('button','登录 xAI').onclick();await flush();
 assert.equal(starts(),1);
 const waiting=find('button','等待登录…');assert(waiting&&waiting.disabled,'pending button not disabled');
 assert(find('button','取消')&&find('button','再点重开'),'pending controls missing');
 assert(statusText().includes('等待登录')&&walk(cls('provider-status')).some(n=>n.tag==='button'&&n.textContent==='取消'),'pending status row missing');
 resolveStatus({state:'pending'});await flush();assert.equal(timers.length,1,'poll sleep missing');
 // Cancelling stops the pending login; the CTA returns without a new start.
 const cancelled=find('button','取消').onclick();await flush();await cancelled;
 assert(find('button','登录 xAI'),'cancelled login cannot restart');
 assert(host.innerText.includes('已取消登录'),'cancel note missing');
 assert(statusText().includes('未登录'),'cancel did not reset status');
 assert(calls.some(c=>c.path.endsWith('/cancel')),'cancel not sent');
 assert.equal(starts(),1,'cancel started a new login');
 // A failed login: separate error card, then re-login and cancel as separate controls.
 const retry=find('button','登录 xAI').onclick();await flush();assert.equal(starts(),2);
 resolveStatus({state:'failed',error:'授权未完成，请重试'});await retry;await flush();
 // Flat failure: one short red line, no tinted error card.
 const failLine=cls('provider-auth-fail');
 assert(failLine&&failLine.textContent==='登录未完成，请重试。'&&!failLine.children.length,'flat failure line missing');
 assert(!walk(host).some(n=>/provider-auth-error/.test(String(n.className))),'tinted error card still rendered');
 assert(host.innerText.includes('授权未完成'),'server failure text missing'); 
 const relogin=find('button','重新登录'),stop=find('button','取消');
 assert(relogin&&stop&&relogin!==stop&&relogin.textContent==='重新登录'&&stop.textContent==='取消','failed controls missing');
 assert(!find('button','登录 xAI'),'idle CTA kept next to the failure');
 assert(statusText().includes('登录未完成')&&!walk(cls('provider-status')).some(n=>String(n.className).includes('sdot ok')),'failed status row wrong');
 // Re-login succeeds; the OAuth binding pulls its catalog read-only.
 // The same cancel error twice must leave 取消 clickable.
 cancelFail=true;
 for(let i=0;i<2;i++){const c=stop.onclick();await flush();await c}
 assert.equal(stop.disabled,false,'cancel stuck disabled after a repeated error');
 assert(host.innerText.includes('暂时无法取消'),'cancel error hidden');
 cancelFail=false;
 const again=find('button','重新登录').onclick();await flush();assert.equal(starts(),3);
 configured=true;resolveStatus({state:'succeeded'});await again;await flush();
 // Logged in: the header carries only the 退出登录 icon; no 已登录 / 重新登录 words anywhere.
 const logoutBtn=()=>walk(cls('provider-status')).find(n=>n.className==='provider-logout');
 assert(logoutBtn()&&logoutBtn().attrs['aria-label']==='退出登录'&&logoutBtn().title==='退出登录','logout icon missing');
 assert(!host.innerText.includes('已登录')&&!host.innerText.includes('重新登录'),'logged-in detail shows 已登录 or 重新登录');
 assert(cls('provider-hosted-hint')&&cls('provider-hosted-hint').textContent.includes('已用厂商搜索')&&!statusText().includes('厂商搜索'),'hosted note not above the models');
 assert(!find('button','登录 xAI'),'logged-in detail still shows the login CTA');
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
 expired=false;
 find('button','模型与供应商').onclick();
 await find('button','xAI').onclick();await flush();assert(logoutBtn(),'logged-in detail missing');
 // A failed logout says so and leaves the icon usable.
 logoutFail=true;logoutBtn().onclick();await flush();
 assert(statusText().includes('无法删除授权凭证')&&logoutBtn().disabled===false,'failed logout hidden or stuck');
 assert(configured,'failed logout changed the binding');
 // Logging out calls the API and redraws the not-logged-in page.
 logoutFail=false;logoutBtn().onclick();await flush();await flush();
 assert(calls.filter(c=>c.path==='/api/providers/oauth/xai/logout').length===2,'logout not sent');
 assert(find('button','登录 xAI')&&statusText().includes('未登录'),'logout did not return to the login CTA');
 assert(find('summary','用 API 密钥')&&!logoutBtn(),'logged-out page wrong');
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
 assert(find('button','登录 xAI'),'key-configured xAI lost the login CTA');
 assert(find('summary','用 API 密钥'),'folded key entry missing');
 assert(find('button','保存密钥'),'key form missing');
 assert(statusText().includes('未登录'),'header status is not OAuth');
 assert(!statusText().includes('密钥已配置'),'saved key shown on the header row');
 const fold=cls('provider-key-alt');assert(!fold.open,'key fold opened by default');
 assert(fold.innerText.includes('密钥已配置 · 可用'),'saved key state missing from the fold');
 assert.equal(host.innerText.split('密钥已配置').length,2,'saved key state shown outside the fold');
 assert(host.innerText.includes('登录 xAI 账号即可使用'),'login lead missing');
 assert(!calls.some(c=>c.endsWith('/xai/sync')||c.includes('/api/providers/models')),'key-only xAI fetched models before login');
 assert(!host.innerText.includes('拉取模型失败'),'failed fetch shown before any fetch');
});
}
(async()=>{for(const run of cases)await run();completed=true})().catch(e=>{console.error(e);process.exitCode=1});`
	if out, err := exec.Command(node, "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("xai key-configured login CTA: %v: %s", err, out)
	}
}
