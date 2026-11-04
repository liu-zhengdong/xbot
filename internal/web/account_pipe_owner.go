package web

import (
	"errors"
	"net/http"
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
	return s.AuthorizeOwner(r)
}
