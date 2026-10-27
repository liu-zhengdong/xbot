package web

import "testing"

// Find the editable field by its visible label, then save through the real UI.
func TestProfileNameBrowser(t *testing.T) {
	runChatStreamBrowser(t, chatStreamFixture+panelFixture, profileNameDriver)
}

const profileNameDriver = `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const waitFor=async(fn,msg)=>{for(let i=0;i<600;i++){if(fn())return;await new Promise(r=>setTimeout(r,10))}throw Error(msg)};
(async()=>{
 await waitFor(()=>!$('#shell').hidden&&$('#title').textContent.includes(bots[0].name),'app did not boot');
 $('#head-chip').click();
 await waitFor(()=>$('#panel-actions button'),'edit action missing');
 $('#panel-actions button').click();
 await waitFor(()=>!$('#modal').hidden&&$('#modal-fields input'),'profile did not open');
 const field=label=>[...document.querySelectorAll('#modal-fields label')].find(n=>n.textContent.trim()===label)?.querySelector('input');
 const name=field('名字'),description=field('简介');
 assert(name&&name.value===bots[0].name,'名字 must label the existing Bot name input');
 assert(description,'简介 field missing');
 assert(!$('#modal-fields').textContent.includes('标识'),'engineering label still visible');
 assert($('#modal-fields [aria-label="人设卡：未选用"]'),'persona card picker missing');
 assert(!/说话方式|免打扰/.test($('#modal-fields').textContent),'removed fields returned');
 name.value='名字热修验证';description.value='保留简介与人设卡';
 $('#modal-form').requestSubmit();
 await waitFor(()=>$('#modal').hidden&&$('#title').textContent.includes('名字热修验证'),'save did not update Bot name');
 const patch=fixture.calls.find(([path,opt])=>opt.method==='PATCH');
 assert(patch,'profile was not saved');
 const body=JSON.parse(patch[1].body);
 assert(body.name==='名字热修验证'&&body.description==='保留简介与人设卡','wrong saved fields');
 assert(Object.keys(body).sort().join(',')==='description,name','save changed unrelated fields');
 assert(testErrors.length===0,testErrors.join('; '));
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`
