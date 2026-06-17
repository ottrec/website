// Command ottrec-website-check builds the website's static assets and reports
// any compilation failures, without serving anything. It does NOT type-check
// TypeScript (use tsc for that).
package main

import (
	"fmt"
	"os"
	"path"
	"sort"

	"github.com/ottrec/website/internal/asset"
	"github.com/ottrec/website/static"
	"github.com/spf13/pflag"
)

var (
	CSS  = pflag.Bool("css", false, "check stylesheet (lightningcss) compilation")
	JS   = pflag.Bool("js", false, "check script (esbuild bundling, no type checking) compilation")
	All  = pflag.Bool("all", false, "build every asset (also fonts, images, vendored files)")
	Help = pflag.BoolP("help", "h", false, "show this help text")
)

func main() {
	pflag.Parse()

	if *Help || pflag.NArg() != 0 {
		fmt.Printf("usage: %s [options]\n\n", os.Args[0])
		fmt.Print(pflag.CommandLine.FlagUsages())
		fmt.Print("\nWith no kind selected, both --css and --js are checked.\n")
		if *Help {
			return
		}
		os.Exit(2)
	}

	// default to the compiled kinds when none is selected
	if !*CSS && !*JS && !*All {
		*CSS, *JS = true, true
	}

	want := func(a *asset.Asset) bool {
		if *All {
			return true
		}
		switch path.Ext(a.Source()) {
		case ".css":
			return *CSS
		case ".ts":
			return *JS
		}
		return false
	}

	// collect and sort for stable output
	var sources []string
	assets := map[string]*asset.Asset{}
	for a := range static.Assets() {
		if want(a) {
			sources = append(sources, a.Source())
			assets[a.Source()] = a
		}
	}
	sort.Strings(sources)

	var failed int
	for _, src := range sources {
		if _, err := assets[src].Built(); err != nil {
			failed++
			fmt.Fprintf(os.Stderr, "FAIL %s\n%v\n\n", src, err)
		} else {
			fmt.Printf("ok   %s\n", src)
		}
	}

	if failed > 0 {
		fmt.Fprintf(os.Stderr, "%d asset(s) failed to build\n", failed)
		os.Exit(1)
	}
}
