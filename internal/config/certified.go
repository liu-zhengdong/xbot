package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"xbot/internal/fsutil"
)

// CertifiedFile is the list of provider/model ids that passed the proactivity suite.
// It stores no secrets.
type CertifiedFile struct {
	Models []string `json:"models"`
}

// ModelInfo is one configured model and whether it is on the certified list.
type ModelInfo struct {
	ID           string `json:"id"`
	Provider     string `json:"provider"`
	Model        string `json:"model"`
	Context      int    `json:"context"`
	Certified    bool   `json:"certified"`
	Protocol     string `json:"protocol"`
	Vision       bool   `json:"vision"`
	HostedSearch bool   `json:"hosted_web_search"`
	Available    bool   `json:"available"`

	HostedImageGen bool   `json:"hosted_image_generation"`
	ImageGenModel  string `json:"image_generation_model,omitempty"`
	// Registered says the model is in providers.json, so its settings can be saved.
	Registered bool `json:"registered"`
	// Brand and OAuth let the UI show the vendor's display name, not the
	// configured provider name.
	Custom bool   `json:"custom,omitempty"`
	Title  string `json:"title,omitempty"`
	Brand  string `json:"brand,omitempty"`
	OAuth  string `json:"oauth,omitempty"`
}

// CertifiedPath is the list under the data root.
func CertifiedPath(root string) string { return filepath.Join(root, "certified.json") }

// LoadCertified reads the list. A missing file is empty.
func LoadCertified(root string) (CertifiedFile, error) {
	raw, err := os.ReadFile(CertifiedPath(root))
	if errors.Is(err, os.ErrNotExist) {
		return CertifiedFile{}, nil
	}
	if err != nil {
		return CertifiedFile{}, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var f CertifiedFile
	if err := dec.Decode(&f); err != nil {
		return CertifiedFile{}, fmt.Errorf("certified: %w", err)
	}
	return f, nil
}

// ModelID is the certified-list key.
func ModelID(provider, model string) string {
	return provider + "/" + model
}

// SplitModelID splits provider/model on the first slash.
func SplitModelID(id string) (string, string, error) {
	provider, model, ok := strings.Cut(strings.TrimSpace(id), "/")
	if !ok || provider == "" || model == "" {
		return "", "", fmt.Errorf("eval: want provider/model, got %q", id)
	}
	return provider, model, nil
}

// IsCertified reports whether id is on the list.
func IsCertified(root, id string) (bool, error) {
	f, err := LoadCertified(root)
	if err != nil {
		return false, err
	}
	for _, m := range f.Models {
		if m == id {
			return true, nil
		}
	}
	return false, nil
}

// Certify adds id to the list. A second call leaves the file as it was.
func Certify(root, id string) error {
	if _, _, err := SplitModelID(id); err != nil {
		return err
	}
	f, err := LoadCertified(root)
	if err != nil {
		return err
	}
	for _, m := range f.Models {
		if m == id {
			return nil
		}
	}
	f.Models = append(f.Models, id)
	sort.Strings(f.Models)
	raw, err := fsutil.Marshal(f)
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(CertifiedPath(root), raw, 0600)
}

// AllowBind refuses an uncertified model unless uncertified is set.
func AllowBind(root, provider, model string, uncertified bool) error {
	ok, err := IsCertified(root, ModelID(provider, model))
	if err != nil {
		return err
	}
	if ok || uncertified {
		return nil
	}
	return fmt.Errorf("providers: %s is not certified; pass --uncertified", ModelID(provider, model))
}

// VisibleModels lists configured models. all is false when only certified models should show.
func VisibleModels(root string, all bool) ([]ModelInfo, error) {
	f, err := Load(root)
	if err != nil {
		return nil, err
	}
	cert, err := LoadCertified(root)
	if err != nil {
		return nil, err
	}
	have := map[string]bool{}
	for _, id := range cert.Models {
		have[id] = true
	}
	var out []ModelInfo
	for _, p := range f.Providers {
		for _, m := range p.Models {
			id := ModelID(p.Name, m.Name)
			ok := have[id]
			if !all && !ok {
				continue
			}
			out = append(out, ModelInfo{
				Protocol: p.WireProtocol(), Vision: m.SeesImages(), HostedSearch: m.HasHostedSearch(p.WireProtocol()), Available: SupportedBinding(p.WireProtocol(), m) == nil,
				HostedImageGen: m.HasHostedImageGen(p.WireProtocol()), ImageGenModel: m.ImageGenModel, Registered: true,
				ID: id, Provider: p.Name, Model: m.Name, Context: m.Context, Certified: ok,
				Custom: IsCustomProvider(p), Title: ProviderTitle(p), Brand: BrandForBase(p.BaseURL), OAuth: p.OAuth,
			})
		}
	}
	if out == nil {
		out = []ModelInfo{}
	}
	return out, nil
}
