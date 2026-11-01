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
	"xbot/internal/inbox"
	"xbot/internal/link"
)

const cliProfileGuide = `资料全部通过当前实例 CLI：在 Shell 用 XBOT_BIN 指向的二进制先执行 profile --help，再以 --self --json show/set；set 可组合 --name/--description/--timezone/--avatar-seed，avatar --file/--clear 是独立动作。改名后原 endpoint 继续认同一 Bot，XBOT_BOT 只作诊断；失败回执若已 moved/applied，要按实际结果继续，别盲重试。group仍原独立入口。MCP 同用 mcp --help 与 --self --json 的 list/tools/describe/call，工具用远端原名，本机产物 file 不经 tools/links 载入 schema。Bot发现用 list --self --json，状态用 info [NAME] --self --steps 10 --json（无NAME看原状态范围，不等于发现列表）；仅主Bot可 new NAME --self --persona TEXT 或 --persona-file FILE|-、delete NAME --self，均可--json。创建前问清缺失的名字/明确职责/现在是否建，已明确或授权取名就直接做，不编默认职责；删除保primary/last保护。partial按applied/removed/stage实际结果处理，大列表读完整file；主Bot置顶用pin/unpin NAME --self，转交用primary set NAME --self，均可--json；primary回执applied:true即指针已写成，后续pin阶段错也已转交，旧source不能再冒主资格，勿盲重试。任何Bot整写自己职责用persona set --self (--text TEXT|--file FILE|-) [--json]；完整32KiB职责用文件或stdin以避Shell命令16KiB限，written:true/next_wake表示已保存且下次唤醒生效，不改卡或记忆、不额外唤醒。fork和消息群仍原门。`

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
	calls        int
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
	if k.workOrigin == "" {
		k.workOrigin = k.Bot.Name()
	}
	for _, item := range k.snapshotLinks() {
		k.bindCurrentPaths(item)
		if coding, ok := item.(*code.Link); ok {
			coding.UseEnvironment(k.CLIEnvironment, k.closeIdleOperations)
		}
	}
	handlers := cliops.Handlers{Profile: k.profileOperation, ProfileAvatar: k.profileAvatarOperation, Skills: k.SkillHandlers(), Routines: k.RoutineHandlers(), Bots: k.BotHandlers()}
	if k.Resources != nil {
		mcpHandlers := k.Resources.MCPHandlers()
		handlers.MCP = &mcpHandlers
	}
	server, err := cliops.Start(root, handlers)
	if err != nil {
		return err
	}
	k.operations.server, k.operations.binary, k.operations.root = server, binary, root
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
func (k *Kernel) holdOperations() func() {
	_, release := k.holdSourceOperations()
	return release
}

func (k *Kernel) holdSourceOperations() (*cliops.Server, func()) {
	k.operations.mu.Lock()
	server := k.operations.server
	if server == nil {
		k.operations.mu.Unlock()
		return nil, func() {}
	}
	k.operations.calls++
	k.operations.mu.Unlock()
	return server, func() {
		k.operations.mu.Lock()
		k.operations.calls--
		k.operations.mu.Unlock()
		k.closeIdleOperations()
	}
}

func (k *Kernel) closeIdleOperations() {
	k.operations.mu.Lock()
	server := k.operations.server
	retiring := k.operations.retiring && server != nil && k.operations.calls == 0
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
	k.operations.mu.Lock()
	if !k.operations.retiring || k.operations.server != server || k.operations.calls != 0 {
		k.operations.mu.Unlock()
		return
	}
	k.operations.server = nil
	stop := k.operations.stopContext
	k.operations.stopContext = nil
	k.operations.mu.Unlock()
	if stop != nil {
		stop()
	}
	_ = server.Close()
}

// UseBot refreshes a host's loaded handle between turns while a local coding
// process may still query its runtime endpoint.
func (k *Kernel) UseBot(b *bot.Bot) {
	k.operations.mu.Lock()
	k.Bot = b
	k.operations.mu.Unlock()
}

func (k *Kernel) bindCurrentPaths(item link.Link) {
	if coding, ok := item.(*code.Link); ok {
		coding.UseCurrentIO(k.withCurrentIO)
	}
	// MountOrgans may have bound this object using its assembly-only Kernel.
	// Rebind before runtime publication so the actual source owns its route.
	if timer, ok := item.(*link.Timer); ok {
		timer.WithDirectoryIO = func(fn func(string) error) error {
			return k.withCurrentIO(func(b *bot.Bot, _ *inbox.Inbox) error { return fn(b.RoutineDir()) })
		}
	}
}

// CLIEnvironment takes identity at command start, so an old profile-tool rename
// retains the endpoint and subsequent commands see the new diagnostic name.
func (k *Kernel) CLIEnvironment(base []string) []string {
	k.operations.mu.Lock()
	defer k.operations.mu.Unlock()
	caller := cliops.Caller{}
	if k.operations.server != nil {
		name := k.Bot.Name()
		available := true
		if r := k.Resources; r != nil {
			r.route.RLock()
			name = r.current.Name()
			available = !r.closed
			r.route.RUnlock()
		}
		if available {
			caller = cliops.Caller{Binary: k.operations.binary, Root: k.operations.root, Endpoint: k.operations.server.Endpoint, Bot: name}
		}
	}

	return caller.Env(base)
}

func (k *Kernel) profileOperation(update *cliops.Update) (cliops.Profile, error) {
	if update == nil {
		k.operations.mu.Lock()
		value, err := k.profileResult(bot.ProfileChangeResult{})
		k.operations.mu.Unlock()
		return value, err
	}
	change, err := bot.ValidateProfileChange(bot.ProfileChange{Name: update.Name, Description: update.Description, Timezone: update.Timezone, AvatarSeed: update.AvatarSeed})
	if err != nil {
		return cliops.Profile{}, fmt.Errorf("%s", k.redactor()(err.Error()))
	}
	return k.ApplyProfileChange(change)
}

func (k *Kernel) publishProfile(b *bot.Bot) { k.publishProfileNotice(ProfileNotice{Name: b.Name()}) }

// RetireOperations lets a host discard a cached Kernel while its real source
// calls and Local/code collections retain their original listener leases.
func (k *Kernel) RetireOperations() { k.retireOperations() }
