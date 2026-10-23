package provider

import (
	"context"
	"testing"
)

type stub struct{ name string }

func (s stub) Chat(context.Context, Request, func(string)) (Message, error) {
	return Message{Content: TextContent(s.name)}, nil
}

func TestFixedSelectorDoesNotRoute(t *testing.T) {
	p, err := FixedSelector{P: stub{name: "bound"}}.Select(context.Background(), Hint{Bot: "ada"})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := p.Chat(context.Background(), Request{}, nil)
	if err != nil || msg.Text() != "bound" {
		t.Fatalf("%+v %v", msg, err)
	}
	if _, err := (FixedSelector{}).Select(context.Background(), Hint{}); err == nil {
		t.Fatal("expected unbound error")
	}
}
