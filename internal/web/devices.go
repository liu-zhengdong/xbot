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
	mux.HandleFunc("POST /api/devices/code", s.newCode)
	mux.HandleFunc("DELETE /api/devices/code", s.cancelCode)
	mux.HandleFunc("POST /api/devices/join", s.joinDevice)
	mux.HandleFunc("POST /api/devices/join/confirm", s.confirmJoin)
	mux.HandleFunc("DELETE /api/devices/join", s.cancelJoin)
	mux.HandleFunc("POST /api/devices/request", s.decideRequest)
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
		rows = append(rows, deviceRow{v, !v.Unpaired && up && !v.Seen.IsZero() && time.Since(v.Seen) < remote.OnlineWindow})
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
	if cfg.GatewayURL() == base {
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

// pairErrCopy is the copy for pairing errors the gateway reports as distinct
// from 连不上网关: its 429 on a code, and no free "X 2"…"X 9" name. Other
// errors keep their caller's copy.
func pairErrCopy(err error) (int, string, bool) {
	switch {
	case errors.Is(err, remote.ErrRateLimited):
		return http.StatusTooManyRequests, "操作太频繁，等一分钟再试", true
	case errors.Is(err, remote.ErrNameFull):
		return http.StatusConflict, "同名设备已满 9 台，先解除或改名旧的，或选择替换旧的", true
	}
	return 0, "", false
}

func (s *Server) newCode(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	var in struct {
		Gateway string `json:"gateway"`
	}
	_ = decode(r, &in) // The body is optional.
	if in.Gateway != "" {
		if base, err := s.saveGateway(r.Context(), in.Gateway); err != nil {
			writeErr(w, http.StatusBadGateway, "连不上网关 "+base+"。检查一下网络，或者改一下网关地址。")
			return
		}
	}
	cfg, err := remote.Load(s.Root)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, _, err := remote.NewShortCode(r.Context(), s.Root, s.owner); err != nil {
		if code, msg, ok := pairErrCopy(err); ok {
			writeErr(w, code, msg)
			return
		}
		writeErr(w, http.StatusBadGateway, "连不上网关 "+cfg.GatewayURL()+"。检查一下网络，或者改一下网关地址。")
		return
	}
	s.writePairing(w)
}

func (s *Server) cancelCode(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	remote.CancelShortCode(r.Context(), s.Root)
	s.writePairing(w)
}

func (s *Server) joinDevice(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	var in struct {
		Code    string `json:"code"`
		Gateway string `json:"gateway"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	if _, _, err := remote.ParseShortCode(in.Code); err != nil {
		// Checked here too so a typo never touches the gateway.
		_ = remote.JoinShortCode(r.Context(), s.Root, in.Code, "")
		s.writePairing(w)
		return
	}
	if in.Gateway != "" {
		base, err := s.saveGateway(r.Context(), in.Gateway)
		if errors.Is(err, errGatewayDown) {
			remote.SetJoinPhase(s.Root, remote.PairingPhase{Phase: "gateway_unreachable", Gateway: base})
			s.writePairing(w)
			return
		}
		if err != nil {
			writeErr(w, http.StatusBadRequest, "网关地址不对："+err.Error())
			return
		}
	}
	// Expected failures (404/410/unreachable/already) are phases, not errors.
	_ = remote.JoinShortCode(r.Context(), s.Root, in.Code, "")
	s.writePairing(w)
}

func (s *Server) confirmJoin(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	if err := remote.ConfirmJoin(r.Context(), s.Root, s.owner); err != nil {
		writeErr(w, http.StatusConflict, "这次配对已经结束了")
		return
	}
	s.writePairing(w)
}

func (s *Server) cancelJoin(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	remote.CancelJoin(s.Root)
	s.writePairing(w)
}

func (s *Server) decideRequest(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	var in struct {
		Decision string `json:"decision"` // allow | replace | add | deny
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	allow := in.Decision == "allow" || in.Decision == "replace" || in.Decision == "add"
	if !allow && in.Decision != "deny" {
		writeErr(w, http.StatusBadRequest, "bad decision")
		return
	}
	if err := remote.DecidePairing(r.Context(), s.Root, allow, in.Decision == "replace"); err != nil {
		if code, msg, ok := pairErrCopy(err); ok {
			writeErr(w, code, msg)
			return
		}
		writeErr(w, http.StatusConflict, "没送到另一台电脑，再点一次试试")
		return
	}
	s.writePairing(w)
}
