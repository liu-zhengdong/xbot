//go:build smoke

package smoke

import (
	"encoding/json"
	"os"
	"os/exec"
	"testing"

	"xbot/internal/browsertest"
)

func TestRandomAvatarJourney(t *testing.T) {
	t.Setenv("XBOT_TEST_CHROME", browsertest.Chrome(t))
	srv := Start(t, StartStub(t))
	srv.CreateBot("Avatar", "随机头像验收")
	cmd := exec.Command("node", "../../internal/web/testdata/random-avatar.cjs", srv.URL)
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("random avatar journey: %v\n%s", err, out)
	} else {
		t.Log(string(out))
	}
	readSeed := func(name string) string {
		t.Helper()
		status, body := srv.do("GET", "/api/bots/"+name, nil)
		var profile struct {
			AvatarSeed string `json:"avatar_seed"`
		}
		if status != 200 || json.Unmarshal(body, &profile) != nil || profile.AvatarSeed == "" {
			t.Fatalf("read avatar %s: %d", name, status)
		}
		return profile.AvatarSeed
	}
	seed := readSeed("Avatar")
	if status, body := srv.do("PATCH", "/api/bots/Avatar", map[string]string{"name": "AvatarRenamed"}); status != 200 {
		t.Fatalf("rename: %d %s", status, body)
	}
	if readSeed("AvatarRenamed") != seed {
		t.Fatal("rename changed the random avatar")
	}
	srv.Restart()
	if readSeed("AvatarRenamed") != seed {
		t.Fatal("process restart changed the random avatar")
	}
}
