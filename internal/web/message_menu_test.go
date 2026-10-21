package web

import (
	"os/exec"
	"strings"
	"testing"
)

func TestStarterIconsFollowTheirSuggestion(t *testing.T) {
	raw, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	a := strings.Index(s, "  const GENERIC_STARTERS =")
	b := strings.Index(s, "  function fillComposer(")
	script := `const assert=require('node:assert/strict');` + s[a:b] + `
 const suggestions=starterPrompts({persona:'帮你读文章，理清思路。'});
 assert.deepEqual(suggestions.map(s=>s.icon),['book-open','message-circle-question','list-checks']);
 const shifted=starterPrompts({routines:[{name:'晨报',enabled:true}]});
 assert.equal(shifted[0].icon,'alarm-clock');
 assert.equal(shifted[1].text,'你能帮我做些什么？');
 assert.equal(shifted[1].icon,'message-circle-question');
 const originals=new Map(GENERIC_STARTERS.map(s=>[s.text,s.icon]));
 for(const s of shifted.slice(1))assert.equal(s.icon,originals.get(s.text));
 `
	if out, err := exec.Command("node", "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestMessageMenuStaysInsideViewport(t *testing.T) {
	raw, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	a := strings.Index(s, "  function messageMenuPosition(")
	if a < 0 {
		t.Fatal("missing message menu positioning")
	}
	b := strings.Index(s[a:], "  function closeMessageMenu(")
	script := `const assert=require('node:assert/strict');` + s[a:a+b] + `
 assert.deepEqual(messageMenuPosition(100,200,160,76,1440,900),{left:100,top:200});
 assert.deepEqual(messageMenuPosition(1430,890,160,76,1440,900),{left:1270,top:814});
 assert.deepEqual(messageMenuPosition(370,895,160,76,375,900),{left:207,top:816});
 assert.deepEqual(messageMenuPosition(0,-70,160,76,375,900),{left:8,top:8});
 const bubble={left:32,right:343,top:300,bottom:342};
 assert.deepEqual(mobileMessageMenuPosition(bubble,160,76,375,900,false,64),{left:32,top:216});
 assert.deepEqual(mobileMessageMenuPosition(bubble,160,76,375,900,true,64),{left:183,top:216});
 // The menu stays beside its target even when that overlaps its neighbour.
 assert.equal(mobileMessageMenuPosition(bubble,160,76,375,900,false,64).top,216);
 assert.deepEqual(mobileMessageMenuPosition({...bubble,top:140,bottom:182},160,76,375,900,false,64),{left:32,top:190});
 assert.deepEqual(mobileMessageMenuPosition({...bubble,top:90,bottom:132},160,76,375,900,true,64),{left:183,top:140});
 `
	if out, err := exec.Command("node", "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestMessageMenuOmitsUnavailableReply(t *testing.T) {
	raw, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	a := strings.Index(s, "  function showMessageMenu(")
	b := strings.Index(s[a:], `  thread.addEventListener("contextmenu"`)
	script := `const assert=require('node:assert/strict');
 let shown, messageMenu, messageTarget, copyReceipt, messagePress;
 const animateMessageMenu=()=>{};
 const classes={add(){},remove(){},contains(){return false}};
 function el(tag,cls,text){return {tag,text,children:[],style:{},classList:classes,
 offsetWidth:160,offsetHeight:76,setAttribute(){},addEventListener(){},
 append(...nodes){this.children.push(...nodes)},querySelector(){return {focus(){}}}}}
 const icon=()=>({}),closePops=()=>{},window={getSelection:()=>null};
 const document={body:{append(node){shown=node}},querySelector:()=>null,documentElement:{classList:classes}};
 const innerWidth=1440,innerHeight=900;
 function messageMenuPosition(){return {left:100,top:100}}
 const bubble={getBoundingClientRect:()=>({left:100,top:100}),classList:classes};
 ` + s[a:a+b] + `
 showMessageMenu({bubble,message:{},text:'group reply'},100,100,false);
 assert.deepEqual(shown.children.map(b=>b.children[1].text),['复制']);
 showMessageMenu({bubble,message:{seq:7},text:'bot reply'},100,100,false);
 assert.deepEqual(shown.children.map(b=>b.children[1].text),['回复','复制']);
 `
	if out, err := exec.Command("node", "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestPopoverSpringHasOnePeakAndReducedMotionOnlyFades(t *testing.T) {
	raw, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	a := strings.Index(s, "  function popoverSpringFrames(")
	b := strings.Index(s, "  function closeMessageMenu(")
	script := `const assert=require('node:assert/strict');
 const values={'--ease-spring':'cubic-bezier(0.34,1.56,0.64,1)','--dur-pop':'220ms','--dur-motion-reduced':'100ms','--pop-scale-start':'.92','--pop-scale-overshoot':'.03','--press-scale':'.97'};
 const getComputedStyle=()=>({getPropertyValue:key=>values[key]});let reduced=false;const matchMedia=()=>({matches:reduced});
 ` + s[a:b] + `
 const frames=popoverSpringFrames(values['--ease-spring'],.92,.03),scales=frames.map(f=>Number(f.transform.slice(6,-1)));
 assert.equal(scales[0],.92);assert.equal(scales.at(-1),1);assert.equal(Math.max(...scales),1.03);
 const peak=scales.indexOf(1.03);for(let i=1;i<scales.length;i++){assert.ok(frames[i].offset>frames[i-1].offset);assert.ok(i<=peak?scales[i]>=scales[i-1]:scales[i]<=scales[i-1]);}
 assert.equal(frames[0].opacity,0);assert.equal(frames.at(-1).opacity,1);
 const calls=[],menu={animate:(f,o)=>calls.push({f,o})},bubble={animate:(f,o)=>calls.push({f,o})};
 animateMessageMenu(menu,bubble);assert.equal(calls.length,2);assert.equal(calls[0].o.duration,220);assert.equal(calls[1].o.easing,values['--ease-spring']);
 calls.length=0;reduced=true;animateMessageMenu(menu,bubble);assert.equal(calls.length,1);assert.equal(calls[0].o.duration,100);assert.deepEqual(calls[0].f,[{opacity:0},{opacity:1}]);
 `
	if out, err := exec.Command("node", "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
