package cliops

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTextRuntimeRejectsBareQueryBeforeDomain(t *testing.T) {
	calls := 0
	experiences := &ExperienceHandlers{
		Record: func(context.Context, ExperienceRecord) (ExperienceRecordReply, error) {
			calls++
			return ExperienceRecordReply{}, nil
		},
		Search: func(context.Context, ExperienceSearch) (ExperienceSearchReply, error) {
			calls++
			return ExperienceSearchReply{}, nil
		},
	}
	transcript := &TranscriptHandlers{
		Search: func(context.Context, TranscriptSearch) (TranscriptReadReply, error) {
			calls++
			return TranscriptReadReply{}, nil
		},
		Expand: func(context.Context, TranscriptExpand) (TranscriptReadReply, error) {
			calls++
			return TranscriptReadReply{}, nil
		},
		Detail: func(context.Context, TranscriptDetail) (TranscriptReadReply, error) {
			calls++
			return TranscriptReadReply{}, nil
		},
	}
	for _, serve := range []http.HandlerFunc{
		func(w http.ResponseWriter, r *http.Request) { serveExperienceRecord(w, r, experiences) },
		func(w http.ResponseWriter, r *http.Request) { serveExperienceSearch(w, r, experiences) },
		func(w http.ResponseWriter, r *http.Request) {
			serveMemoryRecall(w, r, func(context.Context, MemoryRecall) (MemoryRecallReply, error) {
				calls++
				return MemoryRecallReply{}, nil
			})
		},
		func(w http.ResponseWriter, r *http.Request) { serveTranscriptSearch(w, r, transcript) },
		func(w http.ResponseWriter, r *http.Request) { serveTranscriptExpand(w, r, transcript) },
		func(w http.ResponseWriter, r *http.Request) { serveTranscriptDetail(w, r, transcript) },
	} {
		w := httptest.NewRecorder()
		serve(w, httptest.NewRequest(http.MethodPost, "/?", strings.NewReader(`{}`)))
		if w.Code != http.StatusBadRequest || calls != 0 {
			t.Fatalf("bare URL query reached domain: code=%d calls=%d", w.Code, calls)
		}
	}
}

func TestDecodePOSTJSONPreservesTypedNullSemantics(t *testing.T) {
	// Decode directly into the actual typed input. A second representation must
	// not assign different meaning to duplicate keys or explicit field nulls.
	var input ExperienceRecord
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"summary":"retained","summary":null,"from":-2}`))
	if status, err := decodePOSTJSON(w, r, "experience", &input); err != nil || status != 0 || input.Summary != "retained" || input.From != -2 {
		t.Fatalf("typed fields changed: input=%+v status=%d err=%v", input, status, err)
	}
}
