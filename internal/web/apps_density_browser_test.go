package web

import "testing"

// Exercise layout with the shipped styles and store renderer, including the
// per-Bot note that must remain readable next to a clipped description.
func TestAppsStoreDensityBrowser(t *testing.T) {
	runPageInChrome(t, `
(async () => {
 try {
  const host = document.createElement('div');
  host.style.cssText = 'position:fixed;z-index:99999;top:0;left:0;width:720px;background:white';
  document.body.append(host);
  const apps = [
   {id:'context7',title:'Context7',summary:'这是很长的说明。'.repeat(40),scope:'密钥按 Bot 分别保存，只有填过密钥的 Bot 能用。',category:'dev',state:'available'},
   {id:'browser',title:'浏览器',summary:'浏览网页',category:'dev',state:'available'},
   {id:'gmail',title:'Gmail',summary:'邮件',category:'dev',state:'soon'}
  ];
  await XBotApps.mount({host,api:async()=>({apps,connected:0}),isCurrent:()=>true});
  const chip = [...host.querySelectorAll('.ap-chip')].find(x=>x.textContent==='开发'); chip.click();
  const cards = [...host.querySelectorAll('.ap-card')], hint = cards[0].querySelector('.hint'), icon = cards[0].querySelector('.ico');
  const rect = n=>n.getBoundingClientRect();
  const check = (ok,msg)=>{if(!ok) throw Error(msg)};
  check(cards.length===3,'cards');
  check(rect(icon).width===48 && rect(icon).height===48 && rect(icon.querySelector('svg')).width===30,'icon size');
  check(rect(cards[0]).height===rect(cards[1]).height,'same row height');
  check(rect(cards[2]).width===rect(cards[1]).width,'last card width');
  check(hint.scrollHeight>hint.clientHeight && getComputedStyle(hint).webkitLineClamp==='2','description clipped');
  const scope=cards[0].querySelector('.ap-scope');
  check(scope && rect(scope).bottom<=rect(cards[0]).bottom && scope.scrollHeight===scope.clientHeight,'scope visible');
  document.body.setAttribute('data-test-result','passed');
 } catch(e) { document.body.setAttribute('data-test-result','failed: '+e.message); }
})();`)
}
