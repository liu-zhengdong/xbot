package web

import "testing"

// A new submission can reuse words while sending a different picture. Only the
// matching submission may settle its optimistic row.
func TestPendingSendDifferentImagesBrowser(t *testing.T) {
	runChatFramesBrowser(t, pendingSendImagesFixture, pendingSendImagesDriver)
}

const pendingSendImagesFixture = `
const pendingImageIDs=['a'.repeat(32)+'.png','b'.repeat(32)+'.png'];
const pendingImageRequests=[];
const pendingImageSource=Object.getOwnPropertyDescriptor(HTMLImageElement.prototype,'src');
Object.defineProperty(HTMLImageElement.prototype,'src',{...pendingImageSource,set(value){
 if(String(value).includes('/paste/')){
  this.dataset.fixtureImage=value;
  value='data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jf1kAAAAASUVORK5CYII=';
 }
 pendingImageSource.set.call(this,value);
}});
let pendingImageUploads=0;
const pendingImageFetch=window.fetch;
window.fetch=async(path,opts={})=>{
 if(String(path).endsWith('/paste')&&opts.method==='POST')return json({image:pendingImageIDs[pendingImageUploads++]});
 if(String(path).endsWith('/messages')&&opts.method==='POST'){
  pendingImageRequests.push(JSON.parse(opts.body));
  return new Response(JSON.stringify({error:'scripted send failure'}),{status:400,headers:{'Content-Type':'application/json'}});
 }
 return pendingImageFetch(path,opts);
};
`

const pendingSendImagesDriver = `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const waitFor=async(fn,msg)=>{for(let i=0;i<800;i++){if(fn())return;await new Promise(r=>setTimeout(r,10))}throw Error(msg)};
const failedImages=()=>[...document.querySelectorAll('#thread .msg.user')].filter(row=>row.querySelector('.send-failed')).map(row=>row.querySelector('.gallery img')?.dataset.fixtureImage||'');
async function attachPendingImage(n){
 const bytes=Uint8Array.from(atob('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jf1kAAAAASUVORK5CYII='),c=>c.charCodeAt(0));
 const transfer=new DataTransfer();transfer.items.add(new File([bytes],'picture-'+n+'.png',{type:'image/png'}));
 $('#input').dispatchEvent(new ClipboardEvent('paste',{bubbles:true,cancelable:true,clipboardData:transfer}));
 await waitFor(()=>pendingImageUploads===n&&!$('#send').disabled,'picture upload did not finish');
}
(async()=>{
 await waitFor(()=>!$('#shell').hidden&&$('#title').textContent.includes('2号小伙子')&&!$('.thread-loading-cover'),'app did not boot');
 const text='看这张图';
 $('#input').textContent=text;$('#input').dispatchEvent(new Event('input'));
 await attachPendingImage(1);$('#composer').requestSubmit();
 await waitFor(()=>pendingImageRequests.length===1&&failedImages().some(src=>src.endsWith(pendingImageIDs[0])),'first failed picture missing');
 assert($('#input').textContent===text,'failed draft was not restored');
 await attachPendingImage(2);$('#composer').requestSubmit();
 await waitFor(()=>pendingImageRequests.length===2&&failedImages().some(src=>src.endsWith(pendingImageIDs[1])),'second failed picture missing');
 assert(pendingImageRequests.every(request=>request.text===text),'fixture changed the message words');
 assert(pendingImageRequests[0].images[0]===pendingImageIDs[0]&&pendingImageRequests[1].images[0]===pendingImageIDs[1],'fixture did not submit distinct attachments');
 assert(failedImages().some(src=>src.endsWith(pendingImageIDs[0])),'new same-text submission removed a different failed picture');
 assert(failedImages().length===2,'failed submissions did not retain separate identities');
 assert(testErrors.length===0,testErrors.join('; '));
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`
