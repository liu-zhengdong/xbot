package deviceexec

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Handler only accepts Source installed by trusted middleware with WithSource.
// Mount it behind pipe account/member authentication with http.StripPrefix.
// Routes: POST /calls, GET /tasks, GET /tasks/{ref}, POST /tasks/{ref}/stop,
// and GET /tasks/{ref}/artifacts/{id}. Query offsets count raw output bytes.
func (m *Manager) Handler() http.Handler { return http.HandlerFunc(m.serveHTTP) }

// HandlerFor binds a caller-authenticated identity. It does no authentication itself.
func (m *Manager) HandlerFor(source Source) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.serveHTTP(w, r.WithContext(WithSource(r.Context(), source)))
	})
}

func (m *Manager) serveHTTP(w http.ResponseWriter, r *http.Request) {
	source, ok := SourceFromContext(r.Context())
	if !ok || source.Account == "" || (!source.Owner && (source.DeviceID == "" || source.Bot == "" || source.ContextID == "")) {
		http.Error(w, "trusted device source required", http.StatusUnauthorized)
		return
	}
	parts := strings.Split(strings.Trim(r.URL.EscapedPath(), "/"), "/")
	for i, part := range parts {
		decoded, err := url.PathUnescape(part)
		if err != nil {
			http.Error(w, "invalid route encoding", http.StatusBadRequest)
			return
		}
		parts[i] = decoded
	}
	var result any
	var err error
	status := http.StatusOK
	switch {
	case r.Method == http.MethodPost && len(parts) == 1 && parts[0] == "calls":
		r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBytes)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var call Call
		if err = decoder.Decode(&call); err != nil {
			http.Error(w, "invalid call body", http.StatusBadRequest)
			return
		}
		if err = decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
			http.Error(w, "one call body required", http.StatusBadRequest)
			return
		}
		var task Task
		task, err = m.Submit(r.Context(), source, call)
		result = struct {
			Task Task `json:"task"`
		}{task}
		status = http.StatusAccepted
	case r.Method == http.MethodGet && len(parts) == 1 && parts[0] == "tasks":
		offset, limit, e := queryRange(r)
		if e != nil {
			err = e
			break
		}
		filter := source
		if source.Owner {
			filter.DeviceID = r.URL.Query().Get("source_device_id")
			filter.Bot = r.URL.Query().Get("bot")
			filter.ContextID = ""
		}
		page := m.ListPage(&filter, offset, limit)
		for {
			body, _ := json.Marshal(page)
			if len(body)+1 <= MaxTaskPageBytes {
				break
			}
			if len(page.Tasks) <= 1 {
				http.Error(w, "task identity exceeds page budget", http.StatusInternalServerError)
				return
			}
			page.Tasks = page.Tasks[:len(page.Tasks)-1]
			page.NextOffset = page.Offset + int64(len(page.Tasks))
			page.HasMore = true
		}
		result = page
	case len(parts) >= 2 && parts[0] == "tasks":
		ref := parts[1]
		switch {
		case r.Method == http.MethodGet && len(parts) == 2:
			offset, limit, e := queryRange(r)
			if e != nil {
				err = e
				break
			}
			result, err = m.Detail(source, ref, offset, limit)
		case r.Method == http.MethodPost && len(parts) == 3 && parts[2] == "stop":
			var task Task
			task, err = m.Stop(source, ref)
			result = struct {
				Task Task `json:"task"`
			}{task}
		case r.Method == http.MethodGet && len(parts) == 4 && parts[2] == "artifacts":
			offset, limit, e := queryRange(r)
			if e != nil {
				err = e
				break
			}
			var chunk Chunk
			chunk, err = m.Download(source, ref, parts[3], offset, limit)
			if err != nil {
				break
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("X-Offset", strconv.FormatInt(chunk.Offset, 10))
			w.Header().Set("X-Next-Offset", strconv.FormatInt(chunk.NextOffset, 10))
			w.Header().Set("X-Total-Size", strconv.FormatInt(chunk.Total, 10))
			w.Header().Set("Content-Length", strconv.Itoa(len(chunk.Data)))
			_, _ = w.Write(chunk.Data)
			return
		default:
			http.NotFound(w, r)
			return
		}
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		code := http.StatusInternalServerError
		switch {
		case errors.Is(err, ErrNotFound):
			code = http.StatusNotFound
		case errors.Is(err, ErrReadonly):
			code = http.StatusForbidden
		case errors.Is(err, ErrInvalid):
			code = http.StatusBadRequest
		case errors.Is(err, ErrConflict):
			code = http.StatusConflict
		}
		http.Error(w, err.Error(), code)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(result)
}

func queryRange(r *http.Request) (int64, int, error) {
	var offset int64
	var limit int
	var err error
	if value := r.URL.Query().Get("offset"); value != "" {
		offset, err = strconv.ParseInt(value, 10, 64)
		if err != nil || offset < 0 {
			return 0, 0, fmt.Errorf("%w: offset", ErrInvalid)
		}
	}
	if value := r.URL.Query().Get("limit"); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil || limit < 0 {
			return 0, 0, fmt.Errorf("%w: limit", ErrInvalid)
		}
	}
	return offset, limit, nil
}
