// Package deviceexec runs authenticated device calls through the existing tools Jobs.
package deviceexec

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

const (
	DefaultOutputLimit = 16 << 10
	MaxOutputLimit     = 32 << 10
	MaxArtifactChunk   = 256 << 10
	MaxRequestBytes    = 512 << 10
)

var (
	ErrNotFound = errors.New("device execution not found")
	ErrReadonly = errors.New("research context only permits reads and snapshots")
	ErrInvalid  = errors.New("invalid device execution request")
	ErrConflict = errors.New("device execution reference already names another call")
)

// Source comes from an authenticated runtime or pipe. DeviceID names the source installation.
type Source struct {
	Account   string `json:"account"`
	DeviceID  string `json:"device_id"`
	Bot       string `json:"bot"`
	ContextID string `json:"context_id"`
	Readonly  bool   `json:"readonly,omitempty"`
	// Owner is set only by trusted middleware for account-wide task management.
	Owner bool `json:"-"`
}

type Operation struct {
	Tool string          `json:"tool"`
	Args json.RawMessage `json:"args"`
}

// Call.Ref must be generated before sending. Retries use the same complete call.
// Steps is a desktop operation sequence. The manager holds its desktop lock for all steps.
// Artifacts names files to expose through bounded downloads after execution.
type Call struct {
	Ref         string          `json:"ref"`
	Kind        string          `json:"kind"`
	Tool        string          `json:"tool,omitempty"`
	Description string          `json:"description"`
	Args        json.RawMessage `json:"args,omitempty"`
	Steps       []Operation     `json:"steps,omitempty"`
	Artifacts   []string        `json:"artifacts,omitempty"`
}

type Artifact struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

type Task struct {
	ID          string     `json:"id"`
	Ref         string     `json:"ref"`
	DeviceID    string     `json:"device_id"`
	DeviceName  string     `json:"device_name"`
	Bot         string     `json:"bot"`
	ContextID   string     `json:"context_id"`
	Kind        string     `json:"kind"`
	Description string     `json:"description"`
	Status      string     `json:"status"`
	StartedAt   time.Time  `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
	Error       string     `json:"error,omitempty"`
	Artifacts   []Artifact `json:"artifacts"`
}

type Detail struct {
	Task          Task   `json:"task"`
	Output        string `json:"output"`
	OutputExpired bool   `json:"output_expired,omitempty"`
	Offset        int64  `json:"offset"`
	NextOffset    int64  `json:"next_offset"`
	Total         int64  `json:"total"`
	DroppedBefore int64  `json:"dropped_before,omitempty"`
}

// Chunk.Data is raw bytes in Go and the HTTP artifact route. It is not a conversation DTO.
type Chunk struct {
	Data       []byte `json:"-"`
	Offset     int64  `json:"offset"`
	NextOffset int64  `json:"next_offset"`
	Total      int64  `json:"total"`
}

func NewRef() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "dc_" + hex.EncodeToString(b[:]), nil
}

type sourceKey struct{}

// WithSource binds a trusted identity. HTTP bodies and headers cannot set it.
func WithSource(ctx context.Context, source Source) context.Context {
	return context.WithValue(ctx, sourceKey{}, source)
}

func SourceFromContext(ctx context.Context) (Source, bool) {
	source, ok := ctx.Value(sourceKey{}).(Source)
	return source, ok
}
