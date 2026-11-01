package web

import "testing"

func TestOwnerAvatarBrowser(t *testing.T) {
	runPageInChrome(t, `
(async()=>{
 const $=id=>document.getElementById(id),assert=(v,m)=>{if(!v)throw Error(m)};
 const until=async(f,m)=>{for(let i=0;i<200;i++){if(f())return;await new Promise(r=>setTimeout(r,10))}throw Error(m)};
 await until(()=>!$('shell').hidden,'app not open');
 const base=window.fetch;let stored=false,fail=false,posts=0;
 const custom='/api/owner/avatar';
 const photoURL='data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII=#';
 window.fetch=async(p,o={})=>{
  if(p===custom){
   if(o.method==='POST'){
    posts++;assert(o.body instanceof FormData&&o.body.get('file'),'not multipart file');
    if(fail)return new Response(JSON.stringify({error:'disk unavailable'}),{status:500,headers:{'Content-Type':'application/json'}});
    stored=true;
   }else if(o.method==='DELETE'){stored=false;}
   return json({owner_avatar_url:stored?photoURL:''});
  }
  if(p==='/api/session')return json({authed:true,owner:'owner',owner_name:'政东',owner_avatar_url:stored?photoURL:''});
  return base(p,o);
 };
 const open=()=>window.XBotSettings.open('general');await open();
 const old=$('me-avatar').querySelector('img').getAttribute('src');
 const choose=(file)=>{const dt=new DataTransfer();dt.items.add(file);$('settings-owner-file').files=dt.files;$('settings-owner-file').dispatchEvent(new Event('change'))};
 const file=new File([new Uint8Array([137,80,78,71,13,10,26,10])],'face.png',{type:'image/png'});
 choose(file);await until(()=>stored&&!$('settings-owner-upload').disabled,'upload not settled');
 const photo=()=>$('me-avatar').querySelector('img')?.getAttribute('src');
 assert(photo()?.startsWith(photoURL),'me did not use saved URL');
 assert($('settings-owner-preview').querySelector('img').getAttribute('src')===photo(),'preview is not same source');
 window.XBotSettings.close();await open();assert($('settings-owner-preview').querySelector('img').getAttribute('src')===photo(),'reopen lost picture');
 fail=true;choose(file);await until(()=>!$('settings-owner-upload').disabled,'failure hung');
 assert(photo()?.startsWith(photoURL)&&$('settings-owner-error').textContent,'failure erased picture or error');
 choose(new File(['x'],'wrong.svg',{type:'image/svg+xml'}));assert(posts===2,'invalid type uploaded');
 $('settings-owner-clear').click();await until(()=>!stored&&!$('settings-owner-clear').disabled,'clear hung');
 assert(photo()===old,'clear did not return original owner portrait');
 assert($('settings-owner-clear').hidden,'clear stays visible without upload');
 assert(!testErrors.length,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`)
}
