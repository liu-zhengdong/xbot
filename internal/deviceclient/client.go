// Package deviceclient connects a bound installation to account devices.
package deviceclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"xbot/internal/account"
	"xbot/internal/cliops"
	"xbot/internal/deviceexec"
	"xbot/internal/pipe"
	"xbot/internal/work"
)

type Binding struct {
	Key       string `json:"key"`
	DeviceID  string `json:"device_id"`
	Token     string `json:"binding_token,omitempty"`
	BindingID string `json:"binding_id,omitempty"`
}

func ReadBinding(root string) (Binding, error) {
	var b Binding
	raw, err := os.ReadFile(filepath.Join(root, "account-binding.json"))
	if err != nil {
		return b, err
	}
	err = json.Unmarshal(raw, &b)
	return b, err
}
func Accounts(root string) (account.Client, error) {
	if base := strings.TrimSpace(os.Getenv("XBOT_ACCOUNT_API")); base != "" {
		return account.NewRemote(base), nil
	}
	svc, err := account.OpenLocal(filepath.Join(root, "account-api"))
	if err != nil {
		return nil, err
	}
	return account.Local{S: svc}, nil
}
func Hub() string {
	if v := strings.TrimSpace(os.Getenv("XBOT_PIPE_HUB")); v != "" {
		return v
	}
	return "https://pipe.babelgo.cn:8443"
}

// Client uses installation credentials. No UI cookie is sent through this path.
type Client struct {
	Root     string
	Accounts account.Client
	Hub      string
}

func (c Client) credentials(ctx context.Context) (account.Client, Binding, error) {
	b, err := ReadBinding(c.Root)
	if err != nil || b.Token == "" {
		return nil, b, errors.New("device: this installation is not bound to an account")
	}
	api := c.Accounts
	if api == nil {
		api, err = Accounts(c.Root)
	}
	if err != nil {
		return nil, b, err
	}
	id, err := api.AuthenticateBinding(ctx, b.Token)
	if err != nil {
		return nil, b, err
	}
	if id.AccountKey != b.Key || id.DeviceID != b.DeviceID || id.ID != b.BindingID {
		return nil, b, errors.New("device: binding changed")
	}
	return api, b, nil
}
func (c Client) Devices(ctx context.Context) ([]account.DeviceView, error) {
	api, b, err := c.credentials(ctx)
	if err != nil {
		return nil, err
	}
	return api.BindingDevices(ctx, b.Token)
}

// Do sends exactly one operation. Transport failure never triggers a replay.
func (c Client) Do(ctx context.Context, source deviceexec.Source, target, method, path string, body []byte) (int, []byte, string, error) {
	_, b, err := c.credentials(ctx)
	if err != nil {
		return 0, nil, "", err
	}
	hub := c.Hub
	if hub == "" {
		hub = Hub()
	}
	ticket, err := pipe.NewExecutionTicket(ctx, hub, b.Token, target, pipe.ExecutionIdentity{Bot: source.Bot, ContextID: source.ContextID, Readonly: source.Readonly, Owner: source.Owner})
	if err != nil {
		return 0, nil, "", err
	}
	conn, err := pipe.Dial(ctx, hub, ticket)
	if err != nil {
		return 0, nil, "", err
	}
	defer conn.Close()
	return conn.Do(ctx, method, "/api/device-execution"+path, body, "application/json")
}
func (c Client) Call(ctx context.Context, source deviceexec.Source, in cliops.DeviceRequest) (json.RawMessage, error) {
	if in.Action == "list" {
		list, err := c.Devices(ctx)
		if err != nil {
			return nil, err
		}
		raw, err := json.Marshal(map[string]any{"devices": list})
		return raw, err
	}
	if in.DeviceID == "" {
		return nil, errors.New("device: select --device or choose a device in the message")
	}
	method, path := "GET", "/tasks"
	var body []byte
	switch in.Action {
	case "exec", "read", "write", "desktop", "browser":
		method, path = "POST", "/calls"
		body, _ = json.Marshal(in.Call)
	case "tasks":
		path += fmt.Sprintf("?offset=%d&limit=%d", in.Offset, in.Limit)
	case "task":
		path += "/" + in.Ref + fmt.Sprintf("?offset=%d&limit=%d", in.Offset, in.Limit)
	case "stop":
		method = "POST"
		path += "/" + in.Ref + "/stop"
	case "artifact":
		path += "/" + in.Ref + "/artifacts/" + in.ArtifactID + fmt.Sprintf("?offset=%d&limit=%d", in.Offset, in.Limit)
	default:
		return nil, errors.New("device: unknown operation")
	}
	code, raw, _, err := c.Do(ctx, source, in.DeviceID, method, path, body)
	if err != nil {
		return nil, fmt.Errorf("device: receipt unknown; query original ref %q before any retry: %w", in.Call.Ref, err)
	}
	if code < 200 || code >= 300 {
		err := fmt.Errorf("device: target returned %d: %s", code, strings.TrimSpace(string(raw)))
		data, _ := json.Marshal(map[string]string{"error": err.Error()})
		return data, err
	}
	if in.Action == "artifact" {
		workspace := filepath.Join(c.Root, "device-downloads")
		file, err := work.WriteResult(workspace, raw, "bin")
		if err != nil {
			return nil, err
		}
		data, err := json.Marshal(map[string]any{"file": filepath.Join(workspace, file), "bytes": len(raw), "device_id": in.DeviceID, "ref": in.Ref, "artifact_id": in.ArtifactID, "offset": in.Offset, "next_offset": in.Offset + len(raw)})
		return data, err
	}
	return json.RawMessage(raw), nil
}
