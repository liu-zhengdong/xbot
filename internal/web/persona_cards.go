package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"xbot/internal/bot"
	"xbot/internal/peer"
	"xbot/internal/personacard"
)

// The settings page keeps the owner's persona-card library; a bot picks one card in its profile drawer.
// Ids go to the page so it can pick and edit, but the page never shows them.

// maxCardImport caps the Markdown file the page sends; the card body has its own, smaller cap.
const maxCardImport = 256 << 10

// cardSearch is how much of the body the list carries for the page's search.
const cardSearch = 1 << 10

func cardJSON(c personacard.Card) map[string]any {
	return map[string]any{"id": c.ID, "name": c.Name, "body": c.Body, "updated": c.Updated}
}

func clipUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

func cardPreview(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if line = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "#")); line != "" {
			return clipUTF8(line, 240)
		}
	}
	return ""
}

func cardErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, personacard.ErrNotFound):
		writeErr(w, http.StatusNotFound, "没有找到这张人设卡")
	case errors.Is(err, personacard.ErrEmpty):
		writeErr(w, http.StatusBadRequest, "人设卡正文不能为空")
	case errors.Is(err, personacard.ErrLarge):
		writeErr(w, http.StatusBadRequest, "人设卡正文太长了，请删短一些")
	case errors.Is(err, personacard.ErrFull):
		writeErr(w, http.StatusBadRequest, "人设卡已经满了，先删掉几张再试")
	default:
		writeErr(w, http.StatusInternalServerError, "没能保存，请再试一次")
	}
}

// cardUsers are the owner's bots bound to the card.
func (s *Server) cardUsers(id string) ([]string, error) {
	cfgs, err := peer.OwnerBots(s.Root, s.owner)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, cfg := range cfgs {
		if cfg.PersonaCard == id {
			out = append(out, cfg.Name)
		}
	}
	return out, nil
}

// pickedCard is the bot's card for the profile drawer, or nil when none is picked or it is gone.
func (s *Server) pickedCard(b *bot.Bot) map[string]any {
	id := b.Config().PersonaCard
	if id == "" || len(b.Config().Owners) != 1 {
		return nil
	}
	c, err := personacard.Get(s.Root, b.Config().Owners[0], id)
	if err != nil {
		return nil
	}
	return map[string]any{"id": c.ID, "name": c.Name}
}

func (s *Server) listPersonaCards(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	cards, err := personacard.List(s.Root, s.owner)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "没能读取人设卡")
		return
	}
	out := make([]map[string]any, 0, len(cards))
	for _, c := range cards {
		out = append(out, map[string]any{
			"id": c.ID, "name": c.Name, "preview": cardPreview(c.Body),
			"search": clipUTF8(c.Body, cardSearch), "updated": c.Updated,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"cards": out})
}

func (s *Server) readPersonaCard(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	c, err := personacard.Get(s.Root, s.owner, r.PathValue("id"))
	if err != nil {
		cardErr(w, err)
		return
	}
	users, err := s.cardUsers(c.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "没能读取 Bot")
		return
	}
	out := cardJSON(c)
	out["bots"] = len(users)
	writeJSON(w, http.StatusOK, out)
}

type cardBody struct {
	Name *string `json:"name"`
	Body *string `json:"body"`
}

func (s *Server) createPersonaCard(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	var in cardBody
	if err := decode(r, &in); err != nil || in.Body == nil {
		writeErr(w, http.StatusBadRequest, "没能读取这张人设卡")
		return
	}
	name := ""
	if in.Name != nil {
		name = *in.Name
	}
	c, err := personacard.Create(s.Root, s.owner, name, *in.Body)
	if err != nil {
		cardErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cardJSON(c))
}

func (s *Server) updatePersonaCard(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	var in cardBody
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "没能读取这张人设卡")
		return
	}
	c, err := personacard.Update(s.Root, s.owner, r.PathValue("id"), in.Name, in.Body)
	if err != nil {
		cardErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cardJSON(c))
}

// deletePersonaCard asks first when bots still use the card: without force it answers 409 with the count.
// With force those bots go back to no card, then the card is removed.
func (s *Server) deletePersonaCard(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	id := r.PathValue("id")
	if _, err := personacard.Get(s.Root, s.owner, id); err != nil {
		cardErr(w, err)
		return
	}
	force, _ := strconv.ParseBool(r.URL.Query().Get("force"))
	s.skillMu.Lock()
	defer s.skillMu.Unlock()
	users, err := s.cardUsers(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "没能读取 Bot")
		return
	}
	if len(users) > 0 && !force {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": strconv.Itoa(len(users)) + " 个 Bot 还在用这张人设卡", "conflict": true, "bots": len(users),
		})
		return
	}
	for _, name := range users {
		b, err := s.owned(name)
		if err != nil {
			continue
		}
		if err := b.SetPersonaCard(""); err != nil {
			writeErr(w, http.StatusInternalServerError, "没能解除 "+name+" 的选用，请再试一次")
			return
		}
	}
	if err := personacard.Delete(s.Root, s.owner, id); err != nil {
		cardErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "unbound": len(users)})
}

// importPersonaCard makes a new card from a Markdown file the page read. It never overwrites:
// a name already in the library answers 409 until the page confirms, then the new card gets a " (2)" suffix.
func (s *Server) importPersonaCard(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	var in struct {
		Filename string `json:"filename"`
		Content  string `json:"content"`
		Confirm  bool   `json:"confirm"`
	}
	if err := decode(r, &in); err != nil || len(in.Content) > maxCardImport {
		writeErr(w, http.StatusBadRequest, "没能读取这个文件，请换一个再试")
		return
	}
	c, err := personacard.Import(s.Root, s.owner, in.Filename, in.Content, in.Confirm)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, cardJSON(c))
	case errors.Is(err, personacard.ErrExists):
		writeJSON(w, http.StatusConflict, map[string]any{"error": "库里已有同名人设卡", "conflict": true, "name": c.Name})
	case errors.Is(err, personacard.ErrEmpty):
		writeErr(w, http.StatusBadRequest, "这个文件是空的，导入不了")
	default:
		cardErr(w, err)
	}
}

// pickPersonaCard binds the bot to one card, or to none when id is empty.
func (s *Server) pickPersonaCard(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	var in struct {
		ID string `json:"id"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	id := strings.TrimSpace(in.ID)
	var picked map[string]any
	if id != "" {
		c, err := personacard.Get(s.Root, s.owner, id)
		if err != nil {
			cardErr(w, err)
			return
		}
		picked = map[string]any{"id": c.ID, "name": c.Name}
	}
	s.skillMu.Lock()
	defer s.skillMu.Unlock()
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	if id != "" && len(b.Config().Owners) != 1 {
		writeErr(w, http.StatusBadRequest, "多人共用的 Bot 暂不能选用人设卡")
		return
	}
	if err := b.SetPersonaCard(id); err != nil {
		writeErr(w, http.StatusBadRequest, "没能选用，请再试一次")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": b.Name(), "persona_card": picked})
}
