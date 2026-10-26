package web

import (
	"testing"
)

// The bubble math journey end to end: $…$ renders inline through the vendored
// KaTeX, $$…$$ renders as a display block, a partial stream (no closing $ yet)
// keeps the source visible, a missing KaTeX falls back to the raw source
// without breaking the bubble, and the persisted transcript renders math again
// after its whole-segment re-render. Runs the shipped page in headless Chrome.
func TestBubbleMathBrowser(t *testing.T) {
	runPageInChrome(t, `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const waitFor=async(fn,msg)=>{for(let i=0;i<600;i++){if(fn())return;await new Promise(r=>setTimeout(r,10))}throw Error(msg)};
const text=()=>$('#thread').textContent;
const send=async value=>{
 const n=fixture.streams.length;const box=$('#input');box.textContent=value;box.dispatchEvent(new Event('input'));$('#composer').requestSubmit();
 await waitFor(()=>fixture.streams.length>n&&$('#thread .live'),'send did not start');
 return fixture.streams.at(-1);
};
(async()=>{
 await waitFor(()=>!$('#shell').hidden&&$('#title').textContent.includes('2号小伙子'),'app did not boot');
 assert(!!window.katex&&typeof window.katex.renderToString==='function','katex missing from the page');
 // Inline math in the live stream, wired to the vendored CSS.
 const s=await send('算一下');
 emit(s,'delta',{text:'质能式 $E=mc^2$ 很有名。'});
 await waitFor(()=>document.querySelector('#thread .math-inline .katex'),'inline math missing');
 const inline=document.querySelector('#thread .math-inline .katex');
 assert(getComputedStyle(inline).fontFamily.includes('KaTeX_Main'),'katex css not wired: '+getComputedStyle(inline).fontFamily);
 assert(text().includes('很有名'),'prose around math lost');
 // Block math in the same stream.
 emit(s,'delta',{text:'质能式 $E=mc^2$ 很有名。\n\n$$\\frac{a}{b}$$\n完'});
 await waitFor(()=>document.querySelector('#thread .math-block .katex'),'block math missing');
 assert(text().includes('完'),'prose after block lost');
 // Fallback: katex gone -> raw source, bubble intact, no stale markup.
 const realKatex=window.katex;delete window.katex;
 emit(s,'delta',{text:'质能式 $E=mc^2$ 很有名。\n\n$$\\frac{a}{b}$$\n完\n坏 $x$ 坏'});
 await waitFor(()=>document.querySelector('#thread .math-raw'),'fallback missing');
 assert([...document.querySelectorAll('#thread .math-raw')].some(n=>n.textContent==='$x$'),'fallback text wrong: '+[...document.querySelectorAll('#thread .math-raw')].map(n=>n.textContent).join('|'));
 assert(!document.querySelector('#thread .math-inline .katex'),'stale katex after fallback');
 assert(text().includes('很有名'),'bubble broke in fallback');
 // Persisted transcript renders math again after the whole-segment re-render.
 window.katex=realKatex;
 finish(s,'质能式 $E=mc^2$ 很有名。\n\n$$\\frac{a}{b}$$');
 await waitFor(()=>!$('#thread .live'),'stream did not settle');
 const reads=fixture.reads;await waitFor(()=>fixture.reads>reads,'poll did not run');
 await waitFor(()=>document.querySelector('#thread .math-block .katex'),'persisted math missing');
 assert(text().includes('很有名'),'persisted prose lost');
 // An unclosed $ stays source while streaming and completes once closed.
 const s2=await send('流式');
 emit(s2,'delta',{text:'未闭合 $E=mc'});
 await waitFor(()=>text().includes('未闭合 $E=mc'),'partial source lost');
 assert(!document.querySelector('#thread .live .math-inline .katex'),'unclosed $ became math');
 emit(s2,'delta',{text:'未闭合 $E=mc^2$ 闭合'});
 await waitFor(()=>document.querySelector('#thread .live .math-inline .katex'),'completed math missing');
 finish(s2,'未闭合 $E=mc^2$ 闭合');
 await waitFor(()=>!$('#thread .live'),'second stream did not settle');
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`)
}
