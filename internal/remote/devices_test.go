package remote

import (
	"crypto/ed25519"
	"encoding/base64"
	"testing"

	"xbot/internal/person"
)

// The drawer removes by ID inside the locked update: only that device goes.
func TestRemoveDeviceID(t *testing.T) {
	root := t.TempDir()
	_, err := person.Identify(root, person.Account{Link: "terminal", ExternalID: "uid:1"}, "owner")
	mustRemote(t, err)
	add := func(name string) Device {
		pub, _, err := ed25519.GenerateKey(nil)
		mustRemote(t, err)
		d, err := AddDevice(root, name, base64.StdEncoding.EncodeToString(pub), base64.StdEncoding.EncodeToString(make([]byte, 32)), "")
		mustRemote(t, err)
		return d
	}
	a, b := add("mbp"), add("imac")
	mustRemote(t, RemoveDeviceID(root, a.ID))
	if RemoveDeviceID(root, a.ID) == nil {
		t.Fatal("removed twice")
	}
	cfg, err := Load(root)
	mustRemote(t, err)
	if len(cfg.Devices) != 1 || cfg.Devices[0].ID != b.ID {
		t.Fatalf("devices after remove: %+v", cfg.Devices)
	}
}
