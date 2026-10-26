package web

import (
	"os/exec"
	"strings"
	"testing"
)

// Use the real streaming and history renderers. Untrusted Markdown image
// URLs stay filtered; validated Entry.Images are tested separately.
func TestImageGenBubbleBrowser(t *testing.T) {
	runPageInChrome(t, `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const waitFor=async(fn,msg)=>{for(let i=0;i<600;i++){if(fn())return;await new Promise(r=>setTimeout(r,10))}throw Error(msg)};
(async()=>{
 await waitFor(()=>!$('#shell').hidden&&$('#title').textContent.includes('2号小伙子'),'boot');
 const box=$('#input');box.textContent='画图';box.dispatchEvent(new Event('input'));$('#composer').requestSubmit();
 await waitFor(()=>fixture.streams.length&&$('#thread .live'),'stream');
 const s=fixture.streams.at(-1),B='\x60\x60\x60';
 let md='画好了。\n\n';emit(s,'delta',{text:md});
 // Streaming prefixes, then the complete image; alt must never leak as !alt.
 for(const delta of ['![简笔画小猫坐在窗台上','](https://example.com/cat', '.png)']){
  md+=delta;emit(s,'delta',{text:delta});
  await new Promise(r=>setTimeout(r,20));
  assert(!$('#thread .live').textContent.includes('简笔画小猫'),'image fragment in stream');
  assert(!$('#thread .live img'),'image attached to live bubble');
 }
 const tail='\n\n保留！ !important 和 !flag。 [普通链接](https://example.com/example.png)\n\n'+B+'md\n![代码示例](generated/example.png)\n'+B+'\n\n\x60![行内示例](https://example.com/example.png)\x60\n\n![本地图](generated/cat.png)\n\n![残缺图](generated/incomplete';
 md+=tail;emit(s,'delta',{text:tail});
 await waitFor(()=>$('#thread .live a'),'normal link');
 finish(s,md);await waitFor(()=>!$('#thread .live')&&$('#thread .msg.bot .bot-bubble'),'history');
 const bubble=$('#thread .msg.bot .bot-bubble'),text=bubble.textContent;
 assert(!/简笔画小猫|本地图|残缺图/.test(text),'image fragment in history: '+text);
 assert(!bubble.querySelector('img,.gallery'),'image attached to history');
 assert(text.includes('保留！ !important 和 !flag。'),'ordinary punctuation stripped');
 assert(text.includes('![代码示例](generated/example.png)')&&text.includes('![行内示例](https://example.com/example.png)'),'code examples stripped');
 assert(bubble.querySelector('a')?.href==='https://example.com/example.png','ordinary link changed');
 assert(!bubble.querySelector('script'),'markup injection');
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`)
}

func TestBotImageMarkupFiltering(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	raw, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	start, end := strings.Index(src, "  function stripBotImages("), strings.Index(src, "  function appendBotContent(")
	if start < 0 || end <= start {
		t.Fatal("assistant image filter missing")
	}
	script := src[start:end] + `
const assert=require('node:assert/strict'), B=String.fromCharCode(96);
const cases=[
 ['[doc](https://example.com/a![v1].html)','[doc](https://example.com/a![v1].html)'],
 ['![cat](generated/cat.png "(title") tail',' tail'],
 ['![cat\nwindow](generated/cat.png) tail',' tail'],
 ['before ![cat](generated/cat.png) after','before  after'],
 ['![nested [alt]](generated/cat(1).png) tail',' tail'],
 ['![alt][ref] tail',' tail'],
 ['![alt',''], ['![alt](', ''], ['![alt](https://example.com/a', ''],
 ['![broken\nKeep next line','\nKeep next line'],
 ['![a](javascript:alert(1)) safe',' safe'],
 ['![<img src=x onerror=alert(1)>](x) safe',' safe'],
 ['Keep !important and !flag; nice!','Keep !important and !flag; nice!'],
 ['[normal](https://example.com/a.png)','[normal](https://example.com/a.png)'],
 [B+'![code](https://example.com/a.png)'+B,B+'![code](https://example.com/a.png)'+B],
 [B.repeat(3)+'md\n![code](x)\n'+B.repeat(3),B.repeat(3)+'md\n![code](x)\n'+B.repeat(3)],
 [B.repeat(3)+'md\n![unfinished code',B.repeat(3)+'md\n![unfinished code'],
 ['$x![a](x)$','$x![a](x)$'], ['$$x![a](x)$$','$$x![a](x)$$'],
 ['\\![escaped](x)','\\![escaped](x)']
];
for(const [input,want] of cases) assert.equal(stripBotImages(input),want,input);
`
	out, err := exec.Command(node, "-e", script).CombinedOutput()
	if err != nil {
		t.Fatalf("assistant image filter: %v\n%s", err, out)
	}
}
