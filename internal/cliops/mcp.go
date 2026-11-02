package cliops

import (
	"context"
	"encoding/json"

	"xbot/internal/mcp"
)

const MCPMaxReply = 64 << 10
const MCPMaxRequest = 1 << 20

type MCPCall struct {
	Server   string          `json:"server"`
	Tool     string          `json:"tool"`
	Args     json.RawMessage `json:"args"`
	ReadOnly bool            `json:"read_only,omitempty"`
}

type MCPArtifact struct {
	Type     string `json:"type"`
	MIMEType string `json:"mime_type,omitempty"`
	File     string `json:"file"`
	Access   string `json:"access"`
}

type MCPContent struct {
	Type            string `json:"type"`
	Text            string `json:"text,omitempty"`
	Preview         string `json:"preview,omitempty"`
	MIMEType        string `json:"mime_type,omitempty"`
	URI             string `json:"uri,omitempty"`
	File            string `json:"file,omitempty"`
	Access          string `json:"access,omitempty"`
	ProjectionError string `json:"projection_error,omitempty"`
}

type MCPReply struct {
	Data             json.RawMessage `json:"data,omitempty"`
	Content          []MCPContent    `json:"content,omitempty"`
	Raw              *MCPArtifact    `json:"raw,omitempty"`
	Structured       *MCPArtifact    `json:"structured_content,omitempty"`
	Artifact         *MCPArtifact    `json:"artifact,omitempty"`
	IsError          bool            `json:"is_error"`
	Saved            bool            `json:"saved,omitempty"`
	RuntimeError     string          `json:"runtime_error,omitempty"`
	ProjectionErrors []string        `json:"projection_errors,omitempty"`
}

type MCPHandlers struct {
	List     func(context.Context, string) (MCPReply, error)
	Add      func(context.Context, string, mcp.Spec) (MCPReply, error)
	Remove   func(context.Context, string, string) (MCPReply, error)
	Tools    func(context.Context, string) (MCPReply, error)
	Describe func(context.Context, string, string) (MCPReply, error)
	Call     func(context.Context, MCPCall) (MCPReply, error)
}

type Handlers struct {
	Profile       func(*Update) (Profile, error)
	ProfileAvatar func(data []byte, clear bool) (Profile, error)
	MCP           *MCPHandlers
	Skills        *SkillHandlers
	Routines      *RoutineHandlers
	Bots          *BotHandlers
	ChatGroups    *ChatGroupHandlers
	PeerMessage   func(context.Context, PeerMessage) (PeerMessageReply, error)
	Experiences   *ExperienceHandlers
	Recall        func(context.Context, MemoryRecall) (MemoryRecallReply, error)
}
