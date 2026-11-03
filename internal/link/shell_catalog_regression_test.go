package link

import (
	"testing"

	"xbot/internal/tools"
)

func TestGenericShellIsNotRegistered(t *testing.T) {
	for _, tool := range tools.NewShells() {
		if tool.Name() == "shell" {
			t.Fatal("generic shell is exposed to the model")
		}
	}
}
