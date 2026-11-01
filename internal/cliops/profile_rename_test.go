package cliops

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestProfileRenamePartialResponse(t *testing.T) {
	root := t.TempDir()
	problem := errors.New("fixture: directory moved before later I/O failure")
	want := Profile{Name: "notes", PreviousName: "ada", Description: "new description", Timezone: "UTC", Avatar: "generated", AvatarSeed: "new#1", Scope: "runtime", Applied: true, Moved: true, AppliedFields: []string{"name", "description", "timezone", "avatar_seed"}, Error: problem.Error()}
	updates := make(chan Update, 1)
	server, err := Start(root, Handlers{Profile: func(update *Update) (Profile, error) {
		if update != nil {
			updates <- *update
		}
		return want, problem
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	name, description, timezone, seed := "notes", "new description", "UTC", "new#1"
	actual, err := Call(context.Background(), server.Endpoint, root, &Update{Name: &name, Description: &description, Timezone: &timezone, AvatarSeed: &seed})
	if err == nil || !reflect.DeepEqual(actual, want) {
		t.Fatalf("partial=%+v error=%v", actual, err)
	}
	update := <-updates
	if update.Name == nil || *update.Name != name || update.Description == nil || *update.Description != description || update.Timezone == nil || *update.Timezone != timezone || update.AvatarSeed == nil || *update.AvatarSeed != seed {
		t.Fatalf("update=%+v", update)
	}
	// Later server failures must preserve the same actual result as a 4xx.
	failed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(want)
	}))
	defer failed.Close()
	actual, err = Call(context.Background(), failed.URL, root, nil)
	if err == nil || !reflect.DeepEqual(actual, want) {
		t.Fatalf("5xx partial=%+v error=%v", actual, err)
	}
}
