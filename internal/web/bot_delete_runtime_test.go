package web

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/kernel"
	"xbot/internal/tools"
	"xbot/internal/vault"
)

func TestBotDeleteRuntimeRemovedFact(t *testing.T) {
	for _, mode := range []string{"direct", "host resources", "host cold"} {
		for _, keyFailure := range []bool{false, true} {
			outcome := "complete"
			if keyFailure {
				outcome = "vault key error"
			}
			t.Run(mode+"/"+outcome, func(t *testing.T) {
				f := newMutationHostFixture(t)
				deleteRuntime := f.s.DeleteRuntimeBot
				var resources *kernel.BotResources
				if mode != "host cold" {
					cached, _, err := f.s.kernel(f.target)
					if err != nil {
						t.Fatal(err)
					}
					resources = cached.Resources
					if mode == "direct" {
						deleteRuntime = resources.DeleteRuntime
					}
				}
				if mode != "direct" {
					if _, ok, err := f.s.acquireHandle(f.target); err != nil || !ok {
						t.Fatalf("acquire host hold: ok=%v err=%v", ok, err)
					}
				}
				key := filepath.Join(filepath.Dir(vault.GlobalKeyPath(f.s.Root)), "target.key")
				if keyFailure {
					// With no legacy ciphertext, this directory is ignored by migration
					// and fails only os.Remove after the Bot directory was removed.
					if err := os.MkdirAll(key, 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(key, "block"), []byte("fixture"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				removed, err := deleteRuntime(context.Background(), "target")
				if !removed || (err != nil) != keyFailure {
					t.Fatalf("delete: removed=%v err=%v", removed, err)
				}
				if keyFailure {
					var pathErr *os.PathError
					if !errors.As(err, &pathErr) || pathErr.Op != "remove" || pathErr.Path != key {
						t.Fatalf("expected actual vault key removal error, got %v", err)
					}
				}
				if _, err := os.Stat(bot.Dir(f.s.Root, "target")); !os.IsNotExist(err) {
					t.Fatalf("removed fact disagrees with directory: %v", err)
				}
				if mode != "direct" && (f.s.kernels["target"] != nil || f.s.holds["target"] != nil) {
					t.Fatal("removed target retained its host entry or hold")
				}
				if resources != nil {
					if resources.Jobs.AdmissionOpen() || resources.ResumeAdmission() == nil {
						t.Fatal("removed resources reopened admission")
					}
					result := resources.Jobs.RunCall(context.Background(), tools.CallRequest{Name: "after-delete", Run: func(context.Context) tools.CallResult {
						t.Error("removed resources ran a new call")
						return tools.CallResult{}
					}})
					if result.Err == nil {
						t.Fatal("removed Jobs accepted a call")
					}
					if _, err := os.Stat(bot.Dir(f.s.Root, "target")); !os.IsNotExist(err) {
						t.Fatalf("late admission recreated directory: %v", err)
					}
				}
			})
		}
	}
}

func TestBotDeleteRuntimeFailureFact(t *testing.T) {
	for _, mode := range []string{"direct", "host"} {
		for _, failure := range []string{"protected", "canceled", "before removal", "target changed"} {
			if mode == "host" && failure == "target changed" {
				continue // The direct wrapper can receive a name unlike its retained route.
			}
			t.Run(mode+"/"+failure, func(t *testing.T) {
				f := newMutationHostFixture(t)
				r, err := f.s.ResourcesFor(f.target)
				if err != nil {
					t.Fatal(err)
				}
				entry, jobs, ref := f.s.kernels["target"], r.Jobs, r.MCP
				deleteRuntime := f.s.DeleteRuntimeBot
				if mode == "direct" {
					deleteRuntime = r.DeleteRuntime
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				name := "target"
				switch failure {
				case "protected":
					if err := bot.SetPrimary(f.s.Root, name); err != nil {
						t.Fatal(err)
					}
				case "canceled":
					cancel()
				case "before removal":
					if err := os.WriteFile(filepath.Join(f.s.Root, "vault"), []byte("blocked"), 0600); err != nil {
						t.Fatal(err)
					}
				case "target changed":
					name = "other"
					if _, err := bot.Create(f.s.Root, name, "fixture", nil); err != nil {
						t.Fatal(err)
					}
				}
				removed, err := deleteRuntime(ctx, name)
				if removed || err == nil {
					t.Fatalf("failure: removed=%v err=%v", removed, err)
				}
				if failure == "protected" && !errors.Is(err, bot.ErrPrimary) {
					t.Fatalf("expected primary protection: %v", err)
				}
				if failure == "canceled" && !errors.Is(err, context.Canceled) {
					t.Fatalf("expected cancellation: %v", err)
				}
				if f.s.kernels["target"] != entry || r.Jobs != jobs || r.MCP != ref || !r.AdmissionOpen() {
					t.Fatal("failed deletion replaced or closed surviving resources")
				}
				if _, err := os.Stat(bot.Dir(f.s.Root, name)); err != nil {
					t.Fatalf("failed deletion removed target: %v", err)
				}
			})
		}
	}
}
