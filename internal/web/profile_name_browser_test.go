package web

import "testing"

// Find the editable field by its visible label, then save through the real UI.
// Details show 职责 and the card's name; the drawer edits the duty and picks a card from a menu.
func TestProfileNameBrowser(t *testing.T) {
	runChatStreamBrowser(t, chatStreamFixture+panelFixture, profileNameDriver)
}

const profileNameDriver = `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const waitFor=async(fn,msg)=>{for(let i=0;i<600;i++){if(fn())return;await new Promise(r=>setTimeout(r,10))}throw Error(msg)};
(async()=>{
 await waitFor(()=>!$('#shell').hidden&&$('#title').textContent.includes(bots[0].name),'app did not boot');
 const cards=[{id:'aaaaaaaaaaaaaaaa',name:'工程合伙人',preview:'长期技术搭档',default:true},{id:'bbbbbbbbbbbbbbbb',name:'碎嘴群友',preview:'爱接梗'}];
 let bound=cards[0];
 const panelFetch=window.fetch;
 window.fetch=async(path,opts={})=>{
  path=String(path);
  const json=v=>new Response(JSON.stringify(v),{headers:{'Content-Type':'application/json'}});
  if(path==='/api/persona-cards')return json({cards});
  if(path.endsWith('/persona-card')&&opts.method==='PUT'){fixture.calls.push([path,opts]);bound=cards.find(c=>c.id===JSON.parse(opts.body).id);return json({persona_card:bound})}
  const res=await panelFetch(path,opts);
  if(/^\/api\/bots\/[^/]+$/.test(path)&&(opts.method||'GET')==='GET'){const v=await res.json();return json({...v,persona_card:bound})}
  return res;
 };
 $('#head-chip').click();
 await waitFor(()=>$('#panel-actions button'),'edit action missing');
 $('#panel-actions button').click();
 await waitFor(()=>!$('#modal').hidden&&$('#modal-fields input'),'profile did not open');
 const field=label=>[...document.querySelectorAll('#modal-fields label')].find(n=>n.textContent.trim()===label)?.querySelector('input');
 const name=field('名字'),description=field('简介');
 assert(name&&name.value===bots[0].name,'名字 must label the existing Bot name input');
 assert(description,'简介 field missing');
 assert(!$('#modal-fields').textContent.includes('标识'),'engineering label still visible');
 const duty=$('#modal-fields textarea[aria-label="职责"]');
 assert(duty&&duty.value.startsWith('You are XBot'),'duty editor missing');
 const menu=$('#modal-fields select[aria-label="人设卡"]');
 assert(menu,'persona card menu missing');
 await waitFor(()=>!menu.disabled&&menu.options.length===2,'card menu did not load');
 assert(menu.value===cards[0].id&&menu.options[0].textContent==='工程合伙人（默认）','default card not shown');
 assert(![...menu.options].some(o=>/未选用/.test(o.textContent)),'menu offers 未选用');
 menu.value=cards[1].id;menu.dispatchEvent(new Event('change'));
 await waitFor(()=>fixture.calls.some(([p,o])=>p.endsWith('/persona-card')),'pick did not bind');
 assert(JSON.parse(fixture.calls.find(([p])=>p.endsWith('/persona-card'))[1].body).id===cards[1].id,'wrong card bound');
 duty.value='管周报和排期，不碰线上。';
 assert(!/说话方式|免打扰/.test($('#modal-fields').textContent),'removed fields returned');
 name.value='名字热修验证';description.value='保留简介与人设卡';
 $('#modal-form').requestSubmit();
 await waitFor(()=>$('#modal').hidden&&$('#title').textContent.includes('名字热修验证'),'save did not update Bot name');
 const patch=fixture.calls.find(([path,opt])=>opt.method==='PATCH');
 assert(patch,'profile was not saved');
 const body=JSON.parse(patch[1].body);
 assert(body.name==='名字热修验证'&&body.description==='保留简介与人设卡','wrong saved fields');
 assert(body.persona==='管周报和排期，不碰线上。','duty not saved');
 assert(Object.keys(body).sort().join(',')==='description,name,persona','save changed unrelated fields');
 // The save reloads the panel: 职责 first, then the picked card by name only.
 const cardSec=()=>[...document.querySelectorAll('.psec')].find(n=>n.querySelector('h4')?.textContent==='人设卡');
 await waitFor(()=>cardSec()?.textContent.includes('碎嘴群友'),'details lack the picked card');
 const heads=[...document.querySelectorAll('.psec h4')].map(n=>n.textContent);
 assert(heads.indexOf('职责')>=0&&heads.indexOf('职责')<heads.indexOf('人设卡'),'details must show 职责 before 人设卡');
 assert(!heads.includes('人设'),'details still label the duty 人设');
 assert(!cardSec().querySelector('select')&&!cardSec().textContent.includes('爱接梗爱接梗'),'details must show the card name only');
 assert(testErrors.length===0,testErrors.join('; '));
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`
