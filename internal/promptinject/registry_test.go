package promptinject

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestRegisterOrderReplaceAndEmpty(t *testing.T) {
	var r Registry
	source := func(name string, order int, text string) Source {
		return Source{Name: name, Order: order, Render: func(context.Context) (string, error) { return text, nil }}
	}
	r.Register(source("reminder", 20, "reminder"))
	r.Register(source("environment", 0, "old"))
	r.Register(source("empty", 10, ""))
	r.Register(source("environment", 0, "clock"))
	got, err := r.Render(context.Background())
	if err != nil || !reflect.DeepEqual(got, []string{"clock", "reminder"}) {
		t.Fatalf("%v %v", got, err)
	}
	failure := errors.New("sample failed")
	r.Register(Source{Name: "environment", Render: func(context.Context) (string, error) { return "", failure }})
	if _, err := r.Render(context.Background()); !errors.Is(err, failure) {
		t.Fatal(err)
	}
}
