package web

import (
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func pathFunctions(t *testing.T, layout bool) string {
	t.Helper()
	raw, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	a := strings.Index(s, "  function compactPath(")
	end := "  const pathLabels"
	if layout {
		end = "  function appendInline("
	}
	b := strings.Index(s, end)
	if a < 0 || b < a {
		t.Fatal("path functions missing")
	}
	return s[a:b]
}
func TestCompactPathKeepsSegmentsAndTheWholeFilename(t *testing.T) {
	script := `const assert=require('node:assert/strict');` + pathFunctions(t, false) + `
 const path='~/.config/xbot/first/second/日常工作/.zshrc';
 const keep='~/.config/xbot/…/日常工作/.zshrc',short='~/.config/xbot/…/.zshrc';
 assert.equal(compactPath(path,s=>s.length<=keep.length),keep);
 assert.equal(compactPath(path,s=>s.length<=short.length),short);
 assert.equal(compactPath(path,()=>true),path);
 assert.equal(compactPath('/Users/me/a/b/daily/file.txt',s=>s.length<='/Users/me/…/daily/file.txt'.length),'/Users/me/…/daily/file.txt');
 assert.equal(compactPath('C:\\Users\\me\\a\\b\\daily\\file.txt',s=>s.length<='C:\\Users\\me\\…\\daily\\file.txt'.length),'C:\\Users\\me\\…\\daily\\file.txt');
 assert.equal(compactPath('projects/xbot/a/b/daily/file.txt',s=>s.length<=34),'projects/xbot/a/b/daily/file.txt');
 const file='x'.repeat(130)+'.txt';assert.equal(compactPath('~/.config/xbot/middle/'+file,()=>false),file);
 assert.equal(compactPath('/a/b/file.txt',()=>false),'file.txt');
 `
	if out, err := exec.Command("node", "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestCardAndBubblePathsShareFittingAcrossWidths(t *testing.T) {
	chrome := "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
	if _, err := os.Stat(chrome); err != nil {
		t.Skip("Chrome unavailable")
	}
	page := `<!doctype html><meta charset="utf-8"><style>.chat{width:520px}.msg{width:100%}.body{max-width:86%}.bot-bubble{padding:9px 15px}body{font:13px monospace}.notable-text{display:flex;width:320px}.notable-prefix{flex:none}.compact-path{min-width:0;display:inline-block;max-width:100%;overflow-wrap:anywhere;white-space:normal}.notable-text .compact-path{flex:1}</style><div class="chat"><div class="msg"><div class="body"><div class="bot-bubble"><p></p></div></div></div></div><div class="notable-text has-path"><span class="notable-prefix">写入了 </span></div><script>
 window.requestAnimationFrame=fn=>setTimeout(()=>fn(performance.now()),1);
 const el=(tag,cls,text)=>{const n=document.createElement(tag);n.className=cls||'';n.textContent=text||'';return n},taskSelected=()=>false;
 ` + pathFunctions(t, true) + `
 const path='~/.config/xbot/这个目录名字很长用来核对尾部保留/再加一段/日常工作/.zshrc';
 const card=pathLabel(path);document.querySelector('.notable-text').append(card);
 const p=document.querySelector('p');appendPathText(p,'值得留意：写入了 '+path+'。');
 const bubble=p.querySelector('.compact-path');
 setTimeout(()=>{try{
  const want='~/.config/xbot/…/日常工作/.zshrc';
  if(card.textContent!==want||bubble.textContent!==want)throw Error('wide: '+card.textContent+' / '+bubble.textContent);
  if(card.title!==path||bubble.title!==path)throw Error('full path title missing');
  const canvas=document.createElement('canvas'),ctx=canvas.getContext('2d');ctx.font=getComputedStyle(card).font;const narrow='~/.config/xbot/…/.zshrc';const available=(ctx.measureText(want).width+ctx.measureText(narrow).width)/2;document.querySelector('.notable-text').style.width=(available+document.querySelector('.notable-prefix').getBoundingClientRect().width)+'px';queuePathLayout();
  setTimeout(()=>{try{if(card.textContent!=='~/.config/xbot/…/.zshrc')throw Error('narrow: '+card.textContent);document.body.dataset.result='paths-pass'}catch(e){document.body.dataset.result=e.message}},100);
 }catch(e){document.body.dataset.result=e.message}},100);
 </script>`
	dir := t.TempDir()
	p := filepath.Join(dir, "paths.html")
	if err := os.WriteFile(p, []byte(page), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(chrome, "--headless=new", "--disable-gpu", "--no-sandbox", "--no-first-run", "--user-data-dir="+filepath.Join(dir, "chrome"), "--virtual-time-budget=1000", "--dump-dom", (&url.URL{Scheme: "file", Path: p}).String()).CombinedOutput()
	if err != nil || !strings.Contains(string(out), `data-result="paths-pass"`) {
		t.Fatalf("path rendering: %v\n%s", err, out)
	}
}
