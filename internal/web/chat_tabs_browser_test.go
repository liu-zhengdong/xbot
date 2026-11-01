package web

import "testing"

// Authored independently of the UI implementation. The shipped page owns all
// navigation, rendering, SSE and scrolling; only HTTP/model data is scripted.
func TestChatTabsPreserveChatBrowser(t *testing.T) {
	runChatFramesBrowser(t, transcriptWindowFixture+memoryPanelFixture, chatTabsPreserveDriver)
}

const chatTabsPreserveDriver = `
const $=s=>document.querySelector(s),assert=(v,m)=>{if(!v)throw Error(m)};
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const waitFor=async(f,m)=>{for(let i=0;i<900;i++){if(f())return;await sleep(10)}throw Error(m)};
const tab=name=>[...document.querySelectorAll('#workspace-tabs [role=tab]')].find(n=>n.textContent.trim()===name);
(async()=>{
 await waitFor(()=>$('#thread [data-seq="420"]')&&!$('.thread-loading-cover'),'chat boot');
 await waitFor(()=>tab('聊天')&&tab('记忆')&&tab('轨迹'),'top navigation absent');
 assert(tab('聊天').getAttribute('aria-selected')==='true','opening did not select chat');
 assert([...document.querySelectorAll('#workspace-tabs [role=tab]')][0]===tab('聊天'),'chat is not first');
 const thread=$('#thread'),row=$('#thread [data-seq="420"]'),input=$('#input');
 input.textContent='切换后仍在的草稿';input.dispatchEvent(new InputEvent('input',{bubbles:true,inputType:'insertText'}));
 thread.scrollTop=100;thread.dispatchEvent(new Event('scroll'));await sleep(100);
 const scroll=thread.scrollTop,reads=historyFixture.requests.length;
 tab('记忆').click();await waitFor(()=>$('.memory-view'),'memory unavailable');
 assert($('.memory-document').textContent.includes('关于你'),'whole memory unavailable');
 tab('轨迹').click();await sleep(100);
 document.querySelector('[data-bot="'+bots[0].name+'"]').click();await sleep(100);
 assert($('#input')===input&&input.textContent==='切换后仍在的草稿','draft or input node lost');
 assert($('#thread [data-seq="420"]')===row,'tab switch recreated chat rows');
 assert(Math.abs(thread.scrollTop-scroll)<2,'tab switch lost historical scroll position');
 assert(historyFixture.requests.length===reads,'tab switch refetched chat window');
 const first=tab('聊天');first.focus();first.dispatchEvent(new KeyboardEvent('keydown',{key:'ArrowRight',bubbles:true}));
 assert(tab('记忆').getAttribute('aria-selected')==='true'&&document.activeElement===tab('记忆'),'keyboard navigation lost focus');
 tab('聊天').click();
 $('#workspace-more').click();
 const option=()=>[...document.querySelectorAll('#workspace-more-menu button')].find(b=>b.textContent.includes('收件箱'));
 assert(option(),'more does not expose inbox preference');option().click();
 assert(!tab('收件箱'),'hidden tab still shown');
 if($('#workspace-more').getAttribute('aria-expanded')!=='true')$('#workspace-more').click();
 option().click();assert(tab('收件箱'),'tab cannot be restored');
 document.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true}));
 assert($('#workspace-more').getAttribute('aria-expanded')==='false','Escape did not close tab menu');
 // A model stream must continue while the user reads another full-page view.
 input.textContent='切页时继续回复';input.dispatchEvent(new InputEvent('input',{bubbles:true,inputType:'insertText'}));
 $('#composer').dispatchEvent(new Event('submit',{bubbles:true,cancelable:true}));
 await waitFor(()=>fixture.streams.length,'message not sent');const stream=fixture.streams.at(-1);
 tab('记忆').click();say(stream,'在记忆页期间到达的回复');
 finish(stream,'');await sleep(150);tab('聊天').click();
 await waitFor(()=>thread.textContent.includes('在记忆页期间到达的回复'),'stream result lost while chat hidden');
 assert(thread.textContent.split('在记忆页期间到达的回复').length===2,'stream reply duplicated');
 assert(testErrors.length===0,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`

func TestChatTabsIgnoreOldBotResponseBrowser(t *testing.T) {
	runChatFramesBrowser(t, transcriptWindowFixture+memoryPanelFixture+chatTabsDelayedFixture, chatTabsDelayedDriver)
}

const chatTabsDelayedFixture = `
let holdPrimaryContext=false,releasePrimaryContext;
const tabsBaseFetch=window.fetch;
window.fetch=async(path,opts={})=>{
 const route=String(path),primary=encodeURIComponent(bots[0].name);
 if(holdPrimaryContext&&route==='/api/bots/'+primary+'/context'){
  holdPrimaryContext=false;await new Promise(resolve=>releasePrimaryContext=resolve);
 }
 const response=await tabsBaseFetch(path,opts);
 const match=route.match(/^\/api\/bots\/([^/]+)$/);
 if(match&&(!opts.method||opts.method==='GET')){
  const data=await response.json(),name=decodeURIComponent(match[1]);
  data.memory.person.markdown+='\n\n'+(name===bots[0].name?'PRIMARY_ONLY_MARKER':'SECONDARY_ONLY_MARKER');
  return json(data);
 }
 return response;
};
`

const chatTabsDelayedDriver = `
const $=s=>document.querySelector(s),assert=(v,m)=>{if(!v)throw Error(m)};
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const waitFor=async(f,m)=>{for(let i=0;i<900;i++){if(f())return;await sleep(10)}throw Error(m)};
const tab=name=>[...document.querySelectorAll('#workspace-tabs [role=tab]')].find(n=>n.textContent.trim()===name);
(async()=>{
 await waitFor(()=>$('#thread [data-seq="420"]')&&!$('.thread-loading-cover')&&tab('轨迹'),'chat boot');
 holdPrimaryContext=true;tab('轨迹').click();
 await waitFor(()=>releasePrimaryContext,'did not start delayed profile load');
 const other=document.querySelector('[data-bot="'+bots[1].name+'"]');other.click();
 await waitFor(()=>$('#title').textContent.includes(bots[1].name)&&$('#thread').textContent.includes('独立对话'),'second bot did not open');
 assert(tab('聊天').getAttribute('aria-selected')==='true','switch bot did not reset to chat');
 releasePrimaryContext();await sleep(200);tab('记忆').click();
 await waitFor(()=>$('.memory-view')?.textContent.includes('SECONDARY_ONLY_MARKER'),'second bot memory absent');
 assert(!$('.memory-view').textContent.includes('PRIMARY_ONLY_MARKER'),'old profile response leaked into new bot');
 assert(!$('#thread').textContent.includes('历史行 420'),'old chat leaked into new bot');
 assert(testErrors.length===0,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`

func TestChatTabsProfileRefreshesLateCatalogBrowser(t *testing.T) {
	runChatFramesBrowser(t, panelFixture+`
let releaseModelCatalog;
const lateCatalogFetch=window.fetch;
window.fetch=async(path,opts={})=>{
 if(String(path).startsWith('/api/models'))await new Promise(resolve=>releaseModelCatalog=resolve);
 return lateCatalogFetch(path,opts);
};
`, `
const $=s=>document.querySelector(s),assert=(v,m)=>{if(!v)throw Error(m)};
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const waitFor=async(f,m)=>{for(let i=0;i<900;i++){if(f())return;await sleep(10)}throw Error(m)};
(async()=>{
 await waitFor(()=>$('#title').textContent.includes(bots[0].name)&&!$('.thread-loading-cover')&&releaseModelCatalog,'catalog request missing');
 $('#head-chip').click();
 await waitFor(()=>$('.tab-body:not([hidden]) .binding-trigger'),'profile view missing');
 releaseModelCatalog();
 await waitFor(()=>$('.tab-body:not([hidden]) .bt-sub')?.textContent==='fake · Completions','late catalog did not update profile metadata');
 assert(testErrors.length===0,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>document.body.dataset.testResult='failed: '+e.message);
`)
}

func TestChatTabsDesktopShellBrowser(t *testing.T) {
	for _, platform := range []string{"darwin", "windows", "linux"} {
		t.Run(platform, func(t *testing.T) {
			fixture := `document.documentElement.style.setProperty('--mygo-titlebar-height','32px');window.mygo={platform:'` + platform + `'};`
			runChatFramesBrowser(t, fixture, chatTabsDesktopDriver)
		})
	}
}

const chatTabsDesktopDriver = `
const $=s=>document.querySelector(s),assert=(v,m)=>{if(!v)throw Error(m)};
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const waitFor=async(f,m)=>{for(let i=0;i<900;i++){if(f())return;await sleep(10)}throw Error(m)};
(async()=>{
 await waitFor(()=>$('#workspace-tabs [role=tab]')&&!$('.thread-loading-cover'),'shell boot');
 assert(document.documentElement.classList.contains('in-app'),'desktop environment not applied');
 for(const button of document.querySelectorAll('#workspace-tabs button,#workspace-more,#head-chip,#workspace-theme')){
  button.scrollIntoView({block:'nearest',inline:'nearest'});await sleep(30);
  const r=button.getBoundingClientRect(),hit=document.elementFromPoint(r.left+r.width/2,r.top+r.height/2);
  assert(r.width>0&&r.height>0&&button.contains(hit),'shell control covered: '+button.id);
  assert(getComputedStyle(button).getPropertyValue('-webkit-app-region')!=='drag','control swallowed by caption drag: '+button.id);
 }
 assert(getComputedStyle($('#workspace-tabs').parentElement).getPropertyValue('-webkit-app-region')==='no-drag','tabs are in draggable region');
 const top=$('.top').getBoundingClientRect(),thread=$('#thread').getBoundingClientRect();
 assert(thread.top>=top.bottom-1,'desktop caption overlaps content');
 assert(testErrors.length===0,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>document.body.dataset.testResult='failed: '+e.message);
`

func TestChatTabsCrossPageFeedbackBrowser(t *testing.T) {
	runChatFramesBrowser(t, panelFixture+memoryPanelFixture, chatTabsFeedbackDriver)
}

const chatTabsFeedbackDriver = `
const $=s=>document.querySelector(s),assert=(v,m)=>{if(!v)throw Error(m)};
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const waitFor=async(f,m)=>{for(let i=0;i<900;i++){if(f())return;await sleep(10)}throw Error(m)};
const tab=name=>[...document.querySelectorAll('#workspace-tabs [role=tab]')].find(n=>n.textContent.trim()===name);
(async()=>{
 await waitFor(()=>tab('记忆')&&!$('.thread-loading-cover'),'boot');tab('记忆').click();
 await waitFor(()=>$('.memory-view'),'memory');
 const bot=$('[data-bot="'+bots[0].name+'"]');bot.dispatchEvent(new MouseEvent('contextmenu',{bubbles:true,cancelable:true}));
 [...document.querySelectorAll('.panel-more button')].find(b=>b.textContent.includes('复制 Bot ID')).click();
 await waitFor(()=>!$('#toast').hidden,'copy feedback absent');
 assert($('#toast').checkVisibility(),'copy feedback hidden behind non-chat page');
 $('#chat-search').click();await sleep(50);
 assert($('#history-search-panel').checkVisibility(),'search button opens an invisible panel');
 assert(document.activeElement===$('#history-search'),'search cannot focus its input');
 assert(tab('聊天').getAttribute('aria-selected')==='true','global chat search did not return to chat');
 assert(testErrors.length===0,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>document.body.dataset.testResult='failed: '+e.message);
`

func TestChatTabsKeepTaskDraftAfterReadFailureBrowser(t *testing.T) {
	runChatFramesBrowser(t, routineSkillsFixture+`
let failTaskProfile=false;
const taskProfileFetch=window.fetch;
window.fetch=async(path,opts={})=>{
 if(failTaskProfile&&/^\/api\/bots\/[^/]+$/.test(String(path)))return new Response('{"error":"scripted profile failure"}',{status:500});
 return taskProfileFetch(path,opts);
};
`, chatTabsTaskFailureDriver)
}

const chatTabsTaskFailureDriver = `
const $=s=>document.querySelector(s),assert=(v,m)=>{if(!v)throw Error(m)};
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const waitFor=async(f,m)=>{for(let i=0;i<900;i++){if(f())return;await sleep(10)}throw Error(m)};
const tab=name=>[...document.querySelectorAll('#workspace-tabs [role=tab]')].find(n=>n.textContent.trim()===name);
(async()=>{
 await waitFor(()=>tab('任务')&&!$('.thread-loading-cover'),'boot');tab('任务').click();
 await waitFor(()=>$('[data-routine=r1] .routine-skills-summary'),'routine');
 $('[data-routine=r1] .routine-skills-summary').click();
 const input=$('[data-routine=r1] form input');input.value='尚未保存的技能草稿';
 failTaskProfile=true;tab('轨迹').click();
 await waitFor(()=>$('.workspace-view:not([hidden]) .workspace-error'),'error not surfaced');
 tab('任务').click();await sleep(100);
 assert(input.isConnected&&input.value==='尚未保存的技能草稿','read failure destroyed cached task draft');
 failTaskProfile=false;
 const retry=$('.workspace-view:not([hidden]) .workspace-error button');if(retry)retry.click();
 await sleep(250);assert($('[data-routine=r1] form input')===input&&input.value==='尚未保存的技能草稿','recovery replaced task draft');
 assert(testErrors.length===0,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>document.body.dataset.testResult='failed: '+e.message);
`

func TestChatTabsKeepLoadedExperienceWhenNewArrivesBrowser(t *testing.T) {
	runChatFramesBrowser(t, memoryPanelFixture, chatTabsExperienceDriver)
}

const chatTabsExperienceDriver = `
const $=s=>document.querySelector(s),assert=(v,m)=>{if(!v)throw Error(m)};
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const waitFor=async(f,m)=>{for(let i=0;i<900;i++){if(f())return;await sleep(10)}throw Error(m)};
(async()=>{
 await waitFor(()=>$('#workspace-tabs [role=tab]')&&!$('.thread-loading-cover'),'boot');
 [...document.querySelectorAll('#workspace-tabs [role=tab]')].find(n=>n.textContent==='记忆').click();
 await waitFor(()=>$('.memory-earlier'),'memory');
 const old=$('[data-experience="42"]');old.open=true;$('.memory-earlier').click();
 if(typeof holdingMemoryPage==='undefined')await waitFor(()=>document.querySelectorAll('.memory-experience').length===3,'older page');
 else await waitFor(()=>releaseMemoryPage,'pagination request missing');
 memoryData.experiences.items=[{id:44,created_at:'2026-10-31T08:00:00Z',summary:'新到达的经历'},memoryData.experiences.items[0]];
 memoryData.experiences.next_before=43;
 document.dispatchEvent(new Event('visibilitychange'));
 await waitFor(()=>$('[data-experience="44"]'),'latest experience absent');
 assert(old.isConnected&&old.open,'pending pagination lost the first-page boundary');
 if(typeof holdingMemoryPage!=='undefined')releaseMemoryPage();
 await waitFor(()=>document.querySelectorAll('.memory-experience').length===4,'merged experience page');
 assert(old.isConnected&&old.open&&document.querySelectorAll('.memory-experience').length===4,'new first page evicted a loaded experience');
 assert(testErrors.length===0,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>document.body.dataset.testResult='failed: '+e.message);
`

func TestChatTabsKeepExperienceDuringPaginationBrowser(t *testing.T) {
	runChatFramesBrowser(t, memoryPanelFixture+`
const holdingMemoryPage=true;
let releaseMemoryPage;
const pendingExperienceFetch=window.fetch;
window.fetch=async(path,opts={})=>{
 if(String(path).includes('/experiences?before='))await new Promise(resolve=>releaseMemoryPage=resolve);
 return pendingExperienceFetch(path,opts);
};
`, chatTabsExperienceDriver)
}
