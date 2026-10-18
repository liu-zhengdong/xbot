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
