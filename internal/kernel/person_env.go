package kernel

import (
	"context"
	"fmt"
	"maps"
	"path/filepath"
	"strings"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

// saveState preserves environment updates received while compaction or setup ran.
// Environment publishers take the same lock and save their own updated record.
func (k *Kernel) saveState(st bot.State) error {
	k.envMu.Lock()
	defer k.envMu.Unlock()
	cur, err := k.Bot.LoadState()
	if err != nil {
		return err
	}
	st.Env = cur.Env
	return k.Bot.SaveState(st)
}

// personEvent reuses the authenticated Event identity; a timer carrying an
// owner's id is still a timer, not that person's new words.
func personEvent(ev link.Event) bool {
	return memory.ValidID(ev.Sender) && ev.SenderKind != link.KindBot && ev.SenderKind != link.KindGroup && (ev.SenderKind == link.KindPerson || ev.Source == link.NameTerminal || ev.Source == link.NameWeb || ev.Source == link.NameApproval || ev.Source == link.NameRemoteOwner)
}

func (k *Kernel) personText(ctx context.Context, ev link.Event) (string, error) {
	if k.Memory == nil || !personEvent(ev) {
		return "", nil
	}
	facts, err := k.Memory.Recall(ctx, memory.Query{Scope: memory.ScopePerson, PersonID: ev.Sender, Tier: memory.TierProfile})
	if err != nil {
		return "", err
	}
	paths := map[memory.Scope]string{memory.ScopePerson: filepath.Join(k.Bot.Root, "people", ev.Sender, "memory", "memory.md")}
	var personFacts []memory.Fact
	for _, fact := range facts {
		if fact.Scope == memory.ScopePerson && fact.PersonID == ev.Sender {
			personFacts = append(personFacts, fact)
		}
	}
	return clipMemory(renderMemoryPaths(memory.Resolve(memory.Visible(personFacts, k.Bot.Name(), ev.Sender)), nil, paths)), nil
}

func personChange(prev, next bot.EnvRecord) string {
	if !next.PersonActive {
		if prev.Source == next.Source {
			return ""
		}
		return fmt.Sprintf("source: %s", next.Source)
	}
	label := ""
	if !prev.PersonActive || prev.Source != next.Source || prev.Sender != next.Sender {
		label = fmt.Sprintf("source: %s; sender: %s (input seq %d)", next.Source, next.Sender, next.SenderSeq)
	}
	// Each person is compared with their own last model-visible version.
	before, after := prev.People[next.Sender], next.People[next.Sender]
	data := ""
	if !before.Seen {
		data = tools.NeutralizeUntrusted(after.Text)
	} else {
		data = memoryDiff(before.Text, after.Text, before.Own)
	}

	if data != "" {
		label = strings.TrimSpace(label + fmt.Sprintf("\n<person_data sender=%q>\n%s\n</person_data>", next.Sender, data))
	}
	return label
}

// receiveEnvironment runs while the Event still has its authenticated identity.
// The raw words have already been persisted; nothing is inferred from their text.
func (k *Kernel) receiveEnvironment(ev link.Event, seq int) error {
	k.envMu.Lock()
	defer k.envMu.Unlock()
	st, err := k.Bot.LoadState()
	if err != nil {
		return err
	}
	if seq < st.Env.SenderSeq {
		return nil
	}
	k.memWriteMu.Lock()
	defer k.memWriteMu.Unlock()
	person, err := k.personText(context.Background(), ev)
	if err != nil {
		return err
	}
	k.drainMemOwn(&st)
	prev := st.Env
	st.Env.Sender, st.Env.SenderSeq, st.Env.Source = ev.Sender, seq, ev.Source
	st.Env.PersonActive = personEvent(ev)
	if st.Env.PersonActive {
		st.Env.People = maps.Clone(st.Env.People)
		if st.Env.People == nil {
			st.Env.People = map[string]bot.PersonEnv{}
		}
		st.Env.People[ev.Sender] = bot.PersonEnv{Text: person, Seen: true}
	}
	diff := personChange(prev, st.Env)
	if st.Env.At.IsZero() {
		st.Env.At = k.now()
	}
	if diff != "" {
		if err := k.Bot.Append(bot.Entry{Time: k.now(), Origin: bot.OriginInternal, Message: provider.Message{Role: provider.RoleUser, Content: "<environment_update>\n" + diff + "\n</environment_update>"}}); err != nil {
			return err
		}
	}
	return k.Bot.SaveState(st)
}

// updateTaskEnvironment samples the same record immediately before each request.
func (k *Kernel) updateTaskEnvironment() error {
	k.envMu.Lock()
	defer k.envMu.Unlock()
	st, err := k.Bot.LoadState()
	if err != nil {
		return err
	}
	next, err := k.taskEnvironment(st.Env.Tasks, link.Event{})
	if err != nil {
		return err
	}
	prev := st.Env
	st.Env.Tasks = next
	if diff := DiffEnv(prev, st.Env); strings.TrimSpace(diff) != "" {
		if err := k.Bot.Append(bot.Entry{Time: k.now(), Origin: bot.OriginInternal, Message: provider.Message{Role: provider.RoleUser, Content: diff}}); err != nil {
			return err
		}
	}
	return k.Bot.SaveState(st)
}
