package kernel

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"xbot/internal/bot"
	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/provider"
)

var (
	ErrNoQuestion       = errors.New("no such question")
	ErrQuestionAnswered = errors.New("question already answered")
	ErrQuestionChoice   = errors.New("choice must match one of options")
	questionsMu         sync.Mutex
	questions           = map[string]*ownerQuestion{}
)

type ownerQuestion struct {
	b                   *bot.Bot
	id, sender, branch  string
	question            string
	options             []string
	recommended, choice string
	wait                time.Duration
	deadline            time.Time
	timer               *time.Timer
	done, automatic     bool
}

func (q *ownerQuestion) append(state string) error {
	raw, err := json.Marshal(struct {
		Question    string   `json:"question"`
		Options     []string `json:"options"`
		Recommended string   `json:"recommended"`
		Choice      string   `json:"choice,omitempty"`
	}{q.question, q.options, q.recommended, q.choice})
	if err != nil {
		return err
	}
	text := bot.ApprovalCardTextAt(q.id, "ask_user", q.question, "", string(raw), state, q.deadline.Format(time.RFC3339Nano))
	if q.automatic {
		text = bot.ApprovalCardTextWithNotice(q.id, "ask_user", q.question, "", string(raw), state, bot.CardWaitText(q.wait)+"没人选，按推荐选了 "+q.choice)
	}
	return q.b.Append(bot.Entry{Branch: q.branch, Origin: bot.OriginOwner, Message: provider.Message{Role: provider.RoleUser, Content: text}})
}

func (k *Kernel) askOwner(ctx context.Context, question string, options []string, recommended string) error {
	if k == nil || k.Bot == nil {
		return errors.New("ask_user: no bot")
	}
	wait := k.ApprovalWait
	if wait <= 0 {
		wait = bot.DefaultCardWait
	}
	sender := ""
	turn := k.turnFor(ctx)
	if turn.turnTrusted {
		sender = turn.turnSender
	}
	if sender == "" {
		owners := k.Bot.Config().Owners
		if len(owners) > 0 {
			sender = owners[0]
		}
	}
	q := &ownerQuestion{b: k.Bot, id: "ask_" + rand.Text(), sender: sender, branch: BranchOf(ctx), question: question, options: options, recommended: recommended, wait: wait, deadline: time.Now().Add(wait)}
	questionsMu.Lock()
	defer questionsMu.Unlock()
	// Completed cards are remembered for one decision window so a late click
	// on an auto-choice can still be delivered as a fresh owner message.
	for key, old := range questions {
		if old.done && time.Now().After(old.deadline.Add(old.wait)) {
			delete(questions, key)
		}
	}
	if err := q.append(bot.ApprovalPending); err != nil {
		return err
	}
	questions[botKey(q.b)+"/"+q.id] = q
	q.timer = time.AfterFunc(wait, func() {
		questionsMu.Lock()
		if q.done {
			questionsMu.Unlock()
			return
		}
		err := finishQuestionLocked(q, q.recommended, true)
		questionsMu.Unlock()
		if err == nil {
			deliverQuestion(q, q.choice, false)
		}
	})
	return nil
}

func finishQuestionLocked(q *ownerQuestion, choice string, automatic bool) error {
	q.choice, q.automatic = choice, automatic
	state := bot.ApprovalApproved
	if automatic {
		state = bot.ApprovalAutoApproved
	}
	if err := q.append(state); err != nil {
		return err
	}
	q.done = true
	if q.timer != nil {
		q.timer.Stop()
	}
	return nil
}

// ChooseQuestion accepts a card choice. After an automatic choice, further
// choices are ordinary owner messages, without changing the timeout record.
func ChooseQuestion(b *bot.Bot, id, choice string) error {
	questionsMu.Lock()
	q := questions[botKey(b)+"/"+id]
	if q == nil {
		// Completed choices remain on the append-only transcript even after
		// the in-memory retention window, including across a restart.
		entries, err := b.Entries()
		if err != nil {
			questionsMu.Unlock()
			return err
		}
		for _, e := range entries {
			c, ok := bot.ParseApprovalCard(e.Message.Content)
			if !ok || c.ID != id || c.Tool != "ask_user" {
				continue
			}
			question, options, recommended, err := parseAsk(json.RawMessage(c.Summary))
			if err != nil {
				questionsMu.Unlock()
				return err
			}
			q = &ownerQuestion{b: b, id: id, question: question, options: options, recommended: recommended, done: true, automatic: c.State == bot.ApprovalAutoApproved}
			owners := b.Config().Owners
			if len(owners) > 0 {
				q.sender = owners[0]
			}
		}
		if q == nil {
			questionsMu.Unlock()
			return ErrNoQuestion
		}
	}
	valid := false
	for _, o := range q.options {
		if choice == o {
			valid = true
		}
	}
	if !valid {
		questionsMu.Unlock()
		return ErrQuestionChoice
	}
	if q.done {
		automatic := q.automatic
		questionsMu.Unlock()
		if !automatic {
			return ErrQuestionAnswered
		}
		deliverQuestion(q, choice, true)
		return nil
	}
	// Resolve the deadline before accepting a late click; the timer and the
	// HTTP request use the same lock, so a manual reply cannot overwrite it.
	if !time.Now().Before(q.deadline) {
		err := finishQuestionLocked(q, q.recommended, true)
		questionsMu.Unlock()
		if err != nil {
			return err
		}
		deliverQuestion(q, q.recommended, false)
		deliverQuestion(q, choice, true)
		return nil
	}
	err := finishQuestionLocked(q, choice, false)
	questionsMu.Unlock()
	if err != nil {
		return err
	}
	deliverQuestion(q, choice, false)
	return nil
}

// AnswerQuestions stops outstanding question timers when the owner types.
// The incoming message itself is delivered by Handle/Interject, not twice.
func AnswerQuestions(b *bot.Bot, text string) {
	var expired []*ownerQuestion
	questionsMu.Lock()
	for _, q := range questions {
		if botKey(q.b) != botKey(b) || q.done {
			continue
		}
		if !time.Now().Before(q.deadline) {
			if finishQuestionLocked(q, q.recommended, true) == nil {
				expired = append(expired, q)
			}
		} else {
			_ = finishQuestionLocked(q, text, false)
		}
	}
	questionsMu.Unlock()
	for _, q := range expired {
		deliverQuestion(q, q.recommended, false)
	}
}

func deliverQuestion(q *ownerQuestion, text string, fresh bool) {
	if fresh {
		AnswerQuestions(q.b, text)
	}
	if InterjectInternal(q.b, provider.Message{Role: provider.RoleUser, Content: text, Internal: !fresh}) {
		return
	}
	source := link.NameApproval
	if fresh {
		source = link.NameWeb
	}
	ev := link.NewEvent(source, link.TypeMessage, text, q.sender, time.Now())
	ev.SenderKind = link.KindPerson
	ev.Untrusted = false
	box, err := inbox.Open(q.b.InboxPath())
	if err != nil || box.Post(ev) != nil {
		return
	}
	if v, ok := wakers.Load(q.b.Root); ok {
		v.(func(*bot.Bot))(q.b)
	}
}
