package provider

import "testing"

func TestLocalCallNonceDiffers(t *testing.T) {
	if a, b := localCallNonce(), localCallNonce(); a == "" || a == b {
		t.Fatalf("local call nonce must be unique per turn: %q %q", a, b)
	}
}
