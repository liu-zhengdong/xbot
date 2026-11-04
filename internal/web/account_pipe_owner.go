package web

import (
	"errors"
	"net/http"

	"xbot/internal/deviceidentity"
)

// AuthorizeAccountPipe checks membership before attaching the console credential.
func (s *Server) AuthorizeAccountPipe(token string, r *http.Request) error {
	b, err := s.readBinding()
	if err != nil {
		return err
	}
	identity, err := s.acct.client.AuthenticateBinding(r.Context(), token)
	if err != nil {
		return err
	}
	if identity.AccountKey != b.Key || identity.DeviceID != b.DeviceID || identity.ID != b.BindingID {
		return errors.New("device membership changed")
	}
	if session := deviceidentity.ConsoleSession(r.Context()); session != "" {
		me, err := s.acct.client.Me(r.Context(), session)
		if err != nil {
			return err
		}
		if !me.Verified || me.Key != identity.AccountKey {
			return errors.New("console account changed")
		}
		r.AddCookie(&http.Cookie{Name: accountCookie, Value: session})
	}
	return s.AuthorizeOwner(r)
}
