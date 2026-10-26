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
func TestImageGenSettingsBrowser(t *testing.T) {
	chrome, err := exec.LookPath("/Applications/Google Chrome.app/Contents/MacOS/Google Chrome")
	if err != nil {
		t.Skip("Chrome unavailable")
	}
	dir := t.TempDir()
	if evidence := os.Getenv("XBOT_IMAGE_GEN_EVIDENCE"); evidence != "" {
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
			page = strings.Replace(page, "<head>", "<head><script>"+providersFixture+imageGenFixture+"</script>", 1)
			page = strings.Replace(page, "</body>", "<script>"+imageGenDriver+"</script></body>", 1)
			raw = []byte(page)
		}
		if path == "static/providers.js" || path == "static/image-gen.js" {
			raw = []byte(strings.ReplaceAll(string(raw), "`/provider-icons/", "`./provider-icons/"))
		}
		return os.WriteFile(dst, raw, 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"", "theme=dark"} {
		page := (&url.URL{Scheme: "file", Path: filepath.Join(dir, "index.html"), RawQuery: query}).String()
		out, err := exec.Command(chrome, "--headless=new", "--disable-gpu", "--no-sandbox", "--no-first-run", "--no-default-browser-check", "--allow-file-access-from-files", "--user-data-dir="+filepath.Join(t.TempDir(), "chrome"), "--virtual-time-budget=20000", "--dump-dom", page).CombinedOutput()
		if err != nil {
			t.Fatalf("Chrome %q: %v\n%s", query, err, out)
		}
		checkProvidersBrowser(t, query, string(out))
	}
}

const imageGenFixture = `
const imageState={enabled:false,provider:'openai',providers:[{id:'openai',name:'OpenAI',logged_in:false},{id:'xai',name:'Grok（xAI）',logged_in:false}]};
const originalFetch=window.fetch;
window.fetch=async(path,opts={})=>{
 if(path==='/api/image-gen'){
  if(opts.method==='PUT'){
   if(fixture.imageSaveFail)return json({error:'save failed'},500);
   if(fixture.imageSaveSlow)await new Promise(r=>fixture.releaseImageSave=r);
   Object.assign(imageState,JSON.parse(opts.body));
  }
  return json(imageState);
 }
 return originalFetch(path,opts);
};
`

const imageGenDriver = `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const waitFor=async(fn,msg)=>{for(let i=0;i<400;i++){if(fn())return;await new Promise(r=>setTimeout(r,10))}throw Error(msg)};
const visible=n=>!!n&&n.getClientRects().length>0;
(async()=>{
 await waitFor(()=>window.XBotSettings,'settings not ready');
 const params=new URLSearchParams(location.search),shot=params.get('shot');
 if(shot){
  imageState.enabled=shot!=='off';imageState.provider=shot==='need-login'?'xai':'openai';imageState.providers[0].logged_in=true;
  await XBotSettings.open('image-gen');document.body.dataset.testResult='passed';return;
 }
 await XBotSettings.open('image-gen');
 const tabs=[...document.querySelectorAll('[data-set]')].map(b=>b.dataset.set),i=tabs.indexOf('image-gen');
 assert(tabs[i-1]==='persona'&&tabs[i+1]==='devices','navigation order');
 const toggle=()=>$('#settings-image-gen [role=switch]'),radio=id=>$('#settings-image-gen [data-provider="'+id+'"]');
 const saved=async()=>waitFor(()=>toggle()&&!toggle().disabled,'save stuck');
 assert(toggle().getAttribute('aria-checked')==='false','default not off');
 assert(!visible($('#settings-image-gen [role=radiogroup]')),'off exposes choices');
 assert(!visible($('#settings-image-gen .ws-provider-foot')),'off exposes footnote');
 toggle().click();await saved();
 assert(visible(radio('openai'))&&visible(radio('xai')),'missing choices');
 assert(!$('#settings-image-gen input'),'login form in image settings');
 const guide=()=>[...document.querySelectorAll('#settings-image-gen .ig-guide')].find(visible);
 assert(guide(),'missing login guide');guide().querySelector('button').click();
 await waitFor(()=>$('#settings-providers h2')?.textContent==='ChatGPT','OpenAI did not reach OAuth detail');
 assert(!fixture.calls.some(p=>p.endsWith('/start')),'navigation started login');
 assert([...document.querySelectorAll('#settings-providers button')].some(b=>b.textContent==='登录'),'missing existing ChatGPT login');
 await XBotSettings.open('image-gen');radio('xai').click();await saved();guide().querySelector('button').click();
 await waitFor(()=>$('#settings-providers .provider-detail-title h2')?.textContent==='xAI','Grok did not reach xAI detail');
 assert(!fixture.calls.some(p=>p.endsWith('/start')),'xAI navigation started login');
 imageState.providers[1].logged_in=true;
 await XBotSettings.open('image-gen');
 assert(!guide(),'logged-in selection still asks login');
 assert(radio('xai').querySelector('.ws-meta').textContent==='已登录','missing login state');
 fixture.imageSaveFail=true;toggle().click();await saved();
 assert(toggle().getAttribute('aria-checked')==='true','failed PUT changed setting');
 assert(visible($('#settings-image-gen [role=alert]')),'failed PUT silent');fixture.imageSaveFail=false;
 // A late PUT cannot redraw a closed pane or overwrite a newer mount.
 fixture.imageSaveSlow=true;toggle().click();await waitFor(()=>fixture.releaseImageSave,'PUT did not start');
 XBotSettings.close();fixture.releaseImageSave();fixture.imageSaveSlow=false;
 await new Promise(r=>setTimeout(r,30));assert(!$('#settings-image-gen').children.length,'late response repainted closed pane');
 await XBotSettings.open('image-gen');assert(toggle().getAttribute('aria-checked')==='false','reopen did not reload saved settings');
 toggle().click();await saved();radio('xai').focus();radio('xai').dispatchEvent(new KeyboardEvent('keydown',{key:'ArrowUp',bubbles:true}));await saved();
 assert(radio('openai').getAttribute('aria-checked')==='true','keyboard did not select provider');
 assert(testErrors.length===0,testErrors.join(';'));
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`
