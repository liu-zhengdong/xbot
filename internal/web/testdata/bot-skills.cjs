// Real serve APIs and shipped assets. No HTTP route replacements or model messages.
const {chromium}=require('playwright');
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const cfg=JSON.parse(fs.readFileSync(0,'utf8'));
(async()=>{
 const browser=await chromium.launch({executablePath:require('../../browsertest/chrome.cjs')(),headless:true});
 try{
  const errors=[],messagePosts=[],assets=new Map();
  const newPage=async theme=>{
   const context=await browser.newContext({viewport:{width:1440,height:900},deviceScaleFactor:2,colorScheme:theme});
   await context.addCookies([{name:cfg.cookie,value:cfg.token,url:cfg.url}]);
   await context.addInitScript(theme=>{localStorage.setItem('xbot-theme',theme);sessionStorage.setItem('xbot-selected-bot','SkillBot')},theme);
   const page=await context.newPage();page.setDefaultTimeout(15000);
   page.on('pageerror',error=>errors.push(error.message));
   page.on('request',request=>{if(request.method()==='POST'&&new URL(request.url()).pathname.endsWith('/messages'))messagePosts.push('POST /messages')});
   page.on('response',response=>{
    const asset=new URL(response.url()).pathname;
    if(asset==='/bot-skills.js'||asset==='/bot-skills.css')assets.set(asset,{status:response.status(),type:response.headers()['content-type']||''});
   });
   await page.goto(cfg.url);
   await page.waitForFunction(()=>document.querySelector('#title')?.textContent.includes('SkillBot')&&!document.querySelector('.thread-loading-cover'));
   await page.locator('#input').waitFor({state:'visible'});
   return {context,page};
  };
  const {context,page:p}=await newPage('light');
  const root=p.locator('#workspace-view-skills');
  const field=root.locator('.bot-skills-search input');
  const row=slug=>root.locator('.bot-skill-row[data-slug="'+slug+'"]');
  const detail=root.locator('.bot-skill-detail');
  const slugs=async expected=>{
   await p.waitForFunction(expected=>[...document.querySelectorAll('#workspace-view-skills .bot-skill-row[data-slug]')].filter(node=>node.checkVisibility()).map(node=>node.dataset.slug).sort().join(',')===expected,expected);
  };
  const shot=async(page,theme,name)=>{
   await page.evaluate(()=>document.fonts.ready);
   await page.evaluate(()=>new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve))));
   assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1),false,'horizontal overflow: '+theme+'/'+name);
   assert.equal(await page.evaluate(()=>devicePixelRatio),2,'evidence must use DPR 2');
    assert(await page.evaluate(()=>{
     const strip=document.querySelector('#workspace-tabs').getBoundingClientRect(),active=document.querySelector('#workspace-tab-skills').getBoundingClientRect();
     return active.left>=strip.left-1&&active.right<=strip.right+1;
    }),'selected skills tab is outside the visible navigation: '+theme+'/'+name);
   if(cfg.evidence){
    const directory=path.join(cfg.evidence,theme);fs.mkdirSync(directory,{recursive:true});
    await page.screenshot({path:path.join(directory,name+'.png'),animations:'disabled'});
   }
  };
  const openSkills=()=>p.locator('#workspace-tab-skills').click();
  const select=async name=>{
   await p.locator('[data-bot="'+name+'"]').first().click();
   await p.waitForFunction(name=>document.querySelector('#title')?.textContent.includes(name)&&!document.querySelector('.thread-loading-cover'),name);
  };
  await p.locator('#input').fill('真实技能页面切换后保留的草稿');
  await p.evaluate(()=>{window.skillJourneyInput=document.querySelector('#input');window.skillJourneyThread=document.querySelector('#thread')});
  await openSkills();await slugs('journey-guide,plain-slug');
  assert((await row('journey-guide').innerText()).startsWith('技能旅程手册'),'title priority missing');
  assert((await row('plain-slug').innerText()).startsWith('plain-slug'),'slug display fallback missing');
  assert.equal(await row('disabled-skill').count(),0,'disabled skill appeared');
  await shot(p,'light','desktop-list');
  for(const query of ['旅程手册','JOURNEY-GUIDE','sEaRcHnEeDlE','中文整理']){
   await field.fill('');await slugs('journey-guide,plain-slug');
   await field.fill(query);await slugs('journey-guide');
  }
  await field.fill('PLAIN-SLUG');await slugs('plain-slug');
  await field.fill('无匹配验收');await slugs('');
  await root.locator('.bot-skills-clear').click();await slugs('journey-guide,plain-slug');
  assert.equal(await field.inputValue(),'','clear lost its effect');
  await row('journey-guide').click();
  await detail.getByText('真实技能粗体',{exact:true}).waitFor({state:'visible'});
  assert((await detail.innerText()).includes('技能正文验收'),'real Markdown body missing');
  assert(!(await detail.innerText()).includes('# 技能正文验收'),'Markdown heading stayed raw');
  assert.equal(await detail.locator('li').count(),2,'real Markdown list missing');
  assert.equal(await detail.locator('textarea,input,[contenteditable="true"],[role="switch"]').count(),0,'read-only detail exposes editing');
  assert.equal(await detail.locator('script,iframe,[onerror],[onclick],a[href^="javascript:"]').count(),0,'unsafe markup reached the detail');
  assert.equal(await p.evaluate(()=>!!window.skillJourneyUnsafe),false,'unsafe markup executed');
  const raw=detail.locator('.bot-skill-raw details,details.bot-skill-raw');
  assert.equal(await raw.getAttribute('open'),null,'raw disclosure starts open');
  await raw.locator('summary').click();
  assert.equal(await raw.locator('pre').textContent(),cfg.body,'real skill source changed');
  assert.equal(await raw.locator('pre img,pre a,pre strong').count(),0,'source was parsed as markup');
  await raw.locator('summary').click();await shot(p,'light','desktop-detail');
  await root.locator('.bot-skill-close').click();
  // Change the real shared library's per-Bot switch through its settings UI.
  await root.locator('.bot-skills-manage').click();
  await p.locator('#setpage').waitFor({state:'visible'});
  assert.equal(await p.locator('#setpage [data-set="skills"]').getAttribute('aria-current'),'page','manage opened the wrong settings');
  await p.locator('#settings-skills .skrow').filter({hasText:'停用验收技能'}).click();
  const enable=p.locator('#settings-skills').getByRole('switch',{name:'在 SkillBot 里启用「停用验收技能」',exact:true});
  assert.equal(await enable.getAttribute('aria-checked'),'false','seeded Bot disable was not persisted');
  assert.equal(await p.locator('#settings-skills').getByRole('switch',{name:'在 OtherBot 里启用「停用验收技能」',exact:true}).getAttribute('aria-checked'),'true','other Bot switch was coupled');
  const [saved]=await Promise.all([
   p.waitForResponse(response=>new URL(response.url()).pathname==='/api/bots/SkillBot/skills/disabled-skill/enable'&&response.request().method()==='POST'),
   enable.click()
  ]);
  assert.equal(saved.status(),200,'real settings save failed');
  await p.keyboard.press('Escape');await p.locator('#setpage').waitFor({state:'hidden'});
  await slugs('disabled-skill,journey-guide,plain-slug');
  assert.equal(await field.inputValue(),'','settings return changed query');
  await p.getByRole('tab',{name:'聊天',exact:true}).click();
  assert.equal(await p.locator('#input').innerText(),'真实技能页面切换后保留的草稿','skills browsing lost chat draft');
  assert(await p.evaluate(()=>window.skillJourneyInput===document.querySelector('#input')&&window.skillJourneyThread===document.querySelector('#thread')),'skills tab rebuilt chat');
  await openSkills();await slugs('disabled-skill,journey-guide,plain-slug');
  await field.fill('plain');await slugs('plain-slug');
  await select('OtherBot');await openSkills();await slugs('disabled-skill,journey-guide');
  assert.equal(await field.inputValue(),'','query crossed Bots');
  assert.equal(await detail.isVisible(),false,'detail crossed Bots');
  assert.equal(await row('plain-slug').count(),0,'other Bot disable was lost');
  await select('SkillBot');
  assert.equal(await p.locator('#input').innerText(),'真实技能页面切换后保留的草稿','Bot switch lost the chat draft');
  await openSkills();await slugs('disabled-skill,journey-guide,plain-slug');
  await p.setViewportSize({width:390,height:844});
  await p.waitForFunction(()=>document.querySelector('#sidebar').getBoundingClientRect().right<=1);
  await shot(p,'light','mobile-list');
  await field.fill('旅程');await slugs('journey-guide');await row('journey-guide').click();
  await detail.getByText('真实技能粗体',{exact:true}).waitFor({state:'visible'});
  assert.equal(await field.isVisible(),false,'mobile detail retained list toolbar');
  assert.equal(await row('journey-guide').isVisible(),false,'mobile detail retained list');
  await root.locator('.bot-skill-back').waitFor({state:'visible'});await shot(p,'light','mobile-detail');
  await root.locator('.bot-skill-back').click();await slugs('journey-guide');
  assert.equal(await field.inputValue(),'旅程','mobile back discarded query');
  assert.equal(await detail.isVisible(),false,'mobile back retained detail');
  await context.close();
  const dark=await newPage('dark');
  await dark.page.locator('#workspace-tab-skills').click();
  await dark.page.waitForFunction(()=>document.querySelectorAll('#workspace-view-skills .bot-skill-row').length===3);
  await shot(dark.page,'dark','desktop-list');await dark.context.close();
  assert.equal(assets.get('/bot-skills.js')?.status,200,'real serve did not load bot-skills.js');
  assert.match(assets.get('/bot-skills.js').type,/javascript/i,'module MIME type is wrong');
  assert.equal(assets.get('/bot-skills.css')?.status,200,'real serve did not load bot-skills.css');
  assert.match(assets.get('/bot-skills.css').type,/text\/css/i,'stylesheet MIME type is wrong');
  assert.deepEqual(messagePosts,[],'read-only journey sent a model message');
  assert.deepEqual(errors,[],'browser script errors');
  console.log('passed bot skills real serve journey; desktop/mobile/light/dark; DPR 2; five evidence views');
 }finally{await browser.close()}
})().catch(error=>{
 const message=String(error.stack||error);
 console.error(cfg.token?message.split(cfg.token).join('[redacted]'):message);process.exit(1);
});
