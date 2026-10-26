package account

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is how xbot serve talks to the Account API: in process (Local) while
// the hosted service is not set up, or over HTTP (Remote) once it is.
type Client interface {
	Register(ctx context.Context, email, pass string) (Pending, error)
	Login(ctx context.Context, email, pass string) (Signed, Pending, error)
	Magic(ctx context.Context, email string) (Pending, error)
	Resend(ctx context.Context, challenge string) error
	Code(ctx context.Context, challenge, code string) (Signed, error)
	Poll(ctx context.Context, challenge string) (PollResult, error)
	Me(ctx context.Context, session string) (Me, error)
	Logout(ctx context.Context, session string) error
	Devices(ctx context.Context, session string) ([]DeviceView, error)
	Report(ctx context.Context, session, id, name string, bots []string) error
	// Hosted says the service is the remote one rather than the local stand-in.
	Hosted() bool
}

// Local runs the service in this process.
type Local struct{ S *Service }

func (l Local) Register(ctx context.Context, e, p string) (Pending, error) {
	return l.S.Register(ctx, e, p)
}
func (l Local) Login(ctx context.Context, e, p string) (Signed, Pending, error) {
	return l.S.Login(ctx, e, p)
}
func (l Local) Magic(ctx context.Context, e string) (Pending, error) { return l.S.Magic(ctx, e) }
func (l Local) Resend(ctx context.Context, id string) error          { return l.S.Resend(ctx, id) }
func (l Local) Code(_ context.Context, id, c string) (Signed, error) { return l.S.ConfirmCode(id, c) }
func (l Local) Poll(_ context.Context, id string) (PollResult, error) {
	return l.S.Poll(id)
}
func (l Local) Me(_ context.Context, t string) (Me, error) { return l.S.Session(t) }
func (l Local) Logout(_ context.Context, t string) error   { return l.S.Logout(t) }
func (l Local) Hosted() bool                               { return false }
func (l Local) Devices(_ context.Context, t string) ([]DeviceView, error) {
	return l.S.Devices(t)
}
func (l Local) Report(_ context.Context, t, id, name string, bots []string) error {
	return l.S.ReportDevice(t, id, name, bots)
}

// Remote calls a hosted Account API at Base (such as https://account.example.com).
type Remote struct {
	Base string
	HTTP *http.Client
}

func NewRemote(base string) Remote {
	return Remote{Base: strings.TrimRight(base, "/"), HTTP: &http.Client{Timeout: 10 * time.Second}}
}

func (c Remote) Hosted() bool { return true }

func (c Remote) call(ctx context.Context, method, path, session string, in, out any) (map[string]string, error) {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+Prefix+path, body)
	if err != nil {
		return nil, err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if session != "" {
		req.Header.Set("Authorization", "Bearer "+session)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("account: service unreachable")
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 300 {
		var e map[string]string
		_ = json.Unmarshal(raw, &e)
		return e, CodeError(e["error"])
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func (c Remote) Register(ctx context.Context, e, p string) (Pending, error) {
	var out Pending
	_, err := c.call(ctx, "POST", "register", "", credsIn{e, p}, &out)
	return out, err
}

func (c Remote) Login(ctx context.Context, e, p string) (Signed, Pending, error) {
	var out Signed
	extra, err := c.call(ctx, "POST", "login", "", credsIn{e, p}, &out)
	if err == ErrUnverified {
		return Signed{}, Pending{Challenge: extra["challenge"], Email: extra["email"]}, err
	}
	return out, Pending{}, err
}

func (c Remote) Magic(ctx context.Context, e string) (Pending, error) {
	var out Pending
	_, err := c.call(ctx, "POST", "magic", "", map[string]string{"email": e}, &out)
	return out, err
}

func (c Remote) Resend(ctx context.Context, id string) error {
	_, err := c.call(ctx, "POST", "resend", "", challengeIn{Challenge: id}, nil)
	return err
}

func (c Remote) Code(ctx context.Context, id, code string) (Signed, error) {
	var out Signed
	_, err := c.call(ctx, "POST", "code", "", challengeIn{Challenge: id, Code: code}, &out)
	return out, err
}

func (c Remote) Poll(ctx context.Context, id string) (PollResult, error) {
	var out PollResult
	_, err := c.call(ctx, "POST", "poll", "", challengeIn{Challenge: id}, &out)
	return out, err
}

func (c Remote) Me(ctx context.Context, t string) (Me, error) {
	var out Me
	_, err := c.call(ctx, "GET", "me", t, nil, &out)
	return out, err
}

func (c Remote) Logout(ctx context.Context, t string) error {
	_, err := c.call(ctx, "POST", "logout", t, nil, nil)
	return err
}

func (c Remote) Devices(ctx context.Context, t string) ([]DeviceView, error) {
	var out struct {
		Devices []DeviceView `json:"devices"`
	}
	_, err := c.call(ctx, "GET", "devices", t, nil, &out)
	return out.Devices, err
}

func (c Remote) Report(ctx context.Context, t, id, name string, bots []string) error {
	in := map[string]any{"id": id, "name": name, "bots": bots}
	_, err := c.call(ctx, "POST", "devices", t, in, nil)
	return err
}
