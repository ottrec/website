//go:build ignore

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/pgaskin/ottrec-website/pkg/ottrecdl"
	"github.com/pgaskin/ottrec-website/pkg/ottrecidx"
	"github.com/pgaskin/ottrec-website/pkg/ottrecql"
	"github.com/pgaskin/ottrec/schema"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Printf("usage: %s query\n", os.Args[0])
		os.Exit(2)
	}
	if err := run(os.Args[1]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(q string) error {
	ctx := context.Background()

	ast, err := ottrecql.Parse(q)
	if err != nil {
		return fmt.Errorf("parse query: %w", err)
	}

	fmt.Println(ottrecql.Cost(ast), ottrecql.Render(ast))

	expr, err := ottrecql.Compile(ast, &ottrecql.Context{})
	if err != nil {
		return fmt.Errorf("compile query: %w", err)
	}

	tf := filepath.Join(os.TempDir(), "ottrec-"+time.Now().Format("2006-01-02")+".pb")

	pb, err := os.ReadFile(tf)
	if err != nil {
		cl := &ottrecdl.Client{
			Base: "https://data.ottrec.ca/",
		}

		pb, err = cl.Get(ctx, "latest", "pb")
		if err != nil {
			return fmt.Errorf("get data: %w", err)
		}

		if err := os.WriteFile(tf, pb, 0644); err != nil {
			return fmt.Errorf("save cached data: %w", err)
		}
	}

	idx, err := new(ottrecidx.Indexer).Load(pb)
	if err != nil {
		return fmt.Errorf("load data: %w", err)
	}

	fmt.Println(idx)

	data := idx.Data()
	fmt.Printf("before: fac(%d) grp(%d) sch(%d) act(%d) tm(%d)\n",
		data.Facilities().Len(),
		data.ScheduleGroups().Len(),
		data.Schedules().Len(),
		data.Activities().Len(),
		data.Times().Len(),
	)

	now := time.Now()
	data = expr.Filter(idx.Data())
	dur := time.Since(now)
	fmt.Println(dur)

	fmt.Printf("after: fac(%d) grp(%d) sch(%d) act(%d) tm(%d)\n",
		data.Facilities().Len(),
		data.ScheduleGroups().Len(),
		data.Schedules().Len(),
		data.Activities().Len(),
		data.Times().Len(),
	)

	return tmpl.ExecuteTemplate(os.Stdout, "", data)
}

var tmpl = template.Must(template.New("").Funcs(template.FuncMap{
	"ansi": ansi,
	"tmr": func(tm ottrecidx.TimeRef) schema.ClockRange {
		r, _ := tm.GetRange()
		return r
	},
	"rr": func(act ottrecidx.ActivityRef) string {
		if required, definite := act.GuessReservationRequirement(); required {
			if !definite {
				return " (reservation might be required)"
			}
			return " (reservation required)"
		}
		return ""
	},
	"wd": func(sch ottrecidx.ScheduleRef, i int) string {
		if d, ok := sch.GetDayDate(i); ok {
			return d.String()
		}
		return sch.GetDay(i)
	},
}).Parse(`
{{- range $f := .Facilities }}
{{- range $g := .ScheduleGroups }}
{{- range $s := .Schedules }}
{{ansi 33}}{{$f.GetName}} ({{$s.GetDate}}){{ansi}}
{{- range $a := .Activities }}
+ {{ansi 33}}{{.GetName}}{{ansi}}{{rr $a}}
    {{- range $i := $s.NumDays }}
    {{- $tm := $a.DayTimes $i }}
    {{- if not $tm.Empty }}
    {{ansi 35}}{{wd $s $i | printf "%-20s"}}{{ansi}}
    {{- end }}
    {{- range $t := $tm }}   {{(tmr $t).Start.Format false}}-{{(tmr $t).End.Format false}}{{ end }}
    {{- end }}
{{""}}
{{- end }}
{{- end }}
{{- end }}
{{- end }}
`))

// ansi formats an ansi escape sequence.
func ansi(i ...int) string {
	if len(i) == 0 {
		return "\x1b[0m"
	}
	var b strings.Builder
	b.WriteString("\x1b[")
	for x, y := range i {
		if x != 0 {
			b.WriteByte(';')
		}
		b.WriteString(strconv.Itoa(y))
	}
	b.WriteByte('m')
	return b.String()
}
