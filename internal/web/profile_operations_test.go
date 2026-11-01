package web

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/kernel"
)

func TestDiscardedKernelClosesProfileEndpoint(t *testing.T) {
	root := t.TempDir()
	b, err := bot.Create(root, "ada", "duty", nil)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Root: root, NewKernel: func(b *bot.Bot) (*kernel.Kernel, error) {
		k := &kernel.Kernel{Bot: b}
		return k, k.StartOperations()
	}}
	// The host stays alive while a cached kernel is discarded and rebuilt.
	host := httptest.NewServer(muxOf(s))
	defer host.Close()
	defer s.CloseOperations()
	old, _, err := s.kernel(b)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := func(k *kernel.Kernel) string {
		for _, value := range k.CLIEnvironment(nil) {
			if strings.HasPrefix(value, "XBOT_ENDPOINT=") {
				return strings.TrimPrefix(value, "XBOT_ENDPOINT=")
			}
		}
		t.Fatal("missing endpoint")
		return ""
	}
	oldEndpoint := endpoint(old)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := cliops.Call(ctx, oldEndpoint, root, nil); err != nil {
		t.Fatal(err)
	}
	s.forgetKernel(b.Name())
	desc := "must not write through retired endpoint"
	if _, err := cliops.Call(ctx, oldEndpoint, root, &cliops.Update{Description: &desc}); err == nil {
		t.Fatal("retired endpoint still writable")
	}
	next, _, err := s.kernel(b)
	if err != nil {
		t.Fatal(err)
	}
	if next == old || endpoint(next) == oldEndpoint {
		t.Fatal("kernel was not replaced")
	}
	desc = "new live kernel"
	nextEndpoint := endpoint(next)
	result, err := cliops.Call(ctx, nextEndpoint, root, &cliops.Update{Description: &desc})
	if err != nil || result.Description != desc {
		t.Fatalf("new endpoint: %+v %v", result, err)
	}
	s.CloseOperations()
	if _, err := cliops.Call(ctx, nextEndpoint, root, nil); err == nil {
		t.Fatal("closed service retained endpoint")
	}
}
