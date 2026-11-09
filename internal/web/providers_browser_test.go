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

// Use the shipped app and settings entrypoint; only HTTP data is scripted.
func TestProvidersSettingsBrowser(t *testing.T) {
	chrome := browsertest.Chrome(t)
	var err error
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
	for _, query := range []string{"", "state=loginsweep", "state=xai", "state=xaiunconfigured", "state=xaifailure", "state=opencode", "state=remove"} {
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
if(['xai','remove','removepreview'].includes(scene))providers.push({name:'xai-key',brand:'xai',protocol:'chat_completions',key_configured:true,models:[]});
if(['loggedin','chatgptdetail','remove','removepreview'].includes(scene))providers.push({name:'chatgpt',oauth:'chatgpt',protocol:'responses',key_configured:true,models:[]});
if(['loginsweep','remove','removepreview'].includes(scene))providers.push({name:'xai',oauth:'xai',protocol:'responses',key_configured:true,models:[]});
if(scene==='xaiunconfigured'){providers.push({name:'xai',oauth:'xai',protocol:'responses',key_configured:false,models:[]});fixture.xaiIdle=true;}
const vendors=[['gemini','Google Gemini'],['vertex','Vertex AI'],['xai','xAI'],['kimi','Kimi'],['kimi-code','Kimi Code'],['zhipu','Zhipu GLM'],['zai','Z.ai'],['minimax','MiniMax'],['stepfun','StepFun'],['mimo','Xiaomi MiMo'],['qianfan','Baidu Qianfan'],['tencent','Tencent Cloud'],['opencode-go','OpenCode Go']].map(([brand,title])=>({brand,title,protocol:'chat_completions',key_login:brand==='vertex',models:['m1','m2']}));
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
 if(/^\/api\/providers\/oauth\/(chatgpt|xai)\/local$/.test(path))return json({available:false});
 if(path==='/api/providers/oauth/chatgpt/start'){fixture.oauthPolls=0;return json({url:'https://auth.example/authorize'})}
 if(path==='/api/providers/oauth/chatgpt/status'){
  if(scene==='loginerror')return json({state:'failed',error:'private-upstream-body'});
  if(scene==='pending'||fixture.oauthPolls++===0)return json({state:'pending'});
  if(!providers.some(p=>p.oauth==='chatgpt'))providers.push({name:'chatgpt',oauth:'chatgpt',protocol:'responses',key_configured:true,models:[]});
  return json({state:'succeeded'});
 }
 if(path==='/api/providers/oauth/xai/start'){fixture.xaiStarts=(fixture.xaiStarts||0)+1;fixture.xaiCancelled=false;return json({url:'https://auth.example/authorize'})}
 const removeRoute=/^\/api\/providers\/oauth\/(chatgpt|xai)\/logout$/.exec(path);
 if(removeRoute){
  if(opts.method!=='POST')return json({error:'method'},405);
  if(fixture.slowRemove)await new Promise(r=>{fixture.releaseRemove=r});
  if(fixture.removeFail)return json({error:'移除失败，请重试'},500);
  const kind=removeRoute[1],i=providers.findIndex(p=>p.oauth===kind);if(i>=0)providers.splice(i,1);
  if(kind==='xai'){fixture.xaiSucceed=false;fixture.xaiIdle=true}return json({status:'logged_out'});
 }
 if(path==='/api/providers/oauth/xai/status'){
  if(fixture.xaiIdle)return json({state:'idle'});
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
const card=name=>[...document.querySelectorAll(name==='xAI'?'.provider-card:not(.provider-subscription)':'.provider-card')].find(n=>n.textContent.includes(name));
const subcard=name=>[...document.querySelectorAll('.provider-subscription')].find(n=>n.textContent.includes(name));
const subscriptionGroup=()=>[...document.querySelectorAll('.provider-subhead')].find(h=>h.textContent.includes('订阅'))?.closest('.provider-group');
const apiHead=()=>[...document.querySelectorAll('.provider-subhead')].find(h=>h.textContent.includes('API 接入'));
const catalogCards=()=>[...document.querySelectorAll('.provider-catalog .provider-card')];
const gap=(above,below)=>below.getBoundingClientRect().top-above.getBoundingClientRect().bottom;
const fullWidth=()=>$('.provider-models').getBoundingClientRect().width;
// xAI OAuth: login and catalog still work. Remove the subscription from its
// list card and keep the separate API key.
async function xaiLoggedIn(){
 subcard('xAI').click();await waitFor(()=>subcard('xAI').textContent.includes('等待登录'),'xai pending state missing');
 subcard('xAI').click();assert(fixture.xaiStarts===1,'pending click started another OAuth');fixture.xaiSucceed=true;
 await waitFor(()=>subcard('xAI')?.querySelector('[aria-label="已登录"]'),'xai subscription did not sign in');
 assert(fixture.xaiStarts===1,'xai subscription did not use OAuth');
 assert(providers.some(p=>p.name==='xai-key'&&p.key_configured),'xai key binding lost');
 subcard('xAI').click();await waitFor(()=>$('.provider-model-row'),'xai subscription detail missing');
 assert(!$('.provider-logout'),'old detail removal entry survived');
 assert(!/已登录|重新登录/.test($('#settings-providers').innerText),'xai logged-in page shows 已登录 or 重新登录');
 assert(!btn('登录')&&!$('.provider-status').textContent.includes('密钥已配置'),'xai logged-in detail kept login or key state');
 assert(gap($('.provider-hosted-hint'),$('.provider-model-count'))>=0&&$('.provider-hosted-hint').textContent.includes('已用厂商搜索'),'xai hosted note not above the models');
 assert(/grok-4/.test($('.provider-models').textContent)&&/厂商搜索/.test($('.provider-model-row').textContent)&&/看图/.test($('.provider-model-row').textContent),'xai models missing');
 assert($('.provider-auth-slot').getBoundingClientRect().height===0,'empty re-login slot takes space');
 assert($('.provider-key-alt')&&!$('.provider-key-alt').open,'logged-in key fold missing');
 assert($('.provider-key-alt .provider-key input')&&$('.provider-key-alt .provider-key button[type=submit]'),'subscription key form missing');
 fixture.fail=true;$('.provider-back').click();subcard('xAI').click();
 await waitFor(()=>btn('重试'),'catalog failure missing');
 assert($('.provider-key-alt .provider-key input'),'catalog failure hid key form');fixture.fail=false;
 $('.provider-back').click();assert(card('xAI').querySelector('strong').textContent==='xAI'&&!card('xAI').querySelector('strong').textContent.includes('xai-key'),'xai key card title is not human readable');card('xAI').click();
 await waitFor(()=>$('.provider-key-alt'),'old key detail missing');
 $('.provider-key-alt').open=true;
 const input=$('.provider-key input');assert(input&&$('.provider-key button[type=submit]'),'key form missing while logged in');
 input.value='scripted-key-12345';input.dispatchEvent(new Event('input'));$('.provider-key').requestSubmit();
 await waitFor(()=>fixture.saves===1,'key form did not submit');
 assert(!$('.provider-status').textContent.includes('账号登录占用'),'key status claims account occupies it');
 $('.provider-back').click();
 subcard('xAI').closest('.provider-subscription-wrap').querySelector('.provider-remove').click();
 await waitFor(()=>subcard('xAI')?.closest('.provider-directory'),'removed xai did not return to directory');
 assert(fixture.calls.includes('/api/providers/oauth/xai/logout'),'xai removal not sent');
 assert(providers.some(p=>p.name==='xai-key'&&p.key_configured),'removing xai subscription removed its key');
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
   if(scene==='remove'){
    const remove=kind=>subcard(kind).closest('.provider-subscription-wrap').querySelector('.provider-remove');
    const chat=remove('ChatGPT'),wrap=chat.parentElement,dot=wrap.querySelector('.sdot');
    assert(chat&&!chat.parentElement.closest('button'),'remove button is nested');
    const before=dot.getBoundingClientRect();chat.focus();
    assert(getComputedStyle(chat).opacity==='1'&&getComputedStyle(dot).opacity==='0','keyboard did not reveal removal');
    const hit=chat.getBoundingClientRect();
    assert(hit.width===36&&hit.height===36&&Math.abs(hit.x+hit.width/2-before.x-before.width/2)<1&&Math.abs(hit.y+hit.height/2-before.y-before.height/2)<1,'removal moved from the status point');
    assert(chat.contains(document.elementFromPoint(hit.x+hit.width/2,hit.y+hit.height/2)),'remove button cannot receive a click');
    fixture.removeFail=true;chat.click();await waitFor(()=>wrap.querySelector('[role=status]').textContent.trim()&&!chat.disabled,'removal error missing');
    assert(providers.some(p=>p.oauth==='chatgpt')&&!chat.disabled&&$('.provider-search'),'failure lost card or opened detail');
    const search=$('.provider-search input');search.value='订阅';search.dispatchEvent(new Event('input'));
    fixture.removeFail=false;fixture.slowRemove=true;
    const requestCount=fixture.calls.filter(p=>p.endsWith('/chatgpt/logout')).length;
    chat.click();chat.click();await waitFor(()=>fixture.releaseRemove,'removal request missing');
    assert(chat.disabled&&subcard('ChatGPT').disabled&&fixture.calls.filter(p=>p.endsWith('/chatgpt/logout')).length===requestCount+1,'duplicate removal was sent');
    fixture.slowRemove=false;fixture.releaseRemove();
    await waitFor(()=>subcard('ChatGPT')?.closest('.provider-directory'),'ChatGPT did not return to directory');
    assert($('.provider-search input').value==='订阅'&&!providers.some(p=>p.oauth==='chatgpt'),'removal lost query or account');
    assert(document.activeElement===subcard('ChatGPT'),'removal lost focus');
    remove('xAI').click();await waitFor(()=>!providers.some(p=>p.oauth==='xai')&&!remove('xAI'),'xAI removal did not finish');
    assert(providers.some(p=>p.name==='xai-key'&&p.key_configured),'subscription removal deleted API key');
    $('.provider-search input').value='';$('.provider-search input').dispatchEvent(new Event('input'));
    assert(card('xAI').closest('.provider-enabled')&&subcard('xAI').closest('.provider-directory'),'xAI identities merged after removal');
    assert(!$('.provider-enabled .provider-remove')&&!testErrors.length,'unexpected removal control or browser error');
   }
  if(scene==='loginsweep'){
   // After OAuth the subscription joins the enabled group and leaves the
   // directory; the empty subscription group and the API head must not stay.
   subcard('ChatGPT').click();
   await waitFor(()=>subcard('ChatGPT')?.querySelector('[aria-label="已登录"]'),'chatgpt subscription did not sign in');
   await waitFor(()=>subscriptionGroup().hidden,'empty subscription group stayed after login');
   const wrap=subcard('ChatGPT').closest('.provider-subscription-wrap');
   assert(subcard('ChatGPT').closest('.provider-enabled'),'subscription not moved to enabled');
   assert(apiHead().hidden,'api head visible without subscription group');
   const search=$('.provider-search input');search.value='xai';search.dispatchEvent(new Event('input'));
   assert(wrap.hidden,'enabled subscription stayed visible during search');
   search.value='';search.dispatchEvent(new Event('input'));
   assert(wrap.closest('.provider-enabled')&&subcard('xAI')&&subscriptionGroup().hidden&&apiHead().hidden,'clearing search lost entries');
   assert(catalogCards().filter(n=>n.querySelector('strong')?.textContent==='xAI').length>=1,'clearing search lost api cards');
  }
  if(scene==='opencode'||scene==='opencodepreview'){
   assert(!subcard('OpenCode Go'),'Go must not be an OAuth subscription');
   card('OpenCode Go').click();await waitFor(()=>$('.provider-key input'),'Go key form missing');
   if(scene==='opencode'){
    $('.provider-key input').value='scripted-go-key-12345';$('.provider-key').requestSubmit();
    await waitFor(()=>$('.provider-model-row'),'Go live models missing after key save');
    assert(fixture.calls.includes('/api/providers/opencode-go/key')&&fixture.calls.includes('/api/providers/opencode-go/sync'),'Go bypassed vendor routes');
    assert(!fixture.calls.some(p=>p.includes('/oauth/')&&!p.endsWith('/local')),'Go started OAuth');
   }
  }
  if(scene==='xaifailure'){fixture.xaiFail=true;subcard('xAI').click();await waitFor(()=>subcard('xAI').textContent.includes('登录未完成'),'xai failure missing');fixture.xaiFail=false;fixture.xaiSucceed=true;subcard('xAI').click();await waitFor(()=>subcard('xAI').querySelector('[aria-label="已登录"]'),'xai retry failed');}
  if(scene==='xaiunconfigured'){
   assert([...document.querySelectorAll('.provider-catalog .provider-card')].filter(n=>!n.classList.contains('provider-subscription')&&n.querySelector('strong')?.textContent==='xAI').length===1,'xaiunconfigured duplicated xAI');
   card('xAI').click();await waitFor(()=>btn('去订阅'),'unconfigured OAuth guidance missing');
   assert(!btn('登录')&&!btn('重新登录'),'unconfigured OAuth entry shows login CTA');
   assert($('.provider-status').textContent.includes('未登录'),'unconfigured OAuth status wrong');
   assert(!fixture.calls.some(p=>p.includes('/api/providers/models')||p.endsWith('/start')),'unconfigured OAuth fetched models or started login');
  }
  document.body.dataset.testResult='passed';return;
 }
 assert(!card('ChatGPT').disabled,'subscription login disabled');
 assert(subcard('ChatGPT')&&subcard('xAI'),'subscription cards missing');
 // Enabled entries move to the top group and leave the API catalog; unconfigured
 // subscriptions stay in the directory. xAI OAuth never joins the API catalog.
 const enabled=$('.provider-enabled'),directory=$('.provider-directory');
 assert(enabled&&!enabled.hidden&&/已启用/.test(enabled.textContent),'enabled section missing');
 assert(directory&&!directory.hidden&&/添加供应商/.test(directory.textContent),'directory section missing');
 assert(enabled.compareDocumentPosition(directory)&Node.DOCUMENT_POSITION_FOLLOWING,'enabled section not on top');
 assert(catalogCards().filter(n=>n.querySelector('strong')?.textContent==='DeepSeek').length===0,'configured DeepSeek still in catalog');
 assert([...enabled.querySelectorAll('.provider-card')].some(n=>n.querySelector('strong')?.textContent==='DeepSeek'&&n.textContent.includes('API 密钥')),'DeepSeek not in enabled');
 assert(!subscriptionGroup().hidden,'subscription group hidden without term');
 assert(catalogCards().filter(n=>!n.classList.contains('provider-subscription')&&n.querySelector('strong')?.textContent==='xAI').length===1,'xAI catalog entry missing or duplicated');
 const search=$('.provider-search input');search.value='xai';search.dispatchEvent(new Event('input'));
 assert(!directory.hidden&&subcard('xAI')&&!subcard('xAI').hidden&&subcard('ChatGPT').hidden,'xai subscription search failed');
 search.value='chatgpt';search.dispatchEvent(new Event('input'));assert(subcard('ChatGPT')&&!subcard('ChatGPT').hidden&&subcard('xAI').hidden,'chatgpt subscription search failed');
 search.value='';search.dispatchEvent(new Event('input'));
 for(const t of ['Google Gemini','Vertex AI','xAI','Kimi Code','Zhipu GLM','Z.ai','MiniMax','StepFun','Xiaomi MiMo','Baidu Qianfan','Tencent Cloud'])assert(card(t),'missing '+t);
 assert([...document.querySelectorAll('.provider-card img')].every(i=>/provider-icons\/[a-z-]+\.svg(\?v=\d+)?$/.test(i.getAttribute('src'))),'bad glyph');
 card('Vertex AI').click();await waitFor(()=>$('.provider-empty'),'vertex state');assert(!$('.provider-key')&&$('.provider-empty').textContent.includes('Google Cloud'),'vertex shown as keyable');assert(document.querySelectorAll('.provider-model-row').length===2,'vertex default models');
 $('.provider-back').click();
 // The vendor detail directs account login to the subscription, while keeping the key form.
 card('xAI').click();await waitFor(()=>btn('去订阅'),'xai subscription guidance missing');
 assert(!btn('登录')&&$('.provider-key-alt')&&!$('.provider-key-alt').open,'vendor detail still leads with login');
 btn('去订阅').click();assert(subcard('xAI').getAttribute('aria-current')==='true'&&document.activeElement===subcard('xAI'),'subscription not highlighted');
 card('xAI').click();await waitFor(()=>$('.provider-key input'),'missing key form');
 const typed=$('.provider-key input');typed.value='xai-0123456789';$('.provider-key').requestSubmit();
 await waitFor(()=>$('.provider-key-alt')?.textContent.includes('密钥已配置 · 可用'),'saved key missing');assert(fixture.saves===1,'key saved once');
 const words=/API_KEY|providers\.json|web\.json|xbot provider|hosted|vision|\benv\b|backend/i;
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
 assert([...document.querySelectorAll('.provider-card')].filter(n=>n.getClientRects().length).length===1,'search failed');card('OpenAI').click();await waitFor(()=>$('.provider-model-row'),'models missing');
 assert(document.querySelectorAll('.provider-model-row').length===2,'runtime models missing');assert(!$('#settings-providers table'),'obsolete table');
 assert(!words.test($('#settings-providers').textContent),'engineering words in key-only detail');assert(/厂商搜索/.test($('.provider-model-row').textContent)&&/看图/.test($('.provider-model-row').textContent),'plain wording missing');
 $('.provider-back').click();assert($('.provider-search input').value==='OpenAI','search reset on return');
 fixture.fail=true;card('OpenAI').click();await waitFor(()=>$('.provider-empty button'),'missing retry');assert($('.provider-empty').textContent.includes('失败'),'failed list silent');assert(!words.test($('#settings-providers').textContent),'engineering words in failed state');
 fixture.fail=false;$('.provider-empty button').click();await waitFor(()=>$('.provider-model-row'),'retry did not recover');assert(!words.test($('#settings-providers').textContent),'engineering words after retry');
 $('.provider-back').click();fixture.large=true;card('OpenAI').click();await waitFor(()=>document.querySelectorAll('.provider-model-row').length===2048,'large catalog missing');
 assert(!fixture.xaiStarts,'unexpected xAI login starts');assert(!fixture.calls.some(p=>p.includes('/oauth/chatgpt/start')),'OAuth started without subscription click');
 $('.provider-back').click();$('.provider-search input').value='chatgpt';$('.provider-search input').dispatchEvent(new Event('input'));
 assert(subcard('ChatGPT')&&!subcard('ChatGPT').hidden,'ChatGPT search hid subscription');
 card('ChatGPT').click();assert(card('ChatGPT').disabled,'start request remained clickable');
 await waitFor(()=>card('ChatGPT')?.querySelector('[aria-label="已登录"]'),'subscription did not sign in');
 assert(fixture.calls.filter(p=>p.endsWith('/chatgpt/start')).length===1,'duplicate OAuth start');
 assert([...document.querySelectorAll('.provider-card')].filter(n=>n.getClientRects().length).length===1,'duplicate ChatGPT vendor card');
 card('ChatGPT').click();await waitFor(()=>$('.provider-model-row'),'subscription detail missing');
 assert($('.provider-status').textContent.includes('已登录'),'subscription status missing');
 assert($('.provider-status button').textContent==='重新登录','relogin missing');assert(!$('#settings-providers table'),'subscription uses table');
 $('.provider-back').click();assert(document.activeElement===card('ChatGPT'),'subscription return lost focus');
 assert(testErrors.length===0,testErrors.join(';'));
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`
