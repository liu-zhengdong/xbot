package web

import "testing"

func TestFormulaBlocksBrowser(t *testing.T) {
	runPageInChrome(t, `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const waitFor=async(fn,msg)=>{for(let i=0;i<600;i++){if(fn())return;await new Promise(r=>setTimeout(r,10))}throw Error(msg)};
(async()=>{
 await waitFor(()=>!$('#shell').hidden&&$('#title').textContent.includes('2号小伙子'),'boot');
 let copied='';Object.defineProperty(navigator,'clipboard',{configurable:true,value:{writeText:async text=>{copied=text}}});
 $('#input').textContent='公式';$('#input').dispatchEvent(new Event('input'));$('#composer').requestSubmit();
 await waitFor(()=>fixture.streams.length&&$('#thread .live'),'send');
 const s=fixture.streams.at(-1),fence=String.fromCharCode(96).repeat(3);
 const src=fence+'\nℕ⁺ = { n ∈ ℕ | n ≥ 1 }\n\nf(x) = ax² + bx + c    (a ≠ 0)\nlimₙ→∞ (1 + 1/n)ⁿ = e\n'+fence+'\n'+fence+'python\ndef square(x):\n    return x**2\n'+fence+'\n'+fence+'tex\n\\begin{aligned}x&=1\\\\y&=2\\end{aligned}\n'+fence+'\n'+fence+'latex\n\\badcommand\nx² = 1\n'+fence+'\n正文 $E=mc^2$\n\n$$x^2$$';
 say(s,src);
 await waitFor(()=>document.querySelectorAll('#thread .formula-block').length===6,'formula count / split');
 const blocks=[...document.querySelectorAll('#thread .formula-block')],first=blocks[0];
 assert(blocks.slice(0,3).every(n=>n.querySelector('.formula-body')&&!n.querySelector('.code-head')),'Unicode must be plain math font, no code head');
 assert(blocks[3].querySelector('.katex'),'valid multiline TeX must stay rendered in one block');
 assert(blocks[4].dataset.copy==='\\badcommand'&&blocks[5].dataset.copy==='x² = 1','bad TeX split');
 assert($('#thread .code-block .kw')&&$('#thread .code-head').textContent.includes('python'),'real python code lost');
 assert($('#thread .math-inline .katex'),'dollar math broke');
 for(const theme of ['light','dark']){
  document.documentElement.dataset.theme=theme;
  assert(getComputedStyle(first).backgroundColor==='rgba(0, 0, 0, 0)','default formula background must be transparent');
  const probe=document.createElement('span');probe.style.color='var(--x-text)';document.body.append(probe);
  assert(getComputedStyle(first).color===getComputedStyle(probe).color,'theme text mismatch');probe.remove();
  first.focus();assert(first===document.activeElement,'formula not keyboard focusable');
  first.blur();
 }
 first.click();await waitFor(()=>first.classList.contains('is-copied'),'click feedback');
 assert(copied===first.dataset.copy&&!copied.includes('f(x)'),'click must copy only this line');
 assert(getComputedStyle(first.querySelector('.copied-tip')).visibility==='visible','copied feedback hidden');
 await waitFor(()=>!first.classList.contains('is-copied'),'feedback not cleared');
 blocks[1].dispatchEvent(new KeyboardEvent('keydown',{key:'Enter',bubbles:true,cancelable:true}));
 await waitFor(()=>copied===blocks[1].dataset.copy,'Enter copy');
 blocks[2].dispatchEvent(new KeyboardEvent('keydown',{key:' ',bubbles:true,cancelable:true}));
 await waitFor(()=>copied===blocks[2].dataset.copy,'Space copy');
 assert(getComputedStyle(first.querySelector('.copy')).pointerEvents==='none','hint must not intercept clicks');
 finish(s,src);await waitFor(()=>!$('#thread .live'),'settle');
 assert(document.querySelectorAll('#thread .formula-block').length===6,'persisted formulas changed');
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`)
}
