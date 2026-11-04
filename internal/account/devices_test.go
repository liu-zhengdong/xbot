package account

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// UC-DEVICE-04: saved names and revocation survive both heartbeat APIs.
func TestDeviceBindingLifecycle(t *testing.T) {
	for _, mode := range []string{"local", "remote"} {
		t.Run(mode, func(t *testing.T) {
			e := newEnv(t)
			var c Client = e.cli
			if mode == "local" {
				c = Local{S: e.svc}
			}
			ctx := context.Background()
			signed, err := c.Register(ctx, "owner@example.com", "password 1")
			if err != nil {
				t.Fatal(err)
			}
			report := DeviceReport{ID: "install-1", Name: "Computer", OS: "darwin", Bots: []string{"first"}, Capabilities: []string{"shell", "files"}, RunningCount: 2}
			binding, err := c.BindDevice(ctx, signed.Session, report)
			if err != nil || binding.Token == "" || binding.ID == "" || binding.AccountKey == "" || binding.DeviceID != report.ID {
				t.Fatalf("bind identity incomplete: %v", err)
			}
			me, err := c.Me(ctx, signed.Session)
			if err != nil || binding.AccountKey != me.Key {
				t.Fatalf("binding account differs from session: %v", err)
			}
			identity, err := c.AuthenticateBinding(ctx, binding.Token)
			if err != nil || identity != (BindingIdentity{ID: binding.ID, DeviceID: report.ID, AccountKey: me.Key}) {
				t.Fatalf("authenticate identity mismatch: %v", err)
			}
			if err := c.RenameDevice(ctx, signed.Session, report.ID, "  Office Mac  "); err != nil {
				t.Fatal(err)
			}
			if err := c.Report(ctx, signed.Session, report.ID, "old hostname", []string{"first", "second"}); err != nil {
				t.Fatal(err)
			}
			list, err := c.Devices(ctx, signed.Session)
			if err != nil || len(list) != 1 || list[0].Name != "Office Mac" || list[0].OS != "darwin" || list[0].RunningCount != 2 || !reflect.DeepEqual(list[0].Capabilities, report.Capabilities) {
				t.Fatalf("legacy report changed authoritative metadata: %+v %v", list, err)
			}
			if err := c.Logout(ctx, signed.Session); err != nil {
				t.Fatal(err)
			}
			// Installation identity survives logout and session expiry. All bots inherit it.
			e.clk.add(31 * 24 * time.Hour)
			report.Bots = []string{"first", "second", "new-bot"}
			report.RunningCount = 1
			if err := c.ReportMetadata(ctx, binding.Token, report); err != nil {
				t.Fatal(err)
			}
			list, err = c.BindingDevices(ctx, binding.Token)
			if err != nil || len(list) != 1 || list[0].Name != "Office Mac" || !list[0].Online || !list[0].LastSeen.Equal(e.clk.now()) || list[0].RunningCount != 1 || len(list[0].Bots) != 3 || list[0].BindingID != binding.ID {
				t.Fatalf("binding heartbeat: %+v %v", list, err)
			}
			if _, err := c.Me(ctx, binding.Token); err != ErrSession {
				t.Fatalf("binding acquired UI identity: %v", err)
			}
			if _, err := c.Devices(ctx, binding.Token); err != ErrSession {
				t.Fatalf("binding acquired UI device access: %v", err)
			}
			signed, _, err = c.Login(ctx, "owner@example.com", "password 1")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := c.AuthenticateBinding(ctx, signed.Session); err != ErrBinding {
				t.Fatalf("UI session acquired execution identity: %v", err)
			}
			if err := c.DeleteDevice(ctx, signed.Session, report.ID); err != nil {
				t.Fatal(err)
			}
			if err := c.DeleteDevice(ctx, signed.Session, report.ID); err != nil {
				t.Fatalf("delete retry: %v", err)
			}
			if _, err := c.AuthenticateBinding(ctx, binding.Token); err != ErrBinding {
				t.Fatalf("deleted binding authenticated: %v", err)
			}
			if _, err := c.BindingDevices(ctx, binding.Token); err != ErrBinding {
				t.Fatalf("deleted binding listed devices: %v", err)
			}
			if err := c.ReportMetadata(ctx, binding.Token, report); err != ErrBinding {
				t.Fatalf("heartbeat revived deleted binding: %v", err)
			}
			if err := c.Report(ctx, signed.Session, report.ID, "revive", nil); err != ErrRevoked {
				t.Fatalf("legacy report revived deleted device: %v", err)
			}
			if list, err := c.Devices(ctx, signed.Session); err != nil || len(list) != 0 {
				t.Fatalf("deleted device remains visible: %+v %v", list, err)
			}
			rebound, err := c.BindDevice(ctx, signed.Session, report)
			if err != nil || rebound.ID == binding.ID || rebound.Token == binding.Token {
				t.Fatalf("explicit rebind did not replace identity: %v", err)
			}
			if _, err := c.AuthenticateBinding(ctx, binding.Token); err != ErrBinding {
				t.Fatalf("rebind restored old execution identity: %v", err)
			}
			if list, err := c.BindingDevices(ctx, rebound.Token); err != nil || len(list) != 1 || list[0].BindingID != rebound.ID {
				t.Fatalf("rebound member generation missing: %+v %v", list, err)
			}
			raw, err := os.ReadFile(filepath.Join(filepath.Dir(e.outbox), "accounts.json"))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), binding.Token) || strings.Contains(string(raw), rebound.Token) {
				t.Fatal("binding secret stored in clear")
			}
		})
	}
}

func TestDeviceBindingRotationAndMetadataValidation(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	signed, err := e.cli.Register(ctx, "owner@example.com", "password 1")
	if err != nil {
		t.Fatal(err)
	}
	// Legacy registration remains visible but cannot authenticate executions.
	if err := e.cli.Report(ctx, signed.Session, "device", "Legacy computer", nil); err != nil {
		t.Fatal(err)
	}
	list, err := e.cli.Devices(ctx, signed.Session)
	if err != nil || len(list) != 1 || list[0].BindingID != "" {
		t.Fatalf("legacy member unexpectedly has execution identity: %+v %v", list, err)
	}
	first, err := e.cli.BindDevice(ctx, signed.Session, DeviceReport{ID: "device", Name: "Hostname", OS: "linux"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := e.cli.BindDevice(ctx, signed.Session, DeviceReport{ID: "device", Name: "Hostname", OS: "linux"})
	if err != nil || second.ID == first.ID || second.Token == first.Token {
		t.Fatalf("active rebind did not rotate identity: %v", err)
	}
	if _, err := e.cli.AuthenticateBinding(ctx, first.Token); err != ErrBinding {
		t.Fatalf("replaced binding authenticated: %v", err)
	}
	if _, err := e.cli.BindDevice(ctx, second.Token, DeviceReport{ID: "new", Name: "No UI session"}); err != ErrSession {
		t.Fatalf("binding credential granted membership: %v", err)
	}
	if err := e.cli.RenameDevice(ctx, second.Token, "device", "No UI session"); err != ErrSession {
		t.Fatalf("binding credential renamed member: %v", err)
	}
	if err := e.cli.DeleteDevice(ctx, second.Token, "device"); err != ErrSession {
		t.Fatalf("binding credential deleted member: %v", err)
	}
	before := e.clk.now()
	e.clk.add(time.Minute)
	if err := e.cli.ReportMetadata(ctx, second.Token, DeviceReport{RunningCount: -1}); err != ErrInput {
		t.Fatalf("negative task count accepted: %v", err)
	}
	for _, name := range []string{"   ", strings.Repeat("长", maxNameLen+1)} {
		if err := e.cli.RenameDevice(ctx, signed.Session, "device", name); err != ErrInput {
			t.Fatalf("invalid name accepted: %v", err)
		}
	}
	list, err = e.cli.BindingDevices(ctx, second.Token)
	if err != nil || len(list) != 1 || list[0].Name != "Legacy computer" || !list[0].LastSeen.Equal(before) || list[0].OS != "linux" {
		t.Fatalf("invalid input changed metadata: %+v %v", list, err)
	}
	// The token identifies the installation when the report omits its ID.
	if err := e.cli.ReportMetadata(ctx, second.Token, DeviceReport{OS: "linux", Capabilities: []string{"shell"}}); err != nil {
		t.Fatalf("implicit device ID heartbeat: %v", err)
	}
	e.clk.add(4 * time.Minute)
	list, err = e.cli.BindingDevices(ctx, second.Token)
	if err != nil || len(list) != 1 || list[0].Online || !reflect.DeepEqual(list[0].Capabilities, []string{"shell"}) {
		t.Fatalf("offline metadata: %+v %v", list, err)
	}
}

// UC-DEVICE-01: account membership belongs to the installation, not a bot list.
func TestDeviceAccountIsolation(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	a, _ := e.cli.Register(ctx, "a@example.com", "password 1")
	b, _ := e.cli.Register(ctx, "b@example.com", "password 1")
	source, err := e.cli.BindDevice(ctx, a.Session, DeviceReport{ID: "source", Name: "Source"})
	if err != nil {
		t.Fatal(err)
	}
	target, err := e.cli.BindDevice(ctx, a.Session, DeviceReport{ID: "target", Name: "Target"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := e.cli.BindDevice(ctx, b.Session, DeviceReport{ID: "target", Name: "Other account"})
	if err != nil {
		t.Fatal(err)
	}
	if source.AccountKey != target.AccountKey || source.AccountKey == other.AccountKey {
		t.Fatal("binding account identity is incorrect")
	}
	if err := e.cli.RenameDevice(ctx, b.Session, "source", "stolen"); err != ErrDevice {
		t.Fatalf("cross-account rename: %v", err)
	}
	if err := e.cli.DeleteDevice(ctx, b.Session, "source"); err != ErrDevice {
		t.Fatalf("cross-account delete: %v", err)
	}
	if err := e.cli.ReportMetadata(ctx, source.Token, DeviceReport{ID: "target", Name: "stolen"}); err != ErrInput {
		t.Fatalf("token reported another installation: %v", err)
	}
	if err := e.cli.ReportMetadata(ctx, a.Session, DeviceReport{ID: "source", Name: "UI impersonation"}); err != ErrBinding {
		t.Fatalf("session reported binding heartbeat: %v", err)
	}
	if list, err := e.cli.BindingDevices(ctx, source.Token); err != nil || len(list) != 2 {
		t.Fatalf("installation does not see shared pool: %+v %v", list, err)
	}
	if err := e.cli.DeleteDevice(ctx, a.Session, "target"); err != nil {
		t.Fatal(err)
	}
	if list, err := e.cli.BindingDevices(ctx, source.Token); err != nil || len(list) != 1 || list[0].ID != "source" {
		t.Fatalf("pool retained revoked target: %+v %v", list, err)
	}
	if _, err := e.cli.AuthenticateBinding(ctx, other.Token); err != nil {
		t.Fatalf("another account's member was revoked: %v", err)
	}
}
