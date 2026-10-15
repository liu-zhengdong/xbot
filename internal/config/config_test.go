package config

import "testing"

func TestFromEnvDefaults(t *testing.T) {
	m := FromEnv(func(string) string { return "" })
	if m.BaseURL != defaultBaseURL || m.Model != defaultModel {
		t.Fatalf("defaults: %+v", m)
	}
	if m.APIKey != "" {
		t.Fatal("expected empty key")
	}
}

func TestFromEnvOverrides(t *testing.T) {
	env := map[string]string{
		"XBOT_API_KEY":  "secret",
		"XBOT_BASE_URL": "http://127.0.0.1:9/v1/",
		"XBOT_MODEL":    "mock",
	}
	m := FromEnv(func(k string) string { return env[k] })
	if m.APIKey != "secret" || m.BaseURL != "http://127.0.0.1:9/v1" || m.Model != "mock" {
		t.Fatalf("%+v", m)
	}
}
