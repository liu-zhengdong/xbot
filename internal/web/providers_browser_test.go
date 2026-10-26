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

// Use the shipped app and settings entrypoint; only HTTP data is scripted.
func TestProvidersSettingsBrowser(t *testing.T) {
	chrome, err := exec.LookPath("/Applications/Google Chrome.app/Contents/MacOS/Google Chrome")
	if err != nil {
		t.Skip("Chrome unavailable")
	}
	dir := t.TempDir()
	if evidence := os.Getenv("XBOT_PROVIDER_EVIDENCE"); evidence != "" {
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
			page = strings.Replace(page, "<head>", "<head><script>"+providersFixture+"</script>", 1)
			page = strings.Replace(page, "</body>", "<script>"+providersDriver+"</script></body>", 1)
			raw = []byte(page)
		}
		if path == "static/providers.js" {
			raw = []byte(strings.ReplaceAll(string(raw), "`/provider-icons/", "`./provider-icons/"))
		}
		return os.WriteFile(dst, raw, 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	// The xai scene runs the OAuth login path on its own page: a logged-in
	// xai binding cannot share a name with the key binding the main run saves.
	for _, query := range []string{"", "state=xai"} {
		page := (&url.URL{Scheme: "file", Path: filepath.Join(dir, "index.html"), RawQuery: query}).String()
		out, err := exec.Command(chrome, "--headless=new", "--disable-gpu", "--no-sandbox", "--no-first-run", "--no-default-browser-check", "--allow-file-access-from-files", "--user-data-dir="+filepath.Join(t.TempDir(), "chrome"), "--virtual-time-budget=20000", "--dump-dom", page).CombinedOutput()
		if err != nil {
			t.Fatalf("Chrome %q: %v\n%s", query, err, out)
		}
		checkProvidersBrowser(t, query, string(out))
	}
}

func checkProvidersBrowser(t *testing.T, query, out string) {
	t.Helper()

	if !strings.Contains(out, `data-test-result="passed"`) {
		result := out
		if i := strings.Index(result, "data-test-result="); i >= 0 {
			result = result[i:min(i+800, len(result))]
		} else if len(result) > 1500 {
			result = result[:1500]
		}
		t.Fatalf("provider browser %q: %s", query, result)
	}
}

const providersFixture = `
const requestedState=new URLSearchParams(location.search).get('state')||'';
const scene=requestedState.split('-')[0];
localStorage.setItem('xbot-theme',requestedState.endsWith('-dark')?'dark':new URLSearchParams(location.search).get('theme')||'light');
window.testErrors=[];window.addEventListener('error',e=>testErrors.push(e.message));window.addEventListener('unhandledrejection',e=>testErrors.push(String(e.reason)));
const fixture={calls:[],fail:false,empty:false,large:false,oauthPolls:0};
window.open=()=>({opener:null,closed:false,location:{replace(){}},close(){this.closed=true}});
const providers=[{name:'',brand:'deepseek',protocol:'chat_completions',api_key_env:'XBOT_API_KEY',key_configured:true,models:[]},{name:'openai',protocol:'responses',api_key_env:'OPENAI_API_KEY',key_configured:true,models:[]},{name:'anthropic',protocol:'anthropic_messages',api_key_env:'ANTHROPIC_API_KEY',key_configured:true,models:[]}];
if(['loggedin','chatgptdetail'].includes(scene))providers.push({name:'chatgpt',oauth:'chatgpt',protocol:'responses',key_configured:true,models:[]});
const vendors=[['gemini','Google Gemini'],['vertex','Vertex AI'],['xai','xAI'],['kimi','Kimi'],['kimi-code','Kimi Code'],['zhipu','Zhipu GLM'],['zai','Z.ai'],['minimax','MiniMax'],['stepfun','StepFun'],['mimo','Xiaomi MiMo'],['qianfan','Baidu Qianfan'],['tencent','Tencent Cloud']].map(([brand,title])=>({brand,title,protocol:'chat_completions',key_login:brand==='vertex',models:['m1','m2']}));
const json=(v,status=200)=>new Response(JSON.stringify(v),{status,headers:{'Content-Type':'application/json'}});
window.fetch=async(path,opts={})=>{
 fixture.calls.push(path);
 if(path==='/api/account/me')return json({account:{email:'zh***@example.com',owner:true,hosted:false,gateway:'xbot.babelgo.cn',official:true,gateway_ok:true}});
 if(path==='/api/session')return json({authed:true,owner:'owner',owner_name:'政东',version:'dev-test'});
 if(path==='/api/bots/status'||path==='/api/bots')return json({bots:[]});
 if(path==='/api/notifications')return json({notifications:[]});
 if(path==='/api/groups')return json({groups:[],folders:[]});
 if(path==='/api/providers')return json({providers,vendors});
 const keyRoute=/^\/api\/providers\/([a-z-]+)\/key$/.exec(path);
 if(keyRoute){const brand=keyRoute[1];fixture.saves=(fixture.saves||0)+1;if(scene==='keyfail')return json({error:'这不像一个 API key'},400);if(fixture.slowSave)await new Promise(r=>{fixture.releaseSave=r});providers.push({name:brand,brand,protocol:'chat_completions',api_key_env:brand.toUpperCase()+'_API_KEY',key_configured:true,models:[]});return json({status:'ok',provider:brand})}
 if(path==='/api/providers/oauth/chatgpt/start'){fixture.oauthPolls=0;return json({url:'https://auth.example/authorize'})}
 if(path==='/api/providers/oauth/chatgpt/status'){
  if(scene==='loginerror')return json({state:'failed',error:'private-upstream-body'});
  if(scene==='pending'||fixture.oauthPolls++===0)return json({state:'pending'});
  if(!providers.some(p=>p.oauth==='chatgpt'))providers.push({name:'chatgpt',oauth:'chatgpt',protocol:'responses',key_configured:true,models:[]});
  return json({state:'succeeded'});
 }
 if(path==='/api/providers/oauth/xai/start'){fixture.xaiStarts=(fixture.xaiStarts||0)+1;fixture.xaiCancelled=false;return json({url:'https://auth.example/authorize'})}
 if(path==='/api/providers/oauth/xai/status'){
  if(fixture.xaiCancelled)return json({state:'cancelled'});
  if(fixture.xaiSucceed){if(!providers.some(p=>p.oauth==='xai'))providers.push({name:'xai',oauth:'xai',protocol:'responses',key_configured:true,models:[]});return json({state:'succeeded'})}
  if(fixture.xaiFail)return json({state:'failed',error:'授权未完成，请重试'});
  return json({state:'pending'});
 }
 if(path==='/api/providers/oauth/xai/cancel'){fixture.xaiCancelled=true;return json({status:'cancelled'})}
 if(String(path).startsWith('/api/providers/models?')||/^\/api\/providers\/[a-z-]+\/sync$/.test(path)){fixture.catalogCalls=(fixture.catalogCalls||[]).concat(path);
  if(fixture.fail||scene==='error')return json({error:'upstream failed'},422);
  if(fixture.empty||scene==='empty')return json({models:[]});
  if(String(path).endsWith('provider=xai'))return json({models:[{model:'grok-4',context:256000,vision:true,hosted_web_search:true},{model:'grok-3',context:131072,vision:true,hosted_web_search:true}]});
  if(fixture.large)return json({models:Array.from({length:2048},(_,i)=>({model:'runtime-'+i,context:128000,vision:false,hosted_web_search:false}))});
  return json({models:[{model:'gpt-5.5',context:1000000,vision:true,hosted_web_search:true},{model:'gpt-5.4',context:272000,vision:true,hosted_web_search:true}]});
 }
 return json({});
};
`

const providersDriver = `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)},btn=text=>[...document.querySelectorAll('button')].find(b=>b.textContent===text);
const waitFor=async(fn,msg)=>{for(let i=0;i<400;i++){if(fn())return;await new Promise(r=>setTimeout(r,10))}throw Error(msg)};
const card=name=>[...document.querySelectorAll('.provider-card')].find(n=>n.textContent.includes(name));
const gap=(above,below)=>below.getBoundingClientRect().top-above.getBoundingClientRect().bottom;
const fullWidth=()=>$('.provider-models').getBoundingClientRect().width;
// xAI OAuth: log in, then a failed re-login from the header shows the error
// card above its own buttons with a visible gap, and cancel returns to logged in.
async function xaiLoggedIn(){
 card('xAI').click();await waitFor(()=>btn('登录 xAI'),'xai login CTA missing');
 fixture.xaiSucceed=true;btn('登录 xAI').click();
 await waitFor(()=>$('.provider-status').textContent.includes('已登录')&&$('.provider-model-row'),'xai did not log in');
 assert($('.provider-status .sdot.ok')&&$('.provider-status .provider-relink').textContent==='重新登录','xai logged-in status row missing');
 assert(!btn('登录 xAI')&&!$('.provider-status').textContent.includes('密钥已配置'),'xai logged-in detail kept login or key state');
 assert(/grok-4/.test($('.provider-models').textContent)&&/厂商搜索/.test($('.provider-model-row').textContent)&&/看图/.test($('.provider-model-row').textContent),'xai models missing');
 assert($('.provider-auth-slot').getBoundingClientRect().height===0,'empty re-login slot takes space');
 fixture.xaiSucceed=false;$('.provider-status .provider-relink').click();
 await waitFor(()=>$('.provider-status').textContent.includes('等待登录')&&$('.provider-auth-slot .provider-btn-sec'),'xai re-login pending missing');
 fixture.xaiFail=true;await waitFor(()=>$('.provider-auth-slot .provider-auth-fail').textContent.includes('登录未完成，请重试'),'xai re-login failure line missing');
 const err=$('.provider-auth-slot .provider-auth-fail'),again=$('.provider-auth-slot .provider-btn-pri');
 assert(again.textContent==='重新登录'&&gap(err,again)>=8,'xai re-login failure line touches its button: '+gap(err,again));
 assert(gap(again,$('.provider-model-count'))>=8,'xai re-login buttons touch the model list');
 assert($('.provider-status').textContent.includes('登录未完成')&&!$('.provider-status .sdot.ok'),'xai re-login failed status row wrong');
 assert(!/登录 xAI登录未完成|重新登录登录未完成/.test($('#settings-providers').innerText),'xai re-login button and error glued together');
 const cancels=fixture.calls.filter(p=>p==='/api/providers/oauth/xai/cancel').length;
 $('.provider-auth-slot .provider-btn-sec').click();
 await waitFor(()=>$('.provider-status').textContent.includes('已登录')&&!$('.provider-auth-slot .provider-auth-fail').textContent,'xai cancel did not return to logged in');
 assert(fixture.calls.filter(p=>p==='/api/providers/oauth/xai/cancel').length===cancels+1,'xai cancel not sent');
 assert($('.provider-auth-slot').getBoundingClientRect().height===0,'cancelled re-login slot takes space');
 assert(testErrors.length===0,testErrors.join(';'));
}
(async()=>{
 await waitFor(()=>window.XBotSettings&&!$('#shell').hidden,'app did not boot');
 // Enter through the account menu, as in daily use.
 $('#account').click();$('#account-settings').click();await XBotSettings.open('providers');
 if(scene){
  if(['pending','loginerror','chatgptdetail'].includes(scene))card('ChatGPT').click();
  if(['detail','error','empty','unconfigured'].includes(scene))card(scene==='unconfigured'?'OpenRouter':'OpenAI').click();
  if(scene==='search'){$('.provider-search input').value='OpenAI';$('.provider-search input').dispatchEvent(new Event('input'))}
  if(scene==='xai')await xaiLoggedIn();
  document.body.dataset.testResult='passed';return;
 }
 assert(!card('ChatGPT').disabled,'subscription login disabled');
 assert(document.querySelectorAll('.provider-card').length===17,'missing provider');
 for(const t of ['Google Gemini','Vertex AI','xAI','Kimi Code','Zhipu GLM','Z.ai','MiniMax','StepFun','Xiaomi MiMo','Baidu Qianfan','Tencent Cloud'])assert(card(t),'missing '+t);
 assert([...document.querySelectorAll('.provider-card img')].every(i=>/provider-icons\/[a-z-]+\.svg$/.test(i.getAttribute('src'))),'bad glyph');
 card('Vertex AI').click();await waitFor(()=>$('.provider-empty'),'vertex state');assert(!$('.provider-key')&&$('.provider-empty').textContent.includes('Google Cloud'),'vertex shown as keyable');assert(document.querySelectorAll('.provider-model-row').length===2,'vertex default models');
 $('.provider-back').click();
 // xAI leads with account login; the key form folds away as a secondary entry.
 card('xAI').click();await waitFor(()=>btn('登录 xAI'),'xai login CTA missing');
 assert(!$('.provider-key-alt').open,'xai key form not folded');assert($('.provider-key input'),'xai key form missing');
 assert($('.provider-status').textContent.includes('未登录')&&!$('#settings-providers').textContent.includes('密钥已配置'),'xai header status is not OAuth');
 // Full width: the login button fills the column and the state sits on the header's right edge.
 assert($('.provider-models').getBoundingClientRect().width>=$('#settings-providers').getBoundingClientRect().width-2&&btn('登录 xAI').getBoundingClientRect().width>=fullWidth()-2,'xai login CTA does not fill the column');
 assert($('.provider-detail-heading').getBoundingClientRect().right-$('.provider-status').getBoundingClientRect().right<2,'xai state not on the header right');
 assert(!/API_KEY|providers\.json|web\.json|xbot provider|hosted/.test($('#settings-providers').textContent),'engineering words shown');
 btn('登录 xAI').click();await waitFor(()=>btn('取消')&&btn('再点重开'),'xai pending controls missing');
 assert(btn('等待登录…').disabled&&$('.provider-status').textContent.includes('等待登录'),'xai pending state missing');
 // Cancelling a pending login by clicking 取消 sends cancel and returns to 登录 xAI.
 $('.provider-auth .provider-btn-sec').click();
 await waitFor(()=>btn('登录 xAI')&&$('.provider-status').textContent.includes('未登录'),'xai cancel did not return to idle');
 assert(fixture.calls.filter(p=>p==='/api/providers/oauth/xai/cancel').length===1,'xai pending cancel not sent');
 // A failed login keeps a readable error with cancel and re-login visible.
 btn('登录 xAI').click();await waitFor(()=>btn('再点重开'),'xai second login not pending');
 fixture.xaiFail=true;await waitFor(()=>btn('重新登录')&&$('.provider-auth .provider-btn-sec'),'xai retry after failure missing');
 // Flat failure: a short red line (the lead gives way), then both buttons on one full-width row.
 const failLine=$('.provider-auth-fail');
 assert(failLine.textContent==='登录未完成，请重试。'&&!$('.provider-auth-error')&&$('.provider-auth .provider-login-note').textContent.includes('授权未完成'),'xai flat failure missing');
 assert(getComputedStyle(failLine).backgroundColor==='rgba(0, 0, 0, 0)'&&getComputedStyle($('.provider-xai-lead')).display==='none','xai failure is not flat');
 const again=btn('重新登录'),stop=$('.provider-auth .provider-btn-sec');
 assert(gap(failLine,again)>=8&&Math.abs(again.getBoundingClientRect().top-stop.getBoundingClientRect().top)<1,'xai failure layout wrong');
 assert(stop.getBoundingClientRect().right-again.getBoundingClientRect().left>=fullWidth()-2,'xai failure buttons do not fill the column');
 assert(!/登录 xAI登录未完成|重新登录登录未完成/.test($('#settings-providers').innerText),'xai button and error glued together');
 assert($('.provider-status').textContent.includes('登录未完成')&&!$('.provider-status .sdot.ok'),'xai failed status row wrong');
 // Cancelling the failed login by clicking 取消 returns to 登录 xAI.
 $('.provider-auth .provider-btn-sec').click();
 await waitFor(()=>btn('登录 xAI')&&!$('.provider-auth-fail').textContent,'xai failed cancel did not return to idle');
 assert(fixture.calls.filter(p=>p==='/api/providers/oauth/xai/cancel').length===2,'xai failed cancel not sent');
 fixture.xaiFail=false;$('.provider-back').click();
 card('xAI').click();await waitFor(()=>$('.provider-key input'),'missing key form');
 const typed=$('.provider-key input');assert(typed.type==='password','key input visible');typed.value='xai-0123456789';$('.provider-key').requestSubmit();
 // A saved key is secondary on xAI: the detail still leads with account login and fetches nothing.
 await waitFor(()=>btn('登录 xAI')&&$('.provider-key-alt').textContent.includes('密钥已配置 · 可用'),'xai login after key');assert(fixture.saves===1,'key saved once');
 assert(!$('.provider-status').textContent.includes('密钥已配置')&&$('.provider-status').textContent.includes('未登录'),'saved xai key shown on the header row');
 assert(!(fixture.catalogCalls||[]).includes('/api/providers/xai/sync'),'key-only xai fetched models before login');
 const words=/API_KEY|providers\.json|web\.json|xbot provider|hosted|vision|\benv\b|backend/i;
 assert(!words.test($('#settings-providers').textContent),'engineering words after save');
 $('.provider-back').click();card('xAI').click();await waitFor(()=>btn('登录 xAI')&&$('.provider-key-alt').textContent.includes('密钥已配置'),'configured xai detail');assert(!$('.provider-key-alt').open&&!$('.provider-status').textContent.includes('密钥已配置'),'configured xai key left the fold');assert(!words.test($('#settings-providers').textContent),'engineering words in configured detail');
 // A save that finishes late must not repaint over the page the person moved to.
 $('.provider-back').click();card('MiniMax').click();await waitFor(()=>$('.provider-key input'),'minimax form');
 $('.provider-key input').value='minimax-0123456789';fixture.slowSave=true;$('.provider-key').requestSubmit();await waitFor(()=>fixture.releaseSave,'slow save pending');
 $('.provider-back').click();card('Kimi').click();await waitFor(()=>$('.provider-key input'),'kimi form');
 const kimi=$('.provider-key input');kimi.value='kimi-typed-0123456789';fixture.slowSave=false;fixture.releaseSave();
 await new Promise(r=>setTimeout(r,150));
 assert($('.provider-key input')===kimi&&kimi.value==='kimi-typed-0123456789'&&$('.provider-detail-title h2').textContent==='Kimi','late save overwrote the new page');
 $('.provider-back').click();assert(card('xAI').querySelector('[aria-label="已配置"]'),'configured dot');
 const before=fixture.calls.length;card('OpenRouter').click();await waitFor(()=>$('.provider-empty'),'missing key state');assert(!fixture.calls.slice(before).some(p=>p.startsWith('/api/providers/models')),'unconfigured provider requested models');
 $('.provider-back').click();const input=$('.provider-search input');input.focus();input.value='OpenAI';input.dispatchEvent(new Event('input'));assert(document.activeElement===input,'search lost focus');
 assert(document.querySelectorAll('.provider-card:not(:disabled)').length===2,'search failed');card('OpenAI').click();await waitFor(()=>$('.provider-model-row'),'models missing');
 assert(document.querySelectorAll('.provider-model-row').length===2,'runtime models missing');assert(!$('#settings-providers table'),'obsolete table');
 assert(!words.test($('#settings-providers').textContent),'engineering words in key-only detail');assert(/厂商搜索/.test($('.provider-model-row').textContent)&&/看图/.test($('.provider-model-row').textContent),'plain wording missing');
 $('.provider-back').click();assert($('.provider-search input').value==='OpenAI','search reset on return');
 fixture.fail=true;card('OpenAI').click();await waitFor(()=>$('.provider-empty button'),'missing retry');assert($('.provider-empty').textContent.includes('失败'),'failed list silent');assert(!words.test($('#settings-providers').textContent),'engineering words in failed state');
 fixture.fail=false;$('.provider-empty button').click();await waitFor(()=>$('.provider-model-row'),'retry did not recover');assert(!words.test($('#settings-providers').textContent),'engineering words after retry');
 $('.provider-back').click();fixture.large=true;card('OpenAI').click();await waitFor(()=>document.querySelectorAll('.provider-model-row').length===2048,'large catalog missing');
 assert(fixture.xaiStarts===2,'unexpected xAI login starts');assert(!fixture.calls.some(p=>p.includes('/oauth/chatgpt/start')),'OAuth started without subscription click');
 $('.provider-back').click();$('.provider-search input').value='chatgpt';$('.provider-search input').dispatchEvent(new Event('input'));
 assert(!$('.provider-section').hidden,'ChatGPT search hid subscription');
 card('ChatGPT').click();assert(card('ChatGPT').disabled,'start request remained clickable');
 await waitFor(()=>card('ChatGPT')?.querySelector('[aria-label="已登录"]'),'subscription did not sign in');
 assert(fixture.calls.filter(p=>p.endsWith('/chatgpt/start')).length===1,'duplicate OAuth start');
 assert(document.querySelectorAll('.provider-card').length===1,'duplicate ChatGPT vendor card');
 card('ChatGPT').click();await waitFor(()=>$('.provider-model-row'),'subscription detail missing');
 assert($('.provider-status').textContent.includes('已登录'),'subscription status missing');
 assert($('.provider-status button').textContent==='重新登录','relogin missing');assert(!$('#settings-providers table'),'subscription uses table');
 $('.provider-back').click();assert(document.activeElement===card('ChatGPT'),'subscription return lost focus');
 assert(testErrors.length===0,testErrors.join(';'));
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`
