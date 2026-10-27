package web

import "testing"

// The task tab's Webhook section on the real page, with fake HTTP data:
// empty, create (bad name stays in the card), created with the gateway's
// public address, list, detail with reveal, remove back to empty.
func TestWebhookTaskTabBrowser(t *testing.T) {
	runChatStreamBrowser(t, chatStreamFixture+webhookFixture, webhookDriver)
}

const webhookFixture = `
const hookFixture={hooks:[],public:new URLSearchParams(location.search).get('gw')!=='local',calls:[]};
const hookView=h=>{const local='http://127.0.0.1:8787/webhook/2号小伙子/'+h.name,pub='https://pipe.example/pipe/v1/hook/mini-1/webhook/2号小伙子/'+h.name;
 return {name:h.name,auth:h.auth,local_url:local,url:hookFixture.public?pub:local,...(hookFixture.public?{public_url:pub}:{})}};
const hookBase=window.fetch;
window.fetch=async(path,opts={})=>{
 const p=String(path).match(/^\/api\/bots\/([^/]+)$/);
 if(p&&p[1]!=='status'&&(opts.method||'GET')==='GET'){const r=await hookBase(path,opts);if(!r.ok)return r;
  return json({...await r.json(),webhook_public:hookFixture.public?'https://pipe.example/pipe/v1/hook/mini-1':''})}
 const m=String(path).match(/^\/api\/bots\/[^/]+\/webhooks(?:\/([^/]+))?$/);
 if(!m)return hookBase(path,opts);
 const method=opts.method||'GET';hookFixture.calls.push(method+' '+(m[1]||''));
 if(method==='GET'&&!m[1])return json({hooks:hookFixture.hooks.map(hookView),public:hookFixture.public?'https://pipe.example/pipe/v1/hook/mini-1':''});
 if(method==='POST'){const b=JSON.parse(opts.body);
  if(hookFixture.hooks.some(h=>h.name===b.name))return new Response(JSON.stringify({error:'已有同名 Webhook'}),{status:409});
  const h={name:b.name,auth:b.auth||'hmac',secret:'s3cr3t'.padEnd(64,'0')};hookFixture.hooks.push(h);
  return new Response(JSON.stringify({...hookView(h),secret:h.secret}),{status:201,headers:{'Content-Type':'application/json'}})}
 const h=hookFixture.hooks.find(x=>x.name===decodeURIComponent(m[1]));
 if(!h)return new Response(JSON.stringify({error:'not found'}),{status:404});
 if(method==='DELETE'){hookFixture.hooks.splice(hookFixture.hooks.indexOf(h),1);return new Response(null,{status:204})}
 return json({...hookView(h),secret:h.secret});
};
`

const webhookDriver = `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const waitFor=async(fn,msg)=>{for(let i=0;i<600;i++){if(fn())return;await new Promise(r=>setTimeout(r,10))}throw Error(msg)};
const shot=new URLSearchParams(location.search).get('shot');
const stop=name=>{if(shot===name){document.body.dataset.testResult='passed';return true}return false};
(async()=>{
 await waitFor(()=>!$('#head-chip').disabled&&!$('#shell').hidden,'app did not boot');
 $('#head-chip').click();await waitFor(()=>$('#panel [role=tab]'),'panel did not open');
 [...document.querySelectorAll('#panel [role=tab]')].find(b=>b.textContent==='任务').click();
 await waitFor(()=>$('.hook-empty'),'webhook empty state missing');
 assert($('.hook-section h4').textContent.startsWith('Webhook'),'section title');
 assert($('.task-view').lastElementChild===$('.hook-section'),'webhook section is not last in the task tab');
 if(stop('empty'))return;
 $('.hook-empty .primary').click();await waitFor(()=>!$('#modal').hidden,'new card missing');
 assert($('#modal-title').textContent==='新建 Webhook','new title');
 const name=$('#modal-fields input:not([type=radio])');
 name.value='GitHub';$('#modal-form').requestSubmit();
 await waitFor(()=>!$('#modal-error').hidden,'bad name not flagged');
 assert($('#modal-error').textContent.includes('小写字母、数字和短横线')&&!$('#modal').hidden,'bad name error');
 assert(!hookFixture.calls.some(c=>c.startsWith('POST')),'bad name reached the API');
 if(stop('new'))return;
 name.value='github';$('#modal-form').requestSubmit();
 await waitFor(()=>$('#modal-title').textContent==='已创建'&&!$('#modal').hidden,'created card missing');
 const boxes=[...document.querySelectorAll('#modal-fields .hook-copy')].map(b=>b.textContent);
 if(hookFixture.public){
  assert(boxes[0].includes('公网地址')&&boxes[0].includes('https://pipe.example/'),'public address is not the main one: '+boxes);
  assert($('#modal-fields .hook-local summary').textContent==='本机','local address not tucked away');
 }else{
  assert(boxes[0].includes('http://127.0.0.1:8787/'),'local address missing');
  assert($('#modal-fields .hook-gw').textContent.includes('登录网关后可得到公网地址'),'gateway hint missing');
 }
 assert(boxes.at(-1).includes('s3cr3t'),'secret not shown on create');
 assert($('#modal-cancel').hidden,'created card keeps a cancel button');
 if(stop('created'))return;
 $('#modal-form').requestSubmit();await waitFor(()=>$('#modal').hidden&&$('.hook-row'),'list missing after create');
 assert($('.hook-row .hook-auth').textContent==='签名校验','auth label');
 assert(!$('.hook-section').textContent.includes('s3cr3t'),'list shows the secret');
 assert(!/hmac|bearer/i.test($('.hook-section').textContent),'engineering word in the section');
 if(stop('list'))return;
 $('.hook-row').click();await waitFor(()=>$('.hook-detail'),'detail missing');
 const secret=()=>[...document.querySelectorAll('.hook-detail .hook-copy')].at(-1).textContent;
 assert(!secret().includes('s3cr3t')&&secret().includes('••••'),'detail secret not masked');
 if(stop('detail'))return;
 $('.hook-reveal').click();await waitFor(()=>secret().includes('s3cr3t'),'reveal did not show the secret');
 if(stop('detail-secret'))return;
 // The gateway drops and comes back while the detail is open.
 const addr=()=>$('.hook-detail .hook-addr .hook-copy').textContent,gw=()=>{hookFixture.public=!hookFixture.public;document.dispatchEvent(new Event('visibilitychange'))};
 const follows=()=>hookFixture.public?addr().includes('公网地址')&&!$('.hook-detail .hook-gw'):addr().includes('127.0.0.1')&&$('.hook-detail .hook-gw');
 gw();await waitFor(follows,'detail did not follow the gateway change');
 gw();await waitFor(follows,'detail did not follow the gateway coming back');
 assert(secret().includes('s3cr3t'),'gateway change hid the revealed secret');
 $('.hook-remove').click();await waitFor(()=>!$('#modal').hidden&&$('#modal-title').textContent.includes('撤掉'),'remove confirm missing');
 if(stop('remove'))return;
 $('#modal-form').requestSubmit();await waitFor(()=>$('#modal').hidden&&$('.hook-empty'),'not back to empty after remove');
 assert(hookFixture.calls.includes('DELETE github'),'remove call');
 assert(!testErrors.length,'page errors: '+testErrors.join('; '));
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`
