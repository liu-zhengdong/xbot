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
 `
	if out, err := exec.Command("node", "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
