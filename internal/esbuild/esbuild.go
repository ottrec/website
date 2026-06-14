// Package esbuild compiles and bundles frontend TypeScript with esbuild.
package esbuild

import (
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
)

// LocalFS returns a plugin that resolves the bundle's relative imports (./,
// ../) against fsys, loading them as TypeScript/JavaScript. This lets the
// standalone entry modules import shared sibling modules even when the sources
// are embedded rather than on disk.
func LocalFS(fsys fs.FS) api.Plugin {
	const namespace = "local"
	return api.Plugin{
		Name: "local",
		Setup: func(build api.PluginBuild) {
			build.OnResolve(api.OnResolveOptions{Filter: `^\.`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				dir := "." // the entry's imports resolve from the fs root
				if args.Namespace == namespace {
					dir = path.Dir(args.Importer)
				}
				p := path.Join(dir, args.Path)
				for _, cand := range []string{p, p + ".ts", p + ".js"} {
					if st, err := fs.Stat(fsys, cand); err == nil && !st.IsDir() {
						return api.OnResolveResult{Path: cand, Namespace: namespace}, nil
					}
				}
				return api.OnResolveResult{}, fmt.Errorf("local import %q not found (from %q)", args.Path, args.Importer)
			})
			build.OnLoad(api.OnLoadOptions{Filter: `.*`, Namespace: namespace}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				data, err := fs.ReadFile(fsys, args.Path)
				if err != nil {
					return api.OnLoadResult{}, err
				}
				contents := string(data)
				loader := api.LoaderTS
				if path.Ext(args.Path) == ".js" {
					loader = api.LoaderJS
				}
				return api.OnLoadResult{Contents: &contents, Loader: loader}, nil
			})
		},
	}
}

// Transform compiles a TypeScript module into minified JavaScript wrapped in an
// IIFE, bundling and tree-shaking any imports. Imports are resolved by the
// given plugins (e.g. an npm resolver); without a plugin that handles them, a
// bare import is an error. The targets should roughly match the browserslist
// query used for the CSS.
func Transform(name, ts string, plugins ...api.Plugin) (string, error) {
	res := api.Build(api.BuildOptions{
		Stdin: &api.StdinOptions{
			Contents:   ts,
			Sourcefile: name,
			Loader:     api.LoaderTS,
		},
		Bundle:      true,
		Format:      api.FormatIIFE,
		Charset:     api.CharsetUTF8,
		Platform:    api.PlatformBrowser,
		TreeShaking: api.TreeShakingTrue,
		Engines: []api.Engine{
			{Name: api.EngineSafari, Version: "15"},
			{Name: api.EngineChrome, Version: "110"},
			{Name: api.EngineFirefox, Version: "110"},
		},
		MinifyWhitespace:  true,
		MinifyIdentifiers: true,
		MinifySyntax:      true,
		Plugins:           plugins,
		Write:             false,
	})
	if len(res.Errors) > 0 {
		msgs := api.FormatMessages(res.Errors, api.FormatMessagesOptions{})
		return "", fmt.Errorf("esbuild: %s", strings.TrimSpace(strings.Join(msgs, "")))
	}
	return string(res.OutputFiles[0].Contents), nil
}
