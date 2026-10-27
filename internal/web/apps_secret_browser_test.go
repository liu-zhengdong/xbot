package web

import "testing"

func TestAppsSecretLayerBrowser(t *testing.T) {
	runPageInChrome(t, `
(async () => {
 try {
  const host = document.createElement('div'); document.body.append(host);
  let installed = false, attempts = 0;
  const api = async (path, opt) => {
   if (!opt) return {connected: installed ? 1 : 0, apps: [{id:'context7',title:'Context7',summary:'查文档',category:'dev',state:installed?'connected':'available',card:true,fields:['API 密钥']}]};
   attempts++; if (attempts===1) throw Error('failed'); installed=true; return {};
  };
  await XBotApps.mount({host, api, isCurrent:()=>true});
  const card = host.querySelector('.ap-card'); card.querySelector('button').click();
  const dialog=host.querySelector('[role=dialog]'), input=dialog.querySelector('input');
  const submit=[...dialog.querySelectorAll('button')].find(x=>x.textContent==='添加');
  if (!dialog.classList.contains('nestlayer') || !dialog.querySelector('.nestcard') || input.type!=='password' || !dialog.textContent.includes('API 密钥') || dialog.textContent.includes('CONTEXT7')) throw Error('layer');
  submit.click(); if (attempts) throw Error('empty posted');
  input.value='fake-key'; submit.click(); await new Promise(r=>setTimeout(r,0));
  if (!host.contains(dialog) || !dialog.textContent.includes('密钥无效或连不上')) throw Error('failure');
  submit.click(); await new Promise(r=>setTimeout(r,0));
  if (host.querySelector('[role=dialog]') || !installed || !host.querySelector('.ap-card button').textContent.includes('移除')) throw Error('success');
  document.body.setAttribute('data-test-result','passed');
 } catch(e) { document.body.setAttribute('data-test-result','failed: '+e.message); }
})();`)
}
