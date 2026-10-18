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
