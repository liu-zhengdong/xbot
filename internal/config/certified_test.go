package config

import "testing"

func TestVisibleModelsHideUncertified(t *testing.T) {
	root := t.TempDir()
	if err := Add(root, Provider{Name: "fake", BaseURL: "http://127.0.0.1:9/v1", APIKeyEnv: "XBOT_FAKE_UNUSED"}, ModelSpec{Name: "scripted", Context: 1024}); err != nil {
		t.Fatal(err)
	}
	if err := Add(root, Provider{Name: "other", BaseURL: "http://127.0.0.1:9/v1", APIKeyEnv: "XBOT_OTHER_UNUSED"}, ModelSpec{Name: "hidden", Context: 2048}); err != nil {
		t.Fatal(err)
	}
	got, err := VisibleModels(root, false)
	if err != nil || len(got) != 0 {
		t.Fatalf("before certify %+v %v", got, err)
	}
	if err := Certify(root, "fake/scripted"); err != nil {
		t.Fatal(err)
	}
	if err := Certify(root, "fake/scripted"); err != nil {
		t.Fatal(err)
	}
	got, err = VisibleModels(root, false)
	if err != nil || len(got) != 1 || got[0].ID != "fake/scripted" || !got[0].Certified {
		t.Fatalf("certified %+v %v", got, err)
	}
	all, err := VisibleModels(root, true)
	if err != nil || len(all) != 2 {
		t.Fatalf("all %+v %v", all, err)
	}
	if err := AllowBind(root, "other", "hidden", false); err == nil {
		t.Fatal("uncertified bind allowed")
	}
	if err := AllowBind(root, "other", "hidden", true); err != nil {
		t.Fatal(err)
	}
}
