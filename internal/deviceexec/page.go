package deviceexec

import (
	"encoding/json"
	"sort"
	"unicode/utf8"
)

const MaxTaskPageBytes = 24 << 10

// TaskPage offsets count matching tasks in the stable, newest-first ordering.
type TaskPage struct {
	Tasks      []Task `json:"tasks"`
	Offset     int64  `json:"offset"`
	NextOffset int64  `json:"next_offset"`
	HasMore    bool   `json:"has_more"`
}

// ListPage reads all matching records, including calls outside List's recent window.
func (m *Manager) ListPage(filter *Source, offset int64, limit int) TaskPage {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 10
	}
	if limit > 200 {
		limit = 200
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	tasks := []Task{}
	for _, rec := range m.records {
		s := rec.Source
		if filter != nil && ((filter.Account != "" && filter.Account != s.Account) || (filter.DeviceID != "" && filter.DeviceID != s.DeviceID) || (filter.Bot != "" && filter.Bot != s.Bot) || (filter.ContextID != "" && filter.ContextID != s.ContextID)) {
			continue
		}
		tasks = append(tasks, m.taskLocked(rec))
	}
	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].StartedAt.Equal(tasks[j].StartedAt) {
			return tasks[i].Ref < tasks[j].Ref
		}
		return tasks[i].StartedAt.After(tasks[j].StartedAt)
	})
	page := TaskPage{Tasks: []Task{}, Offset: offset, NextOffset: offset}
	if offset >= int64(len(tasks)) {
		return page
	}
	end := offset + min(int64(limit), int64(len(tasks))-offset)
	for _, task := range tasks[offset:end] {
		page.Tasks = append(page.Tasks, boundedPageTask(task))
	}
	page.NextOffset = end
	page.HasMore = end < int64(len(tasks))
	return page
}

// Only display fields are summarized. References and execution facts stay exact.
func boundedPageTask(task Task) Task {
	for textLimit := 1024; ; textLimit /= 2 {
		task.Description = pageText(task.Description, textLimit)
		task.Error = pageText(task.Error, textLimit)
		task.DeviceName = pageText(task.DeviceName, min(256, textLimit))
		task.Bot = pageText(task.Bot, min(256, textLimit))
		task.ContextID = pageText(task.ContextID, min(256, textLimit))
		for i := range task.Artifacts {
			task.Artifacts[i].Name = pageText(task.Artifacts[i].Name, min(128, textLimit))
		}
		body, _ := json.Marshal(task)
		if len(body) <= 20<<10 || textLimit <= 1 {
			return task
		}
	}
}

func pageText(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	end := limit
	for end > 0 && !utf8.RuneStart(text[end]) {
		end--
	}
	return text[:end]
}
