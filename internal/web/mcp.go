package web

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path"
	"strings"

	"xbot/internal/cliops"
	"xbot/internal/config"
	"xbot/internal/mcp"
)

// The existing UI keeps its bot-local configuration shape while delegating real
// runtime saves to the same resource methods used by the CLI.
func (s *Server) listMCP(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	resources, err := s.ResourcesFor(b)
	if err != nil {
		writeMCPErr(w, err)
		return
	}
	if resources != nil {
		reply, err := resources.MCPList(r.Context(), "bot")
		if err != nil {
			writeMCPErr(w, err)
			return
		}
		if len(reply.Data) != 0 {
			writeJSON(w, http.StatusOK, reply.Data)
		} else {
			writeJSON(w, http.StatusOK, reply)
		}
		return
	}
	// Older fixtures have no runtime factory. Preserve their file-only setup.
	list, err := mcp.LoadMCP(b.MCPPath(), mcp.Options{IsProviderKey: config.ProviderKey})
	if err != nil {
		writeMCPErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"servers": list})
}

func (s *Server) addMCP(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	var in mcp.Spec
	if err := decodeMCPRequest(w, r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	resources, err := s.ResourcesFor(b)
	if err != nil {
		writeMCPErr(w, err)
		return
	}
	if resources != nil {
		reply, err := resources.MCPAdd(r.Context(), "bot", in)
		if err != nil {
			writeMCPErr(w, err)
			return
		}
		in.Name = strings.TrimSpace(in.Name)
		writeJSON(w, http.StatusOK, struct {
			mcp.Spec
			Saved        bool   `json:"saved"`
			State        string `json:"state"`
			RuntimeError string `json:"runtime_error,omitempty"`
		}{in, reply.Saved, "configured", reply.RuntimeError})
		return
	}
	if err := mcp.AddMCP(b.MCPPath(), in, mcp.Options{IsProviderKey: config.ProviderKey}); err != nil {
		writeMCPErr(w, err)
		return
	}
	s.refreshMCP()
	writeJSON(w, http.StatusOK, in)
}

func (s *Server) removeMCP(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	name := r.PathValue("server")
	resources, err := s.ResourcesFor(b)
	if err != nil {
		writeMCPErr(w, err)
		return
	}
	if resources != nil {
		reply, err := resources.MCPRemove(r.Context(), "bot", name)
		if err != nil {
			writeMCPErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"name": name, "saved": reply.Saved, "runtime_error": reply.RuntimeError})
		return
	}
	if err := mcp.RemoveMCP(b.MCPPath(), name, mcp.Options{IsProviderKey: config.ProviderKey}); err != nil {
		writeMCPErr(w, err)
		return
	}
	s.refreshMCP()
	writeJSON(w, http.StatusOK, map[string]string{"name": name})
}

// mcpRuntime is the human, owner-authenticated counterpart of the source CLI
// routes. It borrows existing resources and never creates a Kernel or Session.
func (s *Server) mcpRuntime(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	name := r.PathValue("name")
	if name == "" {
		name = r.PathValue("bot")
	}
	b, err := s.owned(name)
	if err != nil {
		writeMissing(w, err)
		return
	}
	if supplied := r.Header.Get("X-XBot-Root"); supplied != "" {
		root, err := cliops.CanonicalRoot(s.Root)
		if err != nil || supplied != root {
			writeErr(w, http.StatusConflict, "runtime data root mismatch")
			return
		}
	}
	resources, err := s.ResourcesFor(b)
	if err != nil {
		writeMCPErr(w, err)
		return
	}
	if resources == nil {
		writeErr(w, http.StatusServiceUnavailable, "MCP runtime is unavailable")
		return
	}
	cliops.ServeMCP(w, r, resources.MCPHandlers(), path.Base(r.URL.Path))
}

func decodeMCPRequest(w http.ResponseWriter, r *http.Request, value any) error {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, cliops.MCPMaxRequest)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(value); err != nil {
		return err
	}
	if err := dec.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("mcp: expected one JSON object")
	}
	return nil
}

func writeMCPErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, mcp.ErrMCPExists):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, mcp.ErrMCPMissing):
		writeErr(w, http.StatusNotFound, "not found")
	default:
		message := config.RedactHeld(err.Error())
		if len(message) > 4096 {
			message = message[:4096]
		}
		writeErr(w, http.StatusBadRequest, message)
	}
}
