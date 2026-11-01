package cliops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	if status, err := decodeExperienceRequest(w, r, &input); err != nil {
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
	if status, err := decodeExperienceRequest(w, r, &input); err != nil {
		fail(status, err.Error())
		return
	}
	reply, problem := handlers.Search(r.Context(), input)
	if problem != nil {
		reply.Error = problem.Error()
	}
	reply, displayErr := boundExperienceSearchReply(reply)
	status := http.StatusOK
	if problem != nil || displayErr != nil || reply.Error != "" {
		status = http.StatusBadRequest
	}
	writeExperienceReply(w, status, reply)
}

func decodeExperienceRequest(w http.ResponseWriter, r *http.Request, input any) (int, error) {
	if r.Method != http.MethodPost {
		return http.StatusMethodNotAllowed, errors.New("experience: requires POST")
	}
	if r.URL.RawQuery != "" {
		return http.StatusBadRequest, errors.New("experience: query fields are not supported")
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, ExperienceMaxRequest))
	if err != nil {
		return http.StatusBadRequest, errors.New("experience: invalid request or request exceeds 1 MiB")
	}
	raw = bytes.TrimLeft(raw, " \t\r\n")
	if len(raw) == 0 || raw[0] != '{' {
		return http.StatusBadRequest, errors.New("experience: request must be one JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(input); err != nil {
		return http.StatusBadRequest, errors.New("experience: invalid request fields")
	}
	if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return http.StatusBadRequest, errors.New("experience: request must be one JSON object followed by EOF")
	}
	wire, err := json.Marshal(input)
	if err != nil || len(wire) > ExperienceMaxRequest {
		return http.StatusBadRequest, errors.New("experience: request exceeds 1 MiB after JSON encoding")
	}
	return 0, nil
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
	reply, displayErr := boundExperienceSearchReply(reply)
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
	root, err := sourceRoot(endpoint, root, "experience")
	if err != nil {
		return err
	}
	body, err := json.Marshal(input)
	if err != nil {
		return err
	}
	if len(body) > ExperienceMaxRequest {
		return errors.New("experience: request exceeds 1 MiB after JSON encoding")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/v1/experience/"+action, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("X-XBot-Root", root)
	req.Header.Set("Content-Type", "application/json")
	client := LocalHTTPClient(0)
	defer client.CloseIdleConnections()
	response, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("experience: runtime request: %w", err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, ExperienceMaxReply+1))
	if err != nil || len(raw) > ExperienceMaxReply {
		return errors.New("experience: invalid runtime response or response exceeds 64 KiB")
	}
	if err := json.Unmarshal(raw, reply); err != nil {
		return errors.New("experience: invalid runtime response")
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("experience: runtime returned HTTP %d", response.StatusCode)
	}
	return nil
}
