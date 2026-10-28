package web

import (
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Check rendered surfaces in both themes, including the gap which makes a
// transparent border look like an outline when it exposes the page.
func TestSelectedCardSurfaces(t *testing.T) {
	chrome := ""
	for _, name := range []string{"google-chrome", "chromium", "chromium-browser", "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"} {
		if path, err := exec.LookPath(name); err == nil {
			chrome = path
			break
		}
	}
	if chrome == "" {
		t.Skip("Chrome is not installed")
	}
	var styles strings.Builder
	for _, file := range []string{"static/tokens.css", "static/app.css", "static/providers.css", "static/web-search.css"} {
		data, err := staticFS.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		styles.Write(data)
		styles.WriteByte('\n')
	}
	page := `<!doctype html><style>` + styles.String() + `</style><div id="settings-providers"><div class="provider-grid"><button class="provider-card" aria-current="true">selected</button><button class="provider-card">other</button></div></div><div class="binding-group"><button class="binding-option" aria-checked="true">model</button></div><div class="ws-choices"><button class="ws-choice on">search</button><button class="ws-choice">other</button></div><div class="ws-list"><button class="ws-provider on">provider</button><div class="ws-key">key</div></div><script>
 try {
  const bg = e => getComputedStyle(e).backgroundColor;
  const q = s => document.querySelector(s);
  const rgb = s => (s.match(/\d+/g)||[]).slice(0,3).map(Number);
  for (const theme of ['light','dark']) {
   document.documentElement.dataset.theme=theme;
   const grid=q('.provider-grid'), selected=q('.provider-card[aria-current]'), other=q('.provider-card:not([aria-current])');
   if(bg(grid)!==bg(other)) throw Error(theme+' provider gap differs from card');
   const delta=(a,b)=>rgb(a).reduce((n,v,i)=>n+Math.abs(v-rgb(b)[i]),0);
   if(delta(bg(selected),bg(grid))>30) throw Error(theme+' provider edge contrast too high');
   for (const [surface,parent] of [['.binding-option','.binding-group'],['.ws-choice.on','.ws-choices'],['.ws-provider.on','.ws-list']]) {
    if(delta(bg(q(surface)),bg(q(parent)))>30) throw Error(theme+' '+surface+' edge contrast too high');
   }
   if(bg(q('.ws-key'))!==bg(q('.ws-provider.on'))) throw Error(theme+' expanded key surface differs');
  }
  document.body.dataset.result='pass';
 } catch(e) { document.body.dataset.result=e.message; }
 </script>`
	dir := t.TempDir()
	path := filepath.Join(dir, "surfaces.html")
	if err := os.WriteFile(path, []byte(page), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(chrome, "--headless=new", "--disable-gpu", "--no-sandbox", "--no-first-run", "--no-default-browser-check", "--user-data-dir="+filepath.Join(dir, "chrome"), "--dump-dom", (&url.URL{Scheme: "file", Path: path}).String()).CombinedOutput()
	if err != nil {
		t.Fatalf("Chrome: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), `data-result="pass"`) {
		t.Fatalf("surface regression: %s", out)
	}
}
