package web

import "testing"

func TestStepsScrollBrowser(t *testing.T) {
	runTodoBrowser(t, todoBrowserFixture+`
let stepReads=0;
let stepCount=20;
const stepsFetch=window.fetch;
window.fetch=async(path,opts={})=>{
 if(String(path).endsWith('/steps?n=20')){
  stepReads++;
  return json({steps:Array.from({length:stepCount},(_,i)=>({tool:'shell',summary:'检查运行记录 '+(i+1),state:i===stepCount-1?'running':'done',time:new Date(Date.now()-(stepCount-i)*1000).toISOString()}))});
 }
 return stepsFetch(path,opts);
};
`, traceExplorerSetup+`
(async()=>{
 const preview=()=>$('#roster .item[data-bot="'+bots[0].name+'"] .preview');
 await waitFor(()=>!$('#shell').hidden&&preview()?.matches('.st.run')&&!$('.thread-loading-cover'),'app did not boot');
 preview().click();
 await waitFor(()=>$('#stp li')&&$('#stp li').textContent.includes('检查运行记录'),'steps did not load');
 const pop=$('#stp'),list=pop.querySelector('ol');
 assert(pop.getBoundingClientRect().height<=421,'steps exceed 420px height cap');
 assert(list.scrollHeight>list.clientHeight,'long steps have no scroll range');
 const top=pop.querySelector('.ph').getBoundingClientRect().top;
 list.scrollTop=list.scrollHeight;
 const position=list.scrollTop;
 assert(position>0,'steps cannot scroll');
 assert(Math.abs(pop.querySelector('.ph').getBoundingClientRect().top-top)<1,'header moved with steps');
 const reads=stepReads;
 await waitFor(()=>stepReads>=reads+2,'steps polls missing');
 assert(Math.abs(pop.querySelector('ol').scrollTop-position)<1,'poll reset steps scroll');
 assert(pop.getBoundingClientRect().top>=7&&pop.getBoundingClientRect().bottom<=innerHeight-7,'steps overflow viewport after loading');
 pop.querySelector('.x').click();
 stepCount=2;preview().click();
 await waitFor(()=>$('#stp li')&&$('#stp').querySelectorAll('li').length===2,'short steps did not load');
 assert($('#stp').getBoundingClientRect().height<200,'short list padded to maximum height');
 assert($('#stp ol').scrollTop===0,'reopened steps kept old scroll');
 if(testErrors.length)throw Error(testErrors.join('; '));
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`)
}
