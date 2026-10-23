package web

import (
	"fmt"

	"xbot/internal/bot"
	"xbot/internal/peer"
	"xbot/internal/provider"
)

// ResolveOrphanApprovals closes cards a previous process left pending. A card
// only lived in memory, so after a restart nobody can decide it any more; each
// one is rewritten as no-decision with the restart reason, never left pending
// and never turned into a refusal. Cards decided before the restart are kept.
func ResolveOrphanApprovals(root, owner string) (int, error) {
	cfgs, err := peer.OwnerBots(root, owner)
	if err != nil {
		return 0, err
	}
	closed := 0
	for _, cfg := range cfgs {
		b, err := bot.Load(root, cfg.Name)
		if err != nil {
			continue
		}
		entries, err := b.Entries()
		if err != nil {
			return closed, err
		}
		// Only a card whose newest state is pending is orphaned: a later card
		// with the same id carries the decision.
		latest := map[string]bot.ApprovalCard{}
		order := []string{}
		for _, e := range entries {
			card, ok := bot.ParseApprovalCard(e.Message.Text())
			if !ok {
				continue
			}
			if _, seen := latest[card.ID]; !seen {
				order = append(order, card.ID)
			}
			latest[card.ID] = card
		}
		for _, id := range order {
			card := latest[id]
			if card.State != bot.ApprovalPending {
				continue
			}
			_ = b.Append(bot.Entry{Origin: bot.OriginOwner, Message: botCardMessage(card, bot.NoDecisionText(bot.ApprovalReasonRestart))})
			closed++
		}
	}
	return closed, nil
}

// botCardMessage rewrites one card line with a new state.
func botCardMessage(card bot.ApprovalCard, state string) provider.Message {
	return provider.Message{Content: provider.TextContent(bot.ApprovalCardText(card.ID, card.Tool, card.Title, card.Subtitle, card.Summary, state))}
}

// ResolveRestartOrphans is the startup sweep for this server's owner. It runs
// after EnsureToken has learned the owner, so it can find their bots.
func (s *Server) ResolveRestartOrphans() (int, error) {
	return ResolveOrphanApprovals(s.Root, s.owner)
}

// DescribeOrphans is the log line for the startup sweep. It is empty when the
// sweep closed nothing.
func DescribeOrphans(n int) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("closed %d approval card(s) left pending by a previous run as no-decision (restart)", n)
}
