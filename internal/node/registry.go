package node

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"xbot/internal/fsutil"
)

var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

// Node is one remote machine the owner has registered.
type Node struct {
	Name  string `json:"name"`
	URL   string `json:"url"`
	Token string `json:"token,omitempty"`
	// ID is that machine's node-id, learned when the owner linked it.
	ID string `json:"id,omitempty"`
}

type file struct {
	Nodes []Node `json:"nodes"`
}

// Path is <root>/nodes.json.
func Path(root string) string { return filepath.Join(root, "nodes.json") }

// List reads the registry. A missing file is an empty list.
func List(root string) ([]Node, error) {
	raw, err := os.ReadFile(Path(root))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var doc file
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	return doc.Nodes, nil
}

// Add stores a node. The name and URL are checked. An existing name is refused.
func Add(root string, n Node) error {
	n.Name = strings.TrimSpace(n.Name)
	n.URL = strings.TrimRight(strings.TrimSpace(n.URL), "/")
	n.Token = strings.TrimSpace(n.Token)
	if !namePattern.MatchString(n.Name) {
		return fmt.Errorf("node: bad name %q", n.Name)
	}
	u, err := url.Parse(n.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("node: url must be http or https")
	}
	nodes, err := List(root)
	if err != nil {
		return err
	}
	for _, existing := range nodes {
		if existing.Name == n.Name {
			return fmt.Errorf("node: %s already exists", n.Name)
		}
	}
	nodes = append(nodes, n)
	return save(root, nodes)
}

// Remove deletes a node by name.
func Remove(root, name string) error {
	name = strings.TrimSpace(name)
	nodes, err := List(root)
	if err != nil {
		return err
	}
	var kept []Node
	found := false
	for _, n := range nodes {
		if n.Name == name {
			found = true
			continue
		}
		kept = append(kept, n)
	}
	if !found {
		return fmt.Errorf("node: %s not found", name)
	}
	return save(root, kept)
}

// Lookup finds one registered node.
func Lookup(root, name string) (Node, error) {
	nodes, err := List(root)
	if err != nil {
		return Node{}, err
	}
	for _, n := range nodes {
		if n.Name == name {
			return n, nil
		}
	}
	return Node{}, fmt.Errorf("node: %s not found", name)
}

// RememberDevice stores the node-id learned from an authenticated info call.
// An empty, local, or relay id is ignored. A matching id is left as it is.
func RememberDevice(root, name, id string) error {
	name = strings.TrimSpace(name)
	id = strings.TrimSpace(id)
	if name == "" || id == "" || id == "local" || id == "relay" || strings.ContainsAny(id, "/\\\n\r") {
		return nil
	}
	nodes, err := List(root)
	if err != nil {
		return err
	}
	for i, n := range nodes {
		if n.Name != name {
			continue
		}
		if n.ID == id {
			return nil
		}
		nodes[i].ID = id
		return save(root, nodes)
	}
	return fmt.Errorf("node: %s not found", name)
}

func save(root string, nodes []Node) error {
	if nodes == nil {
		nodes = []Node{}
	}
	raw, err := fsutil.Marshal(file{Nodes: nodes})
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(Path(root), raw, 0600)
}
