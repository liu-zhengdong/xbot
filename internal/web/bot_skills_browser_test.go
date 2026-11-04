package web

import "testing"

// Exercise the shipped page. Only HTTP data and response timing are scripted.
func TestBotSkillsReadOnlyBrowser(t *testing.T) {
	t.Setenv("XBOT_STICK_VIEWPORT", "1440,900")
	runChatFramesBrowser(t, transcriptWindowFixture+botSkillsFixture, botSkillsDriverPrelude+botSkillsReadOnlyDriver)
}

func TestBotSkillsIgnoreLateResponsesBrowser(t *testing.T) {
	t.Setenv("XBOT_STICK_VIEWPORT", "1440,900")
	runChatFramesBrowser(t, transcriptWindowFixture+botSkillsFixture, botSkillsDriverPrelude+botSkillsLateDriver)
}

func TestBotSkillsNarrowRetryBrowser(t *testing.T) {
	t.Setenv("XBOT_STICK_VIEWPORT", "390,844")
	runChatFramesBrowser(t, transcriptWindowFixture+botSkillsFixture, botSkillsDriverPrelude+botSkillsNarrowDriver)
}

const botSkillsFixture = `
const botSkillBody='# 安全技能正文\n\n**只读粗体** 与中文说明。\n\n- 第一项\n- 第二项\n\n'+
 '<img src="data:," onerror="window.botSkillUnsafe=1">\n'+
 '<a href="javascript:window.botSkillUnsafe=2">危险链接</a>\n\n'+
 '[危险 Markdown 链接](javascript:window.botSkillUnsafe=3)\n';
const botSkillData={
 [bots[0].name]:[
  {slug:'alpha',name:'legacy-readable-name',title:'Alpha 标题',description:'PRIMARY_ONLY Common Memo 中文整理',disabled:false},
  {slug:'legacy',name:'Legacy 可读名称',title:'',description:'Common old-format description',disabled:false},
  {slug:'slug-only',name:'',title:'',description:'Common fallback description',disabled:false},
  {slug:'other',name:'other-name',title:'Other 标题',description:'Common description-only-needle',disabled:false},
  {slug:'disabled',name:'Disabled 名称',title:'已停用技能',description:'Common 中文整理',disabled:true}
 ],
 [bots[1].name]:[{slug:'secondary',name:'secondary',title:'第二个 Bot 技能',description:'SECONDARY_ONLY Common',disabled:false}]
};
const botSkillsHTTP={calls:[],gates:new Map(),fail:new Set(),globalReads:0};
const botSkillsPath=(name,slug='')=>'/api/bots/'+encodeURIComponent(name)+'/skills'+(slug?'/'+encodeURIComponent(slug):'');
// Each route owns its promise. Polling cannot consume another request's gate.
const holdBotSkills=route=>{
 let release;const promise=new Promise(resolve=>release=resolve);
 const gate={promise,release,started:false,returned:false};botSkillsHTTP.gates.set(route,gate);return gate;
};
const botSkillsBaseFetch=window.fetch;
window.fetch=async(path,opts={})=>{
 const route=new URL(String(path),location.href).pathname,method=opts.method||'GET';
 const match=route.match(/^\/api\/bots\/([^/]+)\/skills(?:\/([^/]+))?$/);
 if(match){
  botSkillsHTTP.calls.push({route,method});
  if(method!=='GET')return new Response('{"error":"read-only fixture"}',{status:405});
  const name=decodeURIComponent(match[1]),slug=match[2]?decodeURIComponent(match[2]):'',skills=botSkillData[name]||[];
  const skill=skills.find(item=>item.slug===slug);
  const payload=slug?(skill?{...skill,body:slug==='alpha'?botSkillBody:'# '+skill.title+'\n\n'+skill.description}:null):{skills};
  // Snapshot at request start, then deliver even if the UI aborts the old read.
  const body=JSON.stringify(payload),gate=botSkillsHTTP.gates.get(route);
  if(gate){botSkillsHTTP.gates.delete(route);gate.started=true;await gate.promise;gate.returned=true}
  if(botSkillsHTTP.fail.has(route))return new Response('{"error":"scripted skill read failure"}',{status:500});
  return new Response(body,{status:payload?200:404,headers:{'Content-Type':'application/json'}});
 }
 if(route==='/api/skills'&&method==='GET'){
  botSkillsHTTP.globalReads++;
  return json({skills:[{slug:'global-library',name:'global-library',title:'真实全局库入口',description:'GLOBAL_LIBRARY_ONLY'}]});
 }
 return botSkillsBaseFetch(path,opts);
};
`

const botSkillsDriverPrelude = `
const $=selector=>document.querySelector(selector),assert=(value,message)=>{if(!value)throw Error(message)};
const sleep=ms=>new Promise(resolve=>setTimeout(resolve,ms));
const waitFor=async(predicate,message)=>{for(let i=0;i<900;i++){if(predicate())return;await sleep(10)}throw Error(message)};
const frame=()=>new Promise(resolve=>requestAnimationFrame(resolve));
const skillView=()=>$('#workspace-view-skills .bot-skills-view');
const skillField=()=>$('#workspace-view-skills .bot-skills-search input');
const skillRow=slug=>$('#workspace-view-skills .bot-skill-row[data-slug="'+slug+'"]');
const skillDetail=()=>$('#workspace-view-skills .bot-skill-detail');
const skillSlugs=()=>[...document.querySelectorAll('#workspace-view-skills .bot-skill-row[data-slug]')].filter(node=>node.checkVisibility()).map(node=>node.dataset.slug).sort().join(',');
const typeSkillQuery=(value,composing=false)=>{const field=skillField();field.value=value;field.dispatchEvent(new InputEvent('input',{bubbles:true,isComposing:composing}))};
const openSkills=async()=>{
 assert($('#workspace-tab-skills'),'skills tab absent');$('#workspace-tab-skills').click();
 await waitFor(()=>skillView()?.checkVisibility(),'skills view did not open');
};
const bootSkills=async()=>{
 await waitFor(()=>$('#thread [data-seq="420"]')&&!$('.thread-loading-cover'),'chat boot failed');
 assert($('#workspace-tab-skills'),'skills tab absent');
};
const doneSkills=()=>{assert(testErrors.length===0,testErrors.join('; '));document.body.dataset.testResult='passed'};
`

const botSkillsReadOnlyDriver = `
(async()=>{
 await bootSkills();
 const tabs=[...document.querySelectorAll('#workspace-tabs [role=tab]')],inbox=tabs.findIndex(node=>node.textContent.trim()==='收件箱');
 assert(inbox>=0&&tabs[inbox+1]===$('#workspace-tab-skills'),'skills tab must follow inbox');
 const thread=$('#thread'),message=$('#thread [data-seq="420"]'),input=$('#input');
 input.textContent='读技能时保留的聊天草稿';input.dispatchEvent(new InputEvent('input',{bubbles:true,inputType:'insertText'}));
 thread.scrollTop=100;thread.dispatchEvent(new Event('scroll'));await frame();await frame();
 const scroll=thread.scrollTop;
 await openSkills();await waitFor(()=>skillSlugs()==='alpha,legacy,other,slug-only','enabled skill list missing');
 assert(!skillRow('disabled')?.checkVisibility(),'disabled skill appeared in the default list');
 assert(skillRow('alpha').textContent.trim().startsWith('Alpha 标题'),'title did not precede legacy name');
 assert(skillRow('legacy').textContent.trim().startsWith('Legacy 可读名称'),'name fallback missing');
 assert(skillRow('slug-only').textContent.trim().startsWith('slug-only'),'slug fallback missing');
 const field=skillField();assert(field,'search input missing');
 for(const [query,want] of [['aLPHa 标题','alpha'],['LEGACY-READABLE-NAME','alpha'],['DESCRIPTION-ONLY-NEEDLE','other'],['SLUG-ONLY','slug-only'],['中文整理','alpha']]){
  typeSkillQuery('');await waitFor(()=>skillSlugs()==='alpha,legacy,other,slug-only','search reset failed');
  typeSkillQuery(query);await waitFor(()=>skillSlugs()===want,'search did not match title/name/description/slug: '+query);
 }
 typeSkillQuery('no-such-skill');await waitFor(()=>skillSlugs()==='','no-match search retained old rows');
 const clear=$('#workspace-view-skills .bot-skills-clear');assert(clear?.checkVisibility(),'empty search lacks clear action');clear.click();
 await waitFor(()=>skillField().value===''&&skillSlugs()==='alpha,legacy,other,slug-only','clear did not restore enabled skills');
 // Ignore incomplete IME text, then apply the completed Chinese query.
 const composingField=skillField();composingField.dispatchEvent(new CompositionEvent('compositionstart',{bubbles:true}));
 typeSkillQuery('中文整理',true);await sleep(220);
 assert(skillSlugs()==='alpha,legacy,other,slug-only','IME composition filtered incomplete text');
 composingField.dispatchEvent(new CompositionEvent('compositionend',{bubbles:true,data:'中文整理'}));
 await waitFor(()=>skillSlugs()==='alpha','composition end did not filter Chinese text');
 skillRow('alpha').click();
 await waitFor(()=>skillDetail()?.querySelector('strong')?.textContent==='只读粗体','detail did not render Markdown');
 const detail=skillDetail();
 assert(document.activeElement===detail.querySelector('h2'),'detail response lost keyboard focus');
 assert(detail.innerText.includes('安全技能正文')&&!detail.innerText.includes('# 安全技能正文'),'Markdown heading was not rendered');
 assert(detail.querySelectorAll('li').length===2,'Markdown list missing');
 assert(!detail.querySelector('textarea,input,[contenteditable="true"],[role="switch"]'),'skill detail exposes editing controls');
 assert(!detail.querySelector('script,iframe,[onerror],[onclick]'),'harmful HTML survived Markdown rendering');
 assert(![...detail.querySelectorAll('a[href]')].some(node=>/^javascript:/i.test(node.getAttribute('href').trim())),'unsafe link survived Markdown rendering');
 assert(!window.botSkillUnsafe,'skill HTML executed');
 const raw=detail.querySelector('.bot-skill-raw details,details.bot-skill-raw');assert(raw,'raw disclosure missing');
 assert(!raw.open,'raw source must start collapsed');raw.querySelector('summary').click();
 assert(raw.open&&raw.textContent.includes(botSkillBody),'raw source changed Markdown text');
 assert(!raw.querySelector('img,a,strong,script'),'raw source was interpreted as HTML');
 const close=$('#workspace-view-skills .bot-skill-close');assert(close?.checkVisibility(),'desktop detail lacks close action');close.click();
 await waitFor(()=>!skillDetail()?.checkVisibility(),'detail did not close');
 assert(skillField().value==='中文整理'&&skillSlugs()==='alpha','closing detail lost search');
 $('#workspace-view-skills .bot-skills-manage').click();
 await waitFor(()=>$('#setpage')?.checkVisibility()&&$('#settings-skills')?.textContent.includes('GLOBAL_LIBRARY_ONLY'),'manage did not open the real global settings');
 assert($('#setpage [data-set="skills"]').getAttribute('aria-current')==='page'&&botSkillsHTTP.globalReads>0,'manage opened the wrong settings section');
 document.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true}));
 await waitFor(()=>$('#setpage').hidden,'settings did not close');
 [...document.querySelectorAll('#workspace-tabs [role=tab]')].find(node=>node.textContent.trim()==='聊天').click();await frame();await frame();
 assert($('#thread')===thread&&$('#thread [data-seq="420"]')===message,'skills navigation replaced chat DOM');
 assert($('#input')===input&&input.textContent==='读技能时保留的聊天草稿','skills navigation lost chat draft');
 assert(Math.abs(thread.scrollTop-scroll)<2,'skills navigation lost historical scroll');
 assert(botSkillsHTTP.calls.every(call=>call.method==='GET'),'read-only skills sent a mutation');doneSkills();
})().catch(error=>document.body.dataset.testResult='failed: '+error.message);
`

const botSkillsLateDriver = `
(async()=>{
 await bootSkills();
 const primary=bots[0].name,secondary=bots[1].name;
 const select=async name=>{
  document.querySelector('[data-bot="'+name+'"]').click();
  await waitFor(()=>$('#title').textContent.includes(name)&&!$('.thread-loading-cover'),'bot did not open: '+name);
 };
 const flush=async gate=>{gate.release();await waitFor(()=>gate.returned,'held response did not return');await frame();await frame()};
 // A late list must not populate another Bot, even when its skills page is open.
 const oldList=holdBotSkills(botSkillsPath(primary));await openSkills();
 await waitFor(()=>oldList.started,'primary list was not held');
 await select(secondary);await openSkills();await waitFor(()=>skillRow('secondary'),'secondary list missing');
 await flush(oldList);
 assert(skillSlugs()==='secondary'&&!skillView().textContent.includes('PRIMARY_ONLY'),'old list crossed Bots');
 await select(primary);await openSkills();await waitFor(()=>skillRow('alpha')&&skillRow('other'),'primary list missing');
 // Open B while A is pending, then release A after B has rendered.
 const olderDetail=holdBotSkills(botSkillsPath(primary,'alpha'));skillRow('alpha').click();
 await waitFor(()=>olderDetail.started,'first detail was not held');
 const pendingClose=$('#workspace-view-skills .bot-skill-close');assert(pendingClose?.checkVisibility(),'pending detail lacks close action');pendingClose.click();
 skillRow('other').click();await waitFor(()=>skillDetail()?.textContent.includes('description-only-needle'),'new detail missing');
 await flush(olderDetail);
 assert(skillDetail().textContent.includes('description-only-needle')&&!skillDetail().textContent.includes('安全技能正文'),'older detail overwrote new selection');
 $('#workspace-view-skills .bot-skill-close').click();
 // Closing a pending detail invalidates its reply.
 const closedDetail=holdBotSkills(botSkillsPath(primary,'legacy'));skillRow('legacy').click();
 await waitFor(()=>closedDetail.started,'closing detail was not held');$('#workspace-view-skills .bot-skill-close').click();
 await flush(closedDetail);assert(!skillDetail()?.checkVisibility(),'late detail reopened a closed pane');
 // Switching Bots clears both the old query and an in-flight detail.
 typeSkillQuery('SLUG-ONLY');await waitFor(()=>skillSlugs()==='slug-only','primary query missing');
 const oldBotDetail=holdBotSkills(botSkillsPath(primary,'slug-only'));skillRow('slug-only').click();
 await waitFor(()=>oldBotDetail.started,'old bot detail was not held');
 document.querySelector('[data-bot="'+secondary+'"]').click();
 assert(!$('#workspace-view-skills')?.textContent.includes('PRIMARY_ONLY')&&!skillRow('slug-only'),'switch retained old skill data');
 await waitFor(()=>$('#title').textContent.includes(secondary)&&!$('.thread-loading-cover'),'secondary bot did not reopen');
 await openSkills();await waitFor(()=>skillRow('secondary')&&skillField(),'new bot skills missing');
 assert(skillField().value===''&&!skillDetail()?.checkVisibility(),'query or detail crossed Bots');
 await flush(oldBotDetail);
 assert(skillSlugs()==='secondary'&&!skillDetail()?.checkVisibility()&&!skillView().textContent.includes('Common fallback description'),'late detail crossed Bots');
 assert(botSkillsHTTP.calls.every(call=>call.method==='GET'),'read-only navigation sent a mutation');doneSkills();
})().catch(error=>document.body.dataset.testResult='failed: '+error.message);
`

const botSkillsNarrowDriver = `
(async()=>{
 await bootSkills();assert(innerWidth===390,'narrow viewport was not applied');
 const listRoute=botSkillsPath(bots[0].name);botSkillsHTTP.fail.add(listRoute);
 await openSkills();
 await waitFor(()=>$('#workspace-view-skills .bot-skills-retry')?.checkVisibility(),'list failure lacks retry');
 assert(!skillSlugs(),'failed list displayed stale skills');
 typeSkillQuery('Common');
 const before=botSkillsHTTP.calls.filter(call=>call.route===listRoute).length;
 botSkillsHTTP.fail.delete(listRoute);$('#workspace-view-skills .bot-skills-retry').click();
 await waitFor(()=>skillSlugs()==='alpha,legacy,other,slug-only','list retry did not recover');
 assert(skillField().value==='Common','list retry discarded search');
 assert(botSkillsHTTP.calls.filter(call=>call.route===listRoute).length>before,'list retry did not refetch');
 typeSkillQuery('aLPHa');await waitFor(()=>skillSlugs()==='alpha','narrow search missing');
 const detailRoute=botSkillsPath(bots[0].name,'alpha');botSkillsHTTP.fail.add(detailRoute);skillRow('alpha').click();
 await waitFor(()=>skillDetail()?.querySelector('.bot-skills-retry')?.checkVisibility(),'detail failure lacks retry');
 botSkillsHTTP.fail.delete(detailRoute);skillDetail().querySelector('.bot-skills-retry').click();
 await waitFor(()=>skillDetail()?.querySelector('strong')?.textContent==='只读粗体','detail retry did not recover');
 assert(!skillRow('alpha')?.checkVisibility()&&!skillField()?.checkVisibility(),'narrow detail still displays the list page');
 const back=$('#workspace-view-skills .bot-skill-back');assert(back?.checkVisibility(),'narrow detail lacks back action');
 const bounds=back.getBoundingClientRect();assert(bounds.left>=0&&bounds.right<=innerWidth&&bounds.top>=0&&bounds.bottom<=innerHeight,'narrow back action is off screen');
 assert(document.documentElement.scrollWidth<=innerWidth+1,'narrow skill detail overflows the viewport');
 back.click();await waitFor(()=>skillField()?.checkVisibility()&&skillSlugs()==='alpha','back did not restore narrow results');
 assert(skillField().value==='aLPHa'&&!skillDetail()?.checkVisibility(),'back lost query or retained detail');
 assert(botSkillsHTTP.calls.every(call=>call.method==='GET'),'retry sent a mutation');doneSkills();
})().catch(error=>document.body.dataset.testResult='failed: '+error.message);
`
