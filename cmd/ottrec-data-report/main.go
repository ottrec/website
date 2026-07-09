// Command ottrec-data-report renders a dataset report (the /api/data/issues
// issues report, or the /api/data/enrichment debugging report) from a raw
// protobuf data file, without serving anything.
package main

import (
	"fmt"
	"os"

	"github.com/ottrec/data-enrichment/report"
	"github.com/ottrec/website/pkg/ottrecidx"
	"github.com/ottrec/website/routes"
	"github.com/spf13/pflag"
)

var (
	Kind   = pflag.String("kind", "issues", "report to render (issues, enrichment)")
	Output = pflag.StringP("output", "o", "-", "output file (- for stdout)")
	Help   = pflag.BoolP("help", "h", false, "show this help text")
)

func main() {
	pflag.Parse()

	if *Help || pflag.NArg() != 1 {
		fmt.Printf("usage: %s [options] <data.pb>\n\n", os.Args[0])
		fmt.Print(pflag.CommandLine.FlagUsages())
		if *Help {
			return
		}
		os.Exit(2)
	}

	pb, err := os.ReadFile(pflag.Arg(0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	idx, err := new(ottrecidx.Indexer).Load(pb)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: load data: %v\n", err)
		os.Exit(1)
	}
	data := idx.Data()

	var buf []byte
	switch *Kind {
	case "issues":
		buf = routes.BuildIssuesReport(data)
	case "enrichment":
		buf = report.Build("", data)
	default:
		fmt.Fprintf(os.Stderr, "error: unknown report kind %q\n", *Kind)
		os.Exit(2)
	}

	if *Output == "-" {
		os.Stdout.Write(buf)
	} else if err := os.WriteFile(*Output, buf, 0666); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
