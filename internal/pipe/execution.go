package pipe

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"xbot/internal/account"
	"xbot/internal/deviceidentity"
)

type bindingAccounts interface {
	AuthenticateBinding(context.Context, string) (account.BindingIdentity, error)
	BindingDevices(context.Context, string) ([]account.DeviceView, error)
}

// serveExecutionTicket accepts an installation credential, never an owner session.
func (h *Hub) serveExecutionTicket(w http.ResponseWriter, r *http.Request) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	api, supported := h.Accounts.(bindingAccounts)
	if !ok || !supported {
		writeErr(w, 401, "bound runtime required")
		return
	}
	source, err := api.AuthenticateBinding(r.Context(), token)
	if err != nil {
		writeErr(w, 401, "bound runtime required")
		return
	}
	var in struct {
		DeviceID  string `json:"device_id"`
		Bot       string `json:"bot"`
		ContextID string `json:"context_id"`
		Readonly  bool   `json:"readonly"`
		Owner     bool   `json:"owner,omitempty"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if dec.Decode(&in) != nil || in.Bot == "" || len(in.Bot) > 64 || in.ContextID == "" || len(in.ContextID) > 128 {
		writeErr(w, 400, "invalid runtime identity")
		return
	}
	devices, err := api.BindingDevices(r.Context(), token)
	if err != nil {
		writeErr(w, 401, "membership unavailable")
		return
	}
	found := false
	targetBindingID := ""
	for _, d := range devices {
		if d.ID == in.DeviceID && d.BindingID != "" {
			found = true
			targetBindingID = d.BindingID
			break
		}
	}
	if !found {
		writeErr(w, 404, "no such bound device")
		return
	}
	identity := &ExecutionIdentity{AccountKey: source.AccountKey, DeviceID: source.DeviceID, BindingID: source.ID, TargetBindingID: targetBindingID, Bot: in.Bot, ContextID: in.ContextID, Readonly: in.Readonly, Owner: in.Owner}
	h.issueTicket(w, r, source.AccountKey, in.DeviceID, identity, "")
}

// NewExecutionTicket fixes source scope before any operation is sent.
func NewExecutionTicket(ctx context.Context, hub, token, target string, source ExecutionIdentity) (TicketOut, error) {
	body, _ := json.Marshal(map[string]any{"device_id": target, "bot": source.Bot, "context_id": source.ContextID, "readonly": source.Readonly, "owner": source.Owner})
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(hub, "/")+"/pipe/v1/execute-ticket", strings.NewReader(string(body)))
	if err != nil {
		return TicketOut{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return TicketOut{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return TicketOut{}, &HTTPError{Status: res.StatusCode, Message: "execution device unavailable"}
	}
	var out TicketOut
	err = json.NewDecoder(http.MaxBytesReader(nil, res.Body, 64<<10)).Decode(&out)
	return out, err
}

// ExecutionSource reads the identity fixed by the hub, outside request payloads.
func ExecutionSource(ctx context.Context) (ExecutionIdentity, bool) {
	return deviceidentity.Source(ctx)
}
