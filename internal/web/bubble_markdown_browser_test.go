package web

import "testing"

// The bubble markdown journey in the shipped page: a python fence shows
// keyword, string and comment colors that differ from the code text (in the
// bot bubble and in the owner's bubble), > / >> quotes render as ruled blocks
// with no > in the visible text and math still rendering inside, and a pipe
// table renders with header and cell borders and a clickable cell link.
func TestBubbleMarkdownBrowser(t *testing.T) {
	runPageInChrome(t, `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const waitFor=async(fn,msg)=>{for(let i=0;i<600;i++){if(fn())return;await new Promise(r=>setTimeout(r,10))}throw Error(msg)};
const send=async value=>{
 const n=fixture.streams.length;const box=$('#input');box.textContent=value;box.dispatchEvent(new Event('input'));$('#composer').requestSubmit();
 await waitFor(()=>fixture.streams.length>n&&$('#thread .live'),'send did not start');
 return fixture.streams.at(-1);
};
const color=n=>getComputedStyle(n).color;
const colored=(root,label)=>{
 const code=root.querySelector('.code-block code');assert(code,label+': no code');
 for(const c of ['kw','str','cm']){const t=code.querySelector('.'+c);assert(t,label+': no .'+c);assert(color(t)!==color(code),label+': .'+c+' not colored ('+color(t)+')')}
 assert(color(code.querySelector('.kw'))!==color(code.querySelector('.str')),label+': kw and str share a color');
};
const B='\x60\x60\x60';
const md=B+'python\ndef f(x):\n    # note\n    return "s"\n'+B+'\n\n> 引用 $x^2$\n>> 嵌套 **粗**\n\n| 名 | 链接 | 值 |\n|---|---|---:|\n| a | [站](https://example.com) | 1 |\n\n完';
(async()=>{
 await waitFor(()=>!$('#shell').hidden&&$('#title').textContent.includes('2号小伙子'),'app did not boot');
 const s=await send(B+'python\ndef g():\n    return "u"  # c\n'+B);
 await waitFor(()=>document.querySelector('#thread .msg.user .code-block'),'owner code block missing');
 colored(document.querySelector('#thread .msg.user .bubble'),'owner bubble');
 say(s,md);
 await waitFor(()=>document.querySelector('#thread .msg.bot:not(.live) table'),'table missing');
 const live=document.querySelector('#thread .msg.bot:not(.live)');
 colored(live,'bot bubble');
 const q=live.querySelectorAll('blockquote');
 assert(q.length===2&&q[0].contains(q[1]),'quotes not nested: '+q.length);
 assert(parseFloat(getComputedStyle(q[0]).borderLeftWidth)>0,'quote has no rule');
 assert(!live.textContent.includes('>'),'> leaked: '+live.textContent);
 assert(q[0].querySelector('.math-inline .katex'),'math in quote did not render');
 assert(q[1].querySelector('strong'),'bold in nested quote lost');
 const table=live.querySelector('table');
 assert(table.querySelectorAll('thead th').length===3&&table.querySelectorAll('tbody td').length===3,'table shape wrong');
 const td=table.querySelector('td');
 assert(parseFloat(getComputedStyle(td).borderTopWidth)>0,'cells have no borders');
 assert(getComputedStyle(table.querySelectorAll('th')[2]).textAlign==='right','alignment lost');
 const a=table.querySelector('a');assert(a&&a.href==='https://example.com/'&&a.target==='_blank','cell link wrong');
 assert(!live.textContent.includes('|'),'raw pipes leaked: '+live.textContent);
 // Persisted transcript after the whole-segment re-render.
 finish(s,md);
 await waitFor(()=>!$('#thread .live'),'stream did not settle');
 await waitFor(()=>document.querySelector('#thread .msg.bot table')&&document.querySelectorAll('#thread .msg.bot blockquote').length===2,'persisted markdown missing');
 colored(document.querySelector('#thread .msg.bot .bot-bubble'),'persisted bot bubble');
 // Dark theme swaps both palettes; tokens stay colored on both fills.
 document.documentElement.dataset.theme='dark';
 colored(document.querySelector('#thread .msg.bot .bot-bubble'),'dark bot bubble');
 colored(document.querySelector('#thread .msg.user .bubble'),'dark owner bubble');
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`)
}
