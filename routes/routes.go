// Package routes contains handlers.
package routes

import (
	"bytes"
	"iter"
	"net/http"
	"strconv"
	"time"

	"github.com/pgaskin/xmlwriter"
)

func commonMiddleware(next http.Handler) http.Handler {
	// TODO: request ID, etc
	return next
}

func iterPrev[T any](seq iter.Seq[T]) iter.Seq2[T, T] {
	return func(yield func(T, T) bool) {
		var x T
		for y := range seq {
			if !yield(x, y) {
				return
			}
			x = y
		}
	}
}

func iterLimit[T any](seq iter.Seq[T], n int) iter.Seq[T] {
	if n < 0 {
		return seq
	}
	return func(yield func(T) bool) {
		var i int
		for v := range seq {
			if !yield(v) {
				return
			}
			if i++; i >= n {
				break
			}
		}
	}
}

func reqScheme(r *http.Request) string {
	switch v := r.Header.Get("X-Forwarded-Proto"); v {
	case "http", "https":
		return v
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

const sitemapNS xmlwriter.NS = "http://www.sitemaps.org/schemas/sitemap/0.9"

// sitemapXML renders a sitemap for the provided urls with the specified last
// modification date.
func sitemapXML(lastmod string, urls []string) ([]byte, error) {
	var b bytes.Buffer
	x := xmlwriter.New(&b)
	x.Indent("\t")
	x.DefaultProcInst()
	x.Start(sitemapNS, "urlset", sitemapNS.Bind(""))
	for _, u := range urls {
		x.Start(nil, "url")
		x.Start(nil, "loc")
		x.Text(false, u)
		x.End(false)
		x.Start(nil, "lastmod")
		x.Text(false, lastmod)
		x.End(false)
		x.End(false)
	}
	x.End(false)
	if err := x.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

type serverTiming []serverTimingEntry

type serverTimingEntry struct {
	name string
	dur  time.Duration
}

func (st *serverTiming) Add(name string, dur time.Duration) {
	*st = append(*st, serverTimingEntry{name, dur})
}

func (st serverTiming) String() string {
	var b []byte
	for i, e := range st {
		if i > 0 {
			b = append(b, ", "...)
		}
		b = append(b, e.name...)
		b = append(b, ";dur="...)
		b = strconv.AppendFloat(b, float64(e.dur.Microseconds())/1000, 'f', 3, 64)
	}
	return string(b)
}
