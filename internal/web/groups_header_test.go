package web

import (
	"strings"
	"testing"
)

// beta.3 review: with no groups the sidebar shows no empty 「群组」 header;
// 新建 already offers 新建群组.
func TestGroupsHeaderOnlyWithGroups(t *testing.T) {
	b, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(b)
	if !strings.Contains(js, `if (state.groups.length) roster.append(sectionLabel("群组"));`) {
		t.Error("the 群组 header must depend on there being groups")
	}
	if strings.Count(js, `sectionLabel("群组")`) != 1 {
		t.Error("one 群组 header only")
	}
}

// Group identity remains a public route to its real member list, while Bot-only
// pages and model editing must not be offered for the selected group.
func TestGroupProfileMembersBrowser(t *testing.T) {
	runChatStreamBrowser(t, chatStreamFixture+`
const groupProfileFetch=window.fetch;
window.fetch=async(path,opts={})=>{
 if(path==='/api/groups')return json({groups:[{id:'group',name:'讨论组',members:bots.map(b=>b.name)}],folders:[]});
 if(path==='/api/groups/group')return json({id:'group',name:'讨论组',members:bots.map(b=>b.name),messages:[]});
 return groupProfileFetch(path,opts);
};
`, `
const $=s=>document.querySelector(s),assert=(v,m)=>{if(!v)throw Error(m)};
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const waitFor=async(fn,msg)=>{for(let i=0;i<600;i++){if(fn())return;await sleep(10)}throw Error(msg)};
const tab=name=>[...document.querySelectorAll('#workspace-tabs [role=tab]')].find(n=>n.textContent===name);
(async()=>{
 await waitFor(()=>!$('#shell').hidden&&$('#roster button[aria-label="讨论组"]')&&!$('.thread-loading-cover'),'boot');
 $('#roster button[aria-label="讨论组"]').click();
 await waitFor(()=>$('#title').textContent.includes('讨论组'),'group not selected');
 assert(tab('聊天')&&!tab('记忆')&&!tab('任务')&&!tab('上下文'),'group exposes Bot-only pages');
 $('#workspace-more').click();
 const entry=[...document.querySelectorAll('#workspace-more-menu button')].find(n=>n.textContent.includes('成员'));
 assert(entry?.checkVisibility(),'group members entry unreachable');entry.click();
 await waitFor(()=>$('#panel').checkVisibility()&&$('#panel-body').textContent.includes(bots[1].name),'members missing');
 assert(bots.every(b=>$('#panel-body').textContent.includes(b.name)),'member list lost a Bot');
 assert(!$('#panel-actions button')?.checkVisibility()&&!$('#panel-body .binding-trigger'),'group offers Bot profile/model edits');
 tab('聊天').click();await waitFor(()=>$('#thread').checkVisibility(),'group chat not restored');
 const botRow=()=>$('#sidebar [data-bot="'+bots[0].name+'"]');
 await waitFor(()=>botRow(),'Bot sidebar row missing');botRow().click();
 await waitFor(()=>$('#title').textContent.includes(bots[0].name)&&tab('记忆'),'Bot pages not restored');
 assert(testErrors.length===0,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>document.body.dataset.testResult='failed: '+e.stack);
`)
}
