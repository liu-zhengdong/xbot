package web

import (
	"context"
	"errors"
	"testing"

	"xbot/internal/account"
	"xbot/internal/fsutil"
)

// A hosted account service older than device binding answers 404 for
// /devices/bind. Nothing can be persisted there, but the account screen must
// still open: the standby credential is not what the owner asked for.
type bindlessClient struct{ account.Client }

func (bindlessClient) BindDevice(context.Context, string, account.DeviceReport) (account.Binding, error) {
	return account.Binding{}, errors.New("account: not found")
}

func TestAccountMeSurvivesServiceWithoutDeviceBinding(t *testing.T) {
	e := newAcctEnv(t, true)
	ctx := context.Background()
	signed, err := e.s.acct.client.Register(ctx, "old@example.com", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	me, err := e.s.acct.client.Me(ctx, signed.Session)
	if err != nil {
		t.Fatal(err)
	}
	b, err := e.s.bindIfFree(me.Key, true)
	if err != nil {
		t.Fatal(err)
	}
	// The old service never issued an installation credential.
	b.Token, b.BindingID = "", ""
	raw, err := fsutil.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if err := fsutil.WriteAtomic(e.s.bindingPath(), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	e.s.acct.client = bindlessClient{e.s.acct.client}

	client := e.browser()
	status, out := e.do(client, "POST", "/api/account/login", map[string]string{"email": "old@example.com", "password": "correct horse battery"}, nil)
	if status != 200 {
		t.Fatalf("login against binding-less service: %d %v", status, out)
	}
	status, out = e.do(client, "GET", "/api/account/me", nil, nil)
	if status != 200 {
		t.Fatalf("account me must not fail on persisted standby: %d %v", status, out)
	}
	view, _ := out["account"].(map[string]any)
	if view == nil || view["email"] == "" {
		t.Fatalf("account view missing: %v", out)
	}
	if view["owner"] != true {
		t.Fatalf("owner lost on account view: %v", view)
	}
}
