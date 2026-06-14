// Package npm vendors npm packages for offline use: it parses an npm
// package-lock.json, downloads the locked tarballs, and stores each as an
// uncompressed tar archive. The same archives are read back at runtime via
// [Store], which exposes each package as an [fs.FS] and provides an esbuild
// resolver plugin so vendored packages can be bundled.
package npm

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// Package is a single locked dependency from a package-lock.json.
type Package struct {
	Name      string // package name, e.g. "leaflet" or "@scope/pkg"
	Version   string
	Tarball   string // resolved tarball URL
	Integrity string // Subresource Integrity string, e.g. "sha512-..."
}

// ParseLock returns the runtime registry-tarball packages locked in an npm
// package-lock.json (lockfileVersion 2/3), sorted by name. Dev-only packages
// (e.g. the typescript toolchain) are excluded: they exist only for type
// checking, not for bundling or serving, so they are never vendored.
func ParseLock(data []byte) ([]Package, error) {
	var lock struct {
		Packages map[string]struct {
			Version   string `json:"version"`
			Resolved  string `json:"resolved"`
			Integrity string `json:"integrity"`
			Dev       bool   `json:"dev"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, fmt.Errorf("parse lockfile: %w", err)
	}
	var pkgs []Package
	for key, p := range lock.Packages {
		// skip the root project ("") and linked/workspace entries (no resolved
		// registry tarball), and dev-only dependencies
		if key == "" || p.Dev || !strings.HasPrefix(p.Resolved, "http") {
			continue
		}
		_, name, _ := strings.Cut(key, "node_modules/")
		for strings.Contains(name, "node_modules/") {
			_, name, _ = strings.Cut(name, "node_modules/")
		}
		pkgs = append(pkgs, Package{
			Name:      name,
			Version:   p.Version,
			Tarball:   p.Resolved,
			Integrity: p.Integrity,
		})
	}
	slices.SortFunc(pkgs, func(a, b Package) int { return strings.Compare(a.Name, b.Name) })
	return pkgs, nil
}

// Vendor downloads every package locked in lock and writes it as an
// uncompressed tar archive named "<package>.tar" under dir.
func Vendor(lock []byte, dir string) error {
	pkgs, err := ParseLock(lock)
	if err != nil {
		return err
	}
	for _, p := range pkgs {
		slog.Info("downloading npm package", "name", p.Name, "version", p.Version, "url", p.Tarball)
		ar, err := fetch(p)
		if err != nil {
			return fmt.Errorf("vendor %s@%s: %w", p.Name, p.Version, err)
		}
		out := filepath.Join(dir, filepath.FromSlash(p.Name)+".tar")
		if err := os.MkdirAll(filepath.Dir(out), 0777); err != nil {
			return err
		}
		if err := os.WriteFile(out, ar, 0666); err != nil {
			return err
		}
		slog.Info("vendored npm package", "name", p.Name, "version", p.Version, "bytes", len(ar), "path", out)
	}
	return nil
}

// fetch downloads p's tarball, verifies its integrity, and repackages it.
func fetch(p Package) ([]byte, error) {
	resp, err := http.Get(p.Tarball)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: status %d", p.Tarball, resp.StatusCode)
	}
	tgz, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if err := verify(tgz, p.Integrity); err != nil {
		return nil, err
	}
	return tarballToTar(tgz)
}

// verify checks tgz against an SRI integrity string (only sha512 is emitted by
// modern npm). An empty integrity skips the check.
func verify(tgz []byte, integrity string) error {
	alg, want, ok := strings.Cut(integrity, "-")
	if !ok || alg != "sha512" {
		if integrity == "" {
			return nil
		}
		return fmt.Errorf("unsupported integrity %q", integrity)
	}
	sum := sha512.Sum512(tgz)
	if base64.StdEncoding.EncodeToString(sum[:]) != want {
		return fmt.Errorf("integrity mismatch")
	}
	return nil
}

// tarballToTar repackages an npm package tarball (a gzipped tar whose entries
// are under "package/") into an uncompressed tar rooted at the package. It is
// left uncompressed so successive vendored versions delta well under git.
// Headers are normalized (no mtime/uid/etc.) so the output is reproducible.
func tarballToTar(tgz []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(tgz))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		name := strings.TrimPrefix(path.Clean(h.Name), "package/")
		if path.Ext(name) == ".map" {
			continue // sourcemaps are never used and bloat the vendored archive
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, err
		}
		if err := tw.WriteHeader(&tar.Header{
			Name:     name,
			Typeflag: tar.TypeReg,
			Mode:     0o644,
			Size:     int64(len(data)),
		}); err != nil {
			return nil, err
		}
		if _, err := tw.Write(data); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
