package cliops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

func serveExperienceRecord(w http.ResponseWriter, r *http.Request, handlers *ExperienceHandlers) {
	fail := func(status int, message string) {
		writeExperienceReply(w, status, ExperienceRecordReply{Error: message})
	}
	if handlers == nil || handlers.Record == nil {
		fail(http.StatusNotFound, "experience: record runtime unavailable")
		return
	}
	var input ExperienceRecord
	if status, err := decodePOSTJSON(w, r, "experience", &input); err != nil {
		fail(status, err.Error())
		return
	}
	reply, problem := handlers.Record(r.Context(), input)
	if problem != nil {
		reply.Error = problem.Error()
	}
	reply, displayErr := boundExperienceRecordReply(reply)
	status := http.StatusOK
	if problem != nil || displayErr != nil || reply.Error != "" {
		status = http.StatusBadRequest
	}
	writeExperienceReply(w, status, reply)
}

func serveExperienceSearch(w http.ResponseWriter, r *http.Request, handlers *ExperienceHandlers) {
	fail := func(status int, message string) {
		writeExperienceReply(w, status, ExperienceSearchReply{Error: message})
	}
	if handlers == nil || handlers.Search == nil {
		fail(http.StatusNotFound, "experience: search runtime unavailable")
		return
	}
	var input ExperienceSearch
	if status, err := decodePOSTJSON(w, r, "experience", &input); err != nil {
		fail(status, err.Error())
		return
	}
	reply, problem := handlers.Search(r.Context(), input)
	if problem != nil {
		reply.Error = problem.Error()
	}
	reply, displayErr := boundTextResult(reply, "experience: search reply exceeds 32 KiB Shell output budget; source must provide a complete result artifact")
	status := http.StatusOK
	if problem != nil || displayErr != nil || reply.Error != "" {
		status = http.StatusBadRequest
	}
	writeExperienceReply(w, status, reply)
}

func writeExperienceReply(w http.ResponseWriter, status int, reply any) {
	raw, err := json.Marshal(reply)
	if err != nil || len(raw)+1 > ExperienceMaxReply {
		status = http.StatusInternalServerError
		raw = []byte(`{"from":0,"to":0,"recorded":null,"error":"experience: runtime response exceeds 64 KiB"}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(append(raw, '\n'))
}

// Calls use the original endpoint only. Experience primitives do not require or
// validate a source call ID, so these calls deliberately send no such header.
func CallExperienceRecord(ctx context.Context, endpoint, root string, input ExperienceRecord) (ExperienceRecordReply, error) {
	var reply ExperienceRecordReply
	err := callExperience(ctx, endpoint, root, "record", input, &reply)
	if err != nil && reply.Error == "" && reply.ID == 0 && reply.From == 0 && reply.To == 0 && reply.Recorded == nil {
		return reply, err
	}
	reply, displayErr := boundExperienceRecordReply(reply)
	if displayErr != nil {
		return reply, displayErr
	}
	if reply.Error != "" {
		return reply, errors.New(reply.Error)
	}
	if err != nil {
		reply.Error = err.Error()
	}
	return reply, err
}

func CallExperienceSearch(ctx context.Context, endpoint, root string, input ExperienceSearch) (ExperienceSearchReply, error) {
	var reply ExperienceSearchReply
	err := callExperience(ctx, endpoint, root, "search", input, &reply)
	if err != nil && reply.Error == "" && reply.Text == "" && reply.File == "" && reply.Access == "" && reply.Bytes == 0 {
		return reply, err
	}
	reply, displayErr := boundTextResult(reply, "experience: search reply exceeds 32 KiB Shell output budget; source must provide a complete result artifact")
	if displayErr != nil {
		return reply, displayErr
	}
	if reply.Error != "" {
		return reply, errors.New(reply.Error)
	}
	if err != nil {
		reply.Error = err.Error()
	}
	return reply, err
}

func callExperience(ctx context.Context, endpoint, root, action string, input, reply any) error {
	raw, status, err := postJSON(ctx, endpoint, root, "experience", "/v1/experience/"+action, "", input)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, reply); err != nil {
		return errors.New("experience: invalid runtime response")
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return fmt.Errorf("experience: runtime returned HTTP %d", status)
	}
	return nil
}
