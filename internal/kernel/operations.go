package kernel

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/code"
	"xbot/internal/exec"
)

const cliProfileGuide = `普通资料（简介、时区）通过当前实例 CLI 操作：在 Shell 中用环境变量 XBOT_BIN 指向的当前二进制（按该解释器语法）先执行 profile --help，再用 --self --json。实例和当前 Bot 身份由 XBOT_* 环境提供；头像和改名仍用现有 profile 工具。`

func (k *Kernel) cliProfileGuide() string {
	k.operations.mu.Lock()
	defer k.operations.mu.Unlock()
	if k.operations.server == nil {
		return ""
	}
	return cliProfileGuide
}

type operationState struct {
	mu           sync.Mutex // independent of turn/processing locks; also coordinates old-tool rename
	server       *cliops.Server
	binary, root string
	retiring     bool
	stopContext  func() bool
}

// StartOperations binds the local CLI to this existing kernel. Call it last in
// runtime assembly; tools and test/eval kernels do not implicitly open servers.
func (k *Kernel) StartOperations() error {
	binary, err := os.Executable()
	if err != nil {
		return err
	}
	binary, err = filepath.EvalSymlinks(binary)
	if err != nil {
		return err
	}
	root, err := cliops.CanonicalRoot(k.Bot.Root)
	if err != nil {
		return err
	}
	k.operations.mu.Lock()
	defer k.operations.mu.Unlock()
	if k.operations.server != nil {
		return nil
	}
	server, err := cliops.Start(root, k.profileOperation)
	if err != nil {
		return err
	}
	k.operations.server, k.operations.binary, k.operations.root = server, binary, root
	for _, item := range k.snapshotLinks() {
		if coding, ok := item.(*code.Link); ok {
			coding.UseEnvironment(k.CLIEnvironment, k.closeIdleOperations)
		}
	}
	return nil
}

// CloseOperations releases only the CLI listener, without changing Jobs or
// other kernel lifetimes. Hosts call it whenever they discard a cached kernel.
func (k *Kernel) CloseOperations() {
	if k == nil {
		return
	}
	k.operations.mu.Lock()
	server := k.operations.server
	stopContext := k.operations.stopContext
	k.operations.server, k.operations.stopContext = nil, nil
	k.operations.mu.Unlock()
	if stopContext != nil {
		stopContext()
	}
	if server != nil {
		_ = server.Close()
	}
}

// BindOperationsContext makes the real host lifetime an upper bound even when
// a short serve pass leaves local tasks running. Closing removes the callback.
func (k *Kernel) BindOperationsContext(ctx context.Context) {
	k.operations.mu.Lock()
	defer k.operations.mu.Unlock()
	if k.operations.server == nil {
		return
	}
	if k.operations.stopContext != nil {
		k.operations.stopContext()
	}
	k.operations.stopContext = context.AfterFunc(ctx, k.CloseOperations)
}

func (k *Kernel) retireOperations() {
	k.operations.mu.Lock()
	k.operations.retiring = true
	k.operations.mu.Unlock()
	k.closeIdleOperations()
}

// The existing local execution sets own retained endpoints. OnIdle is called
// only after task removal and queue drain, with no task resource lock held.
func (k *Kernel) closeIdleOperations() {
	k.operations.mu.Lock()
	retiring := k.operations.retiring && k.operations.server != nil
	k.operations.mu.Unlock()
	if !retiring {
		return
	}
	if local, ok := k.execSnapshot().(*exec.Local); ok {
		if tasks, _ := local.Tasks(); len(tasks) > 0 {
			return
		}
	}
	for _, item := range k.snapshotLinks() {
		if coding, ok := item.(*code.Link); ok && coding.HasLiveTasks() {
			return
		}
	}
	k.CloseOperations()
}

// UseBot refreshes a host's loaded handle between turns while a local coding
// process may still query its runtime endpoint.
func (k *Kernel) UseBot(b *bot.Bot) {
	k.operations.mu.Lock()
	k.Bot = b
	k.operations.mu.Unlock()
}

// CLIEnvironment takes identity at command start, so an old profile-tool rename
// retains the endpoint and subsequent commands see the new diagnostic name.
func (k *Kernel) CLIEnvironment(base []string) []string {
	k.operations.mu.Lock()
	defer k.operations.mu.Unlock()
	caller := cliops.Caller{}
	if k.operations.server != nil {
		caller = cliops.Caller{Binary: k.operations.binary, Root: k.operations.root, Endpoint: k.operations.server.Endpoint, Bot: k.Bot.Name()}
	}
	return caller.Env(base)
}

func (k *Kernel) profileOperation(update *cliops.Update) (cliops.Profile, error) {
	k.operations.mu.Lock()
	b := k.Bot
	var err error
	if update == nil {
		err = b.RefreshProfile()
	} else {
		err = b.ApplyProfile(bot.ProfileUpdate{Description: update.Description, Timezone: update.Timezone})
	}
	if err != nil {
		k.operations.mu.Unlock()
		return cliops.Profile{}, fmt.Errorf("%s", k.redactor()(err.Error()))
	}
	description, timezone := b.ProfileValues()
	value := cliops.Profile{Name: b.Name(), Description: k.redactor()(description), Timezone: timezone, Scope: "runtime", Applied: update != nil}
	k.operations.mu.Unlock()
	// The business/file locks are released before a live stream can block on IO.
	if update != nil {
		k.publishProfile(b)
	}
	return value, nil
}

func (k *Kernel) publishProfile(b *bot.Bot) {
	if value, ok := liveTurns.Load(botKey(b)); ok {
		live := value.(*liveTurn)
		if sink := live.t.sinkNow(); sink != nil && sink.OnProfile != nil {
			sink.OnProfile(b.Name())
		}
	}
}
