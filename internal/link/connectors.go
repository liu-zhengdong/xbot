package link

import (
	"bytes"
	"encoding/json"
	"errors"

	_ "embed"
)

//go:embed connectors.json
var connectorCatalog []byte

// CatalogItem is one row of the embedded connector catalog. Secret values are not stored.
type CatalogItem struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Summary    string   `json:"summary"`
	Transport  string   `json:"transport"`
	Command    string   `json:"command,omitempty"`
	Args       []string `json:"args,omitempty"`
	URL        string   `json:"url,omitempty"`
	SecretEnvs []string `json:"secret_envs,omitempty"`
	// Category is the store chip (dev, ...). Hidden rows are not shown in the store.
	Category string `json:"category,omitempty"`
	Hidden   bool   `json:"hidden,omitempty"`
}

// Connectors reads the embedded catalog. It is not a hosted marketplace.
func Connectors() ([]CatalogItem, error) {
	dec := json.NewDecoder(bytes.NewReader(connectorCatalog))
	dec.DisallowUnknownFields()
	var list []CatalogItem
	if err := dec.Decode(&list); err != nil {
		return nil, err
	}
	for i := range list {
		if err := ValidateMCP(catalogServer(list[i])); err != nil {
			return nil, err
		}
	}
	return list, nil
}

// LookupConnector finds a catalog row by id.
func LookupConnector(id string) (CatalogItem, error) {
	list, err := Connectors()
	if err != nil {
		return CatalogItem{}, err
	}
	for _, c := range list {
		if c.ID == id {
			return c, nil
		}
	}
	return CatalogItem{}, ErrUnknown
}

// EnableConnector copies one catalog row into the mcp.json at path. Run again, it changes nothing.
// An existing entry that lacks a key the row names (left by an older keyless
// add) is replaced by the row, so the key reaches the server.
func EnableConnector(path, id string) error {
	c, err := LookupConnector(id)
	if err != nil {
		return err
	}
	err = AddMCP(path, catalogServer(c))
	if !errors.Is(err, ErrMCPExists) {
		return err
	}
	return ReplaceMCP(path, catalogServer(c), func(old MCPServer) bool { return !HasSecretEnvs(old, c.SecretEnvs) })
}

// HasSecretEnvs says the entry names every key in names.
func HasSecretEnvs(s MCPServer, names []string) bool {
	for _, n := range names {
		found := false
		for _, have := range s.SecretEnvs {
			if have == n {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// SecretLookup reads one value from a Bot's secrets file at call time, so a
// key filled after the server was mounted is still found on its next start.
func SecretLookup(path string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		m, err := ReadSecrets(path)
		if err != nil {
			return "", false
		}
		v, ok := m[name]
		return v, ok
	}
}

func catalogServer(c CatalogItem) MCPServer {
	return MCPServer{
		Name:       c.ID,
		Command:    c.Command,
		Args:       append([]string{}, c.Args...),
		URL:        c.URL,
		SecretEnvs: append([]string{}, c.SecretEnvs...),
	}
}
