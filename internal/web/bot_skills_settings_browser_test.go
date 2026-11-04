package web

import "testing"

func TestBotSkillsPendingSettingsSaveBrowser(t *testing.T) {
	t.Setenv("XBOT_STICK_VIEWPORT", "1440,900")
	runChatFramesBrowser(t, transcriptWindowFixture+botSkillsFixture+botSkillsPendingSettingsFixture, botSkillsDriverPrelude+botSkillsPendingSettingsDriver)
}

const botSkillsPendingSettingsFixture = `
let releaseSkillSave,skillSaveStarted=false,skillSaveFinished=false;
const pendingSkillSave=new Promise(resolve=>releaseSkillSave=resolve);
const pendingSettingsBaseFetch=window.fetch;
window.fetch=async(path,opts={})=>{
 const route=new URL(String(path),location.href).pathname,method=opts.method||'GET';
 const alpha=botSkillData[bots[0].name].find(row=>row.slug==='alpha');
 if(route==='/api/skills'&&method==='GET')return json({skills:[alpha]});
 if(route==='/api/skills/alpha'&&method==='GET')return json({...alpha,bots:bots.map(bot=>({name:bot.name,enabled:!(botSkillData[bot.name]||[]).find(row=>row.slug==='alpha')?.disabled}))});
 if(route===botSkillsPath(bots[0].name,'alpha')+'/disable'&&method==='POST'){
  skillSaveStarted=true;await pendingSkillSave;
  alpha.disabled=true;skillSaveFinished=true;
  return json({slug:'alpha',disabled:true});
 }
 return pendingSettingsBaseFetch(path,opts);
};
`

const botSkillsPendingSettingsDriver = `
(async()=>{
 await bootSkills();assert(innerWidth===1440,'desktop viewport was not applied');
 await openSkills();await waitFor(()=>skillRow('alpha'),'enabled alpha missing');
 $('#workspace-view-skills .bot-skills-manage').click();
 await waitFor(()=>$('#setpage')?.checkVisibility()&&$('#settings-skills .skrow'),'settings skills list missing');
 const row=[...document.querySelectorAll('#settings-skills .skrow')].find(node=>node.textContent.includes('Alpha 标题'));
 assert(row,'alpha settings row missing');row.click();
 const toggle=()=>[...document.querySelectorAll('#settings-skills [role="switch"]')].find(node=>node.getAttribute('aria-label')==='在 '+bots[0].name+' 里启用「Alpha 标题」');
 await waitFor(()=>toggle(),'current Bot switch missing');
 assert(toggle().getAttribute('aria-checked')==='true','alpha must begin enabled');toggle().click();
 await waitFor(()=>skillSaveStarted,'disable request did not start');
 const closingRead=holdBotSkills(botSkillsPath(bots[0].name));
 document.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true}));
 assert($('#setpage').hidden,'settings did not close while save was pending');
 await waitFor(()=>closingRead.started,'closing settings did not refresh skills');closingRead.release();
 await waitFor(()=>closingRead.returned&&$('#workspace-view-skills .bot-skills-layout').getAttribute('aria-busy')==='false','closing refresh did not finish');
 assert(!skillSaveFinished&&skillRow('alpha')?.checkVisibility(),'closing refresh must still show alpha before save commits');
 releaseSkillSave();await waitFor(()=>skillSaveFinished,'disable request did not commit');
 await waitFor(()=>!skillRow('alpha'),'completed settings save did not refresh the skills tab');
 assert(skillRow('legacy')?.checkVisibility(),'save refresh removed unrelated skills');doneSkills();
})().catch(error=>document.body.dataset.testResult='failed: '+error.message);
`
