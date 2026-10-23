package web

import (
	"os/exec"
	"strings"
	"testing"
)

func TestOAuthDesktopUsesExternalURLWithoutPopupHandle(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	const script = `
const fs=require('fs'),vm=require('vm'),assert=require('assert');
class Element {
 constructor(tag){this.tag=tag;this.children=[];this.isConnected=true;this.textContent='';}
 append(...nodes){this.children.push(...nodes);}
 replaceChildren(...nodes){this.children=nodes;}
 setAttribute(){}
 closest(){return null;}
}
const host=new Element('main'),calls=[],opens=[];
const window={open(url){opens.push(url);return null;}};
const document={createElement:tag=>new Element(tag),documentElement:{classList:{contains:name=>name==='in-app'}}};
vm.runInNewContext(fs.readFileSync('static/providers.js','utf8'),{window,document,URL,Date,setTimeout:fn=>{fn();}});
const api=async(path)=>{calls.push(path);if(path.endsWith('/start'))return {url:'https://auth.example/authorize'};if(path.endsWith('/status'))return {state:'succeeded'};return {providers:[]};};
(async()=>{await window.XBotProviders.settings({api,host});const button=host.children.find(n=>n.tag==='button');await button.onclick();assert(calls.some(p=>p.endsWith('/start')),'desktop login did not start');assert.deepStrictEqual(opens,['https://auth.example/authorize']);assert(calls.some(p=>p.endsWith('/status')),'desktop login did not poll without a handle');})().catch(err=>{console.error(err.message);process.exitCode=1;});
`
	cmd := exec.Command(node, "-e", script)
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("desktop OAuth behavior: %v: %s", err, out.String())
	}
}
