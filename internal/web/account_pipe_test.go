package web

import (
	"context"
	"errors"
	"net/url"
	"os"
	"testing"
	"time"

	"xbot/internal/account"
	"xbot/internal/fsutil"
)

func expectPipeEvent(t *testing.T, events <-chan string, want string) {
	t.Helper()
	select {
	case got := <-events:
		if got != want {
			t.Fatal("wrong installation credential")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("standby lifecycle timed out")
	}
}

func TestAccountPipePersistsRestoresAfterUILogout(t *testing.T) {
	e := newAcctEnv(t, false)
	ctx := context.Background()
	signed, err := e.s.acct.client.Register(ctx, "pipe@example.com", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	me, _ := e.s.acct.client.Me(ctx, signed.Session)
	if _, err := e.s.bindIfFree(me.Key, true); err != nil {
		t.Fatal(err)
	}
	if err := e.s.keepPipeSession(signed.Session); err != nil {
		t.Fatal(err)
	}
	binding, err := e.s.readBinding()
	if err != nil || binding.Token == "" || binding.BindingID == "" || binding.Token == signed.Session {
		t.Fatal("installation credential was not persisted independently")
	}
	info, err := os.Stat(e.s.bindingPath())
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("binding must be 0600")
	}
	if _, err := os.Stat(e.s.accountSessionPath()); !os.IsNotExist(err) {
		t.Fatal("new bindings must not persist UI sessions")
	}
	// The server restores the durable binding even after the UI session ends.
	if err := e.s.acct.client.Logout(ctx, signed.Session); err != nil {
		t.Fatal(err)
	}
	started, stopped := make(chan string, 4), make(chan string, 4)
	run := func(ctx context.Context, token, device string) error {
		if device != binding.DeviceID {
			t.Error("wrong device")
		}
		started <- token
		<-ctx.Done()
		stopped <- token
		return ctx.Err()
	}
	stop := e.s.StartAccountPipe(ctx, run)
	expectPipeEvent(t, started, binding.Token)
	devices, err := e.s.acct.client.BindingDevices(ctx, binding.Token)
	if err != nil || len(devices) != 1 || !devices[0].Online || devices[0].OS == "" {
		t.Fatal("standby did not report actual metadata")
	}
	second, _, err := e.s.acct.client.Login(ctx, "pipe@example.com", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.s.keepPipeSession(second.Session); err != nil {
		t.Fatal(err)
	}
	e.s.forgetPipeSession(signed.Session)
	e.s.forgetPipeSession(second.Session)
	select {
	case <-stopped:
		t.Fatal("UI session changes stopped installation standby")
	case <-started:
		t.Fatal("UI login rotated installation standby")
	default:
	}
	stop()
	expectPipeEvent(t, stopped, binding.Token)
	stop = e.s.StartAccountPipe(ctx, run)
	expectPipeEvent(t, started, binding.Token)
	stop()
	expectPipeEvent(t, stopped, binding.Token)
}

func TestAccountPipeMigratesOnlyValidLegacySession(t *testing.T) {
	for _, mode := range []string{"valid", "expired", "foreign"} {
		t.Run(mode, func(t *testing.T) {
			e := newAcctEnv(t, false)
			ctx := context.Background()
			signed, err := e.s.acct.client.Register(ctx, "pipe@example.com", "correct horse battery")
			if err != nil {
				t.Fatal(err)
			}
			me, _ := e.s.acct.client.Me(ctx, signed.Session)
			key := me.Key
			if mode == "foreign" {
				key = "different-account"
			} else if mode == "expired" {
				_ = e.s.acct.client.Logout(ctx, signed.Session)
			}
			if _, err := e.s.bindIfFree(key, true); err != nil {
				t.Fatal(err)
			}
			if err := fsutil.WriteAtomic(e.s.accountSessionPath(), []byte(signed.Session), 0600); err != nil {
				t.Fatal(err)
			}
			started := make(chan string, 1)
			stop := e.s.StartAccountPipe(ctx, func(ctx context.Context, token, _ string) error {
				started <- token
				<-ctx.Done()
				return ctx.Err()
			})
			defer stop()
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				if _, err := os.Stat(e.s.accountSessionPath()); os.IsNotExist(err) {
					if mode == "valid" {
						b, err := e.s.readBinding()
						if err != nil || b.Token == "" || b.Token == signed.Session {
							t.Fatal("legacy migration did not persist independent identity")
						}
						expectPipeEvent(t, started, b.Token)
					} else {
						select {
						case <-started:
							t.Fatal("invalid legacy session started standby")
						default:
						}
					}
					return
				}
				time.Sleep(5 * time.Millisecond)
			}
			t.Fatal("legacy UI session file retained")
		})
	}
}

func TestAccountHandlersPreserveStandbyOnUILogoutOrExpiry(t *testing.T) {
	for _, expire := range []bool{false, true} {
		t.Run(map[bool]string{false: "logout", true: "expired"}[expire], func(t *testing.T) {
			e := newAcctEnv(t, false)
			started, stopped := make(chan string, 2), make(chan string, 2)
			stop := e.s.StartAccountPipe(context.Background(), func(ctx context.Context, token, _ string) error {
				started <- token
				<-ctx.Done()
				stopped <- token
				return ctx.Err()
			})
			defer stop()
			client := e.browser()
			status, _ := e.do(client, "POST", "/api/account/register", map[string]string{"email": "pipe@example.com", "password": "correct horse battery"}, nil)
			if status != 200 {
				t.Fatal(status)
			}
			e.claim(client, nil)
			b, _ := e.s.readBinding()
			expectPipeEvent(t, started, b.Token)
			if expire {
				u, _ := url.Parse(e.srv.URL)
				for _, cookie := range client.Jar.Cookies(u) {
					if cookie.Name == accountCookie {
						_ = e.s.acct.client.Logout(context.Background(), cookie.Value)
					}
				}
				status, _ = e.do(client, "GET", "/api/account/me", nil, nil)
			} else {
				status, _ = e.do(client, "POST", "/api/account/logout", nil, nil)
			}
			if status != 200 {
				t.Fatal(status)
			}
			if _, err := e.s.acct.client.AuthenticateBinding(context.Background(), b.Token); err != nil {
				t.Fatalf("UI logout/expiry revoked installation: %v", err)
			}
			select {
			case <-stopped:
				t.Fatal("UI logout/expiry stopped installation standby")
			default:
			}
		})
	}
}

func TestAccountRevocationRequiresExplicitOwnerClaim(t *testing.T) {
	e := newAcctEnv(t, false)
	ctx := context.Background()
	started, stopped := make(chan string, 4), make(chan string, 4)
	run := func(ctx context.Context, token, _ string) error {
		started <- token
		<-ctx.Done()
		stopped <- token
		return ctx.Err()
	}
	stop := e.s.StartAccountPipe(ctx, run)
	defer stop()
	client := e.browser()
	if status, _ := e.do(client, "POST", "/api/account/register", map[string]string{"email": "pipe@example.com", "password": "correct horse battery"}, nil); status != 200 {
		t.Fatal(status)
	}
	e.claim(client, nil)
	before, _ := e.s.readBinding()
	expectPipeEvent(t, started, before.Token)
	u, _ := url.Parse(e.srv.URL)
	var session string
	for _, cookie := range client.Jar.Cookies(u) {
		if cookie.Name == accountCookie {
			session = cookie.Value
		}
	}
	if err := e.s.acct.client.DeleteDevice(ctx, session, before.DeviceID); err != nil {
		t.Fatal(err)
	}
	if status, _ := e.do(client, "GET", "/api/account/me", nil, nil); status != 200 {
		t.Fatal(status)
	}
	expectPipeEvent(t, stopped, before.Token)
	// A still-valid UI session cannot revive the member through normal pages.
	if status, _ := e.do(client, "POST", "/api/account/login", map[string]string{"email": "pipe@example.com", "password": "correct horse battery"}, nil); status != 200 {
		t.Fatal(status)
	}
	if status, out := e.do(client, "GET", "/api/account/devices", nil, nil); status != 200 || len(out["devices"].([]any)) != 0 {
		t.Fatal("normal account page restored revoked member")
	}
	if _, err := e.s.boundCredential(ctx, session, false); !errors.Is(err, account.ErrBinding) {
		t.Fatalf("revoked credential silently rebound: %v", err)
	}
	// Restart cannot fall back to a leftover legacy session after revocation.
	stop()
	if err := fsutil.WriteAtomic(e.s.accountSessionPath(), []byte(session), 0600); err != nil {
		t.Fatal(err)
	}
	restarted := e.s.StartAccountPipe(ctx, run)
	defer restarted()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(e.s.accountSessionPath()); os.IsNotExist(err) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("restart retained legacy session beside a revoked binding")
		}
		time.Sleep(5 * time.Millisecond)
	}
	select {
	case <-started:
		t.Fatal("restart restored revoked installation through legacy session")
	default:
	}
	e.claim(client, nil)
	after, _ := e.s.readBinding()
	if after.Token == before.Token || after.BindingID == before.BindingID {
		t.Fatal("explicit rebind did not rotate identity")
	}
	expectPipeEvent(t, started, after.Token)
	if _, err := e.s.acct.client.AuthenticateBinding(ctx, before.Token); err != account.ErrBinding {
		t.Fatalf("old token restored after claim: %v", err)
	}
}

func TestAccountPipeRejectsMismatchedBindingIdentity(t *testing.T) {
	for _, field := range []string{"account", "device", "generation"} {
		t.Run(field, func(t *testing.T) {
			e := newAcctEnv(t, false)
			ctx := context.Background()
			signed, _ := e.s.acct.client.Register(ctx, "pipe@example.com", "correct horse battery")
			me, _ := e.s.acct.client.Me(ctx, signed.Session)
			_, _ = e.s.bindIfFree(me.Key, true)
			token, err := e.s.boundCredential(ctx, signed.Session, false)
			if err != nil {
				t.Fatal(err)
			}
			b, _ := e.s.readBinding()
			switch field {
			case "account":
				b.Key = "wrong"
			case "device":
				b.DeviceID = "wrong"
			case "generation":
				b.BindingID = "wrong"
			}
			raw, _ := fsutil.Marshal(b)
			if err := fsutil.WriteAtomic(e.s.bindingPath(), raw, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := e.s.boundCredential(ctx, signed.Session, false); err != account.ErrBinding {
				t.Fatalf("mismatched credential accepted: %v", err)
			}
			if err := e.s.runAccountPipe(ctx, token, func(context.Context, string, string) error {
				t.Error("mismatched binding ran standby")
				return nil
			}); err != account.ErrBinding {
				t.Fatalf("mismatched binding restored: %v", err)
			}
		})
	}
}
