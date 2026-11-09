package web

import "testing"

func TestQuoteReturnLayoutBrowser(t *testing.T) {
	for _, viewport := range []string{"1100,720", "390,844"} {
		t.Run(viewport, func(t *testing.T) {
			t.Setenv("XBOT_STICK_VIEWPORT", viewport)
			runChatFramesBrowser(t, transcriptWindowFixture+quoteReturnFixture, quoteReturnDriver)
		})
	}
}

const quoteReturnFixture = `
fixture.messages[bots[0].name].at(-1).reply_to=401;
const quoteReturnBaseFetch=window.fetch;
window.fetch=(path,opts)=>String(path).endsWith('/device-tasks')?Promise.resolve(json({tasks:[]})):quoteReturnBaseFetch(path,opts);
`

const quoteReturnDriver = `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const waitFor=async(f,m)=>{for(let i=0;i<800;i++){if(f())return;await new Promise(r=>setTimeout(r,10))}throw Error(m)};
const frames=async()=>{await new Promise(requestAnimationFrame);await new Promise(requestAnimationFrame)};
(async()=>{
 await waitFor(()=>$('#thread [data-seq="420"] .quote-block')&&!$('.thread-loading-cover'),'chat did not open');
 await document.fonts.ready;await frames();
 const thread=$('#thread'),button=$('#quote-return'),placement=button.parentElement,input=$('#input'),composer=$('#composer');
 assert(button.hidden&&placement.getBoundingClientRect().height===0,'hidden return left an empty row');
 const quote=$('#thread [data-seq="420"] .quote-block'),top=thread.scrollTop;
 quote.click();await waitFor(()=>!button.hidden,'quote return did not appear');await frames();
 const rect=button.getBoundingClientRect(),box=composer.getBoundingClientRect();
 assert(rect.bottom<=box.top-11&&Math.abs(rect.right-box.right)<1,'return overlaps or fails to align with composer');
 assert(document.elementFromPoint(rect.x+rect.width/2,rect.y+rect.height/2).closest('button')===button,'return button cannot be clicked');
 assert(thread.scrollTop<top-100,'quote did not reveal earlier content');
 button.focus();await waitFor(()=>!$('.tip').hidden,'return has no keyboard hint');await frames();
 assert($('.tip').getBoundingClientRect().bottom<=rect.top,'return hint covers the control or composer');
 button.click();await frames();
 assert(button.hidden&&placement.getBoundingClientRect().height===0,'return did not collapse its row');
 assert(Math.abs(thread.scrollTop-top)<2,'return lost the original reading position');
 assert(document.activeElement.matches('[data-seq="420"] .quote-block'),'return did not restore quote focus');
 input.textContent='第一行草稿\n第二行草稿\n第三行草稿';input.dispatchEvent(new Event('input'));await frames();
 const draft=input.textContent,multilineTop=thread.scrollTop;
 $('#thread [data-seq="420"] .quote-block').click();await waitFor(()=>!button.hidden,'multiline return missing');await frames();
 assert(button.getBoundingClientRect().bottom<composer.getBoundingClientRect().top,'multiline composer covers return');
 button.click();await frames();
 assert(input.textContent===draft&&Math.abs(thread.scrollTop-multilineTop)<2,'multiline return lost draft or position');
 assert(testErrors.length===0,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`
