// Package asset loads, transforms, content-addresses, and compresses static
// assets from a filesystem. How (and whether) an asset is compiled is
// configured when it is registered; loading, transformation, hashing, and
// compression are each computed lazily and memoized.
package asset

import (
	"bytes"
	"crypto/sha1"
	"encoding/base32"
	"fmt"
	"io/fs"
	"mime"
	"path"
	"strings"
	"sync"

	"github.com/klauspost/compress/gzip"
	"github.com/klauspost/compress/zstd"
)

// Compiler transforms an asset's raw bytes during its build. It receives the
// source name and bytes, and resolve, which maps a referenced asset's source
// name to its public (content-addressed) name. It returns the transformed
// bytes and the file extension of the result, which may differ from the source
// (e.g. ".js" when compiling ".ts").
type Compiler func(name string, data []byte, resolve func(string) (string, error)) (out []byte, ext string, err error)

// Set is a collection of assets loaded from a filesystem. References between
// assets, resolved during compilation, are looked up within the set.
type Set struct {
	fsys     fs.FS
	mimeType func(ext string) string
	mu       sync.Mutex
	assets   map[string]*Asset
}

// NewSet returns a Set loading assets from fsys. mimeType maps a file
// extension (including the leading dot) to a MIME type; where it returns ""
// (or is nil), the type is inferred with [mime.TypeByExtension].
func NewSet(fsys fs.FS, mimeType func(ext string) string) *Set {
	return &Set{fsys: fsys, mimeType: mimeType, assets: map[string]*Asset{}}
}

// Built is the compiled, content-addressed identity form of an asset.
type Built struct {
	Name        string // content-addressed public name (e.g. "app-ab12cd34ef.css")
	ContentType string
	Hash        string
	Data        []byte // identity encoding
}

// Blob is a Built asset together with its compressed encodings.
type Blob struct {
	Name        string
	ContentType string
	Hash        string
	Data        map[string][]byte // content-encoding ("" identity, "gzip", "zstd") -> body
}

// Asset is a single file in a Set together with its built forms.
type Asset struct {
	source string
	built  func() (Built, error) // sync.OnceValues: load + compile + hash
	blob   func() (Blob, error)  // sync.OnceValues: built + compress
}

// Source returns the asset's path within its set's filesystem.
func (a *Asset) Source() string { return a.source }

// Built returns the compiled, content-addressed identity form of the asset.
func (a *Asset) Built() (Built, error) { return a.built() }

// Blob returns the asset's built form together with its compressed encodings.
func (a *Asset) Blob() (Blob, error) { return a.blob() }

// Option configures an asset at registration.
type Option func(*config)

type config struct {
	compile Compiler
}

// Compile sets the compiler used to transform the asset during its build.
func Compile(c Compiler) Option {
	return func(cfg *config) { cfg.compile = c }
}

// Register adds an asset loaded from source, or returns the one already
// registered for it. Compilation is configured by opts.
func (s *Set) Register(source string, opts ...Option) *Asset {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a, ok := s.assets[source]; ok {
		return a
	}
	var cfg config
	for _, opt := range opts {
		opt(&cfg)
	}
	a := &Asset{source: source}
	a.built = sync.OnceValues(func() (Built, error) {
		return s.build(source, cfg)
	})
	a.blob = sync.OnceValues(func() (Blob, error) {
		b, err := a.built()
		if err != nil {
			return Blob{}, err
		}
		return compress(b)
	})
	s.assets[source] = a
	return a
}

func (s *Set) build(source string, cfg config) (Built, error) {
	data, err := fs.ReadFile(s.fsys, source)
	if err != nil {
		return Built{}, err
	}
	ext := path.Ext(source)
	if cfg.compile != nil {
		out, outExt, err := cfg.compile(source, data, s.resolve)
		if err != nil {
			return Built{}, fmt.Errorf("compile %q: %w", source, err)
		}
		data, ext = out, outExt
	}

	var ct string
	if s.mimeType != nil {
		ct = s.mimeType(ext)
	}
	if ct == "" {
		ct = mime.TypeByExtension(ext)
	}
	if ct == "" {
		return Built{}, fmt.Errorf("no content type for extension %q", ext)
	}

	sum := sha1.Sum(data)
	hash := base32.StdEncoding.EncodeToString(sum[:])
	name := strings.TrimSuffix(source, path.Ext(source)) + "-" + hash[:10] + ext

	return Built{Name: name, ContentType: ct, Hash: hash, Data: data}, nil
}

// resolve returns the public, content-addressed name of the asset registered
// for source, building it if necessary.
func (s *Set) resolve(source string) (string, error) {
	s.mu.Lock()
	a, ok := s.assets[source]
	s.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("asset %q not registered", source)
	}
	b, err := a.built()
	if err != nil {
		return "", err
	}
	return b.Name, nil
}

func compress(b Built) (Blob, error) {
	gz, err := gzipBytes(b.Data)
	if err != nil {
		return Blob{}, fmt.Errorf("gzip %q: %w", b.Name, err)
	}
	zs, err := zstdBytes(b.Data)
	if err != nil {
		return Blob{}, fmt.Errorf("zstd %q: %w", b.Name, err)
	}
	return Blob{
		Name:        b.Name,
		ContentType: b.ContentType,
		Hash:        b.Hash,
		Data:        map[string][]byte{"": b.Data, "gzip": gz, "zstd": zs},
	}, nil
}

func gzipBytes(b []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(b); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func zstdBytes(b []byte) ([]byte, error) {
	var buf bytes.Buffer
	w, err := zstd.NewWriter(&buf)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(b); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
