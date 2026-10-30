package web

import "testing"

func TestRoutineSkillsBrowser(t *testing.T) {
	runTodoBrowser(t, routineSkillsFixture, routineSkillsDriver)
}

const routineSkillsFixture = `
localStorage.removeItem('xbot-panel-open');localStorage.removeItem('xbot.side');
bots.forEach(b=>b.pinned=false);
const routineRows=[
 {id:'r1',name:'每天整理文档',enabled:true,cron:'0 8 * * *',skills:['doc-atlas','release-check']},
 {id:'r2',name:'提醒喝水',enabled:false,cron:'0 */2 * * *'}
];
let routineReads=0, failSkills=false, holdSkills=false, releaseSkills;
const routineWrites=[];
const routineFetch=window.fetch;
window.fetch=async(path,opts={})=>{
 const url=String(path);
 if(/^\/api\/bots\/[^/]+\/routines\/[^/]+$/.test(url)&&opts.method==='PATCH'){
  const body=JSON.parse(opts.body);routineWrites.push(body);
  if('skills' in body&&holdSkills)await new Promise(resolve=>releaseSkills=resolve);
  if('skills' in body&&failSkills)return new Response(JSON.stringify({error:'本次保存失败'}),{status:400});
  const row=routineRows.find(r=>r.id===decodeURIComponent(url.split('/').pop()));
  Object.assign(row,body);return json({...row});
 }
 if(/^\/api\/bots\/[^/]+$/.test(url)){
  routineReads++;return json({name:bots[0].name,links:[],agents:[],routines:routineRows.map(r=>({...r}))});
 }
 return routineFetch(path,opts);
};
`

const routineSkillsDriver = `
const $=s=>document.querySelector(s), sleep=ms=>new Promise(r=>setTimeout(r,ms));
const assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const waitFor=async(fn,label)=>{for(let i=0;i<1100;i++){if(fn())return;await sleep(10)}throw Error('timed out: '+label)};
(async()=>{
 await waitFor(()=>!$('#shell').hidden&&$('#title').textContent.includes(bots[0].name)&&!$('#thread .thread-skeleton'),'boot');
 $('#head-chip').click();
 await waitFor(()=>[...document.querySelectorAll('#panel [role=tab]')].some(n=>n.textContent==='任务'),'tabs');
 [...document.querySelectorAll('#panel [role=tab]')].find(n=>n.textContent==='任务').click();
 await waitFor(()=>document.querySelectorAll('.task-routine').length===2,'routines');
 if(new URLSearchParams(location.search).has('shot')){document.body.dataset.testResult='passed';return}
 const card=$('[data-routine=r1]'), trigger=card.querySelector('.routine-skills-summary');
 assert(trigger&&trigger.textContent==='技能：doc-atlas → release-check','ordered skill summary absent');
 assert($('[data-routine=r2] .routine-skills-summary').textContent==='添加技能','empty skill entry absent');
 const toggle=card.querySelector('[role=switch]'), name=card.querySelector('.routine-name');
 trigger.click();
 const form=card.querySelector('form'), input=form.querySelector('input');
 assert(!form.hidden&&document.activeElement===input,'editor did not focus');
 assert(input.value==='doc-atlas, release-check','editor did not load skills');
 input.value='unsaved, draft';input.setSelectionRange(3,8);
 const reads=routineReads;
 await waitFor(()=>routineReads>=reads+2,'two profile polls');
 assert(card.querySelector('input')===input&&input.value==='unsaved, draft','poll replaced draft');
 assert(document.activeElement===input&&input.selectionStart===3&&input.selectionEnd===8,'poll stole focus or selection');
 form.querySelector('[type=button]').click();
 assert(form.hidden&&routineWrites.length===0&&document.activeElement===trigger,'cancel wrote or lost focus');
 trigger.click();assert(input.value==='doc-atlas, release-check','cancel did not discard draft');
 input.value=' release-check, doc-atlas , , missing-skill ';
 failSkills=true;form.requestSubmit();
 await waitFor(()=>!form.querySelector('[type=submit]').disabled,'failed save');
 assert(!form.hidden&&input.value.includes('missing-skill')&&form.querySelector('[role=status]').textContent.includes('本次保存失败'),'failure lost draft or error');
 assert(JSON.stringify(routineWrites[0])===JSON.stringify({skills:['release-check','doc-atlas','missing-skill']}),'save changed fields or order');
 failSkills=false;holdSkills=true;form.requestSubmit();
 await waitFor(()=>releaseSkills,'pending save');
 assert(input.disabled&&form.querySelector('[type=button]').disabled,'pending save allows duplicate edits');
 const pendingReads=routineReads;
 await waitFor(()=>routineReads>pendingReads,'poll during save');
 releaseSkills();holdSkills=false;
 await waitFor(()=>form.hidden,'successful save');
 assert(trigger.textContent==='技能：release-check → doc-atlas → missing-skill','saved summary wrong');
 assert(document.activeElement===trigger&&card.querySelector('[role=switch]')===toggle&&card.querySelector('.routine-name')===name,'save replaced header or lost focus');
 toggle.click();await waitFor(()=>!toggle.disabled,'toggle');
 assert(routineWrites.at(-1).enabled===false&&routineRows[0].skills.length===3,'toggle changed skills');
 trigger.click();input.value=' ,  ';form.requestSubmit();
 await waitFor(()=>form.hidden,'clear');
 assert(JSON.stringify(routineWrites.at(-1))==='{"skills":[]}'&&trigger.textContent==='添加技能','clear did not send empty array');
 trigger.click();input.value='discard';input.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true}));
 assert(form.hidden&&!$('#panel').hidden&&document.activeElement===trigger,'Escape did not cancel locally');
 const empty=$('[data-routine=r2]');empty.querySelector('.routine-skills-summary').click();
 const emptyForm=empty.querySelector('form'), emptyInput=emptyForm.querySelector('input');
 assert(emptyInput.value==='', 'empty routine inherited skills');
 emptyInput.value=' doc-atlas，release-check ';emptyForm.requestSubmit();
 await waitFor(()=>emptyForm.hidden,'add to empty');
 assert(JSON.stringify(routineWrites.at(-1))===JSON.stringify({skills:['doc-atlas','release-check']})&&routineRows[1].enabled===false,'add changed order or enabled state');
 assert(testErrors.length===0,testErrors.join('; '));
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`
