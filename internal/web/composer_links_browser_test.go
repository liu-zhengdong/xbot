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

// The composer journey end to end: draft URLs highlight, a click opens the
// system browser through the bubble links' _blank + noopener path without
// submitting, non-http(s) never highlights, Enter still sends the plain text
// and clears the draft, Shift+Enter and IME Enter do not, and the 16000 cap
// survives the contenteditable switch. Runs the shipped page in headless
// Chrome with the same scripted HTTP as the chat stream browser test.
func TestComposerLinksBrowser(t *testing.T) {
	chrome, err := exec.LookPath("/Applications/Google Chrome.app/Contents/MacOS/Google Chrome")
	if err != nil {
		t.Skip("Chrome unavailable")
	}
	dir := t.TempDir()
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
			page = strings.Replace(page, "<head>", "<head><script>"+chatStreamFixture+"</script>", 1)
			page = strings.Replace(page, "</body>", "<script>"+composerLinksDriver+"</script></body>", 1)
			raw = []byte(page)
		}
		return os.WriteFile(dst, raw, 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(chrome, "--headless=new", "--disable-gpu", "--no-sandbox", "--no-first-run", "--no-default-browser-check", "--allow-file-access-from-files", "--user-data-dir="+filepath.Join(t.TempDir(), "chrome"), "--virtual-time-budget=15000", "--dump-dom", (&url.URL{Scheme: "file", Path: filepath.Join(dir, "index.html")}).String()).CombinedOutput()
	if err != nil {
		t.Fatalf("Chrome: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), `data-test-result="passed"`) {
		result := string(out)
		if i := strings.Index(result, "data-test-result="); i >= 0 {
			result = result[i:min(i+1200, len(result))]
		} else if len(result) > 1500 {
			result = result[:1500]
		}
		t.Fatalf("composer links browser: %s", result)
	}
}

const composerLinksDriver = `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const waitFor=async(fn,msg)=>{for(let i=0;i<600;i++){if(fn())return;await new Promise(r=>setTimeout(r,10))}throw Error(msg)};
let opened=null;window.open=(u,t,r)=>{opened=[String(u),String(t),String(r)]};
const streams=()=>fixture.streams.length;
const type=s=>{const b=$('#input');b.textContent=s;b.dispatchEvent(new Event('input'));return b};
(async()=>{
 await waitFor(()=>!$('#shell').hidden&&$('#title').textContent.includes('2号小伙子'),'app did not boot');
 const box=$('#input');
 assert(box.getAttribute('contenteditable')==='plaintext-only','composer is not plain-text editable');
 // Highlight: only the address is a link; trailing punctuation stays prose.
 type('看 https://example.com/a(b)，再看 http://x.y/z。');
 const links=[...box.querySelectorAll('a')];
 assert(links.length===2,'expected two links, got '+links.length);
 assert(links[0].getAttribute('href')==='https://example.com/a(b','href wrong: '+links[0].getAttribute('href'));
 assert(links[1].getAttribute('href')==='http://x.y/z','href wrong: '+links[1].getAttribute('href'));
 assert(links[0].rel==='noopener noreferrer'&&links[0].target==='_blank','link safety missing');
 assert(links[0].textContent==='https://example.com/a(b','link text changed');
 assert(box.textContent.startsWith('看 https://example.com/a(b)'),'visible text changed');
 // Click opens the browser path and never submits.
 const before=streams();
 links[0].click();
 await waitFor(()=>!!opened,'click did not open');
 assert(opened[0]==='https://example.com/a(b'&&opened[1]==='_blank'&&opened[2].includes('noopener'),'open args wrong: '+JSON.stringify(opened));
 assert(streams()===before,'click submitted the form');
 // Non-http(s) never highlights.
 type('ftp://a/b 与 javascript:alert(1) 与 file:///etc/hosts');
 assert(box.querySelectorAll('a').length===0,'non-http(s) got a link');
 // Enter sends the plain text once and clears the draft.
 type('https://example.com/go');
 box.dispatchEvent(new KeyboardEvent('keydown',{key:'Enter',keyCode:13,bubbles:true}));
 await waitFor(()=>streams()>before,'Enter did not send');
 assert(box.textContent==='','Enter left the draft');
 assert($('#thread').textContent.includes('https://example.com/go'),'sent text lost the address');
 const settled=streams();
 // Shift+Enter keeps the draft and sends nothing.
 type('第一行');
 box.dispatchEvent(new KeyboardEvent('keydown',{key:'Enter',keyCode:13,shiftKey:true,bubbles:true}));
 await new Promise(r=>setTimeout(r,50));
 assert(streams()===settled,'Shift+Enter sent');
 // IME composing Enter must not send either.
 type('中文');
 box.dispatchEvent(new KeyboardEvent('keydown',{key:'Enter',keyCode:229,bubbles:true}));
 await new Promise(r=>setTimeout(r,50));
 assert(streams()===settled,'IME Enter sent');
 // The cap keeps the old maxlength semantics: an edit that would push the
 // draft past 16000 is refused whole instead of being sliced afterwards.
 type('x'.repeat(16100));
 assert(box.textContent==='中文','over-cap replacement must be refused whole: '+JSON.stringify(box.textContent));
 type('a'.repeat(15999)+'Z');
 box.focus();
 const r=document.createRange();r.selectNodeContents(box);r.collapse(true);getSelection().removeAllRanges();getSelection().addRange(r);
 document.execCommand('insertText',false,'X');
 assert(box.textContent.length===16000,'cap length changed: '+box.textContent.length);
 assert(box.textContent.startsWith('aaa'),'over-cap insertion was accepted: '+box.textContent.slice(0,3));
 assert(box.textContent.endsWith('aaZ'),'existing last character was lost: '+box.textContent.slice(-3));
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`
