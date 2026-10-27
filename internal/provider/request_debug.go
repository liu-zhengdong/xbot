package provider

import (
	"log"
	"os"
	"path/filepath"
	"time"
)

// dumpRequest writes exactly the encoded body, including any secrets it contains.
// The marker is checked on every attempt so a running process can be toggled
// without restarting. This is a per-user diagnostic, independent of --root.
func dumpRequest(protocol string, raw []byte) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	root := filepath.Join(home, ".xbot")
	if st, err := os.Stat(filepath.Join(root, "provider-debug.enabled")); err != nil || !st.Mode().IsRegular() {
		return
	}
	if err := writeRequestDump(filepath.Join(root, "provider-debug"), protocol, raw); err != nil {
		// Debugging must not stop a chat; never include the body or headers here.
		log.Printf("provider: request debug dump failed: %v", err)
	}
}

func writeRequestDump(dir, protocol string, raw []byte) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, time.Now().UTC().Format("20060102T150405.000000000Z")+"-"+protocol+"-*.json")
	if err != nil {
		return err
	}
	_, err = f.Write(raw)
	closeErr := f.Close()
	if err != nil {
		_ = os.Remove(f.Name())
		return err
	}
	if closeErr != nil {
		_ = os.Remove(f.Name())
	}
	return closeErr
}
