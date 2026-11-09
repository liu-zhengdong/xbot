package web

import (
	"encoding/base64"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"xbot/internal/browsertest"
)

func TestRandomAvatarBrowser(t *testing.T) {
	runAvatarBrowser(t, panelFixture+randomAvatarFixture, randomAvatarDriver)
}

// Serve real PNG layers over HTTP while the page scripts API responses.
func runAvatarBrowser(t *testing.T, fixture, driver string) {
	t.Helper()
	chrome := browsertest.Chrome(t)
	t.Setenv(devStaticEnv, "")
	s := &Server{Root: t.TempDir(), Owner: "avatar-browser"}
	if _, _, err := s.EnsureToken(); err != nil {
		t.Fatal(err)
	}
	assets, err := fs.Sub(staticFS, "static")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	page := strings.Replace(string(raw), "<head>", "<head><script>"+chatStreamFixture+fixture+"</script>", 1)
	page = strings.Replace(page, "</body>", "<script>"+driver+"</script></body>", 1)
	photo, err := base64.StdEncoding.DecodeString(randomAvatarPhoto)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, page)
	})
	mux.HandleFunc("GET /api/ui/static/{path...}", func(w http.ResponseWriter, r *http.Request) {
		if err := s.AuthorizeOwner(r); err != nil {
			t.Error(err)
			http.Error(w, "fixture authorization failed", http.StatusInternalServerError)
			return
		}
		s.uiStatic(w, r)
	})
	mux.HandleFunc("GET /api/bots/{name}/avatar", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(photo)
	})
	mux.Handle("GET /", http.FileServer(http.FS(assets)))
	server := httptest.NewServer(mux)
	defer server.Close()
	out, err := exec.Command("node", filepath.Join("testdata", "chat-stick.cjs"), chrome, server.URL, filepath.Join(t.TempDir(), "chrome")).CombinedOutput()
	if err != nil {
		t.Fatalf("avatar browser: %v\n%s", err, out)
	}
	t.Log(strings.TrimSpace(string(out)))
}

const randomAvatarPhoto = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII="

const randomAvatarFixture = `
bots[0].avatar='generated';bots[0].avatar_seed='avatar-browser-stable-0';
bots[1].avatar='generated';bots[1].avatar_seed='avatar-browser-stable-1';
fixture.avatarPatches=[];fixture.avatarUploads=0;fixture.avatarClears=0;fixture.avatarCreates=[];
const avatarBaseFetch=window.fetch;
window.fetch=async(path,opts={})=>{
 path=String(path);
 if(path==='/api/persona-cards')return json({cards:[{id:'aaaaaaaaaaaaaaaa',name:'工程合伙人',default:true}]});
 if(path==='/api/bots'&&opts.method==='POST'){
  const body=JSON.parse(opts.body);fixture.avatarCreates.push(body);
  const saved={...body,pinned:true,avatar:'generated',primary:false};bots.push(saved);fixture.messages[body.name]=[];
  return json(saved);
 }
 const profile=path.match(/^\/api\/bots\/([^/]+)$/);
 if(profile&&opts.method==='PATCH'){
  const body=JSON.parse(opts.body);
  if(Object.hasOwn(body,'avatar_seed')){
   fixture.avatarPatches.push(body);
   if(fixture.failAvatarPatch){fixture.failAvatarPatch=false;return new Response('{"error":"disk unavailable"}',{status:500,headers:{'Content-Type':'application/json'}})}
   const saved=bots.find(b=>b.name===decodeURIComponent(profile[1]));
   saved.avatar='generated';saved.avatar_seed=body.avatar_seed;delete saved.avatar_url;
   return json({...saved,scope:'runtime',applied:true,applied_fields:['avatar_seed']});
  }
 }
 const upload=path.match(/^\/api\/bots\/([^/]+)\/avatar$/);
 if(upload){
  const saved=bots.find(b=>b.name===decodeURIComponent(upload[1]));
  if(opts.method==='POST'){
   if(!(opts.body instanceof FormData)||!opts.body.get('file'))throw Error('avatar upload did not submit multipart file');
   fixture.avatarUploads++;saved.avatar='custom';saved.avatar_url=path+'?v=fixture-upload';
  }else if(opts.method==='DELETE'){
   fixture.avatarClears++;saved.avatar='generated';delete saved.avatar_url;
  }
  return json({...saved,scope:'runtime',applied:true});
 }
 return avatarBaseFetch(path,opts);
};
`

const randomAvatarDriver = `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const waitFor=async(fn,msg)=>{for(let i=0;i<600;i++){if(fn())return;await new Promise(r=>setTimeout(r,10))}throw Error(typeof msg==='function'?msg():msg)};
const svgOf=text=>new DOMParser().parseFromString(text,'image/svg+xml').documentElement;
const sources=node=>[...node.querySelectorAll('image')].map(n=>n.getAttribute('href')||n.getAttributeNS('http://www.w3.org/1999/xlink','href'));
const signature=node=>JSON.stringify(sources(node));
const portraitSignature=seed=>signature(svgOf(window.XBotAvatar.portrait(seed)));
const profileFace=()=>$('#modal-fields .avatar');
const sideFace=name=>document.querySelector('[data-bot="'+name+'"] .avatar');
const headerFace=()=>$('#title .avatar');
const randomButton=()=>$('#modal-fields button[aria-label="换一个"]');
const openProfile=async()=>{
 if($('#head-chip').getAttribute('aria-expanded')!=='true')$('#head-chip').click();
 await waitFor(()=>$('#panel-actions button'),'profile edit action missing');
 $('#panel-actions button').click();
 await waitFor(()=>!$('#modal').hidden&&profileFace()&&randomButton(),'profile avatar controls missing');
};
const openNew=async()=>{
 $('#rail-new').click();
 const entry=[...document.querySelectorAll('[role="menuitem"]')].find(n=>n.textContent.trim()==='新建 Bot');
 assert(entry,'new Bot entry missing');entry.click();
 await waitFor(()=>!$('#modal').hidden&&$('#modal-fields .newbot-avatar .avatar'),'new Bot portrait missing');
};
(async()=>{
 const name=bots[0].name;
 await waitFor(()=>!$('#shell').hidden&&headerFace()&&sideFace(name)&&!$('.thread-loading-cover'),'app did not boot');
 const lib=window.XBotAvatar,seed=bots[0].avatar_seed;
 assert(lib&&typeof lib.portrait==='function'&&typeof lib.face==='function','avatar renderer unavailable');
 const portrait=lib.portrait(seed),face=lib.face(seed);
 assert(typeof portrait==='string'&&typeof face==='string','avatar renderer no longer returns SVG text');
 assert(portrait===lib.portrait(seed)&&face===lib.face(seed),'same seed changed its avatar');
 const allSources=new Set(),combinations=new Set();
 for(let i=0;i<24;i++){
  const node=svgOf(lib.portrait('avatar-browser-sample-'+i)),layers=sources(node);
  assert(node.localName==='svg'&&layers.length>=4,'avatar is not composed from image layers');
  assert(layers.every(src=>/^\/api\/ui\/static\/avatar-parts\/.+\.png$/.test(src)),'avatar image is outside the device static source');
  combinations.add(JSON.stringify(layers));layers.forEach(src=>allSources.add(src));
 }
 assert(combinations.size>=12,'different seeds do not produce varied combinations');
 assert(signature(svgOf(face))===signature(svgOf(portrait)),'small and large avatar use different parts');
 assert(headerFace().querySelector('svg image')&&sideFace(name).querySelector('svg image'),'generated portrait is not mounted as SVG DOM');
 assert(!headerFace().querySelector('img[src^="data:image/svg+xml"]'),'avatar still uses an SVG image data URL');
 assert(signature(headerFace())===portraitSignature(seed)&&signature(sideFace(name))===portraitSignature(seed),'header and sidebar disagree with saved seed');
 const remote=document.createElement('div');remote.innerHTML=window.XBotFaces.html('远端Bot',seed,'');document.body.append(remote);
 assert(signature(remote)===portraitSignature(seed),'remote avatar used a different combination');remote.remove();
 await Promise.all([...allSources].map(async src=>{
  const img=new Image();img.src=src;
  try{await img.decode()}catch(e){throw Error('avatar image did not decode: '+src)}
  assert(img.naturalWidth>0&&img.naturalHeight>0,'avatar image is empty: '+src);
 }));
 await openProfile();
 assert(signature(profileFace())===portraitSignature(seed),'profile portrait differs from sidebar');
 assert(!$('#modal-fields [aria-pressed]')&&!$('#modal-fields [role="tablist"]'),'profile still offers avatar part or portrait selection');
 assert($('#modal-fields button[aria-label="上传图片"]'),'profile upload action missing');
 const oldSeed=bots[0].avatar_seed,oldFace=signature(profileFace());
 randomButton().click();
 await waitFor(()=>fixture.avatarPatches.length===1&&bots[0].avatar_seed!==oldSeed&&signature(profileFace())!==oldFace,'random avatar did not save');
 const firstPatch=fixture.avatarPatches[0];
 assert(Object.keys(firstPatch).join(',')==='avatar_seed'&&typeof firstPatch.avatar_seed==='string'&&firstPatch.avatar_seed,'random action changed unrelated profile fields');
 await waitFor(()=>signature(headerFace())===portraitSignature(bots[0].avatar_seed)&&signature(sideFace(name))===portraitSignature(bots[0].avatar_seed),()=> 'saved avatar did not refresh: header='+String(signature(headerFace())===portraitSignature(bots[0].avatar_seed))+', sidebar='+String(signature(sideFace(name))===portraitSignature(bots[0].avatar_seed)));
 const savedSeed=bots[0].avatar_seed,savedFace=signature(profileFace());
 fixture.failAvatarPatch=true;randomButton().click();
 await waitFor(()=>fixture.avatarPatches.length===2&&$('#modal-fields [role="alert"]')?.textContent,'avatar save failure was not shown');
 assert(bots[0].avatar_seed===savedSeed&&signature(profileFace())===savedFace&&signature(headerFace())===savedFace&&signature(sideFace(name))===savedFace,'failed random save replaced the old avatar');
 randomButton().click();
 await waitFor(()=>fixture.avatarPatches.length===3&&bots[0].avatar_seed!==savedSeed&&signature(profileFace())===portraitSignature(bots[0].avatar_seed),'random avatar retry failed');
 const beforeUpload=bots[0].avatar_seed,beforeUploadFace=portraitSignature(beforeUpload);
 const picker=$('#modal-fields input[type="file"]');assert(picker,'avatar file input missing');
 let opened=0;const nativeClick=picker.click;picker.click=()=>{opened++};$('#modal-fields button[aria-label="上传图片"]').click();picker.click=nativeClick;
 assert(opened===1,'upload action did not open the file picker');
 const bytes=Uint8Array.from(atob('` + randomAvatarPhoto + `'),c=>c.charCodeAt(0));
 const dt=new DataTransfer();dt.items.add(new File([bytes],'avatar.png',{type:'image/png'}));picker.files=dt.files;picker.dispatchEvent(new Event('change'));
 const clear=()=>$('#modal-fields button[aria-label="清除上传的图，用回生成的"]');
 await waitFor(()=>fixture.avatarUploads===1&&clear()&&profileFace()?.querySelector('img'),'avatar upload did not settle');
 assert(bots[0].avatar_seed===beforeUpload,'upload lost the previous random seed');
 await waitFor(()=>headerFace()?.querySelector('img')&&sideFace(name)?.querySelector('img'),'upload did not refresh header and sidebar');
 await profileFace().querySelector('img').decode();
 clear().click();
 await waitFor(()=>fixture.avatarClears===1&&!clear()&&signature(profileFace())===beforeUploadFace,'clear did not restore previous random avatar');
 assert(bots[0].avatar_seed===beforeUpload,'clear generated a new seed');
 await waitFor(()=>signature(headerFace())===beforeUploadFace&&signature(sideFace(name))===beforeUploadFace,'clear did not refresh header and sidebar');
 $('#modal-cancel').click();await openProfile();
 assert(signature(profileFace())===beforeUploadFace,'reopening profile lost saved random avatar');
 $('#modal-cancel').click();await openNew();
 const newFace=()=>$('#modal-fields .newbot-avatar .avatar');
 const defaultFace=signature(newFace());
 assert(sources(newFace()).length>=4,'new Bot has no default generated avatar');
 const newName=$('#modal-fields label input:not([type="file"])'),newDuty=$('#modal-fields textarea');
 assert(newName&&newDuty,'new Bot fields missing');
 newName.value='随机头像新建验证';newName.dispatchEvent(new Event('input'));
 assert(signature(newFace())===defaultFace,'typing the Bot name changed its random preview');
 newDuty.value='验证随机头像保存。';
 $('#modal-form').requestSubmit();
 await waitFor(()=>fixture.avatarCreates.length===1&&$('#modal').hidden&&$('#title').textContent.includes('随机头像新建验证'),'new Bot creation did not settle');
 const created=fixture.avatarCreates[0];
 assert(created.name==='随机头像新建验证'&&typeof created.avatar_seed==='string'&&created.avatar_seed,'new Bot omitted its default random seed');
 assert(portraitSignature(created.avatar_seed)===defaultFace,'new Bot did not save the displayed random avatar');
 await waitFor(()=>signature(headerFace())===defaultFace&&signature(sideFace(created.name))===defaultFace,'new Bot displayed a different saved avatar');
 await openNew();
 const anotherInitial=signature(newFace());
 assert(anotherInitial!==defaultFace,'new Bot dialogs reused a fixed default avatar');
 randomButton().click();
 assert(signature(newFace())!==anotherInitial,'new Bot random action did not replace the preview');
 $('#modal-cancel').click();
 assert(fixture.avatarCreates.length===1,'cancel created another Bot');
 assert(testErrors.length===0,testErrors.join('; '));
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`
