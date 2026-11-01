package web

import "testing"

// Routine runs are records: the chat shows only what the bot says to the
// user, and the task tab lists no runs while keeping routine settings.
func TestRoutineRunsStayOutOfTheUIBrowser(t *testing.T) {
	runTodoBrowser(t, `
localStorage.removeItem('xbot-panel-open');localStorage.removeItem('xbot.side');
bots.forEach(b=>b.pinned=false);
const wake=body=>message('user','<untrusted_event source="timer" type="timer">\n'+body+'\n</untrusted_event>');
const rows=fixture.messages[bots[0].name];
rows.push(wake('<routine id="r1" name="早报">\n看看新闻\n</routine>'),...sayRows('今天有三条要紧新闻。'));
rows.push(wake('<routine id="r2" name="巡检">\n检查服务\n</routine>'),message('assistant','SILENT'));
rows.push(wake('Missed routine times from 2026-10-28:\n- "r3" "周报": 2 missed (2026-10-28T08:00:00Z, 2026-10-28T09:00:00Z)'));
rows.push(wake('<routine id="r4" name="提醒">\n叫我出发\n</routine>'));
const projectionBaseFetch=window.fetch;
window.fetch=async(path,opts={})=>{
 if(/^\/api\/bots\/[^/]+$/.test(String(path)))return json({name:bots[0].name,links:[],routines:[{id:'r1',name:'早报',enabled:true,cron:'0 8 * * *'}],agents:[{id:'sa_running0000000001',type:'dev',task:'仍在运行的任务',status:'running',current_step:'检查状态'},{id:'sa_queued0000000002',type:'research',task:'排队中的任务',status:'queued'},{id:'sa_0123456789abcdef',type:'dev',task:'检查任务',status:'done',summary:'任务结果',finished_at:new Date().toISOString()}]});
 return projectionBaseFetch(path,opts);
};
`, `
const $=s=>document.querySelector(s),sleep=ms=>new Promise(r=>setTimeout(r,ms));
const waitFor=async(fn,label)=>{for(let i=0;i<1000;i++){if(fn())return;await sleep(10)}throw Error('timed out: '+label)};
(async()=>{
 await waitFor(()=>!$('#shell').hidden&&$('#thread').textContent.includes('今天有三条要紧新闻。'),'spoken reply missing');
 const chat=$('#thread').textContent;
 for(const record of ['早报','看看新闻','巡检','检查服务','周报','错过','叫我出发','定时','timer','SILENT','没什么要说的'])
  if(chat.includes(record))throw Error('chat shows routine record: '+record);

 const tab=name=>[...document.querySelectorAll('#workspace-tabs [role=tab]')].find(n=>n.textContent===name);
 const view=name=>document.getElementById(tab(name).getAttribute('aria-controls'));
 await waitFor(()=>tab('任务')&&tab('子代理'),'peer task and child tabs');
 tab('任务').click();
 await waitFor(()=>$('.task-routine')?.checkVisibility(),'Routine tab');
 const routine=$('.task-routine');
 if(!view('任务').querySelector('[data-section="webhook"]'))throw Error('Webhook section disappeared');
 if(!routine.querySelector('[role=switch]'))throw Error('routine settings lost');
 if(view('任务').querySelector('[data-agent^="sa_"]'))throw Error('child records still mixed into Routine page');
 tab('子代理').click();
 await waitFor(()=>view('子代理').querySelector('[data-agent="sa_0123456789abcdef"]')?.checkVisibility(),'child records');
 const root=view('子代理'),finished=root.querySelector('[data-agent="sa_0123456789abcdef"]');
 if(!finished.innerText.includes('检查任务')||!finished.innerText.includes('已完成')||finished.closest('section').querySelectorAll('[data-agent]').length!==1)throw Error('finished child group lost its sole recorded result');
 const active=root.querySelector('[data-agent="sa_running0000000001"]'),queued=root.querySelector('[data-agent="sa_queued0000000002"]');
 if(!active?.checkVisibility()||!/进行中|在跑/.test(active.innerText))throw Error('in-progress subagent disappeared');
 if(!queued?.checkVisibility()||!queued.innerText.includes('已排队'))throw Error('queued subagent disappeared');
 if(active.closest('section')===queued.closest('section')||queued.closest('section')===finished.closest('section')||active.closest('section')===finished.closest('section'))throw Error('child states no longer grouped separately');
 tab('任务').click();
 if($('.task-routine')!==routine||!routine.checkVisibility()||!routine.querySelector('[role=switch]'))throw Error('child navigation replaced Routine settings');
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`)
}
