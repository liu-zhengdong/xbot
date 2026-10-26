package web

import (
	"strings"
	"testing"
)

// The draft journey with Markdown: heading/list/code/bold/italic tokens are
// colored but keep their source characters, [text](url) opens externally
// through the same _blank + noopener path as bare URLs without submitting,
// javascript: never links, and Enter still sends the plain source and clears
// the draft. Runs the shipped page in headless Chrome.
func TestComposerMarkdownBrowser(t *testing.T) {
	runPageInChrome(t, strings.ReplaceAll(`
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const waitFor=async(fn,msg)=>{for(let i=0;i<600;i++){if(fn())return;await new Promise(r=>setTimeout(r,10))}throw Error(msg)};
const streams=()=>fixture.streams.length;
const type=s=>{const b=$('#input');b.textContent=s;b.dispatchEvent(new Event('input'));return b};
(async()=>{
 await waitFor(()=>!$('#shell').hidden&&$('#title').textContent.includes('2号小伙子'),'app did not boot');
 const box=$('#input');
 // Colored, but every source character stays visible.
 type('# 标题\n- 列表 @BQ@code@BQ@ **粗** *斜*');
 const q=c=>box.querySelectorAll('.'+c).length;
 assert(q('md-heading')===1,'heading not colored');
 assert(q('md-list')===1,'list not colored');
 assert(q('md-code')===1,'code not colored');
 assert(q('md-bold')===1,'bold not colored');
 assert(q('md-italic')===1,'italic not colored');
 const source='# 标题\n- 列表 @BQ@code@BQ@ **粗** *斜*';
 assert(box.textContent===source,'source characters changed: '+JSON.stringify(box.textContent));
 // [text](url) is clickable and opens externally without submitting.
 let opened=null;window.open=(u,t,r)=>{opened=[String(u),String(t),String(r)]};
 type('看 [文档](https://example.com/x) 就点');
 const md=[...box.querySelectorAll('a')];
 assert(md.length===1,'md link missing, got '+md.length);
 assert(md[0].getAttribute('href')==='https://example.com/x','md href wrong');
 assert(md[0].textContent==='[文档](https://example.com/x)','md link lost its source text');
 const before=streams();
 md[0].click();
 await waitFor(()=>!!opened,'md click did not open');
 assert(opened[0]==='https://example.com/x'&&opened[1]==='_blank'&&opened[2].includes('noopener'),'open args wrong: '+JSON.stringify(opened));
 assert(streams()===before,'md click submitted the form');
 // javascript: never links, not even in [..](..).
 type('[坏](javascript:alert(1))');
 assert(box.querySelectorAll('a').length===0,'javascript: got a link');
 // Enter still sends the plain source and clears the draft.
 type('# 传\n- 带 @BQ@x@BQ@');
 box.dispatchEvent(new KeyboardEvent('keydown',{key:'Enter',keyCode:13,bubbles:true}));
 await waitFor(()=>streams()>before,'Enter did not send');
 assert(box.textContent==='','draft not cleared');
 // The wire carries the plain source; the bubble may render it.
 await waitFor(()=>{const sent=fixture.messages['2号小伙子'].at(-1);return sent&&sent.content.includes('带 @BQ@x@BQ@')},'sent source lost');
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`, "@BQ@", "`"))
}
