package vault

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
)

// OAuthTransport refreshes only the selected credential, then delegates to the
// existing vault egress. It never obtains plaintext from a daemon backend.
func OAuthTransport(b Backend, name string) http.RoundTripper {
	return &oauthTransport{backend: b, name: name}
}

type oauthTransport struct {
	backend Backend
	name    string
}

type oauthModelKey struct{}

func (t *oauthTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Header.Get("Authorization") != "Bearer {{secret:"+t.name+"}}" {
		return nil, errors.New("vault: OAuth authorization mismatch")
	}
	if _, err := EnsureOAuth(r.Context(), t.backend, t.name); err != nil {
		return nil, err
	}
	backend := t.backend
	if s, ok := backend.(*Store); ok {
		// Freeze the values used by this request so another login/refresh cannot
		// remove its redaction forms while its response is still streaming.
		backend = s.snapshot()
	}
	r = r.WithContext(context.WithValue(r.Context(), oauthModelKey{}, true))
	res, err := backend.Transport(nil).RoundTrip(r)
	if err != nil {
		return nil, err
	}
	if s, ok := backend.(*Store); ok {
		for k, values := range res.Header {
			for i, v := range values {
				values[i] = s.Redact(v)
			}
			res.Header[k] = values
		}
		res.Body = &oauthBody{source: res.Body, reader: bufio.NewReaderSize(io.LimitReader(res.Body, maxForwardResp), 64<<10), store: s}
		res.ContentLength = -1
		res.Header.Del("Content-Length")
	}
	return res, nil
}

func (s *Store) snapshot() *Store {
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := &Store{secrets: map[string]Secret{}}
	for name, sec := range s.secrets {
		copy.secrets[name] = sec
	}
	return copy
}

type oauthFlushWriter struct{ w http.ResponseWriter }

func (f oauthFlushWriter) Write(p []byte) (int, error) {
	n, err := f.w.Write(p)
	if flusher, ok := f.w.(http.Flusher); ok {
		flusher.Flush()
	}
	return n, err
}

// Keep only a suffix that could still become a secret, so long ID tokens do
// not delay ordinary SSE deltas. ReadSlice bounds each read to 64 KiB.
type oauthBody struct {
	source  io.ReadCloser
	reader  *bufio.Reader
	store   *Store
	pending []byte
	err     error
	tail    string
}

func (b *oauthBody) Read(p []byte) (int, error) {
	for len(b.pending) == 0 && b.err == nil {
		line, err := b.reader.ReadSlice('\n')
		if err == bufio.ErrBufferFull {
			err = nil
		}
		b.err = err
		red := b.store.Redact(b.tail + string(line))
		b.tail = ""
		if err == nil {
			cut := len(red)
			for _, form := range b.store.Forms() {
				for at := strings.LastIndex(red, form[:1]); at >= 0; at = strings.LastIndex(red[:at], form[:1]) {
					tail := red[at:]
					prefix := len(tail) < len(form) && strings.HasPrefix(form, tail)
					// Existing vault redaction also recognizes fixed-width folded
					// base64. Preserve its incomplete multi-line candidates.
					if !prefix && (at == 0 || red[at-1] == '\n') && len(form) >= minStreamForm && base64Line(form) && strings.Contains(tail, "\n") {
						flat := strings.NewReplacer("\r", "", "\n", "").Replace(tail)
						prefix = len(flat) >= minWrapLine && len(flat) < len(form) && strings.HasPrefix(form, flat)
					}
					if prefix {
						if at < cut {
							cut = at
						}
					}
				}
			}
			b.tail = red[cut:]
			red = red[:cut]
		} else {
			red = b.store.MaskTruncated(red)
		}
		b.pending = []byte(red)
	}
	if len(b.pending) > 0 {
		n := copy(p, b.pending)
		b.pending = b.pending[n:]
		return n, nil
	}
	return 0, b.err
}
func (b *oauthBody) Close() error { return b.source.Close() }
