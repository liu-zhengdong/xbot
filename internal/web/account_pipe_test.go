package web

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestAccountPipePersistsRestoresRotatesAndLogsOut(t *testing.T) {
	e := newAcctEnv(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	signed, err := e.s.acct.client.Register(ctx, "pipe@example.com", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	me, err := e.s.acct.client.Me(ctx, signed.Session)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := e.s.bindIfFree(me.Key, true)
	if err != nil {
		t.Fatal(err)
	}
	started, stopped := make(chan string, 4), make(chan string, 4)
	run := func(ctx context.Context, session, device string) error {
		if device != binding.DeviceID {
			t.Error("wrong device")
		}
		started <- session
		<-ctx.Done()
		stopped <- session
		return ctx.Err()
	}
	expect := func(c <-chan string, want string) {
		t.Helper()
		select {
		case got := <-c:
			if got != want {
				t.Fatal("wrong session")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("standby lifecycle timed out")
		}
	}
	// A session saved before serve starts must restore without a browser.
	if err := e.s.keepPipeSession(signed.Session); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(e.s.accountSessionPath())
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("session must be 0600")
	}
	stop := e.s.StartAccountPipe(ctx, run)
	expect(started, signed.Session)
	// The report is made even without account/me page polling.
	devices, err := e.s.acct.client.Devices(ctx, signed.Session)
	if err != nil || len(devices) != 1 || !devices[0].Online {
		t.Fatal("standby did not report online device")
	}
	second, _, err := e.s.acct.client.Login(ctx, "pipe@example.com", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.s.keepPipeSession(second.Session); err != nil {
		t.Fatal(err)
	}
	expect(stopped, signed.Session)
	expect(started, second.Session)
	e.s.forgetPipeSession(signed.Session) // an older browser cannot stop the newer session
	select {
	case <-stopped:
		t.Fatal("stale logout stopped current standby")
	default:
	}
	e.s.forgetPipeSession(second.Session)
	expect(stopped, second.Session)
	if _, err := os.Stat(e.s.accountSessionPath()); !os.IsNotExist(err) {
		t.Fatal("logout retained session file")
	}
	stop()
}

func TestAccountPipeRejectsExpiredOrForeignSessionOnRestart(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		t.Run(map[bool]string{false: "expired", true: "foreign"}[foreign], func(t *testing.T) {
			e := newAcctEnv(t, false)
			ctx := context.Background()
			signed, err := e.s.acct.client.Register(ctx, "pipe@example.com", "correct horse battery")
			if err != nil {
				t.Fatal(err)
			}
			me, _ := e.s.acct.client.Me(ctx, signed.Session)
			key := me.Key
			if foreign {
				key = "different-account"
			} else {
				_ = e.s.acct.client.Logout(ctx, signed.Session)
			}
			if _, err := e.s.bindIfFree(key, true); err != nil {
				t.Fatal(err)
			}
			if err := e.s.keepPipeSession(signed.Session); err != nil {
				t.Fatal(err)
			}
			called := make(chan struct{}, 1)
			stop := e.s.StartAccountPipe(ctx, func(context.Context, string, string) error { called <- struct{}{}; return nil })
			defer stop()
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				if _, err := os.Stat(e.s.accountSessionPath()); os.IsNotExist(err) {
					select {
					case <-called:
						t.Fatal("invalid session started standby")
					default:
					}
					return
				}
				time.Sleep(5 * time.Millisecond)
			}
			t.Fatal("invalid session file retained")
		})
	}
}

func TestAccountHandlersControlPipeStandby(t *testing.T) {
	for _, expire := range []bool{false, true} {
		t.Run(map[bool]string{false: "logout", true: "expired"}[expire], func(t *testing.T) {
			e := newAcctEnv(t, false)
			started, stopped := make(chan struct{}, 1), make(chan struct{}, 1)
			stop := e.s.StartAccountPipe(context.Background(), func(ctx context.Context, session, id string) error {
				started <- struct{}{}
				<-ctx.Done()
				stopped <- struct{}{}
				return ctx.Err()
			})
			defer stop()
			client := e.browser()
			status, _ := e.do(client, "POST", "/api/account/register", map[string]string{"email": "pipe@example.com", "password": "correct horse battery"}, nil)
			if status != 200 {
				t.Fatal(status)
			}
			if _, err := os.Stat(e.s.accountSessionPath()); !os.IsNotExist(err) {
				t.Fatal("unbound sign-in persisted owner session")
			}
			status, _ = e.do(client, "POST", "/api/account/claim", nil, nil)
			if status != 200 {
				t.Fatal(status)
			}
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("claim did not start standby")
			}
			if expire {
				raw, _ := os.ReadFile(e.s.accountSessionPath())
				_ = e.s.acct.client.Logout(context.Background(), string(raw))
				status, _ = e.do(client, "GET", "/api/account/me", nil, nil)
			} else {
				status, _ = e.do(client, "POST", "/api/account/logout", nil, nil)
			}
			if status != 200 {
				t.Fatal(status)
			}
			select {
			case <-stopped:
			case <-time.After(5 * time.Second):
				t.Fatal("logout/expiry did not stop standby")
			}
			if _, err := os.Stat(e.s.accountSessionPath()); !os.IsNotExist(err) {
				t.Fatal("logout/expiry retained session")
			}
		})
	}
}
