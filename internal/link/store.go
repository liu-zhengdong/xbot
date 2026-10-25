package link

// App is one card in the settings store. The store installs for the owner, so
// every bot sees it; nothing here names a bot.
type App struct {
	ID       string
	Title    string
	Summary  string
	Category string // local | dev | work | chat
	// Kind is how it installs: native grant, MCP server, or soon (shown, not installable).
	Kind string
	// Grant is the native link name for Kind native.
	Grant string
	// NeedsSecret means the owner fills a key on a confirmation card in chat.
	NeedsSecret bool
}

const (
	AppNative = "native"
	AppServer = "server"
	AppSoon   = "soon"
)

// Store category ids, in chip order after "all".
const (
	CatLocal = "local"
	CatDev   = "dev"
	CatWork  = "work"
	CatChat  = "chat"
)

// StoreApps is the curated store list in display order. Files, Webhook, file
// watching and skills are not apps here, and hidden catalog rows are left out.
func StoreApps() ([]App, error) {
	apps := []App{
		{ID: "desktop", Title: "电脑操作", Summary: "看屏幕、点击、输入，帮你操作电脑上的软件。你在用电脑时它会让开。", Category: CatLocal, Kind: AppNative, Grant: NameDesktop},
		{ID: "browser", Title: "浏览器", Summary: "用单独的浏览器窗口打开网页、读内容、点按钮。", Category: CatLocal, Kind: AppNative, Grant: NameBrowser},
	}
	list, err := Connectors()
	if err != nil {
		return nil, err
	}
	for _, c := range list {
		if c.Hidden || c.Category == "" {
			continue
		}
		apps = append(apps, App{ID: c.ID, Title: c.Title, Summary: c.Summary, Category: c.Category, Kind: AppServer})
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

// ResolveApp maps what the prompt shows (a store title, an id, or mcp:<name>)
// back to the id links load understands. Unknown text comes back unchanged.
func ResolveApp(name string) string {
	if apps, err := StoreApps(); err == nil {
		for _, a := range apps {
			if a.Title == name && a.Kind != AppSoon {
				if a.Kind == AppServer {
					return "mcp:" + a.ID
				}
				return a.ID
			}
		}
	}
	return name
}
