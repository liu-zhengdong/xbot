package upgrade

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ErrUnsupported means xbot cannot install its own update on this platform, so
// the caller offers the download instead.
var ErrUnsupported = errors.New("xbot cannot update itself on this platform")

// executable is the running binary with symlinks resolved. Tests replace it.
var executable = func() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(p)
}

// Bundle is the .app the running binary lives in, or "" when it is not in one.
func Bundle() string {
	if goos != "darwin" {
		return ""
	}
	exe, err := executable()
	if err != nil {
		return ""
	}
	// …/XBot.app/Contents/MacOS/XBot
	macOS := filepath.Dir(exe)
	if filepath.Base(macOS) != "MacOS" || filepath.Base(filepath.Dir(macOS)) != "Contents" {
		return ""
	}
	app := filepath.Dir(filepath.Dir(macOS))
	if filepath.Ext(app) != ".app" {
		return ""
	}
	return app
}

// stagePath is where one release's package is unpacked: <root>/update/<tag>.
func stagePath(root, tag string) string {
	return filepath.Join(root, StageDir, strings.TrimPrefix(strings.TrimSpace(tag), "v"))
}

// manifest records what a staged package is, so a restart can pick it up.
type manifest struct {
	Version string `json:"version"`
	Asset   string `json:"asset"`
}

// Stage downloads the release package for this platform, checks it against the
// release's SHA256SUMS, unpacks it under root/update/<tag>, and returns the path
// to install from. Nothing that is already installed is touched.
func Stage(ctx context.Context, info Info, root string, progress func(done, total int64)) (string, error) {
	if !info.Available {
		return "", ErrNotNewer
	}
	name := AssetName(info.Latest)
	if name == "" {
		return "", ErrUnsupported
	}
	url := info.AssetURL(name)
	if url == "" {
		return "", fmt.Errorf("release %s has no asset %s", info.Latest, name)
	}
	// One staged package is all that is ever used, so whatever an earlier
	// check left for another version goes now.
	ClearStaged(root)
	dir := stagePath(root, info.Latest)
	if err := os.RemoveAll(dir); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	archive := filepath.Join(dir, name)
	if err := download(ctx, url, archive, MaxDownload, progress); err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	if err := verifyChecksum(ctx, info, name, archive); err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	var staged string
	var err error
	switch {
	case strings.HasSuffix(name, ".zip"):
		staged, err = unpackZip(archive, dir)
	case strings.HasSuffix(name, ".tar.gz"):
		staged, err = unpackTarGz(archive, dir)
	default:
		err = fmt.Errorf("no unpacker for %s", name)
	}
	if err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	os.Remove(archive)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"),
		[]byte(fmt.Sprintf(`{"version":%q,"asset":%q}`+"\n", info.Latest, name)), 0o644); err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	return staged, nil
}

// unpackZip extracts a macOS app bundle and returns the .app inside it.
func unpackZip(archive, dir string) (string, error) {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return "", err
	}
	defer zr.Close()
	for _, f := range zr.File {
		target, err := safeJoin(dir, f.Name)
		if err != nil {
			return "", err
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return "", err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return "", err
		}
		mode := f.Mode().Perm()
		if mode == 0 {
			mode = 0o644
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		err = copyFile(target, rc, mode)
		rc.Close()
		if err != nil {
			return "", err
		}
	}
	app := filepath.Join(dir, "XBot.app")
	if _, err := os.Stat(filepath.Join(app, "Contents", "MacOS", "XBot")); err != nil {
		return "", errors.New("the package does not hold XBot.app")
	}
	return app, nil
}

// unpackTarGz extracts the Linux tarball and returns the xbot binary in it.
func unpackTarGz(archive, dir string) (string, error) {
	f, err := os.Open(archive)
	if err != nil {
		return "", err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	binary := ""
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		target, err := safeJoin(dir, h.Name)
		if err != nil {
			return "", err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return "", err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return "", err
			}
			mode := os.FileMode(h.Mode).Perm()
			if mode == 0 {
				mode = 0o644
			}
			if err := copyFile(target, tr, mode); err != nil {
				return "", err
			}
			if filepath.Base(target) == "xbot" {
				binary = target
			}
		}
	}
	if binary == "" {
		return "", errors.New("the package does not hold an xbot binary")
	}
	return binary, nil
}

// safeJoin keeps an archive entry inside dir: no absolute paths, no "..".
func safeJoin(dir, name string) (string, error) {
	name = filepath.Clean(filepath.FromSlash(name))
	if name == "." || filepath.IsAbs(name) || !filepath.IsLocal(name) {
		return "", fmt.Errorf("the package holds an unsafe path: %q", name)
	}
	return filepath.Join(dir, name), nil
}

func copyFile(target string, src io.Reader, mode os.FileMode) error {
	dst, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return err
	}
	return dst.Close()
}
