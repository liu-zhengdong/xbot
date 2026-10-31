package web

import "testing"

func TestVaultSettingsBrowser(t *testing.T) {
	runPageInChrome(t, `
(async()=>{try{
 const wait=()=>new Promise(r=>setTimeout(r,0));
 const host=document.createElement('div');document.body.append(host);
 const rows={ada:[],bob:[{name:'BOB_KEY',domains:['bob.example']}]};let attempts=0,fail=false,last;
 const api=async(path,opt)=>{
  const bot=path.split('/')[3];
  if(!opt)return {secrets:rows[bot]};
  attempts++;last=opt.body;if(fail)throw Error('fixture failure');
  if(opt.method==='POST'){
   const existing=rows[bot].find(r=>r.name===opt.body.name);
   if(Object.hasOwn(opt.body,'value')&&opt.body.value!=='fixture-value')throw Error('value not in request body');
   if(existing)existing.domains=opt.body.domains;else rows[bot].push({name:opt.body.name,domains:opt.body.domains});
  }else rows[bot]=rows[bot].filter(r=>r.name!==decodeURIComponent(path.split('/')[5]));
  return {ok:true};
 };
 const mount=bot=>XBotVault.mount({api,host,bot,displayName:bot,isCurrent:()=>true});
 const click=(root,text)=>{const b=[...root.querySelectorAll('button')].find(b=>b.textContent===text);if(!b)throw Error('button '+text);b.click();};
 const dialog=()=>host.querySelector('[role=dialog]');
 await mount('ada');click(host,'本机 sudo');
 let d=dialog(),inputs=d.querySelectorAll('input');
 if(inputs[0].value!=='LOCAL_SUDO'||inputs[1].type!=='password'||inputs[2].value!=='')throw Error('sudo preset');
 click(d,'添加');if(attempts||!host.contains(d))throw Error('empty submitted');
 inputs[1].value='fixture-value';fail=true;click(d,'添加');await wait();
 if(!host.contains(d)||inputs[1].value!=='fixture-value')throw Error('failure lost draft');
 fail=false;click(d,'添加');await wait();
 if(dialog()||!host.textContent.includes('LOCAL_SUDO')||host.textContent.includes('fixture-value'))throw Error('add failed/leaked');
 click(host,'出站 0');if(!host.textContent.includes('没有出站密钥'))throw Error('empty filter');
 click(host,'本机 1');if(host.querySelectorAll('[role=listitem]').length!==1)throw Error('local category');
 const search=host.querySelector('input[type=search]');search.value='no match';search.dispatchEvent(new Event('input'));
 if(!host.textContent.includes('没有匹配的密钥'))throw Error('search empty');
 search.value='local';search.dispatchEvent(new Event('input'));click(host,'管理');d=dialog();
 if(d.querySelector('input[type=password]').value!==''||d.querySelector('input[type=text]').value!=='')throw Error('manage leaked value');
 d.querySelector('input[type=text]').value='example.com';click(d,'保存');await wait();
 if(Object.hasOwn(last,'value')||last.domains[0]!=='example.com')throw Error('domain-only update');
 click(host,'全部 1');search.value=''; // list was rebuilt; query remains local and matches name
 click(host,'管理');d=dialog();click(d,'重新设置值');click(d,'保存');
 if(!host.contains(d)||!d.textContent.includes('请填写新值'))throw Error('blank reset accepted');
 d.querySelector('input[type=password]').value='fixture-value';click(d,'保存');await wait();if(last.value!=='fixture-value')throw Error('reset missing value');
 click(host,'管理');click(dialog(),'删除');d=dialog();click(d,'取消');if(rows.ada.length!==1||!dialog())throw Error('cancel deleted');
 click(dialog(),'删除');click(dialog(),'删除');await wait();if(rows.ada.length||host.textContent.includes('LOCAL_SUDO'))throw Error('delete failed');
 click(host,'添加');dialog().querySelector('input').value='draft';await mount('bob');
 if(dialog()||!host.textContent.includes('BOB_KEY')||host.textContent.includes('draft'))throw Error('scope switch');
 click(host,'添加');click(host,'本机 sudo给 shell 提权 · 模型看不见');if(dialog().querySelector('input').value!=='LOCAL_SUDO')throw Error('add menu');
 let release;const old=XBotVault.mount({host,bot:'ada',displayName:'ada',isCurrent:()=>true,api:()=>new Promise(r=>release=r)});
 await mount('bob');release({secrets:[{name:'STALE',domains:[]}]});await old;
 if(host.textContent.includes('STALE')||!host.textContent.includes('BOB_KEY'))throw Error('stale response');
 XBotVault.unmount();if(host.childNodes.length)throw Error('unmount');
 await XBotSettings.open('vault');if(document.querySelector('[data-spane=vault]').hidden)throw Error('settings seam');
 document.body.setAttribute('data-test-result','passed');
}catch(e){document.body.setAttribute('data-test-result','failed: '+e.message)}})();`)
}
