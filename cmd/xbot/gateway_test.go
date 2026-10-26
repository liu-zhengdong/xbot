package main

import (
	"strings"
	"testing"
)

func TestGatewayServeNeedsTokenOrOpen(t *testing.T) {
	t.Setenv("XBOT_GATEWAY_TOKEN", "")
	err := cmdGateway(t.TempDir(), []string{"serve", "--addr", "127.0.0.1:0"})
	if err == nil || !strings.Contains(err.Error(), "XBOT_GATEWAY_TOKEN") {
		t.Fatalf("err = %v", err)
	}
}

func TestGatewayServeBehindProxyFlag(t *testing.T) {
	t.Setenv("XBOT_GATEWAY_TOKEN", "")
	err := cmdGateway(t.TempDir(), []string{"serve", "--behind-proxy", "--addr", "127.0.0.1:0"})
	if err == nil || !strings.Contains(err.Error(), "XBOT_GATEWAY_TOKEN") {
		t.Fatalf("behind-proxy flag was not accepted: %v", err)
	}
}

func TestGatewayInstallCapFromEnv(t *testing.T) {
	for env, want := range map[string]int{"1000": 1000, " 200 ": 200, "": 64, "0": 64, "-5": 64, "lots": 64} {
		t.Setenv("XBOT_GATEWAY_MAX_INSTALLS", env)
		if got := gatewayInstallCap(); got != want {
			t.Errorf("%q: cap %d, want %d", env, got, want)
		}
	}
}
