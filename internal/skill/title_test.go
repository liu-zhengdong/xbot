package skill_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
	"xbot/internal/skill"
)

func TestWriteTitleRoundTrip(t *testing.T) {
	for _, title := range []string{"", "记一下", "记: 一下 # 私用", "\"记一下\"", "记一下\nname: injected"} {
		t.Run(title, func(t *testing.T) {
			root := t.TempDir()
			if err := skill.Write(root, "owner", "note-capture", "A long description", "body", title); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(filepath.Join(skill.OwnerDir(root, "owner"), "note-capture", "SKILL.md"))
			if err != nil {
				t.Fatal(err)
			}
			var meta map[string]string
			if err := yaml.Unmarshal([]byte(strings.Split(string(raw), "---")[1]), &meta); err != nil {
				t.Fatal(err)
			}
			want := strings.Join(strings.Fields(title), " ")
			if meta["name"] != "note-capture" || meta["title"] != want {
				t.Fatalf("metadata: %#v", meta)
			}
			info, body, err := skill.Open(skill.OwnerDir(root, "owner"), "note-capture")
			if err != nil || info.Name != "note-capture" || info.Title != want || body != "body" {
				t.Fatalf("open: %+v %q %v", info, body, err)
			}
			list, err := skill.List(skill.OwnerDir(root, "owner"))
			if err != nil || len(list) != 1 || list[0] != info {
				t.Fatalf("list: %+v %v", list, err)
			}
		})
	}
}

func TestImportTitleAndLegacyName(t *testing.T) {
	for _, meta := range []string{"name: note-capture\ntitle: '记一下'", "name: 旧短名"} {
		root := t.TempDir()
		if _, err := skill.ImportData(root, "owner", "note-capture", []byte("---\n"+meta+"\ndescription: long description\n---\nbody"), false); err != nil {
			t.Fatal(err)
		}
		info, _, err := skill.Open(skill.OwnerDir(root, "owner"), "note-capture")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(meta, "title:") {
			if info.Title != "记一下" || info.Name != "note-capture" {
				t.Fatal(info)
			}
		} else if info.Title != "" || info.Name != "旧短名" {
			t.Fatal(info)
		}
	}
}
