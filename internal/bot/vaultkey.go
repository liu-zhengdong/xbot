package bot

import (
	"fmt"
	"os"
	"path/filepath"
)

// moveVaultKey moves the key file when a bot is renamed. A missing key is not
// an error: a bot that never stored a secret has none. The destination is
// never overwritten, so a rename cannot adopt another bot's key.
func moveVaultKey(root, from, to string) error {
	src := vaultKeyPath(root, from)
	dst := vaultKeyPath(root, to)
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("bot: vault key for %q already exists", to)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return err
	}
	if err := os.Chmod(filepath.Dir(dst), 0700); err != nil {
		return err
	}
	return os.Rename(src, dst)
}

// removeVaultKey deletes a bot's key file. A missing file is success.
func removeVaultKey(root, name string) error {
	err := os.Remove(vaultKeyPath(root, name))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func vaultKeyPath(root, name string) string {
	return filepath.Join(VaultKeysDir(root), name+".key")
}
