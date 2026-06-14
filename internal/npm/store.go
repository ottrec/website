package npm

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"sync"
	"testing/fstest"

	"github.com/evanw/esbuild/pkg/api"
)

// Store reads vendored packages back from the tar archives written by [Vendor].
// The archives are read from an [fs.FS] (typically an embed.FS), one
// "<package>.tar" per package.
type Store struct {
	fsys    fs.FS
	entries map[string]string // package -> entry-file override for its bare import
	mu      sync.Mutex
	pkgs    map[string]fs.FS
}

// NewStore returns a Store reading "<package>.tar" archives from fsys.
func NewStore(fsys fs.FS) *Store {
	return &Store{fsys: fsys, entries: map[string]string{}, pkgs: map[string]fs.FS{}}
}

// Entry overrides the file a package's bare import resolves to, for packages
// that fail to advertise their ESM build in package.json (e.g. leaflet ships
// dist/leaflet-src.esm.js but declares only the CJS "main", defeating
// tree-shaking). It returns s for chaining.
func (s *Store) Entry(pkg, file string) *Store {
	s.entries[pkg] = file
	return s
}

// FS returns the contents of the named package as a filesystem rooted at the
// package directory (so "package.json", "dist/...", etc. are at the root).
func (s *Store) FS(name string) (fs.FS, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f, ok := s.pkgs[name]; ok {
		return f, nil
	}
	data, err := fs.ReadFile(s.fsys, name+".tar")
	if err != nil {
		return nil, fmt.Errorf("package %q not vendored: %w", name, err)
	}
	f, err := tarFS(data)
	if err != nil {
		return nil, fmt.Errorf("package %q: %w", name, err)
	}
	s.pkgs[name] = f
	return f, nil
}

// tarFS reads an uncompressed tar archive into an in-memory filesystem.
func tarFS(data []byte) (fs.FS, error) {
	tr := tar.NewReader(bytes.NewReader(data))
	m := fstest.MapFS{}
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
		b, err := io.ReadAll(tr)
		if err != nil {
			return nil, err
		}
		m[path.Clean(h.Name)] = &fstest.MapFile{Data: b, Mode: 0o444}
	}
	return m, nil
}

// module identifies a resolved file within a vendored package. It is carried as
// esbuild plugin data so relative imports can be resolved against the same
// package.
type module struct {
	pkg  string
	fsys fs.FS
	file string // path within fsys
}

// Plugin returns an esbuild plugin that resolves and loads bare package imports
// (and the relative imports they make) from the store's vendored packages.
func (s *Store) Plugin() api.Plugin {
	const namespace = "npm"
	return api.Plugin{
		Name: "npm",
		Setup: func(build api.PluginBuild) {
			build.OnResolve(api.OnResolveOptions{Filter: `.*`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				if args.Kind == api.ResolveEntryPoint {
					return api.OnResolveResult{}, nil // let esbuild handle the entry
				}

				// a relative import from an already-vendored module resolves
				// within that same package
				if strings.HasPrefix(args.Path, ".") && args.Namespace == namespace {
					m := args.PluginData.(module)
					file, ok := resolveFile(m.fsys, path.Join(path.Dir(m.file), args.Path))
					if !ok {
						return api.OnResolveResult{}, fmt.Errorf("cannot resolve %q from %s/%s", args.Path, m.pkg, m.file)
					}
					return result(module{m.pkg, m.fsys, file}, namespace), nil
				}

				// a bare specifier names a package (and optional subpath)
				if !strings.HasPrefix(args.Path, ".") && !strings.HasPrefix(args.Path, "/") {
					pkg, sub := splitSpecifier(args.Path)
					if sub == "" {
						sub = s.entries[pkg] // entry-file override, if any
					}
					fsys, err := s.FS(pkg)
					if err != nil {
						return api.OnResolveResult{}, err
					}
					file, err := entry(fsys, sub)
					if err != nil {
						return api.OnResolveResult{}, fmt.Errorf("resolve %q: %w", args.Path, err)
					}
					return result(module{pkg, fsys, file}, namespace), nil
				}

				return api.OnResolveResult{}, nil
			})

			build.OnLoad(api.OnLoadOptions{Filter: `.*`, Namespace: namespace}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				m := args.PluginData.(module)
				data, err := fs.ReadFile(m.fsys, m.file)
				if err != nil {
					return api.OnLoadResult{}, err
				}
				contents := string(data)
				loader := loaderForExt(path.Ext(m.file))
				return api.OnLoadResult{Contents: &contents, Loader: loader, PluginData: m}, nil
			})
		},
	}
}

func result(m module, namespace string) api.OnResolveResult {
	return api.OnResolveResult{Path: m.pkg + "/" + m.file, Namespace: namespace, PluginData: m}
}

// splitSpecifier splits a bare import specifier into a package name and the
// subpath within it (empty for the package root), handling scoped packages.
func splitSpecifier(spec string) (pkg, sub string) {
	parts := strings.SplitN(spec, "/", 3)
	if strings.HasPrefix(spec, "@") && len(parts) >= 2 {
		pkg = parts[0] + "/" + parts[1]
		if len(parts) == 3 {
			sub = parts[2]
		}
		return pkg, sub
	}
	pkg, sub, _ = strings.Cut(spec, "/")
	return pkg, sub
}

// entry resolves the file within a package's filesystem to import. With no
// subpath it consults package.json (exports, then module, then main); a subpath
// is resolved directly.
func entry(fsys fs.FS, sub string) (string, error) {
	if sub != "" {
		if file, ok := resolveFile(fsys, sub); ok {
			return file, nil
		}
		return "", fmt.Errorf("no such file %q", sub)
	}
	main := "index.js"
	if data, err := fs.ReadFile(fsys, "package.json"); err == nil {
		var pj struct {
			Main    string          `json:"main"`
			Module  string          `json:"module"`
			Exports json.RawMessage `json:"exports"`
		}
		if err := json.Unmarshal(data, &pj); err != nil {
			return "", fmt.Errorf("package.json: %w", err)
		}
		switch {
		case len(pj.Exports) > 0:
			if e := resolveExports(pj.Exports); e != "" {
				main = e
			}
		case pj.Module != "":
			main = pj.Module
		case pj.Main != "":
			main = pj.Main
		}
	}
	if file, ok := resolveFile(fsys, main); ok {
		return file, nil
	}
	return "", fmt.Errorf("entry %q not found", main)
}

// resolveExports resolves the "." entry of a package.json "exports" field,
// preferring the import/module/browser/default conditions. It handles the
// string form, the conditions-object form, and the subpath-map form (for ".").
func resolveExports(raw json.RawMessage) string {
	var str string
	if json.Unmarshal(raw, &str) == nil {
		return str
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return ""
	}
	if dot, ok := obj["."]; ok {
		obj = nil
		if json.Unmarshal(dot, &str) == nil {
			return str
		}
		_ = json.Unmarshal(dot, &obj)
	}
	for _, cond := range []string{"import", "module", "browser", "default"} {
		if v, ok := obj[cond]; ok {
			if e := resolveExports(v); e != "" {
				return e
			}
		}
	}
	return ""
}

// resolveFile applies node-style extension and index resolution to p within
// fsys, returning the matched file path.
func resolveFile(fsys fs.FS, p string) (string, bool) {
	p = path.Clean(p)
	for _, cand := range []string{p, p + ".js", p + ".mjs", p + ".cjs", p + ".json"} {
		if isFile(fsys, cand) {
			return cand, true
		}
	}
	if data, err := fs.ReadFile(fsys, path.Join(p, "package.json")); err == nil {
		var pj struct {
			Main   string `json:"main"`
			Module string `json:"module"`
		}
		_ = json.Unmarshal(data, &pj)
		if m := cmpOr(pj.Module, pj.Main); m != "" {
			if file, ok := resolveFile(fsys, path.Join(p, m)); ok {
				return file, true
			}
		}
	}
	for _, cand := range []string{path.Join(p, "index.js"), path.Join(p, "index.mjs"), path.Join(p, "index.cjs")} {
		if isFile(fsys, cand) {
			return cand, true
		}
	}
	return "", false
}

func isFile(fsys fs.FS, p string) bool {
	st, err := fs.Stat(fsys, p)
	return err == nil && !st.IsDir()
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func loaderForExt(ext string) api.Loader {
	switch ext {
	case ".json":
		return api.LoaderJSON
	case ".css":
		return api.LoaderCSS
	case ".ts":
		return api.LoaderTS
	default:
		return api.LoaderJS
	}
}
