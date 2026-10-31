package envprompt

import (
	"context"
	"fmt"
	"testing"
)

func TestRegisterSamplePromptAndCommit(t *testing.T) {
	ctx := context.Background()
	var e Env
	minute := 17
	samples := 0
	e.Register(Field{Key: "time", Sample: func(context.Context) (Value, error) { samples++; return minute, nil }, Prompt: func(v Value, _ Change) string { return fmt.Sprintf("现在是第 %d 分钟", v) }})
	first, commit, err := e.Prepare(ctx, false)
	if err != nil || first != "现在是第 17 分钟" {
		t.Fatalf("%q %v", first, err)
	}
	// A budget preview / failed append must not mark anything announced.
	retry, _, _ := e.Prepare(ctx, false)
	if retry != first {
		t.Fatal("uncommitted rendering suppressed")
	}
	commit()
	same, _, _ := e.Prepare(ctx, false)
	if same != "" || samples != 3 || e.Current("time") != minute {
		t.Fatal("must sample truth without repeating announcement")
	}
	minute++
	next, commit, _ := e.Prepare(ctx, false)
	if next != "现在是第 18 分钟" {
		t.Fatal(next)
	}
	commit()
	full, _, _ := e.Prepare(ctx, true)
	if full != next {
		t.Fatal("full snapshot did not restore semantic text")
	}
}

func TestRegisterReplacesFieldAndEmptyPrompt(t *testing.T) {
	var e Env
	e.Register(Field{Key: "a", Sample: func(context.Context) (Value, error) { return "old", nil }, Prompt: func(v Value, _ Change) string { return v.(string) }})
	e.Register(Field{Key: "a", Sample: func(context.Context) (Value, error) { return "new", nil }, Prompt: func(Value, Change) string { return "" }})
	text, _, err := e.Prepare(context.Background(), true)
	if err != nil || text != "" || e.Current("a") != "new" {
		t.Fatalf("%q %v", text, err)
	}
}
