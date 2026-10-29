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
 $('#head-chip').click();
 await waitFor(()=>[...document.querySelectorAll('#panel [role=tab]')].some(n=>n.textContent==='任务'),'tabs');
 [...document.querySelectorAll('#panel [role=tab]')].find(n=>n.textContent==='任务').click();
 await waitFor(()=>$('.task-record.finished')&&$('.task-routine'),'task tab');
 const finished=[...document.querySelectorAll('.task-record.finished')];
 if(finished.length!==1||!finished[0].textContent.includes('检查任务'))throw Error('finished list should hold only the subagent: '+finished.map(n=>n.textContent).join(' | '));
 if(!document.querySelector('#panel [data-section="进行中"] [data-agent="sa_running0000000001"]'))throw Error('in-progress subagent disappeared');
 if(!document.querySelector('#panel [data-section="排队中"] [data-agent="sa_queued0000000002"]'))throw Error('queued subagent disappeared');
 if(!document.querySelector('#panel [data-section="webhook"]'))throw Error('Webhook section disappeared');
 if(!$('.task-routine [role=switch]'))throw Error('routine settings lost');
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`)
}
