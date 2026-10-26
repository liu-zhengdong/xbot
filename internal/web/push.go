package web

import (
	"context"
	"net/http"
	"time"

	"xbot/internal/bot"
	"xbot/internal/notify"
)

const tabWindow = 12 * time.Second

type tabState struct {
	n    int
	last time.Time
}

func (s *Server) touchTab(owner string) {
	if owner == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tabState(owner).last = time.Now()
}

func (s *Server) enterTab(owner string) {
	if owner == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.tabState(owner)
	st.n++
	st.last = time.Now()
}

func (s *Server) leaveTab(owner string) {
	if owner == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.tabState(owner)
	if st.n > 0 {
		st.n--
	}
	st.last = time.Now()
}

func (s *Server) tabLive(owner string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.tabs[owner]
	if st == nil {
		return false
	}
	if st.n > 0 {
		return true
	}
	return time.Since(st.last) < tabWindow
}

func (s *Server) tabState(owner string) *tabState {
	if s.tabs == nil {
		s.tabs = map[string]*tabState{}
	}
	st := s.tabs[owner]
	if st == nil {
		st = &tabState{}
		s.tabs[owner] = st
	}
	return st
}

func (s *Server) pushClient() *http.Client {
	if s.pushHTTP != nil {
		return s.pushHTTP
	}
	return &http.Client{Timeout: 4 * time.Second}
}

func (s *Server) onNote(botName string, n notify.Note) {
	b, err := bot.Load(s.Root, botName)
	if err != nil {
		return
	}
	for _, owner := range b.Config().Owners {
		if s.tabLive(owner) {
			continue
		}
		subs, err := notify.Subscriptions(s.Root, owner)
		if err != nil {
			continue
		}
		for _, sub := range subs {
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			err := notify.Send(ctx, s.Root, sub, notify.Message{
				Title: botName,
				Body:  n.Text,
				Tag:   n.ID,
				Bot:   botName,
			}, s.pushClient())
			cancel()
			if notify.Gone(err) {
				_ = notify.RemoveSubscription(s.Root, owner, sub.Endpoint)
			}
		}
	}
}

func (s *Server) pushStatus(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	pub, err := notify.PublicKey(s.Root)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	subs, err := notify.Subscriptions(s.Root, s.owner)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"public_key": pub,
		"subscribed": len(subs) > 0,
	})
}

func (s *Server) pushSubscribe(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	var in struct {
		Endpoint string `json:"endpoint"`
		P256dh   string `json:"p256dh"`
		Auth     string `json:"auth"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	err := notify.SaveSubscription(s.Root, s.owner, notify.Subscription{
		Endpoint: in.Endpoint, P256dh: in.P256dh, Auth: in.Auth,
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) pushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	var in struct {
		Endpoint string `json:"endpoint"`
	}
	if r.ContentLength != 0 {
		if err := decode(r, &in); err != nil {
			writeErr(w, http.StatusBadRequest, "bad request")
			return
		}
	}
	if err := notify.RemoveSubscription(s.Root, s.owner, in.Endpoint); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
