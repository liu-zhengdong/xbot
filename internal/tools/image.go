package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	// maxImageSend is the largest image attached to a model call.
	maxImageSend = 10 << 20
	// maxImageFile is the largest file or response we will read before refusing.
	maxImageFile = 32 << 20
)

// ViewImage shows one JPEG, PNG, GIF, or WebP to a model that can see images.
// A model without vision gets a tool error. Confirm never asks; the permission
// table decides.
type ViewImage struct {
	Root    string
	Sandbox string
	// Allowed, when it returns at least one directory, confines a local path
	// to those directories after symlink resolution. A sandbox uses Sandbox.
	Allowed func() []string
	// Model is the bound model id, used in the no-vision error.
	Model string
	// Vision is the model spec's image capability. The kernel sets it.
	Vision bool
	// Guard fetches a public image URL. Nil uses a default guard.
	Guard *Guard
	rootBind
}

func (v *ViewImage) Name() string { return "view_image" }

func (v *ViewImage) Description() string {
	return "Show one local image or public http(s) image to a model that understands images. Pass path or url, not both. JPEG, PNG, GIF, and WebP only, up to 10 MiB. Private addresses are refused. Models without vision return an error."
}

func (v *ViewImage) Parameters() any {
	return objectSchema(map[string]any{
		"path": stringProp("Local file path, relative to the current directory"),
		"url":  stringProp("Public http or https image URL"),
	}, []string{})
}

func (v *ViewImage) Confirm(args json.RawMessage) (bool, string, error) {
	return v.ConfirmContext(context.Background(), args)
}

func (v *ViewImage) ConfirmContext(ctx context.Context, args json.RawMessage) (bool, string, error) {
	in, err := parseImageArgs(args)
	if err != nil {
		return false, "", err
	}
	if in.URL != "" {
		return false, "view " + in.URL, nil
	}
	path, err := v.resolve(ctx, in.Path)
	if err != nil {
		return false, "", err
	}
	return false, "view " + DisplayPath(v.Sandbox, path), nil
}

func (v *ViewImage) ResolvedPath(args json.RawMessage) string {
	return v.ResolvedPathContext(context.Background(), args)
}

func (v *ViewImage) ResolvedPathContext(ctx context.Context, args json.RawMessage) string {
	in, err := parseImageArgs(args)
	if err != nil || in.Path == "" {
		return ""
	}
	path, err := v.resolve(ctx, in.Path)
	if err != nil {
		return ""
	}
	return path
}

type imageArgs struct {
	Path string `json:"path"`
	URL  string `json:"url"`
}

func parseImageArgs(args json.RawMessage) (imageArgs, error) {
	var in imageArgs
	if len(args) > 0 && string(args) != "null" {
		if err := json.Unmarshal(args, &in); err != nil {
			return imageArgs{}, fmt.Errorf("view_image: %w", err)
		}
	}
	in.Path = strings.TrimSpace(in.Path)
	in.URL = strings.TrimSpace(in.URL)
	return in, nil
}

func (v *ViewImage) Run(ctx context.Context, args json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !v.Vision {
		name := strings.TrimSpace(v.Model)
		if name == "" {
			name = "this model"
		}
		return "", fmt.Errorf("model %s does not support vision", name)
	}
	in, err := parseImageArgs(args)
	if err != nil {
		return "", err
	}
	if in.Path != "" && in.URL != "" {
		return "", errors.New("view_image: pass path or url, not both")
	}
	if in.Path == "" && in.URL == "" {
		return "", errors.New("view_image: path or url is required")
	}
	var mime string
	var data []byte
	var label string
	if in.URL != "" {
		mime, data, label, err = v.fromURL(ctx, in.URL)
	} else {
		mime, data, label, err = v.fromFile(ctx, in.Path)
	}
	if err != nil {
		return "", err
	}
	sinkFrom(ctx).add(AttachedImage{MIME: mime, Data: data})
	return label, nil
}

func (v *ViewImage) fromFile(ctx context.Context, raw string) (string, []byte, string, error) {
	path, err := v.resolve(ctx, raw)
	if err != nil {
		return "", nil, "", fmt.Errorf("view_image: %w", err)
	}
	label := DisplayPath(v.Sandbox, path)
	st, err := os.Stat(path)
	if err != nil {
		return "", nil, "", fmt.Errorf("view_image: %s", RewriteHost(v.Sandbox, path, err.Error()))
	}
	if st.IsDir() {
		return "", nil, "", fmt.Errorf("view_image: %s is a directory", label)
	}
	if st.Size() > maxImageFile {
		return "", nil, "", fmt.Errorf("view_image: %s is larger than 32 MiB", label)
	}
	if st.Size() > maxImageSend {
		return "", nil, "", fmt.Errorf("view_image: %s is larger than 10 MiB", label)
	}
	if st.Size() == 0 {
		return "", nil, "", fmt.Errorf("view_image: %s is not a JPEG, PNG, GIF, or WebP", label)
	}
	f, err := os.Open(path)
	if err != nil {
		return "", nil, "", fmt.Errorf("view_image: %s", RewriteHost(v.Sandbox, path, err.Error()))
	}
	defer f.Close()
	buf := make([]byte, st.Size())
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF {
		return "", nil, "", fmt.Errorf("view_image: %s", RewriteHost(v.Sandbox, path, err.Error()))
	}
	data := buf[:n]
	mime, ok := imageMIME(data)
	if !ok {
		return "", nil, "", fmt.Errorf("view_image: %s is not a JPEG, PNG, GIF, or WebP", label)
	}
	return mime, data, fmt.Sprintf("path: %s\nmime: %s\nbytes: %d", label, mime, len(data)), nil
}

func (v *ViewImage) resolve(ctx context.Context, raw string) (string, error) {
	path, err := ResolveRoot(v.rootBind.dir(ctx, v.Root), AliasCase(v.Sandbox, raw))
	if err != nil {
		return "", err
	}
	if v.Sandbox != "" {
		if err := Confine(v.Sandbox, path); err != nil {
			return "", err
		}
		return path, nil
	}
	if v.Allowed != nil {
		roots := v.Allowed()
		if len(roots) > 0 {
			if err := insideAny(path, roots); err != nil {
				return "", err
			}
		}
	}
	return path, nil
}

func insideAny(path string, roots []string) error {
	for _, root := range roots {
		if root == "" {
			continue
		}
		if err := Confine(root, path); err == nil {
			return nil
		}
	}
	return errors.New("path is outside the workspace")
}

func (v *ViewImage) fromURL(ctx context.Context, raw string) (string, []byte, string, error) {
	g := v.Guard
	if g == nil {
		g = &Guard{}
	}
	if g.MaxBytes <= 0 || g.MaxBytes > maxImageFile {
		copied := *g
		copied.MaxBytes = maxImageFile
		g = &copied
	}
	res, err := g.Fetch(ctx, raw)
	if err != nil {
		return "", nil, "", fmt.Errorf("view_image: %s", err.Error())
	}
	if res.Truncated || int64(len(res.Body)) > maxImageSend {
		return "", nil, "", errors.New("view_image: image is larger than 10 MiB")
	}
	mime, ok := imageMIME(res.Body)
	if !ok {
		return "", nil, "", errors.New("view_image: response is not a JPEG, PNG, GIF, or WebP")
	}
	text := fmt.Sprintf("url: %s\nmime: %s\nbytes: %d", res.URL, mime, len(res.Body))
	return mime, append([]byte(nil), res.Body...), wrapUntrusted("view_image", "image", urlHost(res.URL), time.Now().UTC(), text), nil
}

func imageMIME(b []byte) (string, bool) {
	switch {
	case len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF:
		return "image/jpeg", true
	case len(b) >= 8 && bytes.Equal(b[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}):
		return "image/png", true
	case len(b) >= 6 && (bytes.HasPrefix(b, []byte("GIF87a")) || bytes.HasPrefix(b, []byte("GIF89a"))):
		return "image/gif", true
	case len(b) >= 12 && bytes.Equal(b[:4], []byte("RIFF")) && bytes.Equal(b[8:12], []byte("WEBP")):
		return "image/webp", true
	default:
		return "", false
	}
}

func urlHost(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return "web"
}
