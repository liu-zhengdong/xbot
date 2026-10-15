package link

import (
	"encoding/json"
	"errors"
	"os"
	"strings"

	"xbot/internal/fsutil"
)

type secretFile struct {
	Values map[string]string `json:"values"`
}

// WriteSecrets merges name/value pairs into path. The file mode is 0600.
// Empty names and empty values are skipped.
func WriteSecrets(path string, updates map[string]string) error {
	if len(updates) == 0 {
		return nil
	}
	return fsutil.WithLock(path+".lock", func() error {
		cur, err := readSecrets(path)
		if err != nil {
			return err
		}
		for name, value := range updates {
			name = strings.TrimSpace(name)
			if name == "" || value == "" {
				continue
			}
			cur[name] = value
		}
		return writeSecrets(path, cur)
	})
}

// DropSecrets removes names from path. A missing file is success.
func DropSecrets(path string, names []string) error {
	if len(names) == 0 {
		return nil
	}
	return fsutil.WithLock(path+".lock", func() error {
		cur, err := readSecrets(path)
		if err != nil {
			return err
		}
		if len(cur) == 0 {
			return nil
		}
		for _, name := range names {
			delete(cur, strings.TrimSpace(name))
		}
		return writeSecrets(path, cur)
	})
}

// ReadSecrets returns the stored values. A missing file is an empty map.
func ReadSecrets(path string) (map[string]string, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	} else if err != nil {
		return nil, err
	}
	var out map[string]string
	err := fsutil.WithLock(path+".lock", func() error {
		var err error
		out, err = readSecrets(path)
		return err
	})
	return out, err
}

func readSecrets(path string) (map[string]string, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(raw)) == "" {
		return map[string]string{}, nil
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	var file secretFile
	if err := dec.Decode(&file); err != nil {
		return nil, err
	}
	if file.Values == nil {
		return map[string]string{}, nil
	}
	return file.Values, nil
}

func writeSecrets(path string, values map[string]string) error {
	if values == nil {
		values = map[string]string{}
	}
	raw, err := fsutil.Marshal(secretFile{Values: values})
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(path, raw, 0600)
}
