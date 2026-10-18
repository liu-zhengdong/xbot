package web

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"xbot/internal/remote"
)

// G7 设备: pairing by short code and the paired-device list. Pairing state
// lives in the serve process (remote.PairingStatus); nothing here stores keys.

func (s *Server) registerDevicesAPI(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/devices", s.listDevices)
	mux.HandleFunc("GET /api/devices/pairing", s.pairingStatus)
	mux.HandleFunc("PUT /api/devices/gateway", s.putGateway)
	mux.HandleFunc("PATCH /api/devices/self", s.renameSelf)
	mux.HandleFunc("PATCH /api/devices/{id}", s.renameDevice)
	mux.HandleFunc("DELETE /api/devices/{id}", s.unpairDevice)
}

// gatewayProbe caches one health check per gateway URL for 30 seconds, so the
// drawer and sidebar can poll without hammering the gateway.
var gatewayProbe = struct {
	sync.Mutex
	at map[string]time.Time
	ok map[string]bool
}{at: map[string]time.Time{}, ok: map[string]bool{}}

func gatewayUp(ctx context.Context, base string, fresh bool) bool {
	if base == "" {
		return false
	}
	gatewayProbe.Lock()
	at, ok := gatewayProbe.at[base], gatewayProbe.ok[base]
	gatewayProbe.Unlock()
	if !fresh && time.Since(at) < 30*time.Second {
		return ok
	}
	ok = remote.CheckGateway(ctx, base) == nil
	gatewayProbe.Lock()
	gatewayProbe.at[base], gatewayProbe.ok[base] = time.Now(), ok
	gatewayProbe.Unlock()
	return ok
}

type deviceRow struct {
	remote.DeviceView
	Online bool `json:"online"`
}

func (s *Server) listDevices(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	self, err := remote.Self(s.Root, s.owner)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	views, err := remote.Devices(s.Root)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	up := gatewayUp(r.Context(), self.Gateway, r.URL.Query().Get("retry") == "1")
	rows := make([]deviceRow, 0, len(views))
	for _, v := range views {
		rows = append(rows, deviceRow{v, up && !v.Seen.IsZero() && time.Since(v.Seen) < remote.OnlineWindow})
	}
	writeJSON(w, http.StatusOK, map[string]any{"self": self, "gateway_ok": up, "devices": rows, "pairing": remote.PairingStatus(s.Root)})
}

func (s *Server) pairingStatus(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	s.writePairing(w)
}

func (s *Server) writePairing(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, map[string]any{"pairing": remote.PairingStatus(s.Root)})
}

// saveGateway checks a new address before saving it; the old one stays on
// failure. Paired devices are kept either way.
func (s *Server) saveGateway(ctx context.Context, raw string) (string, error) {
	base := remote.NormalizeGateway(raw)
	if base == "" {
		return "", errors.New("请填网关地址")
	}
	cfg, err := remote.Load(s.Root)
	if err != nil {
		return base, err
	}
	if cfg.Gateway == base {
		return base, nil
	}
	if !gatewayUp(ctx, base, true) {
		return base, errGatewayDown
	}
	return base, remote.SetGateway(s.Root, base)
}

var errGatewayDown = errors.New("gateway unreachable")

func (s *Server) putGateway(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	var in struct {
		Gateway string `json:"gateway"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	base, err := s.saveGateway(r.Context(), in.Gateway)
	switch {
	case errors.Is(err, errGatewayDown):
		writeErr(w, http.StatusBadGateway, "连不上网关 "+base+"，还在用原来的地址。")
	case err != nil:
		writeErr(w, http.StatusBadRequest, "网关地址不对："+err.Error())
	default:
		writeJSON(w, http.StatusOK, map[string]string{"gateway": base})
	}
}

func (s *Server) renameSelf(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if err := decode(r, &in); err != nil || remote.SetSelfName(s.Root, in.Name) != nil {
		writeErr(w, http.StatusBadRequest, "名字不能为空")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) renameDevice(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	var in struct {
		Label string `json:"label"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	if err := remote.RenameDevice(s.Root, r.PathValue("id"), in.Label); err != nil {
		writeErr(w, http.StatusBadRequest, "改不了这个名字")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) unpairDevice(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	if err := remote.RemoveDeviceID(s.Root, r.PathValue("id")); err != nil {
		writeErr(w, http.StatusNotFound, "没有这台设备")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
