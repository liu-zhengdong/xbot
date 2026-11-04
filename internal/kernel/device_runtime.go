package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/deviceexec"
	"xbot/internal/inbox"
	"xbot/internal/work"
)

type deviceChildKey struct{}
type deviceChildContext struct {
	ID, DeviceID string
	Readonly     bool
}

func (k *Kernel) selectedDevice(ctx context.Context) string {
	if bound, ok := ctx.Value(deviceChildKey{}).(deviceChildContext); ok {
		return bound.DeviceID
	}
	t, ok := ctx.Value(turnKey{}).(*turn)
	if !ok || t == nil {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.turnDevice
}

// Only new calls see a later owner's selection. Prepared calls hold a copy.
func (t *turn) selectDevice(ev inbox.Event) {
	if ev.Source != inbox.NameWeb && ev.Source != inbox.NameTerminal {
		return
	}
	t.mu.Lock()
	t.turnDevice = ev.DeviceID
	t.mu.Unlock()
}

func deviceSelectionNote(ev inbox.Event) string {
	if ev.DeviceID == "" {
		return ""
	}
	return "\n\n<system-reminder>本条消息选择设备 " + strconv.Quote(ev.DeviceID) + "。用当前实例 device --self CLI 执行该设备任务；默认目标来自本条消息。普通本机 Shell/文件工具仍作用对话主机。已受理工具的设备不会被后续消息改变。设备离线必须报告失败。</system-reminder>"
}

func (k *Kernel) deviceRuntime(ctx context.Context, in cliops.DeviceRequest) (json.RawMessage, error) {
	release := k.holdOperations()
	defer release()
	prepared, cleanup, err := k.runtimeSourceCall(ctx)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	frozen, ok := prepared.Value(turnKey{}).(*turn)
	if !ok || frozen == nil {
		return nil, errors.New("device: frozen source turn unavailable")
	}
	source := prepared.Value(routineCallSourceKey{}).(routineCallSource).device
	if source.Bot == "" || source.ContextID == "" {
		return nil, errors.New("device: prepared execution identity unavailable")
	}
	if source.Readonly && !cliops.DeviceReadAction(in.Action) && !(in.Action == "browser" && in.Call.Tool == "browser_snapshot" && len(in.Call.Steps) == 0) {
		return nil, deviceexec.ErrReadonly
	}
	// A read cannot select a different implementation or hide mutation steps.
	if in.Action == "read" && (in.Call.Tool != "read_file" || len(in.Call.Steps) != 0) {
		return nil, errors.New("device: read only accepts read_file")
	}
	if in.DeviceID == "" {
		in.DeviceID = frozen.turnDevice
	}
	if k.DeviceCall == nil {
		return nil, errors.New("device: source device service unavailable")
	}
	raw, err := k.DeviceCall(prepared, source, in)
	redact := func(text string) string { return scrubSecrets(k.redactor()(text), k.secretValues()) }
	if err != nil {
		err = errors.New(redact(err.Error()))
	}
	if len(raw) > 0 {
		// Reuse the JSON string walker; UseNumber preserves task offsets exactly.
		masked, problem := redactRuntimeJSON(raw, redact)
		if problem != nil {
			return nil, problem
		}
		raw = masked
	}
	if len(raw) > 24<<10 {
		var file string
		writeErr := k.withCurrentIO(func(current *bot.Bot, _ *inbox.Inbox) error {
			workspace := k.currentWorkspaceFor(current)
			if workspace == "" {
				workspace = work.BotDir(current.Root, current.Name())
			}
			var e error
			file, e = work.WriteResult(workspace, raw, "json")
			return e
		})
		if writeErr != nil {
			return nil, writeErr
		}
		raw, _ = json.Marshal(map[string]any{"file": file, "bytes": len(raw), "device_id": in.DeviceID, "ref": in.Ref})
	}
	return raw, err
}
