package bot

import "testing"

func TestSemanticEnvironmentReceiptFraming(t *testing.T) {
	for _, payload := range []string{"记忆已更新：记住用户的新偏好。", `{"memory":{"add":["fact"]}}`} {
		receipt := FrameEnvironment(payload)
		body, got, after := SplitEnvironmentReceipt("written\n\n" + receipt + "\n<system-reminder>next</system-reminder>")
		if body != "written" || got != receipt || after != "\n<system-reminder>next</system-reminder>" {
			t.Fatalf("%q %q %q", body, got, after)
		}
	}
	incomplete := "written\n\n" + EnvironmentOpen + "记忆已更新"
	body, receipt, _ := SplitEnvironmentReceipt(incomplete)
	if body != incomplete || receipt != "" {
		t.Fatal("incomplete receipt accepted")
	}
}
