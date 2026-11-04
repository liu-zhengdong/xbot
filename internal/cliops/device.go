package cliops

import (
	"context"
	"encoding/json"
	"errors"
	"xbot/internal/deviceexec"
)

// DeviceRequest selects a target. It never carries the source identity.
type DeviceRequest struct {
	DeviceID   string          `json:"device_id,omitempty"`
	Action     string          `json:"action"`
	Ref        string          `json:"ref,omitempty"`
	Call       deviceexec.Call `json:"call,omitempty"`
	Offset     int             `json:"offset,omitempty"`
	Limit      int             `json:"limit,omitempty"`
	ArtifactID string          `json:"artifact_id,omitempty"`
}

type DeviceHandler func(context.Context, DeviceRequest) (json.RawMessage, error)

// DeviceReadAction excludes every interpreter, mutation and desktop operation.
func DeviceReadAction(action string) bool {
	switch action {
	case "list", "read", "tasks", "task", "artifact":
		return true
	}
	return false
}

func (in DeviceRequest) Validate() error {
	switch in.Action {
	case "list", "tasks":
	case "exec", "read", "write", "desktop", "browser":
		if in.Call.Ref == "" || in.Ref != in.Call.Ref || in.Call.Kind != in.Action {
			return errors.New("device: call requires the same stable ref and kind as the request")
		}
		if in.Action != "exec" && in.Call.Tool == "" && len(in.Call.Steps) == 0 {
			return errors.New("device: call requires a tool or steps")
		}
	case "task", "stop":
		if in.Ref == "" {
			return errors.New("device: task reference is required")
		}
	case "artifact":
		if in.Ref == "" || in.ArtifactID == "" {
			return errors.New("device: artifact requires task reference and artifact id")
		}
	default:
		return errors.New("device: unknown action")
	}
	if in.Offset < 0 || in.Limit < 0 {
		return errors.New("device: offset and limit must be nonnegative")
	}
	return nil
}
