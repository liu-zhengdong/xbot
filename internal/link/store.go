package link

import "strings"

// App is one card in the settings store. The store installs for the owner, so
// every bot sees it; nothing here names a bot.
type App struct {
	ID       string
	Title    string
	Summary  string
	Category string // dev | work | chat
	// Kind is how it installs: native grant, MCP server, or soon (shown, not installable).
	Kind string
	// Grant is the native link name for Kind native.
	Grant string
	// NeedsSecret means the store asks for a key in settings.
	NeedsSecret bool
}

const (
	AppNative = "native"
	AppServer = "server"
	AppSoon   = "soon"
)

// Store category ids, in chip order after "all".
const (
	CatDev  = "dev"
	CatWork = "work"
	CatChat = "chat"
)

// organs are abilities this process implements itself. Every Bot has them
// from boot; they are never store cards and never grants in apps.json. The
// machine switch, not installation, keeps a dangerous one quiet.
var organs = []App{
	{ID: "desktop", Title: "电脑操作", Summary: "看屏幕、点击、输入，帮你操作电脑上的软件。你在用电脑时它会让开。", Kind: AppNative, Grant: NameDesktop},
}

// Organs lists the native organs.
func Organs() []App { return append([]App(nil), organs...) }

// LookupOrgan finds a native organ by id.
func LookupOrgan(id string) (App, bool) {
	for _, a := range organs {
		if a.ID == id {
			return a, true
		}
	}
	return App{}, false
}

// StoreApps is the curated store list in display order: only Links, abilities
// that depend on something outside this process. Native organs, Files,
// Webhook, file watching and skills are not apps here.
func StoreApps() ([]App, error) {
	// The browser drives the owner's Chrome, so it is a Link, not an organ.
	apps := []App{{ID: "browser", Title: "浏览器", Summary: "用单独的浏览器窗口打开网页、读内容、点按钮。", Category: CatDev, Kind: AppNative, Grant: NameBrowser}}
	list, err := Connectors()
	if err != nil {
		return nil, err
	}
	for _, c := range list {
		if c.Category == "" {
			continue
		}
		// A row that names a key needs settings input before installation.
		apps = append(apps, App{ID: c.ID, Title: c.Title, Summary: c.Summary, Category: c.Category, Kind: AppServer, NeedsSecret: len(c.SecretEnvs) > 0})
	}
	apps = append(apps,
		App{ID: "telegram", Title: "Telegram", Summary: "收到 Telegram 消息时叫醒 Bot；发出去之前先给你看草稿。", Category: CatChat, Kind: AppNative, Grant: NameTelegram, NeedsSecret: true},
		App{ID: "lark", Title: "飞书", Summary: "收到飞书消息时叫醒 Bot；发出去之前先给你看草稿。", Category: CatChat, Kind: AppNative, Grant: NameLark, NeedsSecret: true},
		App{ID: "gmail", Title: "Gmail", Summary: "读写邮件。", Category: CatWork, Kind: AppSoon},
		App{ID: "gdrive", Title: "Google Drive", Summary: "查找和读取云端文件。", Category: CatWork, Kind: AppSoon},
		App{ID: "gcalendar", Title: "Google Calendar", Summary: "查看和安排日程。", Category: CatWork, Kind: AppSoon},
	)
	return apps, nil
}

// StoreGrant says a native grant name is installed from the store, so its
// tools register as a Link's.
func StoreGrant(name string) bool {
	apps, err := StoreApps()
	if err != nil {
		return false
	}
	for _, a := range apps {
		if a.Kind == AppNative && a.Grant == name {
			return true
		}
	}
	return false
}

// LookupApp finds a store card by id.
func LookupApp(id string) (App, bool) {
	apps, err := StoreApps()
	if err != nil {
		return App{}, false
	}
	for _, a := range apps {
		if a.ID == id {
			return a, true
		}
	}
	return App{}, false
}

// ResolveApp maps what the prompt shows (a store or organ title, an id, or
// mcp:<name>) back to the id links load understands. Unknown text comes back unchanged.
func ResolveApp(name string) string {
	name = strings.TrimSpace(name)
	if apps, err := StoreApps(); err == nil {
		for _, a := range append(apps, organs...) {
			if (strings.EqualFold(a.Title, name) || strings.EqualFold(a.ID, name) || (a.Kind == AppServer && strings.EqualFold("mcp:"+a.ID, name))) && a.Kind != AppSoon {
				if a.Kind == AppServer {
					return "mcp:" + a.ID
				}
				return a.ID
			}
		}
	}
	return name
}
