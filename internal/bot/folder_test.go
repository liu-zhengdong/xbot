package bot

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestFoldersRenameAndDeleteKeepBots(t *testing.T) {
	root := t.TempDir()
	ada := mustFolderBot(t, root, "ada", "u1", true)
	aaa := mustFolderBot(t, root, "aaa", "u1", false)
	bob := mustFolderBot(t, root, "bob", "u1", false)
	cara := mustFolderBot(t, root, "cara", "u2", false)
	if err := CreateFolder(root, "u1", " 工作 "); err != nil {
		t.Fatal(err)
	}
	if err := CreateFolder(root, "u1", "生活"); err != nil {
		t.Fatal(err)
	}
	if err := CreateFolder(root, "u1", "工作"); !errors.Is(err, ErrFolderExists) {
		t.Fatalf("dup %v", err)
	}
	if err := CreateFolder(root, "u1", UngroupedName); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("reserved %v", err)
	}
	if err := SetBotFolder(root, "u1", "ada", "工作"); err != nil {
		t.Fatal(err)
	}
	if err := SetBotFolder(root, "u1", "aaa", "工作"); err != nil {
		t.Fatal(err)
	}
	if err := SetBotFolder(root, "u1", "bob", "工作"); err != nil {
		t.Fatal(err)
	}
	if err := SetBotFolder(root, "u1", "cara", "工作"); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("other owner %v", err)
	}
	if err := SetBotFolder(root, "u1", "ada", "没有"); !errors.Is(err, ErrFolderMissing) {
		t.Fatalf("missing folder %v", err)
	}

	view, err := Folders(root, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Order) != 2 || view.Order[0] != "工作" || view.Order[1] != "生活" {
		t.Fatalf("order %+v", view.Order)
	}
	if len(view.Folders) != 3 || view.Folders[0].Name != "工作" || view.Folders[0].Count != 3 || view.Folders[0].Bots[0] != "ada" || view.Folders[0].Bots[1] != "aaa" {
		t.Fatalf("work %+v", view.Folders)
	}
	if view.Folders[1].Count != 0 || len(view.Folders[1].Bots) != 0 {
		t.Fatalf("empty %+v", view.Folders[1])
	}
	if !view.Folders[2].Ungrouped || view.Folders[2].Name != UngroupedName || view.Folders[2].Count != 0 {
		t.Fatalf("loose %+v", view.Folders[2])
	}
	other, err := Folders(root, "u2")
	if err != nil || len(other.Folders) != 1 || other.Folders[0].Bots[0] != "cara" {
		t.Fatalf("u2 %+v %v", other, err)
	}

	if err := RenameFolder(root, "u1", "工作", "工作台"); err != nil {
		t.Fatal(err)
	}
	ada, err = Load(root, "ada")
	if err != nil || ada.Config().Group != "工作台" {
		t.Fatalf("renamed group %q %v", ada.Config().Group, err)
	}
	if err := DeleteFolder(root, "u1", "工作台"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(Dir(root, "ada")); err != nil {
		t.Fatal(err)
	}
	ada, err = Load(root, "ada")
	if err != nil || ada.Config().Group != "" {
		t.Fatalf("cleared %q %v", ada.Config().Group, err)
	}
	bob, err = Load(root, "bob")
	if err != nil || bob.Config().Group != "" {
		t.Fatalf("bob %q %v", bob.Config().Group, err)
	}
	view, err = Folders(root, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Order) != 1 || view.Order[0] != "生活" || view.Folders[len(view.Folders)-1].Count != 3 {
		t.Fatalf("after delete %+v", view)
	}
	if _, err := Load(root, aaa.Name()); err != nil {
		t.Fatal(err)
	}
	_ = cara
}

func mustFolderBot(t *testing.T, root, name, owner string, pinned bool) *Bot {
	t.Helper()
	b, err := Create(root, name, "persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{owner}); err != nil {
		t.Fatal(err)
	}
	if pinned {
		if err := b.SetPinned(true); err != nil {
			t.Fatal(err)
		}
	}
	return b
}
