package main

import (
	"fmt"
	"os"
	"strings"

	"xbot/internal/cliops"
)

// runtimeSource resolves the current source after offline help has returned.
// Domain clients validate the endpoint URL and choose their own call context.
func runtimeSource(operation, rawRoot string, rootExplicit bool) (root, endpoint string, err error) {
	root = os.Getenv("XBOT_ROOT")
	if strings.TrimSpace(root) == "" {
		return "", "", fmt.Errorf("%s: --self requires XBOT_ROOT from an active runtime", operation)
	}
	root, err = cliops.CanonicalRoot(root)
	if err != nil {
		return "", "", fmt.Errorf("%s: runtime root: %w", operation, err)
	}
	if rootExplicit {
		if strings.TrimSpace(rawRoot) == "" {
			return "", "", fmt.Errorf("%s: explicit --root must name the runtime data root", operation)
		}
		given, err := cliops.CanonicalRoot(rawRoot)
		if err != nil {
			return "", "", fmt.Errorf("%s: explicit root: %w", operation, err)
		}
		if given != root {
			return "", "", fmt.Errorf("%s: runtime data root mismatch", operation)
		}
	}
	endpoint = os.Getenv("XBOT_ENDPOINT")
	if endpoint == "" {
		return "", "", fmt.Errorf("%s: --self requires an active local XBot endpoint", operation)
	}
	return root, endpoint, nil
}
