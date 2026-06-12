// Package esbuild compiles frontend TypeScript with esbuild.
package esbuild

import (
	"fmt"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
)

// Transform compiles a standalone TypeScript module (i.e., no imports) into
// minified JavaScript wrapped in an IIFE. The targets should roughly match the
// browserslist query used for the CSS.
func Transform(name, ts string) (string, error) {
	res := api.Transform(ts, api.TransformOptions{
		Sourcefile: name,
		Loader:     api.LoaderTS,
		Format:     api.FormatIIFE,
		Charset:    api.CharsetUTF8,
		Platform:   api.PlatformBrowser,
		Engines: []api.Engine{
			{Name: api.EngineSafari, Version: "15"},
			{Name: api.EngineChrome, Version: "110"},
			{Name: api.EngineFirefox, Version: "110"},
		},
		MinifyWhitespace:  true,
		MinifyIdentifiers: true,
		MinifySyntax:      true,
	})
	if len(res.Errors) > 0 {
		msgs := api.FormatMessages(res.Errors, api.FormatMessagesOptions{})
		return "", fmt.Errorf("esbuild: %s", strings.TrimSpace(strings.Join(msgs, "")))
	}
	return string(res.Code), nil
}
