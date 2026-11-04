package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"runtime"
	"sort"
	"strings"

	"xbot/internal/account"
	"xbot/internal/deviceexec"
	"xbot/internal/deviceidentity"
)

type deviceWire interface {
	Devices(context.Context) ([]account.DeviceView, error)
	Do(context.Context, deviceexec.Source, string, string, string, []byte) (int, []byte, string, error)
}

// SetDeviceClient connects the existing serve transport after account assembly.
func (s *Server) SetDeviceClient(client deviceWire) { s.acct.wire = client }
func (s *Server) boundDevices(ctx context.Context) ([]account.DeviceView, error) {
	b, err := s.readBinding()
	if err != nil {
		return nil, err
	}
	return s.acct.client.BindingDevices(ctx, b.Token)
}

func (s *Server) registerAccountDevices(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/account/devices/{id}", s.accountDeviceDetail)
	mux.HandleFunc("PATCH /api/account/devices/{id}", s.accountDeviceRename)
	mux.HandleFunc("DELETE /api/account/devices/{id}", s.accountDeviceDelete)
	mux.HandleFunc("GET /api/account/devices/{id}/tasks", s.accountDeviceTasks)
	mux.HandleFunc("GET /api/account/devices/{id}/tasks/{ref}", s.accountDeviceTasks)
	mux.HandleFunc("POST /api/account/devices/{id}/tasks/{ref}/stop", s.accountDeviceTasks)
	mux.HandleFunc("GET /api/account/devices/{id}/tasks/{ref}/artifacts/{artifact}", s.accountDeviceTasks)
	mux.HandleFunc("GET /api/bots/{name}/device-tasks", s.botDeviceTasks)
}
func (s *Server) deviceManager() (*deviceexec.Manager, error) {
	s.acct.deviceMu.Lock()
	defer s.acct.deviceMu.Unlock()
	if s.acct.execution != nil {
		return s.acct.execution, nil
	}
	b, err := s.readBinding()
	if err != nil {
		return nil, err
	}
	if b.DeviceID == "" {
		return nil, errors.New("device: installation has no device id")
	}
	m, err := deviceexec.New(s.Root, b.DeviceID, s.selfDeviceName())
	if err != nil {
		return nil, err
	}
	s.acct.execution = m
	return m, nil
}
func (s *Server) CloseDeviceExecution() {
	s.acct.deviceMu.Lock()
	m := s.acct.execution
	s.acct.execution = nil
	s.acct.deviceMu.Unlock()
	if m != nil {
		m.Close()
	}
}
func (s *Server) deviceReport(id string) account.DeviceReport {
	out := account.DeviceReport{ID: id, Name: s.selfDeviceName(), OS: runtime.GOOS, Bots: s.ownerBotNames(), Capabilities: []string{}}
	if m, err := s.deviceManager(); err == nil {
		out.Capabilities = m.Capabilities()
		out.RunningCount = m.RunningCount()
	}
	return out
}

// ValidateDeviceExecution rechecks both membership generations for every request.
func (s *Server) ValidateDeviceExecution(ctx context.Context, source deviceidentity.Identity) error {
	b, err := s.readBinding()
	if err != nil {
		return err
	}
	own, err := s.acct.client.AuthenticateBinding(ctx, b.Token)
	if err != nil {
		return err
	}
	if own.AccountKey != b.Key || own.DeviceID != b.DeviceID || own.ID != b.BindingID || source.AccountKey != own.AccountKey || source.TargetBindingID != own.ID {
		return errors.New("device: account mismatch")
	}
	list, err := s.acct.client.BindingDevices(ctx, b.Token)
	if err != nil {
		return err
	}
	validSource := false
	for _, d := range list {
		if d.ID == own.DeviceID {
			if m, err := s.deviceManager(); err == nil {
				m.SetDeviceName(d.Name)
			}
		}
		if d.ID == source.DeviceID && d.BindingID != "" && d.BindingID == source.BindingID {
			validSource = true
		}
	}
	if validSource {
		return nil
	}
	return errors.New("device: source membership revoked")
}

// DeviceExecutionHandler never attaches an owner cookie to an execution request.
func (s *Server) DeviceExecutionHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, ok := deviceidentity.Source(r.Context())
		if !ok {
			writeErr(w, 403, "bound runtime required")
			return
		}
		m, err := s.deviceManager()
		if err != nil {
			writeErr(w, 503, err.Error())
			return
		}
		source := deviceexec.Source{Account: identity.AccountKey, DeviceID: identity.DeviceID, Bot: identity.Bot, ContextID: identity.ContextID, Readonly: identity.Readonly, Owner: identity.Owner}
		http.StripPrefix("/api/device-execution", m.HandlerFor(source)).ServeHTTP(w, r)
	})
}
func (s *Server) accountDeviceMember(w http.ResponseWriter, r *http.Request) (account.DeviceView, bool) {
	if !s.require(w, r) || !s.accountReady(w, r) {
		return account.DeviceView{}, false
	}
	list, err := s.acct.client.Devices(r.Context(), accountSession(r))
	if err != nil {
		writeAccountErr(w, err)
		return account.DeviceView{}, false
	}
	for _, d := range list {
		if d.ID == r.PathValue("id") {
			return d, true
		}
	}
	writeErr(w, 404, "device not found")
	return account.DeviceView{}, false
}
func (s *Server) accountDeviceRow(d account.DeviceView) accountDevice {
	b, _ := s.readBinding()
	row := accountDevice{ID: d.ID, Name: d.Name, Online: d.Online, Self: b.DeviceID == d.ID && b.BindingID != "" && b.BindingID == d.BindingID, Bots: []accountBot{}, OS: d.OS, Capabilities: d.Capabilities, LastSeen: d.LastSeen, RunningCount: d.RunningCount}
	if row.Self {
		row.Bots = s.localAccountBots()
	} else {
		for _, n := range d.Bots {
			row.Bots = append(row.Bots, accountBot{Name: n})
		}
	}
	return row
}
func (s *Server) managementSource() deviceexec.Source {
	return deviceexec.Source{Bot: "owner", ContextID: "owner", Owner: true}
}
func (s *Server) deviceRequest(ctx context.Context, target, method, path string, body []byte) (int, []byte, string, error) {
	if s.acct.wire == nil {
		return 0, nil, "", errors.New("device transport unavailable")
	}
	return s.acct.wire.Do(ctx, s.managementSource(), target, method, path, body)
}
func (s *Server) deviceTasks(ctx context.Context, target, botName, sourceID string) ([]deviceexec.Task, bool, error) {
	tasks := []deviceexec.Task{}
	offset := int64(0)
	used := 2
	for len(tasks) < 200 {
		query := url.Values{"offset": {fmt.Sprint(offset)}, "limit": {fmt.Sprint(200 - len(tasks))}}
		if botName != "" {
			query.Set("bot", botName)
		}
		if sourceID != "" {
			query.Set("source_device_id", sourceID)
		}
		code, raw, _, err := s.deviceRequest(ctx, target, "GET", "/tasks?"+query.Encode(), nil)
		if err != nil {
			return nil, false, err
		}
		if code != 200 {
			return nil, false, fmt.Errorf("device returned %d: %s", code, strings.TrimSpace(string(raw)))
		}
		var page struct {
			Tasks      []deviceexec.Task `json:"tasks"`
			NextOffset int64             `json:"next_offset"`
			HasMore    bool              `json:"has_more"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, false, err
		}
		for _, task := range page.Tasks {
			encoded, _ := json.Marshal(task)
			if used+len(encoded)+1 > 256<<10 || len(tasks) >= 200 {
				return tasks, true, nil
			}
			used += len(encoded) + 1
			tasks = append(tasks, task)
		}
		if !page.HasMore {
			return tasks, false, nil
		}
		if page.NextOffset <= offset {
			return nil, false, errors.New("device returned a non-advancing task page")
		}
		offset = page.NextOffset
	}
	return tasks, true, nil
}
func (s *Server) accountDeviceDetail(w http.ResponseWriter, r *http.Request) {
	d, ok := s.accountDeviceMember(w, r)
	if !ok {
		return
	}
	tasks := []deviceexec.Task{}
	truncated := false
	if d.Online {
		var err error
		tasks, truncated, err = s.deviceTasks(r.Context(), d.ID, "", "")
		if err != nil {
			writeErr(w, 502, err.Error())
			return
		}
	}
	writeJSON(w, 200, map[string]any{"device": s.accountDeviceRow(d), "tasks": tasks, "tasks_truncated": truncated})
}
func (s *Server) accountDeviceRename(w http.ResponseWriter, r *http.Request) {
	d, ok := s.accountDeviceMember(w, r)
	if !ok {
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if decode(r, &in) != nil {
		writeErr(w, 400, "bad name")
		return
	}
	if err := s.acct.client.RenameDevice(r.Context(), accountSession(r), d.ID, in.Name); err != nil {
		writeAccountErr(w, err)
		return
	}
	d.Name = strings.TrimSpace(in.Name)
	writeJSON(w, 200, map[string]any{"device": s.accountDeviceRow(d)})
}
func (s *Server) accountDeviceDelete(w http.ResponseWriter, r *http.Request) {
	d, ok := s.accountDeviceMember(w, r)
	if !ok {
		return
	}
	if err := s.acct.client.DeleteDevice(r.Context(), accountSession(r), d.ID); err != nil {
		writeAccountErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"unbound": true})
}
func (s *Server) accountDeviceTasks(w http.ResponseWriter, r *http.Request) {
	d, ok := s.accountDeviceMember(w, r)
	if !ok {
		return
	}
	path := "/tasks"
	if ref := r.PathValue("ref"); ref != "" {
		path += "/" + url.PathEscape(ref)
		if a := r.PathValue("artifact"); a != "" {
			path += "/artifacts/" + url.PathEscape(a)
		} else if r.Method == "POST" {
			path += "/stop"
		}
	}
	if r.URL.RawQuery != "" {
		path += "?" + r.URL.RawQuery
	}
	code, body, ct, err := s.deviceRequest(r.Context(), d.ID, r.Method, path, nil)
	if err != nil {
		writeErr(w, 502, err.Error())
		return
	}
	w.Header().Set("Content-Type", ct)
	w.WriteHeader(code)
	_, _ = w.Write(body)
}
func (s *Server) botDeviceTasks(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	binding, err := s.readBinding()
	if err != nil {
		writeErr(w, 500, "account binding unavailable")
		return
	}
	if binding.Key == "" {
		writeJSON(w, 200, map[string]any{"tasks": []deviceexec.Task{}})
		return
	}
	devices, err := s.boundDevices(r.Context())
	if err != nil {
		writeAccountErr(w, err)
		return
	}
	tasks := []deviceexec.Task{}
	truncated := false
	unavailable := []map[string]string{}
	for _, d := range devices {
		if !d.Online {
			unavailable = append(unavailable, map[string]string{"id": d.ID, "name": d.Name, "error": "device offline; task state unavailable"})
			continue
		}
		list, cut, err := s.deviceTasks(r.Context(), d.ID, b.Name(), binding.DeviceID)
		truncated = truncated || cut
		if err != nil {
			unavailable = append(unavailable, map[string]string{"id": d.ID, "name": d.Name, "error": err.Error()})
			continue
		}
		for _, t := range list {
			if t.Bot == b.Name() {
				tasks = append(tasks, t)
			}
		}
	}
	sort.SliceStable(tasks, func(i, j int) bool {
		if (tasks[i].Status == "running") != (tasks[j].Status == "running") {
			return tasks[i].Status == "running"
		}
		return tasks[i].StartedAt.After(tasks[j].StartedAt)
	})
	used := 2
	kept := 0
	for _, task := range tasks {
		encoded, _ := json.Marshal(task)
		if used+len(encoded)+1 > 512<<10 || kept >= 200 {
			truncated = true
			break
		}
		used += len(encoded) + 1
		kept++
	}
	tasks = tasks[:kept]
	writeJSON(w, 200, map[string]any{"tasks": tasks, "unavailable_devices": unavailable, "tasks_truncated": truncated})
}

func (s *Server) validateMessageDevice(ctx context.Context, id string) error {
	if id == "" {
		return nil
	}
	devices, err := s.boundDevices(ctx)
	if err != nil {
		return err
	}
	for _, d := range devices {
		if d.ID == id {
			if !d.Online {
				return errors.New("device is offline")
			}
			if len(d.Capabilities) == 0 {
				return errors.New("device has no available execution capabilities")
			}
			return nil
		}
	}
	return errors.New("device is not a member of this account")
}
