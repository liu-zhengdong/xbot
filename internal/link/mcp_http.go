package link

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// openStreamable speaks MCP over one HTTP endpoint. POST carries each JSON-RPC
// message. A JSON body or an SSE data line is written back as one line so the
// existing read loop can dispatch it. GET, after a session id, carries server
// notifications. A 405 means the server has no inbound stream.
func openStreamable(ctx context.Context, spec MCPServer) (io.ReadWriteCloser, error) {
	if err := validateURL(spec.URL); err != nil {
		return nil, err
	}
	pr, pw := io.Pipe()
	h := &httpConn{
		ctx:     ctx,
		url:     strings.TrimSpace(spec.URL),
		hc:      &http.Client{},
		headers: secretHeaders(spec.SecretEnvs, spec.secretLookup()),
		pr:      pr,
		pw:      pw,
	}
	return h, nil
}

func secretHeaders(names []string, lookup func(string) (string, bool)) http.Header {
	h := make(http.Header)
	if lookup == nil {
		return h
	}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || providerKey(name) {
			continue
		}
		value, ok := lookup(name)
		if !ok || value == "" {
			continue
		}
		h.Set(name, value)
	}
	return h
}

type httpConn struct {
	ctx       context.Context
	url       string
	hc        *http.Client
	headers   http.Header
	pr        *io.PipeReader
	pw        *io.PipeWriter
	mu        sync.Mutex
	session   string
	listen    bool
	pushMu    sync.Mutex
	closeOnce sync.Once
	closeErr  error
}

func (h *httpConn) Read(p []byte) (int, error) { return h.pr.Read(p) }

func (h *httpConn) Write(p []byte) (int, error) {
	body := bytes.TrimSpace(p)
	if len(body) == 0 {
		return len(p), nil
	}
	req, err := http.NewRequestWithContext(h.ctx, http.MethodPost, h.url, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", mcpProtocol)
	h.mu.Lock()
	if h.session != "" {
		req.Header.Set("Mcp-Session-Id", h.session)
	}
	for key, values := range h.headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	h.mu.Unlock()
	res, err := h.hc.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	if id := strings.TrimSpace(res.Header.Get("Mcp-Session-Id")); id != "" {
		h.noteSession(id)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return 0, err
	}
	if res.StatusCode == http.StatusAccepted || res.StatusCode == http.StatusNoContent {
		return len(p), nil
	}
	if res.StatusCode >= 300 {
		return 0, fmt.Errorf("mcp: http %d", res.StatusCode)
	}
	ctype := res.Header.Get("Content-Type")
	if strings.Contains(ctype, "text/event-stream") {
		for _, payload := range ssePayloads(raw) {
			if err := h.pushJSON(payload); err != nil {
				return 0, err
			}
		}
		return len(p), nil
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return len(p), nil
	}
	if err := h.pushJSON(raw); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (h *httpConn) noteSession(id string) {
	h.mu.Lock()
	h.session = id
	start := !h.listen
	h.listen = true
	h.mu.Unlock()
	if start {
		go h.listenGET()
	}
}

func (h *httpConn) listenGET() {
	req, err := http.NewRequestWithContext(h.ctx, http.MethodGet, h.url, nil)
	if err != nil {
		return
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("MCP-Protocol-Version", mcpProtocol)
	h.mu.Lock()
	if h.session != "" {
		req.Header.Set("Mcp-Session-Id", h.session)
	}
	for key, values := range h.headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	h.mu.Unlock()
	res, err := h.hc.Do(req)
	if err != nil {
		return
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusMethodNotAllowed || res.StatusCode == http.StatusNotFound {
		return
	}
	if res.StatusCode >= 300 {
		return
	}
	sc := bufio.NewScanner(res.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	var data []string
	flush := func() {
		if len(data) == 0 {
			return
		}
		_ = h.pushJSON([]byte(strings.Join(data, "\n")))
		data = nil
	}
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	flush()
}

func (h *httpConn) pushJSON(raw []byte) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return nil
	}
	h.pushMu.Lock()
	defer h.pushMu.Unlock()
	_, err := h.pw.Write(append(buf.Bytes(), '\n'))
	return err
}

func (h *httpConn) Close() error {
	h.closeOnce.Do(func() {
		h.closeErr = h.pw.Close()
	})
	return h.closeErr
}

func ssePayloads(raw []byte) [][]byte {
	var out [][]byte
	var data []string
	flush := func() {
		if len(data) == 0 {
			return
		}
		out = append(out, []byte(strings.Join(data, "\n")))
		data = nil
	}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	flush()
	return out
}
