package link

import "testing"

func TestGenericShellIsNotRegistered(t *testing.T) {
	for _, tool := range NewToolsLink().Tools() {
		if tool.Name() == "shell" {
			t.Fatal("generic shell is exposed to the model")
		}
	}
}
