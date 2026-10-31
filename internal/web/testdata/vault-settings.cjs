const {chromium}=require('playwright');
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const cfg=JSON.parse(fs.readFileSync(process.argv[2],'utf8'));
(async()=>{
 const browser=await chromium.launch({executablePath:cfg.chrome,headless:true});
 try {
  const mountTimes=[],readTimes=[];
  for(const theme of ['light','dark']) {
   const context=await browser.newContext({viewport:{width:1440,height:900},deviceScaleFactor:2,colorScheme:theme});
   await context.addCookies(cfg.cookies.map(c=>({name:c.Name,value:c.Value,url:cfg.url})));
   const p=await context.newPage(),errors=[];p.on('pageerror',e=>errors.push(e.message));
   await p.addInitScript(theme=>localStorage.setItem('xbot-theme',theme),theme);
   await p.goto(cfg.url+'/#login-code');await p.locator('#shell').waitFor({state:'visible'});
   await p.locator('[data-bot="ada"]').first().click();
   const openSettings=async()=>{await p.locator('#account').click();await p.locator('#account-settings').click();await p.locator('[data-set=vault]').click()};
   const start=Date.now();await openSettings();
   const host=p.locator('#settings-vault');
   await host.getByText('当前 · ada',{exact:true}).waitFor().catch(async e=>{throw Error(e.message+'; vault='+await host.textContent()+'; settings='+await p.locator('#settings-error').textContent()+'; pageerrors='+errors.join(';'))});
   if(theme==='light')await host.getByText('还没有密钥',{exact:true}).waitFor();else await host.getByText('LOCAL_SUDO',{exact:true}).waitFor();
   mountTimes.push(Date.now()-start);
   const shot=async(name)=>{const dir=path.join(cfg.shots,'after',theme);fs.mkdirSync(dir,{recursive:true});await p.screenshot({path:path.join(dir,name+'.png')})};
   const d=()=>host.locator('[role=dialog]');
   const remove=async(name)=>{
    await host.locator('.vault-card').filter({hasText:name}).getByRole('button',{name:'管理',exact:true}).click();await d().getByRole('button',{name:'删除',exact:true}).click();await d().getByRole('button',{name:'删除',exact:true}).click();
    await host.locator('.vault-card').filter({hasText:name}).waitFor({state:'detached'});
   };
   if(theme==='dark')await remove('LOCAL_SUDO');
   await shot('empty');await host.getByRole('button',{name:'本机 sudo',exact:true}).click();
   assert.equal(await d().getByLabel('名称',{exact:true}).inputValue(),'LOCAL_SUDO');assert.equal(await d().getByLabel('域名',{exact:true}).inputValue(),'');
   await shot('add-sudo');await d().getByRole('button',{name:'添加',exact:true}).click();await d().getByRole('alert').waitFor();
   await d().getByLabel('值',{exact:true}).fill(cfg.password);await d().getByRole('button',{name:'添加',exact:true}).click();
   await host.getByText('LOCAL_SUDO',{exact:true}).waitFor();assert.equal(await host.locator('input[type=password]').count(),0);
   for(const [name,domains] of [['CONTEXT7_API_KEY','context7.com'],['telegram.token','api.telegram.org'],['GH_TOKEN','api.github.com, github.com']]){
    await host.getByRole('button',{name:'添加',exact:true}).click();await host.getByRole('menuitem').filter({hasText:'密钥'}).click();await d().getByLabel('名称',{exact:true}).fill(name);await d().getByLabel('值',{exact:true}).fill('scripted-outbound-fixture');await d().getByLabel('域名',{exact:true}).fill(domains);
    if(name==='GH_TOKEN')await shot('add');await d().getByRole('button',{name:'添加',exact:true}).click();await host.getByText(name,{exact:true}).waitFor();
   }
   await host.getByRole('button',{name:'全部 4',exact:true}).waitFor();await shot('list');
   await host.getByRole('button',{name:'本机 1',exact:true}).click();assert.equal(await host.locator('.vault-card').count(),1);await shot('local');
   await host.getByRole('button',{name:'出站 3',exact:true}).click();assert.equal(await host.locator('.vault-card').count(),3);await shot('outbound');
   const search=host.getByRole('searchbox',{name:'搜索保险库'});await search.fill('not-found');await host.getByText('没有匹配的密钥',{exact:true}).waitFor();await shot('search-none');
   await search.fill('github');assert.equal(await host.locator('.vault-card').count(),1);await search.fill('');await host.getByRole('button',{name:'全部 4',exact:true}).click();
   await host.locator('.vault-card').filter({hasText:'GH_TOKEN'}).getByRole('button',{name:'管理',exact:true}).click();await shot('manage');
   assert.equal(await d().getByLabel('新值',{exact:true}).inputValue(),'');await d().getByLabel('域名',{exact:true}).fill('edited.example');await d().getByRole('button',{name:'保存',exact:true}).click();await host.getByText('edited.example',{exact:true}).waitFor();
   await host.locator('.vault-card').filter({hasText:'GH_TOKEN'}).getByRole('button',{name:'管理',exact:true}).click();await d().getByRole('button',{name:'重新设置值',exact:true}).click();await shot('manage-reset');await d().getByLabel('新值',{exact:true}).fill('scripted-reset-fixture');await d().getByRole('button',{name:'保存',exact:true}).click();await d().waitFor({state:'detached'});
   await host.locator('.vault-card').filter({hasText:'GH_TOKEN'}).getByRole('button',{name:'管理',exact:true}).click();await d().getByRole('button',{name:'删除',exact:true}).click();await shot('delete');await d().getByRole('button',{name:'取消',exact:true}).click();await d().getByRole('button',{name:'取消',exact:true}).click();
   for(const name of ['GH_TOKEN','CONTEXT7_API_KEY','telegram.token'])await remove(name);
   // Switching the selected sidebar Bot reads a different real backend.
   await p.locator('#settings-close').click();await p.locator('[data-bot="bob"]').first().click();await openSettings();await host.getByText('当前 · bob',{exact:true}).waitFor();await host.getByText('还没有密钥',{exact:true}).waitFor();
   await p.locator('#settings-close').click();await p.locator('[data-bot="ada"]').first().click();await openSettings();await host.getByText('LOCAL_SUDO',{exact:true}).waitFor();
   for(let i=0;i<15;i++){const before=Date.now(),res=await context.request.get(cfg.url+'/api/bots/ada/secrets');const text=await res.text();readTimes.push(Date.now()-before);assert.ok(!text.includes('value')&&!text.includes(cfg.password));}
   assert.deepEqual(errors,[]);await context.close();
  }
  readTimes.sort((a,b)=>a-b);console.log('passed: real HTTP vault CRUD/manage/filter/search/Bot switch, light/dark; settings-open ms='+mountTimes.join(',')+'; secrets GET p95 ms='+readTimes[Math.ceil(readTimes.length*.95)-1]);
 }finally{await browser.close()}
})().catch(e=>{console.error(e);process.exit(1)});
