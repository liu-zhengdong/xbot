package remote

import (
	"testing"

	"xbot/internal/person"
)

func TestDefaultDeviceName(t *testing.T) {
	for _, tt := range []struct{ host, want string }{
		{"liuzhengdongdeMacBook-Pro.local", "MacBook Pro"},
		{"Alice-MacBook-Air.local", "MacBook Air"},
		{"政东的MacBook", "MacBook"},
		{"liuzhengdongdeMac-Mini.local", "Mac mini"},
		{"Alice_Mac_Studio.local", "Mac Studio"},
		{"Alice-iMac-Pro.local", "iMac Pro"},
		{"Alice-Mac-Pro.local", "Mac Pro"},
		{"Alice-iMac.local", "iMac"},
		{"DESKTOP-ABC123", "DESKTOP-ABC123"},
		{"build-server.example.com", "build-server"},
		{"macbook-pro-builder.local", "macbook-pro-builder"},
		{"", "这台电脑"},
	} {
		t.Run(tt.host, func(t *testing.T) {
			if got := defaultDeviceName(tt.host); got != tt.want {
				t.Fatalf("defaultDeviceName(%q) = %q, want %q", tt.host, got, tt.want)
			}
		})
	}
}

func TestSelfDeviceNameDefaultAndSaved(t *testing.T) {
	root := t.TempDir()
	p, err := person.Identify(root, person.Account{Link: "terminal", ExternalID: "uid:1"}, "owner")
	mustRemote(t, err)
	self, err := Self(root, p.ID)
	mustRemote(t, err)
	if want := cleanLabel(DefaultDeviceName()); self.Name != want {
		t.Fatalf("default self name = %q, want %q", self.Name, want)
	}
	const saved = "政东的 MacBook Pro.local"
	mustRemote(t, SetSelfName(root, saved))
	self, err = Self(root, p.ID)
	mustRemote(t, err)
	if self.Name != saved {
		t.Fatalf("saved self name changed to %q", self.Name)
	}
}
