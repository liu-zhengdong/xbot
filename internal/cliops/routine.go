package cliops

import "context"

const RoutineMaxRequest = 1 << 20
const RoutineMaxReply = 64 << 10

// RoutineCreate is the typed input shared by routine creation and its review.
type RoutineCreate struct {
	ID       string   `json:"id,omitempty"`
	Name     string   `json:"name"`
	Prompt   string   `json:"prompt"`
	Cron     string   `json:"cron,omitempty"`
	At       string   `json:"at,omitempty"`
	Precheck *string  `json:"precheck,omitempty"`
	Skills   []string `json:"skills,omitempty"`
}

// RoutinePatch preserves omission versus explicit false, empty string or [] for
// fields that can be cleared. JSON null has the same meaning as omission.
type RoutinePatch struct {
	Name     string    `json:"name,omitempty"`
	Prompt   string    `json:"prompt,omitempty"`
	Cron     string    `json:"cron,omitempty"`
	Enabled  *bool     `json:"enabled,omitempty"`
	Precheck *string   `json:"precheck,omitempty"`
	Skills   *[]string `json:"skills,omitempty"`
}

type RoutineWriteReply struct {
	ID      string `json:"id"`
	Applied bool   `json:"applied"`
	Stage   string `json:"stage"`
	Error   string `json:"error,omitempty"`
}

// RoutineView describes persisted routine state without depending on the Timer.
type RoutineView struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Prompt   string   `json:"prompt"`
	Precheck string   `json:"precheck,omitempty"`
	Skills   []string `json:"skills,omitempty"`
	Cron     string   `json:"cron,omitempty"`
	At       string   `json:"at,omitempty"`
	Enabled  bool     `json:"enabled"`
	LastSlot string   `json:"last_slot,omitempty"`
	Zone     string   `json:"zone,omitempty"`
}

type RoutineHandlers struct {
	Create func(context.Context, RoutineCreate) (RoutineWriteReply, error)
	List   func(context.Context) (RoutineListReply, error)
	Update func(context.Context, string, RoutinePatch) (RoutineWriteReply, error)
	Delete func(context.Context, string) (RoutineWriteReply, error)
}

type RoutineListReply struct {
	Routines *[]RoutineView `json:"routines,omitempty"`
	Count    int            `json:"count"`
	File     string         `json:"file,omitempty"`
	Access   string         `json:"access,omitempty"`
	Bytes    int64          `json:"bytes,omitempty"`
	Error    string         `json:"error,omitempty"`
}
