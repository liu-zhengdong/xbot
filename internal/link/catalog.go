package link

import "errors"

const (
	// KindNative is a Link implemented in this process.
	KindNative = "native"
	// KindMCP is a Link backed by an MCP server. Servers are added with xbot mcp, not installed from this catalog.
	KindMCP = "mcp"
)

var (
	// ErrUnavailable means the catalog entry has no runtime yet.
	ErrUnavailable = errors.New("link: not available yet")
	// ErrUnknown means the catalog has no entry with that id.
	ErrUnknown = errors.New("link: not found")
)

// CatalogEntry is one curated marketplace row.
// Grant is the link name installed onto a bot. It is empty until a runtime exists.
type CatalogEntry struct {
	ID           string
	Title        string
	Kind         string
	Capabilities []string
	Secrets      []string
	Summary      string
	Available    bool
	Grant        string
}

// Catalog is the curated list, in display order.
// Entries that are not available yet stay visible and cannot be installed.
func Catalog() []CatalogEntry {
	out := make([]CatalogEntry, len(catalog))
	for i, e := range catalog {
		out[i] = e
		out[i].Capabilities = append([]string{}, e.Capabilities...)
		out[i].Secrets = append([]string{}, e.Secrets...)
	}
	return out
}

// Lookup finds a catalog entry by id.
func Lookup(id string) (CatalogEntry, bool) {
	for _, e := range Catalog() {
		if e.ID == id {
			return e, true
		}
	}
	return CatalogEntry{}, false
}

// GrantName is the link name an available entry installs.
func GrantName(id string) (string, error) {
	e, ok := Lookup(id)
	if !ok {
		return "", ErrUnknown
	}
	if !e.Available || e.Grant == "" {
		return "", ErrUnavailable
	}
	return e.Grant, nil
}

var catalog = []CatalogEntry{
	{
		ID: "webhook", Title: "Webhook", Kind: KindNative,
		Capabilities: []string{"events"}, Secrets: []string{},
		Summary:   "接收 HTTP 回调，写成不可信事件。",
		Available: true, Grant: NameWebhook,
	},
	{
		ID: "watch", Title: "文件监听", Kind: KindNative,
		Capabilities: []string{"events"}, Secrets: []string{},
		Summary:   "路径变化写成不可信事件，正文只有路径和元数据。",
		Available: true, Grant: NameWatch,
	},
	{
		ID: "skills", Title: "技能", Kind: KindNative,
		Capabilities: []string{"tools"}, Secrets: []string{},
		Summary:   "读取共享技能目录里的说明。",
		Available: true, Grant: NameSkills,
	},
	{
		ID: "telegram", Title: "Telegram", Kind: KindNative,
		Capabilities: []string{"tools", "events"}, Secrets: []string{"TELEGRAM_BOT_TOKEN"},
		Summary: "把 Telegram 消息推进来，发出去之前先留草稿。",
	},
	{
		ID: "lark", Title: "飞书", Kind: KindNative,
		Capabilities: []string{"tools", "events"}, Secrets: []string{"LARK_APP_SECRET"},
		Summary: "把飞书消息推进来，发出去之前先留草稿。",
	},
	{
		ID: "mcp", Title: "MCP", Kind: KindMCP,
		Capabilities: []string{"tools", "events"}, Secrets: []string{},
		Summary: "把一个 MCP 服务器包成 Link。用 xbot mcp add 登记命令，不从这里安装。",
	},
	{
		ID: "coding-agent", Title: "编码代理", Kind: KindNative,
		Capabilities: []string{"tools", "events"}, Secrets: []string{},
		Summary: "长时间编码任务，步数和时间按这次任务另定。任务记录还没做。",
	},
}
