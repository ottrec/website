// experimenting with filter params for the /custom page
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"net/url"
	"os"
	"strings"

	"github.com/pgaskin/ottrec-website/pkg/ottrecdl"
	"github.com/pgaskin/ottrec-website/pkg/ottrecexp"
	"github.com/pgaskin/ottrec-website/pkg/ottrecidx"
)

func main() {
	filter, err := Parse(os.Args[1])
	if err != nil {
		panic(err)
	}
	fmt.Fprintln(os.Stderr, Describe(filter))

	pb, err := (&ottrecdl.Client{Base: "http://localhost:8082"}).Get(context.Background(), "latest", "pb")
	if err != nil {
		panic(err)
	}

	idx, err := new(ottrecidx.Indexer).Load(pb)
	if err != nil {
		panic(err)
	}
	fmt.Fprintln(os.Stderr, idx)

	data := Apply(filter, idx.Data())

	exp, err := ottrecexp.New(data)
	if err != nil {
		panic(err)
	}

	buf, err := json.MarshalIndent(json.RawMessage(ottrecexp.JSON(exp)), "", "  ")
	if err != nil {
		panic(err)
	}

	os.Stdout.Write(buf)
	fmt.Println()
}

type Filter struct {
}

func Parse(query string) (*Filter, error) {
	filter := new(Filter)

	var err error
	for key, value := range iterQuery(query)(&err) {
		_ = key
		_ = value
		// TODO
	}
	if err != nil {
		return nil, fmt.Errorf("invalid query string: %w", err)
	}

	return filter, nil
}

func Describe(filter *Filter) string {
	var b strings.Builder

	// TODO

	return b.String()
}

func Apply(filter *Filter, data ottrecidx.DataRef) ottrecidx.DataRef {
	mut := data.Mutate()

	// TODO

	mut.Elide()
	return mut.Data()
}

func iterQuery(query string) func(*error) iter.Seq2[string, string] {
	return func(err *error) iter.Seq2[string, string] {
		return func(yield func(string, string) bool) {
			*err = func() error {
				query := query
				for query != "" {
					var key string
					key, query, _ = strings.Cut(query, "&")
					if strings.Contains(key, ";") {
						return fmt.Errorf("invalid semicolon separator in query")
					}
					if key == "" {
						continue
					}
					key, value, _ := strings.Cut(key, "=")
					key, err := url.QueryUnescape(key)
					if err != nil {
						return err
					}
					value, err = url.QueryUnescape(value)
					if err != nil {
						return err
					}
					if !yield(key, value) {
						return nil
					}
				}
				return nil
			}()
		}
	}
}
