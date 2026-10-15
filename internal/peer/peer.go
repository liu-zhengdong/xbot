// Package peer moves messages between bots that share an owner, and through groups.
// A message is an untrusted inbox event. Private memory is never copied across.
package peer

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"xbot/internal/bot"
	"xbot/internal/fsutil"
	"xbot/internal/inbox"
	"xbot/internal/link"
)

const (
	// MaxHops is the most bot-to-bot deliveries allowed in one chain.
	MaxHops = 3
	// RatePerMinute is the cap for one sender-target pair.
	RatePerMinute = 8
	// MaxText is the largest message body that is forwarded.
	MaxText = 8 << 10
	// MaxMembers is the largest group.
	MaxMembers = 8
)

var groupIDRe = regexp.MustCompile(`^g_[a-z0-9]{8}$`)

// Group is a named set of bots with its own transcript.
type Group struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Members []string  `json:"members"`
	Created time.Time `json:"created"`
}

// Line is one group transcript record.
type Line struct {
	ID     string    `json:"id"`
	Time   time.Time `json:"time"`
	Sender string    `json:"sender"`
	Kind   string    `json:"kind"`
	Text   string    `json:"text"`
}

// Bus posts peer and group events under a data root.
type Bus struct {
	Root   string
	Now    func() time.Time
	Limit  int
	Window time.Duration
}

func (b *Bus) now() time.Time {
	if b != nil && b.Now != nil {
		return b.Now()
	}
	return time.Now().UTC()
}

func (b *Bus) limit() int {
	if b != nil && b.Limit > 0 {
		return b.Limit
	}
	return RatePerMinute
}

func (b *Bus) window() time.Duration {
	if b != nil && b.Window > 0 {
		return b.Window
	}
	return time.Minute
}

// VisibleBots lists bots that share an owner with from, excluding from itself.
func VisibleBots(root, from string) ([]bot.Config, error) {
	self, err := bot.Load(root, from)
	if err != nil {
		return nil, err
	}
	all, err := bot.List(root)
	if err != nil {
		return nil, err
	}
	var out []bot.Config
	for _, cfg := range all {
		if cfg.Name == from {
			continue
		}
		if sharesOwner(self.Config().Owners, cfg.Owners) {
			out = append(out, cfg)
		}
	}
	return out, nil
}

// OwnerBots lists bots that include owner.
func OwnerBots(root, owner string) ([]bot.Config, error) {
	all, err := bot.List(root)
	if err != nil {
		return nil, err
	}
	var out []bot.Config
	for _, cfg := range all {
		if contains(cfg.Owners, owner) {
			out = append(out, cfg)
		}
	}
	return out, nil
}

func sharesOwner(a, b []string) bool {
	for _, x := range a {
		if x != "" && contains(b, x) {
			return true
		}
	}
	return false
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// Message posts text into to's inbox as an untrusted bot event.
// hops is the delivery count for this event, already incremented by the caller.
func (b *Bus) Message(from, to, text, chain string, hops int, now time.Time) error {
	if from == "" || to == "" || from == to {
		return errors.New("peer: bad target")
	}
	if hops < 1 || hops > MaxHops {
		return fmt.Errorf("peer: hop limit %d", MaxHops)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("peer: empty message")
	}
	if len(text) > MaxText {
		return errors.New("peer: message too long")
	}
	if _, err := bot.Load(b.Root, from); err != nil {
		return err
	}
	visible, err := VisibleBots(b.Root, from)
	if err != nil {
		return err
	}
	var ok bool
	for _, cfg := range visible {
		if cfg.Name == to {
			ok = true
			break
		}
	}
	if !ok {
		return fmt.Errorf("peer: %s is not visible to %s", to, from)
	}
	if now.IsZero() {
		now = b.now()
	}
	if err := b.allow(from+"->"+to, now); err != nil {
		return err
	}
	target, err := bot.Load(b.Root, to)
	if err != nil {
		return err
	}
	if chain == "" {
		chain = newID("ch_")
	}
	ev := link.NewEvent("bot:"+from, link.TypeMessage, text, from, now)
	ev.SenderKind = link.KindBot
	ev.Hops = hops
	ev.Chain = chain
	box, err := inbox.Open(target.InboxPath())
	if err != nil {
		return err
	}
	return box.Post(ev)
}

// CreateGroup makes a group. members must be visible to ownerBot. ownerBot is included.
func (b *Bus) CreateGroup(ownerBot, name string, members []string) (Group, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 48 || strings.ContainsAny(name, "/\\\n") {
		return Group{}, errors.New("peer: bad group name")
	}
	if _, err := bot.Load(b.Root, ownerBot); err != nil {
		return Group{}, err
	}
	set := map[string]bool{ownerBot: true}
	visible, err := VisibleBots(b.Root, ownerBot)
	if err != nil {
		return Group{}, err
	}
	allowed := map[string]bool{ownerBot: true}
	for _, cfg := range visible {
		allowed[cfg.Name] = true
	}
	for _, m := range members {
		if !allowed[m] {
			return Group{}, fmt.Errorf("peer: %s is not a member candidate", m)
		}
		set[m] = true
	}
	if len(set) < 2 {
		return Group{}, errors.New("peer: a group needs two bots")
	}
	if len(set) > MaxMembers {
		return Group{}, fmt.Errorf("peer: at most %d members", MaxMembers)
	}
	var names []string
	for m := range set {
		names = append(names, m)
	}
	sort.Strings(names)
	g := Group{ID: newID("g_"), Name: name, Members: names, Created: b.now()}
	if err := b.saveGroup(g); err != nil {
		return Group{}, err
	}
	return g, nil
}

// CreateGroupForOwner is the web path: every member must include owner.
func (b *Bus) CreateGroupForOwner(owner, name string, members []string) (Group, error) {
	if len(members) < 2 {
		return Group{}, errors.New("peer: a group needs two bots")
	}
	owned, err := OwnerBots(b.Root, owner)
	if err != nil {
		return Group{}, err
	}
	allow := map[string]bool{}
	for _, cfg := range owned {
		allow[cfg.Name] = true
	}
	for _, m := range members {
		if !allow[m] {
			return Group{}, fmt.Errorf("peer: %s is not owned by this person", m)
		}
	}
	return b.CreateGroup(members[0], name, members)
}

// PostGroup appends to the group transcript and fans the text out to member inboxes.
// hops is the count stored on each delivered event.
func (b *Bus) PostGroup(kind, sender, groupID, text, chain string, hops int, now time.Time) error {
	if kind != link.KindPerson && kind != link.KindBot {
		return errors.New("peer: bad sender kind")
	}
	if !groupIDRe.MatchString(groupID) {
		return errors.New("peer: bad group id")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("peer: empty message")
	}
	if len(text) > MaxText {
		return errors.New("peer: message too long")
	}
	if hops < 1 || hops > MaxHops {
		return fmt.Errorf("peer: hop limit %d", MaxHops)
	}
	g, err := b.LoadGroup(groupID)
	if err != nil {
		return err
	}
	if kind == link.KindBot && !contains(g.Members, sender) {
		return errors.New("peer: bot is not in the group")
	}
	if kind == link.KindPerson {
		owned, err := OwnerBots(b.Root, sender)
		if err != nil {
			return err
		}
		ownedNames := map[string]bool{}
		for _, cfg := range owned {
			ownedNames[cfg.Name] = true
		}
		for _, m := range g.Members {
			if !ownedNames[m] {
				return errors.New("peer: group is not yours")
			}
		}
	}
	if now.IsZero() {
		now = b.now()
	}
	if err := b.allow(kind+":"+sender+"->group:"+groupID, now); err != nil {
		return err
	}
	if chain == "" {
		chain = newID("ch_")
	}
	line := Line{ID: newID("gm_"), Time: now.UTC(), Sender: sender, Kind: kind, Text: text}
	if err := fsutil.AppendJSONL(b.transcript(groupID), line); err != nil {
		return err
	}
	body := fmt.Sprintf("<group id=%q from=%q kind=%q>\n%s\n</group>", g.ID, sender, kind, text)
	for _, member := range g.Members {
		if kind == link.KindBot && member == sender {
			continue
		}
		target, err := bot.Load(b.Root, member)
		if err != nil {
			return err
		}
		ev := link.NewEvent("group:"+g.ID, link.TypeMessage, body, sender, now)
		ev.SenderKind = kind
		ev.Hops = hops
		ev.Chain = chain
		box, err := inbox.Open(target.InboxPath())
		if err != nil {
			return err
		}
		if err := box.Post(ev); err != nil {
			return err
		}
	}
	return nil
}

// LoadGroup reads one group.
func (b *Bus) LoadGroup(id string) (Group, error) {
	if !groupIDRe.MatchString(id) {
		return Group{}, errors.New("peer: bad group id")
	}
	raw, err := os.ReadFile(b.meta(id))
	if err != nil {
		return Group{}, err
	}
	var g Group
	if err := json.Unmarshal(raw, &g); err != nil {
		return Group{}, err
	}
	return g, nil
}

// ListGroups returns every group, newest name order by id.
func (b *Bus) ListGroups() ([]Group, error) {
	dir := filepath.Join(b.Root, "groups")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Group
	for _, e := range entries {
		if !e.IsDir() || !groupIDRe.MatchString(e.Name()) {
			continue
		}
		g, err := b.LoadGroup(e.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Transcript reads the group log.
func (b *Bus) Transcript(id string) ([]Line, error) {
	if _, err := b.LoadGroup(id); err != nil {
		return nil, err
	}
	var out []Line
	err := fsutil.ReadJSONL(b.transcript(id), func(line []byte) error {
		var ln Line
		if err := json.Unmarshal(line, &ln); err != nil {
			return err
		}
		out = append(out, ln)
		return nil
	})
	return out, err
}

func (b *Bus) saveGroup(g Group) error {
	raw, err := fsutil.Marshal(g)
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(b.meta(g.ID), raw, 0600)
}

func (b *Bus) meta(id string) string {
	return filepath.Join(b.Root, "groups", id, "meta.json")
}

func (b *Bus) transcript(id string) string {
	return filepath.Join(b.Root, "groups", id, "transcript.jsonl")
}

type rateFile struct {
	Hits map[string][]int64 `json:"hits"`
}

func (b *Bus) allow(key string, now time.Time) error {
	path := filepath.Join(b.Root, "peer-rate.json")
	return fsutil.WithLock(path+".lock", func() error {
		var rf rateFile
		raw, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &rf); err != nil {
				return err
			}
		}
		if rf.Hits == nil {
			rf.Hits = map[string][]int64{}
		}
		cut := now.Add(-b.window()).UnixNano()
		var kept []int64
		for _, ts := range rf.Hits[key] {
			if ts >= cut {
				kept = append(kept, ts)
			}
		}
		if len(kept) >= b.limit() {
			return errors.New("peer: rate limit")
		}
		kept = append(kept, now.UnixNano())
		rf.Hits[key] = kept
		if len(rf.Hits) > 256 {
			for k, v := range rf.Hits {
				var live []int64
				for _, ts := range v {
					if ts >= cut {
						live = append(live, ts)
					}
				}
				if len(live) == 0 {
					delete(rf.Hits, k)
				} else {
					rf.Hits[k] = live
				}
			}
		}
		raw, err = fsutil.Marshal(rf)
		if err != nil {
			return err
		}
		return fsutil.WriteAtomic(path, raw, 0600)
	})
}

func newID(prefix string) string {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return prefix + "fallback"
	}
	return prefix + hex.EncodeToString(buf[:])
}
