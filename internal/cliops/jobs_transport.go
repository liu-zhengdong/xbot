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

func serveJobsOutput(w http.ResponseWriter, r *http.Request, handlers *JobsHandlers) {
	var fn func(context.Context, JobsOutput) (JobsReply, error)
	if handlers != nil {
		fn = handlers.Output
	}
	serveJobs(w, r, "output", fn, &JobsOutput{})
}

func serveJobsStatus(w http.ResponseWriter, r *http.Request, handlers *JobsHandlers) {
	var fn func(context.Context, JobsStatus) (JobsReply, error)
	if handlers != nil {
		fn = handlers.Status
	}
	serveJobs(w, r, "status", fn, &JobsStatus{})
}

func serveJobsCancel(w http.ResponseWriter, r *http.Request, handlers *JobsHandlers) {
	var fn func(context.Context, JobsCancel) (JobsReply, error)
	if handlers != nil {
		fn = handlers.Cancel
	}
	serveJobs(w, r, "cancel", fn, &JobsCancel{})
}

// serveJobs enforces one strict JSON object per action. The original methods
// have no source call ID requirement, same as the subagent reads.
func serveJobs[I any](w http.ResponseWriter, r *http.Request, action string, fn func(context.Context, I) (JobsReply, error), input *I) {
	w.Header().Set("Content-Type", "application/json")
	fail := func(status int, message string) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(JobsReply{Error: message})
	}
	if fn == nil {
		fail(http.StatusNotFound, "jobs: "+action+" runtime unavailable")
		return
	}
	if r.Method != http.MethodPost {
		fail(http.StatusMethodNotAllowed, "jobs: requires POST")
		return
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		fail(http.StatusBadRequest, "jobs: query fields are not supported")
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, JobsMaxRequest))
	if err != nil {
		fail(http.StatusBadRequest, "jobs: invalid request or request exceeds 1 MiB")
		return
	}
	raw = bytes.TrimLeft(raw, " \t\r\n")
	if len(raw) == 0 || raw[0] != '{' {
		fail(http.StatusBadRequest, "jobs: request must be one JSON object")
		return
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(input); err != nil {
		fail(http.StatusBadRequest, "jobs: invalid request fields")
		return
	}
	if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		fail(http.StatusBadRequest, "jobs: request must be one JSON object followed by EOF")
		return
	}
	wire, err := json.Marshal(input)
	if err != nil || len(wire) > JobsMaxRequest {
		fail(http.StatusBadRequest, "jobs: request exceeds 1 MiB after JSON encoding")
		return
	}
	reply, problem := fn(r.Context(), *input)
	if problem != nil {
		reply.Error = problem.Error()
	}
	reply, displayErr := boundJobsReply(reply)
	if displayErr != nil {
		fail(http.StatusInternalServerError, displayErr.Error())
		return
	}
	raw, err = json.Marshal(reply)
	if err != nil || len(raw)+1 > JobsMaxReply {
		fail(http.StatusInternalServerError, "jobs: runtime reply exceeds HTTP output budget")
		return
	}
	status := http.StatusOK
	if problem != nil || reply.Error != "" {
		status = http.StatusBadRequest
	}
	w.WriteHeader(status)
	_, _ = w.Write(append(raw, '\n'))
}

// The original methods have no source call ID requirement. These clients send
// only source root and typed input, and never retry the operation.
func CallJobsOutput(ctx context.Context, endpoint, root string, input JobsOutput) (JobsReply, error) {
	return callJobs(ctx, endpoint, root, "output", input)
}

func CallJobsStatus(ctx context.Context, endpoint, root string, input JobsStatus) (JobsReply, error) {
	return callJobs(ctx, endpoint, root, "status", input)
}

func CallJobsCancel(ctx context.Context, endpoint, root string, input JobsCancel) (JobsReply, error) {
	return callJobs(ctx, endpoint, root, "cancel", input)
}

func callJobs(ctx context.Context, endpoint, root, action string, input any) (JobsReply, error) {
	var reply JobsReply
	root, err := sourceRoot(endpoint, root, "jobs")
	if err != nil {
		return reply, err
	}
	body, err := json.Marshal(input)
	if err != nil {
		return reply, err
	}
	if len(body) > JobsMaxRequest {
		return reply, errors.New("jobs: request exceeds 1 MiB after JSON encoding")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/v1/jobs/"+action, bytes.NewReader(body))
	if err != nil {
		return reply, err
	}
	req.Header.Set("X-XBot-Root", root)
	req.Header.Set("Content-Type", "application/json")
	client := LocalHTTPClient(0)
	defer client.CloseIdleConnections()
	response, err := client.Do(req)
	if err != nil {
		return reply, fmt.Errorf("jobs: runtime request: %w", err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, JobsMaxReply+1))
	if err != nil || len(raw) > JobsMaxReply {
		return reply, errors.New("jobs: invalid runtime response or response exceeds 64 KiB")
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		return JobsReply{}, errors.New("jobs: invalid runtime response")
	}
	reply, err = boundJobsReply(reply)
	if err != nil {
		return reply, err
	}
	if reply.Error != "" {
		return reply, errors.New(reply.Error)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		problem := fmt.Errorf("jobs: runtime returned HTTP %d", response.StatusCode)
		reply.Error = problem.Error()
		return reply, problem
	}
	return reply, nil
}
