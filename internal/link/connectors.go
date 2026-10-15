package link

import (
	"bytes"
	"encoding/json"

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

// EnableConnector copies one catalog row into this bot's mcp.json.
func EnableConnector(path, id string) error {
	c, err := LookupConnector(id)
	if err != nil {
		return err
	}
	return AddMCP(path, catalogServer(c))
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
