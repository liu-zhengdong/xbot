package vault

import "testing"

func TestLooksLikeSecret(t *testing.T) {
	yes := []string{
		"-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAA\n",
		"token: ghp_" + "aB3dE5gH7jK9mN1pQ3sT5vX7zA9cE1gI3kM5",
		"GITHUB_TOKEN=github_pat_" + "11ABCDEFG0123456789_abcdefghijklmnopqrstuvwxyzABCD",
		`"key": "sk-` + `proj-AbCdEf0123456789GhIjKl"`,
		"STRIPE=sk_live_" + "4eC39HqLyjWDarjtT1zdp7dc",
		"slack xoxb-" + "1234567890-abcdefghij",
		"aws_access_key_id = AKIA" + "IOSFODNN7EXAMPLQ",
		"maps AIza" + "SyDdI0hCZtE6vySjMm-WEfRq3CPzqKqqsHI",
		"glpat-" + "xxxXXXxxx0123456789ab",
		"Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U",
		"DATABASE_URL=postgres://app:" + "s3cr3tpass@db.internal/app",
		"password = hunter22",
		`api_key: "a1b2c3d4e5f6"`,
		"+  value = Zx8Qp2Lm9Vt4Rk7Wn3Hs6Jd1Fg5Bc0Ya" + "Pq",
	}
	for _, s := range yes {
		if !LooksLikeSecret(s) {
			t.Errorf("missed %q", s)
		}
	}
	no := []string{
		"",
		"func main() {\n\tfmt.Println(\"hello\")\n}",
		"token := strings.TrimSpace(getenv(p.APIKeyEnv))",
		"password = os.Getenv(\"DB_PASSWORD\")",
		"api_key: your_api_key_here",
		"secret: changeme123",
		"commit 9f86d081884c7d659a2feaa0c55ad015a3bf4f1b",
		"sha256 e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		"TestForeignHistoryKeepsCloserAtCap",
		"https://example.com/docs/getting-started",
		"- tokens: 1200 -> 1350",
	}
	for _, s := range no {
		if LooksLikeSecret(s) {
			t.Errorf("false positive %q", s)
		}
	}
}
