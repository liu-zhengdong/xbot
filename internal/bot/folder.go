package bot

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"xbot/internal/fsutil"
)

const (
	// UngroupedName is the sidebar label for bots with no folder.
	// It is not a stored folder and cannot be created or deleted.
	UngroupedName  = "未分组"
	maxFolderRunes = 32
)

var (
	// ErrFolderExists means this owner already has that folder name.
	ErrFolderExists = errors.New("bot: group already exists")
	// ErrFolderMissing means this owner has no folder by that name.
	ErrFolderMissing = errors.New("bot: group not found")
)

// Folder is one sidebar section. Ungrouped is true for the trailing bucket.
type Folder struct {
	Name      string
	Bots      []string
	Count     int
	Ungrouped bool
}

// FolderView is an owner's folder order plus the bots in each folder.
type FolderView struct {
	Order   []string
	Folders []Folder
}

type orderFile struct {
	Order []string `json:"order"`
}

// Folders lists this owner's sidebar folders. Bots they do not own are left out.
// The last folder is always 未分组.
func Folders(root, owner string) (FolderView, error) {
	order, err := LoadOrder(root, owner)
	if err != nil {
		return FolderView{}, err
	}
	cfgs, err := List(root)
	if err != nil {
		return FolderView{}, err
	}
	var owned []Config
	for _, cfg := range cfgs {
		if containsOwner(cfg.Owners, owner) {
			owned = append(owned, cfg)
		}
	}
	owned = Order(owned)
	known := map[string]bool{}
	buckets := map[string][]string{}
	for _, name := range order {
		known[name] = true
		buckets[name] = []string{}
	}
	var loose []string
	for _, cfg := range owned {
		if cfg.Group != "" && known[cfg.Group] {
			buckets[cfg.Group] = append(buckets[cfg.Group], cfg.Name)
			continue
		}
		loose = append(loose, cfg.Name)
	}
	if loose == nil {
		loose = []string{}
	}
	folders := make([]Folder, 0, len(order)+1)
	for _, name := range order {
		bots := buckets[name]
		folders = append(folders, Folder{Name: name, Bots: bots, Count: len(bots)})
	}
	folders = append(folders, Folder{Name: UngroupedName, Bots: loose, Count: len(loose), Ungrouped: true})
	return FolderView{Order: order, Folders: folders}, nil
}

// LoadOrder reads this owner's folder names. A missing file is an empty order.
func LoadOrder(root, owner string) ([]string, error) {
	if !nameRe.MatchString(owner) {
		return nil, fmt.Errorf("bot: invalid owner %q", owner)
	}
	raw, err := os.ReadFile(orderPath(root, owner))
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var file orderFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, err
	}
	if file.Order == nil {
		return []string{}, nil
	}
	return file.Order, nil
}

// CreateFolder appends a folder to this owner's order.
func CreateFolder(root, owner, name string) error {
	name = strings.TrimSpace(name)
	if err := validFolderName(name); err != nil {
		return err
	}
	return withOrder(root, owner, func(order []string) ([]string, error) {
		if indexName(order, name) >= 0 {
			return nil, ErrFolderExists
		}
		return append(order, name), nil
	})
}

// RenameFolder renames a folder and the group field of this owner's bots.
func RenameFolder(root, owner, oldName, newName string) error {
	oldName = strings.TrimSpace(oldName)
	newName = strings.TrimSpace(newName)
	if err := validFolderName(newName); err != nil {
		return err
	}
	return withOrder(root, owner, func(order []string) ([]string, error) {
		i := indexName(order, oldName)
		if i < 0 {
			return nil, ErrFolderMissing
		}
		if oldName == newName {
			return order, nil
		}
		if indexName(order, newName) >= 0 {
			return nil, ErrFolderExists
		}
		if err := rewriteGroup(root, owner, oldName, newName); err != nil {
			return nil, err
		}
		order[i] = newName
		return order, nil
	})
}

// DeleteFolder removes a folder and clears that group on this owner's bots.
// The bot directories stay.
func DeleteFolder(root, owner, name string) error {
	name = strings.TrimSpace(name)
	if name == UngroupedName {
		return errors.New("bot: reserved group name")
	}
	return withOrder(root, owner, func(order []string) ([]string, error) {
		i := indexName(order, name)
		if i < 0 {
			return nil, ErrFolderMissing
		}
		if err := rewriteGroup(root, owner, name, ""); err != nil {
			return nil, err
		}
		return append(order[:i], order[i+1:]...), nil
	})
}

// SetBotFolder sets one owned bot's group. An empty group moves it to 未分组.
// The name must already be in this owner's order.
func SetBotFolder(root, owner, botName, group string) error {
	group = strings.TrimSpace(group)
	if group == UngroupedName {
		return errors.New("bot: reserved group name")
	}
	return withOrder(root, owner, func(order []string) ([]string, error) {
		if group != "" && indexName(order, group) < 0 {
			return nil, ErrFolderMissing
		}
		b, err := Load(root, botName)
		if err != nil {
			return nil, err
		}
		if !containsOwner(b.Config().Owners, owner) {
			return nil, os.ErrPermission
		}
		if err := b.SetGroup(group); err != nil {
			return nil, err
		}
		return order, nil
	})
}

func rewriteGroup(root, owner, from, to string) error {
	cfgs, err := List(root)
	if err != nil {
		return err
	}
	for _, cfg := range cfgs {
		if cfg.Group != from || !containsOwner(cfg.Owners, owner) {
			continue
		}
		b, err := Load(root, cfg.Name)
		if err != nil {
			return err
		}
		if err := b.SetGroup(to); err != nil {
			return err
		}
	}
	return nil
}

func withOrder(root, owner string, fn func([]string) ([]string, error)) error {
	if !nameRe.MatchString(owner) {
		return fmt.Errorf("bot: invalid owner %q", owner)
	}
	path := orderPath(root, owner)
	return fsutil.WithLock(path+".lock", func() error {
		order, err := readOrderFile(path)
		if err != nil {
			return err
		}
		next, err := fn(order)
		if err != nil {
			return err
		}
		if next == nil {
			next = []string{}
		}
		raw, err := fsutil.Marshal(orderFile{Order: next})
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		return fsutil.WriteAtomic(path, raw, 0600)
	})
}

func readOrderFile(path string) ([]string, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var file orderFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, err
	}
	if file.Order == nil {
		return []string{}, nil
	}
	return file.Order, nil
}

func orderPath(root, owner string) string {
	return filepath.Join(root, "folders", owner+".json")
}

func validFolderName(name string) error {
	if name == UngroupedName {
		return errors.New("bot: reserved group name")
	}
	if name == "" || utf8.RuneCountInString(name) > maxFolderRunes || strings.ContainsAny(name, "/\\\n\r\t") {
		return errors.New("bot: bad group name")
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return errors.New("bot: bad group name")
		}
	}
	return nil
}

func indexName(ss []string, s string) int {
	for i, x := range ss {
		if x == s {
			return i
		}
	}
	return -1
}

func containsOwner(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
