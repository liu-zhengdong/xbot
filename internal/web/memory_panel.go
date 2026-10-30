package web

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"xbot/internal/bot"
	"xbot/internal/experience"
	"xbot/internal/fsutil"
)

const memoryDocumentLimit = 256 << 10
const experiencePageSize = 40

type memoryDocument struct {
	Path     string `json:"path"`
	Markdown string `json:"markdown"`
	Empty    bool   `json:"empty"`
}

type memoryLog struct {
	Path  string `json:"path"`
	Empty bool   `json:"empty"`
}

type panelExperience struct {
	ID        int64     `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	Summary   string    `json:"summary"`
	Focus     string    `json:"focus,omitempty"`
	Segment   string    `json:"segment,omitempty"`
}

type experiencePage struct {
	Items      []panelExperience `json:"items"`
	NextBefore int64             `json:"next_before,omitempty"`
}

type memoryPanel struct {
	Person      memoryDocument `json:"person"`
	Bot         memoryDocument `json:"bot"`
	Experiences experiencePage `json:"experiences"`
	MonthLog    memoryLog      `json:"month_log"`
}

// memoryPanel assembles the read-only panel from the files and experience
// store themselves. Recall remains a search/model projection, not this view.
func (s *Server) memoryPanel(b *bot.Bot) (memoryPanel, error) {
	var panel memoryPanel
	for _, scope := range []struct {
		dir string
		doc *memoryDocument
	}{
		{filepath.Join(s.Root, "people", s.owner, "memory"), &panel.Person},
		{filepath.Join(bot.Dir(s.Root, b.Name()), "memory"), &panel.Bot},
	} {
		path, err := filepath.Abs(filepath.Join(scope.dir, "memory.md"))
		if err != nil {
			return panel, err
		}
		scope.doc.Path, scope.doc.Empty = path, true
		if _, err := os.Stat(scope.dir); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return panel, err
		}
		err = fsutil.WithLock(filepath.Join(scope.dir, ".lock"), func() error {
			file, err := os.Open(path)
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			if err != nil {
				return err
			}
			defer file.Close()
			raw, err := io.ReadAll(io.LimitReader(file, memoryDocumentLimit+1))
			if err != nil {
				return err
			}
			if len(raw) > memoryDocumentLimit {
				return fmt.Errorf("memory document exceeds %d bytes: %s", memoryDocumentLimit, path)
			}
			scope.doc.Markdown = string(raw)
			scope.doc.Empty = strings.TrimSpace(string(raw)) == ""
			return nil
		})
		if err != nil {
			return panel, err
		}
	}
	var err error
	panel.Experiences, err = readExperiencePage(b, 0)
	if err != nil {
		return panel, err
	}
	panel.MonthLog.Path = filepath.Join(filepath.Dir(panel.Bot.Path), "log", time.Now().Format("2006-01")+".md")
	info, err := os.Stat(panel.MonthLog.Path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return panel, err
	}
	panel.MonthLog.Empty = info == nil || info.Size() == 0
	return panel, nil
}

func readExperiencePage(b *bot.Bot, before int64) (experiencePage, error) {
	out := experiencePage{Items: []panelExperience{}}
	list, err := experience.At(bot.Dir(b.Root, b.Name())).Page(experiencePageSize+1, before)
	if err != nil {
		return out, err
	}
	if len(list) > experiencePageSize {
		list = list[:experiencePageSize]
		out.NextBefore = list[len(list)-1].ID
	}
	for _, e := range list {
		out.Items = append(out.Items, panelExperience{ID: e.ID, CreatedAt: e.CreatedAt, Summary: e.Summary, Focus: e.Focus, Segment: e.Segment})
	}
	return out, nil
}

func (s *Server) listExperiences(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	var before int64
	if raw := r.URL.Query().Get("before"); raw != "" {
		before, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || before <= 0 {
			writeErr(w, http.StatusBadRequest, "invalid before")
			return
		}
	}
	page, err := readExperiencePage(b, before)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, page)
}

// memoryFile serves one of the panel's fixed file destinations. The desktop
// opens it with the OS; a browser opens the read-only original in a new tab.
func (s *Server) memoryFile(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	var path string
	switch r.PathValue("kind") {
	case "person":
		path = filepath.Join(s.Root, "people", s.owner, "memory", "memory.md")
	case "bot":
		path = filepath.Join(bot.Dir(s.Root, b.Name()), "memory", "memory.md")
	case "month-log":
		path = filepath.Join(bot.Dir(s.Root, b.Name()), "memory", "log", time.Now().Format("2006-01")+".md")
	default:
		writeErr(w, 404, "not found")
		return
	}
	path, err = filepath.Abs(path)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !info.Mode().IsRegular() {
		writeErr(w, 404, "not found")
		return
	}
	if r.Method == http.MethodPost {
		if !s.App {
			writeErr(w, 404, "not found")
			return
		}
		if s.OpenPath == nil {
			writeErr(w, 500, "open file is not configured")
			return
		}
		if err := s.OpenPath(path); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, map[string]string{"status": "ok"})
		return
	}
	if info.Size() > memoryDocumentLimit {
		writeErr(w, 413, "memory file is too large")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	http.ServeFile(w, r, path)
}
