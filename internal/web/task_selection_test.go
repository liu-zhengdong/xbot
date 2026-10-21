package web

import (
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestTaskIDSelectionSurvivesTwoPolls(t *testing.T) {
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
	raw, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	part := func(a, b string) string {
		i, j := strings.Index(src, a), strings.Index(src, b)
		if i < 0 || j <= i {
			t.Fatal("task functions not found")
		}
		return src[i:j]
	}
	page := `<!doctype html><body><script>
 const el=(tag,cls,text)=>{const n=document.createElement(tag);n.className=cls||'';if(text)n.textContent=text;return n};
 const icon=()=>el('span'),spinIcon=icon,avatar=icon,plainText=x=>x,clip=(x,n)=>Array.from(x).slice(0,n).join(''),taskStatus=x=>x,when=x=>x,displayText=x=>x,elapsedWork=()=>'',zhError=x=>x;
 const confirmTaskStop=()=>{},errorDetails=()=>{},controlAgent=async()=>true;
 ` + part("  function taskTypeLabel(", "  function paintGreeting(") + part("  function taskCard(", "  const routineChanges") + `
 const ag={id:'sa_0123456789abcdef',type:'dev',task:'核对页面',status:'running',steps:3},card=taskCard(ag);document.body.append(card);
 const info=card.querySelector('details');info.open=true;
 const id=card.querySelector('.task-id'),range=document.createRange();range.selectNodeContents(id);document.getSelection().addRange(range);
 let polls=0;const timer=setInterval(()=>{
  try {
   updateTaskCard(card,{...ag,steps:3+polls});polls++;
   if(document.getSelection().toString()!==ag.id)throw Error('selection lost after poll '+polls);
   if(polls===2){clearInterval(timer);taskValue(id,'new-id');if(document.getSelection().toString()!==ag.id)throw Error('selected node changed');document.getSelection().removeAllRanges();taskValue(id,'new-id');if(id.textContent!=='new-id')throw Error('update did not resume after selection ended');document.body.dataset.result='passed-two-polls'}
  } catch(e) {clearInterval(timer);document.body.dataset.result=e.message;}
 },3000);
 </script>`
	dir := t.TempDir()
	path := filepath.Join(dir, "selection.html")
	if err := os.WriteFile(path, []byte(page), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(chrome, "--headless=new", "--disable-gpu", "--no-sandbox", "--no-first-run", "--no-default-browser-check", "--user-data-dir="+filepath.Join(dir, "chrome"), "--virtual-time-budget=6500", "--dump-dom", (&url.URL{Scheme: "file", Path: path}).String()).CombinedOutput()
	if err != nil {
		t.Fatalf("Chrome: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), `data-result="passed-two-polls"`) {
		t.Fatalf("task selection did not survive two 3-second polls:\n%s", out)
	}
}
