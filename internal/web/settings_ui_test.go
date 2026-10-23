package web

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

func TestSettingsSystemThemeKeepsPreference(t *testing.T) {
	source, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	_, block, ok := strings.Cut(string(source), `  let themeMode = "system";`)
	if !ok {
		t.Fatal("missing theme controller")
	}
	block, _, ok = strings.Cut(block, "  let authenticated")
	if !ok {
		t.Fatal("missing theme controller end")
	}
	encoded, _ := json.Marshal(`let themeMode = "system";` + block + `;initTheme();return {applyTheme};`)
	script := `const assert=require('node:assert/strict');
 const nodes=new Map();const $=id=>{if(!nodes.has(id)) nodes.set(id,{value:'',attrs:{},setAttribute(k,v){this.attrs[k]=v}});return nodes.get(id)};
 const media={matches:false,addEventListener(event,cb){this.change=cb}};
 const store=new Map([['xbot-theme','system']]);const calls=[];
 const api=(path,opts)=>{calls.push([path,opts.body.theme]);return Promise.resolve()};
 const doc={documentElement:$('root')};
 const run=new Function('$','window','document','localStorage','inApp','api',` + string(encoded) + `);
 const ctrl=run($,{matchMedia:()=>media},doc,{getItem:k=>store.get(k),setItem:(k,v)=>store.set(k,v)},true,api);
 assert.equal(store.get('xbot-theme'),'system');assert.equal($('root').attrs['data-theme'],'light');
 media.matches=true;media.change();assert.equal($('root').attrs['data-theme'],'dark');assert.equal(store.get('xbot-theme'),'system');
 ctrl.applyTheme('light');media.change();assert.equal($('root').attrs['data-theme'],'light');assert.equal($('settings-theme').value,'light');
 ctrl.applyTheme('dark');assert.equal(store.get('xbot-theme'),'dark');assert.equal($('root').attrs['data-theme'],'dark');
 assert.ok(calls.every(([path,theme])=>path==='/api/app/theme'&&['light','dark'].includes(theme)));
 `
	if out, err := exec.Command("node", "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestConnectorSetupRevealsTheLocalChat(t *testing.T) {
	source, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	_, body, ok := strings.Cut(string(source), "        if (entry.secrets.length) {\n")
	if !ok {
		t.Fatal("missing connector setup path")
	}
	body, _, ok = strings.Cut(body, "        } else await change")
	if !ok {
		t.Fatal("missing connector setup end")
	}
	encoded, _ := json.Marshal("return (async()=>{" + body + "})()")
	script := `const assert=require('node:assert/strict');
 const setup=new Function('entry','name','closeSettings','window','selectBot','sendText',` + string(encoded) + `);
 (async()=>{for(const opened of [false,true]){
  let remote=opened,selected='',messages=[];const calls=[];
  await setup({title:'Telegram'},'ada',restore=>{assert.equal(restore,false);calls.push('settings')},
    {xbotRemoteChat:{close(){remote=false;calls.push('remote')}}},async name=>{selected=name;calls.push('select')},
    text=>{assert.equal(remote,false,'remote chat still hides the local reply/card');assert.equal(selected,'ada');messages.push(text);calls.push('send')});
  assert.equal(messages.length,1);assert.ok(messages[0].includes('Telegram'));
 }})().catch(e=>{console.error(e);process.exitCode=1});`
	if out, err := exec.Command("node", "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
