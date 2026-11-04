package account

import (
	"sort"
	"strings"
	"time"
)

// DeviceReport is installation metadata, not an access list for individual bots.
// Name initializes a new member; RenameDevice owns later name changes.
type DeviceReport struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Bots         []string `json:"bots"`
	OS           string   `json:"os"`
	Capabilities []string `json:"capabilities"`
	RunningCount int      `json:"running_count"`
}

// Binding is issued only by explicit account sign-in and device binding.
// Token is independent of UI sessions and is returned only at bind time.
type Binding struct {
	Token      string `json:"binding_token"`
	AccountKey string `json:"account_key"`
	DeviceID   string `json:"device_id"`
	ID         string `json:"binding_id"`
}

// BindingIdentity identifies the current generation of one installation member.
// Execution consumers must recheck membership for every operation.
type BindingIdentity struct {
	AccountKey string `json:"account_key"`
	DeviceID   string `json:"device_id"`
	ID         string `json:"binding_id"`
}

func cleanDeviceReport(in DeviceReport) (DeviceReport, error) {
	in.ID = strings.TrimSpace(in.ID)
	if len([]rune(in.ID)) > maxNameLen || in.RunningCount < 0 {
		return DeviceReport{}, ErrInput
	}
	in.Name, in.OS = clip(in.Name), clip(in.OS)
	in.Bots = cleanDeviceStrings(in.Bots, maxDeviceBots)
	in.Capabilities = cleanDeviceStrings(in.Capabilities, 16)
	return in, nil
}

func cleanDeviceStrings(in []string, limit int) []string {
	out := make([]string, 0, min(len(in), limit))
	for _, v := range in {
		if v = clip(v); v != "" {
			out = append(out, v)
			if len(out) == limit {
				break
			}
		}
	}
	return out
}

// BindDevice explicitly grants installation membership and replaces any old
// credential. Rebinding a revoked record creates a new execution identity.
func (s *Service) BindDevice(session string, report DeviceReport) (Binding, error) {
	report, err := cleanDeviceReport(report)
	if err != nil || report.ID == "" || report.Name == "" {
		return Binding{}, ErrInput
	}
	var out Binding
	err = s.store.Update(func(st *State) error {
		email, ok := s.sessionEmail(st, session)
		if !ok {
			return ErrSession
		}
		d := st.Devices[email+"/"+report.ID]
		if d == nil || !d.RevokedAt.IsZero() {
			if err := deviceCapacity(st, email); err != nil {
				return err
			}
		}
		token, err := randomToken()
		if err != nil {
			return err
		}
		id, err := randomToken()
		if err != nil {
			return err
		}
		if d == nil {
			d = &Device{ID: report.ID, Email: email, Name: report.Name}
			st.Devices[email+"/"+report.ID] = d
		}
		d.BindingID, d.BindingHash, d.RevokedAt = id, hashOf(token), time.Time{}
		s.recordMetadata(d, report)
		out = Binding{Token: token, AccountKey: hashOf("account:" + email)[:24], DeviceID: d.ID, ID: id}
		return nil
	})
	if err != nil {
		return Binding{}, err
	}
	return out, nil
}

// AuthenticateBinding accepts installation credentials only, never UI sessions.
func (s *Service) AuthenticateBinding(token string) (BindingIdentity, error) {
	var out BindingIdentity
	err := s.store.View(func(st *State) error {
		d := bindingDevice(st, token)
		if d == nil {
			return ErrBinding
		}
		out = BindingIdentity{AccountKey: hashOf("account:" + d.Email)[:24], DeviceID: d.ID, ID: d.BindingID}
		return nil
	})
	return out, err
}

func bindingDevice(st *State, token string) *Device {
	if token == "" || len(token) > 128 {
		return nil
	}
	want := hashOf(token)
	for _, d := range st.Devices {
		if d.RevokedAt.IsZero() && d.BindingID != "" && sameHash(d.BindingHash, want) {
			return d
		}
	}
	return nil
}

// ReportMetadata refreshes only the member named by the binding credential.
// An empty ID means that same member. It cannot rename or restore membership.
func (s *Service) ReportMetadata(token string, report DeviceReport) error {
	report, err := cleanDeviceReport(report)
	if err != nil {
		return err
	}
	return s.store.Update(func(st *State) error {
		d := bindingDevice(st, token)
		if d == nil {
			return ErrBinding
		}
		if report.ID != "" && report.ID != d.ID {
			return ErrInput
		}
		s.recordMetadata(d, report)
		return nil
	})
}

func (s *Service) recordMetadata(d *Device, report DeviceReport) {
	d.Bots, d.OS, d.Capabilities = report.Bots, report.OS, report.Capabilities
	d.RunningCount, d.LastSeen = report.RunningCount, s.cfg.Now()
}

// ReportDevice preserves the legacy initial-registration and heartbeat API.
// Existing names and richer metadata belong to their dedicated writers.
func (s *Service) ReportDevice(session, id, name string, bots []string) error {
	id, name = clip(id), clip(name)
	if id == "" || name == "" {
		return ErrInput
	}
	clean := cleanDeviceStrings(bots, maxDeviceBots)
	return s.store.Update(func(st *State) error {
		email, ok := s.sessionEmail(st, session)
		if !ok {
			return ErrSession
		}
		d := st.Devices[email+"/"+id]
		if d != nil && !d.RevokedAt.IsZero() {
			return ErrRevoked
		}
		if d == nil {
			if err := deviceCapacity(st, email); err != nil {
				return err
			}
			d = &Device{ID: id, Email: email, Name: name}
			st.Devices[email+"/"+id] = d
		}
		d.Bots, d.LastSeen = clean, s.cfg.Now()
		return nil
	})
}

func deviceCapacity(st *State, email string) error {
	n := 0
	for _, d := range st.Devices {
		if d.Email == email && d.RevokedAt.IsZero() {
			n++
		}
	}
	if n >= maxDevices {
		return ErrBusy
	}
	return nil
}

func (s *Service) RenameDevice(session, id, name string) error {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > maxNameLen {
		return ErrInput
	}
	return s.store.Update(func(st *State) error {
		email, ok := s.sessionEmail(st, session)
		if !ok {
			return ErrSession
		}
		d := st.Devices[email+"/"+id]
		if d == nil || !d.RevokedAt.IsZero() {
			return ErrDevice
		}
		d.Name = name
		return nil
	})
}

// DeleteDevice revokes the credential and keeps a tombstone for old heartbeats.
func (s *Service) DeleteDevice(session, id string) error {
	return s.store.Update(func(st *State) error {
		email, ok := s.sessionEmail(st, session)
		if !ok {
			return ErrSession
		}
		d := st.Devices[email+"/"+id]
		if d == nil {
			return ErrDevice
		}
		if d.RevokedAt.IsZero() {
			d.RevokedAt = s.cfg.Now()
			d.BindingHash, d.BindingID = "", ""
		}
		return nil
	})
}

// Devices lists active members for a UI session, online members first.
func (s *Service) Devices(session string) ([]DeviceView, error) {
	out := []DeviceView{}
	err := s.store.View(func(st *State) error {
		email, ok := s.sessionEmail(st, session)
		if !ok {
			return ErrSession
		}
		out = s.deviceViews(st, email)
		return nil
	})
	return out, err
}

// BindingDevices lists the current shared pool for a bound installation.
// Bots is descriptive metadata and does not limit access to this pool.
func (s *Service) BindingDevices(token string) ([]DeviceView, error) {
	out := []DeviceView{}
	err := s.store.View(func(st *State) error {
		d := bindingDevice(st, token)
		if d == nil {
			return ErrBinding
		}
		out = s.deviceViews(st, d.Email)
		return nil
	})
	return out, err
}

func (s *Service) deviceViews(st *State, email string) []DeviceView {
	out := []DeviceView{}
	now := s.cfg.Now()
	for _, d := range st.Devices {
		if d.Email == email && d.RevokedAt.IsZero() {
			out = append(out, DeviceView{
				ID: d.ID, Name: d.Name, Online: now.Sub(d.LastSeen) < s.cfg.OnlineFor,
				Bots: append([]string{}, d.Bots...), OS: d.OS,
				Capabilities: append([]string{}, d.Capabilities...), LastSeen: d.LastSeen,
				RunningCount: d.RunningCount, BindingID: d.BindingID,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Online != out[j].Online {
			return out[i].Online
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	return out
}
