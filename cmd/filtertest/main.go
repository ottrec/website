// experimenting with filter params for the /custom page
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/pgaskin/ottrec-website/pkg/ottrecdl"
	"github.com/pgaskin/ottrec-website/pkg/ottrecexp"
	"github.com/pgaskin/ottrec-website/pkg/ottrecidx"
	"github.com/pgaskin/ottrec/schema"
	"golang.org/x/text/unicode/norm"
)

func main() {
	filter, err := Parse(os.Args[1])
	if err != nil {
		panic(err)
	}
	fmt.Fprintln(os.Stderr, filter.String())
	fmt.Fprintln(os.Stderr, filter.Encode())

	pb, err := (&ottrecdl.Client{Base: "http://localhost:8082"}).Get(context.Background(), "latest", "pb")
	if err != nil {
		panic(err)
	}

	idx, err := new(ottrecidx.Indexer).Load(pb)
	if err != nil {
		panic(err)
	}
	fmt.Fprintln(os.Stderr, idx)

	data := filter.Apply(idx.Data())

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

const (
	maxClause            = 50
	maxParameter         = 2048
	allocSingleParameter = 16
	maxSingleParameter   = 64
)

type Filter struct {
	Date   *time.Time
	Clause [maxClause]*Clause
}

type Clause struct {
	Exclude     bool
	Facility    []string
	NotFacility []string
	Activity    []string
	NotActivity []string
	At          []*AtSpec
	NotAt       []*AtSpec
}

type AtSpec struct {
	Time    schema.ClockRange
	Weekday [7]bool
}

func ParseAt(value string) (*AtSpec, error) {
	a := &AtSpec{}
	var wkday bool
	for s := range strings.SplitSeq(value, ".") {
		switch strings.ToUpper(s) {
		case "SU", "SUN", "SUNDAY":
			a.Weekday[time.Sunday] = true
		case "MO", "MON", "MONDAY":
			a.Weekday[time.Monday] = true
		case "TU", "TUE", "TUESDAY":
			a.Weekday[time.Tuesday] = true
		case "WE", "WED", "WEDNESDAY":
			a.Weekday[time.Wednesday] = true
		case "TH", "THU", "THURSDAY":
			a.Weekday[time.Thursday] = true
		case "FR", "FRI", "FRIDAY":
			a.Weekday[time.Friday] = true
		case "SA", "SAT", "SATURDAY":
			a.Weekday[time.Saturday] = true
		default:
			if !wkday {
				start, end, _ := strings.Cut(s, "-")
				if start != "" {
					hhStr, mmStr, _ := strings.Cut(start, ":")
					hh, err := strconv.ParseInt(hhStr, 10, 0)
					if err != nil || !(0 < hh && hh < 24) {
						return nil, fmt.Errorf("invalid time range: invalid start hour")
					}
					mm, err := strconv.ParseInt(mmStr, 10, 0)
					if err != nil || !(0 <= mm && mm < 60) {
						return nil, fmt.Errorf("invalid time range: invalid start minute")
					}
					if hh == 0 && mm == 0 {
						hh = 24
					}
					a.Time.Start = schema.MakeClockTime(int(hh), int(mm))
				}
				if end != "" {
					if start == "" {
						return nil, fmt.Errorf("invalid time range: have end without start")
					}
					hhStr, mmStr, _ := strings.Cut(end, ":")
					hh, err := strconv.ParseInt(hhStr, 10, 0)
					if err != nil || !(0 < hh && hh < 24) {
						return nil, fmt.Errorf("invalid time range: invalid end hour")
					}
					mm, err := strconv.ParseInt(mmStr, 10, 0)
					if err != nil || !(0 <= mm && mm < 60) {
						return nil, fmt.Errorf("invalid time range: invalid end minute")
					}
					if hh == 0 && mm == 0 {
						hh = 24
					}
					a.Time.End = schema.MakeClockTime(int(hh), int(mm))
				} else {
					a.Time.End = a.Time.Start + 1
				}
				if a.Time.End <= a.Time.Start {
					return nil, fmt.Errorf("invalid time range: end must be after start")
				}
				if !a.Time.IsValid() {
					return nil, fmt.Errorf("invalid time range")
				}
				continue
			}
			return nil, fmt.Errorf("invalid weekday")
		}
		wkday = true
	}
	if !wkday {
		for i := range a.Weekday {
			a.Weekday[i] = true
		}
	}
	return a, nil
}

func (a *AtSpec) String() string {
	var b strings.Builder
	if a.Time.IsValid() {
		b.WriteString(a.Time.Start.Format(false))
		if a.Time.End != a.Time.Start+1 {
			b.WriteByte('-')
			b.WriteString(a.Time.End.Format(false))
		}
	}
	var wkday bool
	for _, ok := range a.Weekday {
		if ok {
			wkday = true
		}
	}
	if wkday {
		for wkday, ok := range a.Weekday {
			if ok {
				if b.Len() != 0 {
					b.WriteByte('.')
				}
				b.WriteString(strings.ToUpper(time.Weekday(wkday).String()[:3]))
			}
		}
	}
	return b.String()
}

func Parse(query string) (*Filter, error) {
	f := &Filter{}

	var (
		err   error
		count int
	)
	for key, value := range iterQuery(query)(&err) {
		tmp, ok := strings.CutPrefix(key, "f")
		if !ok {
			continue
		}
		if limit := maxParameter; count >= limit {
			return nil, fmt.Errorf("too many parameters (limit %d)", limit)
		}
		count++

		switch tmp {
		case "d":
			switch value {
			case "":
				f.Date = nil
			case "now":
				var d time.Time
				f.Date = &d
			default:
				d, err := time.ParseInLocation("2006-01-02", value, ottrecidx.TZ)
				if err != nil {
					return nil, fmt.Errorf("invalid filter param %q: invalid date %q", key, value)
				}
				f.Date = &d
			}
			continue
		}

		idxStr, typStr := takePrefix(tmp, "0123456789")
		idx, err := strconv.ParseUint(idxStr, 10, 0)
		if err != nil {
			return nil, fmt.Errorf("invalid filter param %q: missing or invalid index %q", key, idxStr)
		}
		if idx >= maxClause {
			return nil, fmt.Errorf("invalid filter param %q: too many clauses (limit %d)", key, len(f.Clause))
		}
		clause := f.Clause[idx]
		if clause == nil {
			clause = &Clause{
				Facility:    make([]string, 0, allocSingleParameter),
				NotFacility: make([]string, 0, allocSingleParameter),
				Activity:    make([]string, 0, allocSingleParameter),
				NotActivity: make([]string, 0, allocSingleParameter),
				At:          make([]*AtSpec, 0, allocSingleParameter),
				NotAt:       make([]*AtSpec, 0, allocSingleParameter),
			}
			f.Clause[idx] = clause
		}
		if len(typStr) != 1 {
			return nil, fmt.Errorf("invalid filter param %q: missing or invalid type %q", key, typStr)
		}
		typ := typStr[0]

		switch typ {
		case 'e':
			switch value {
			case "1":
				clause.Exclude = true
			case "0":
				clause.Exclude = false
			default:
				return nil, fmt.Errorf("invalid filter param %q: invalid include/exclude value %q", key, value)
			}

		case 'f':
			a := &clause.Facility
			if len(*a) >= maxSingleParameter {
				return nil, fmt.Errorf("invalid filter param %q: too many of a single kind (limit %d)", key, maxSingleParameter)
			}
			norm := normalizeText(value, false, true)
			*a = append(*a, norm)

		case 'F':
			a := &clause.NotFacility
			if len(*a) >= maxSingleParameter {
				return nil, fmt.Errorf("invalid filter param %q: too many of a single kind (limit %d)", key, maxSingleParameter)
			}
			norm := normalizeText(value, false, true)
			*a = append(*a, norm)

		case 'a':
			a := &clause.Activity
			if len(*a) >= maxSingleParameter {
				return nil, fmt.Errorf("invalid filter param %q: too many of a single kind (limit %d)", key, maxSingleParameter)
			}
			norm := normalizeText(value, false, true)
			*a = append(*a, norm)

		case 'A':
			a := &clause.NotActivity
			if len(*a) >= maxSingleParameter {
				return nil, fmt.Errorf("invalid filter param %q: too many of a single kind (limit %d)", key, maxSingleParameter)
			}
			norm := normalizeText(value, false, true)
			*a = append(*a, norm)

		case 't':
			a := &clause.At
			if len(*a) >= maxSingleParameter {
				return nil, fmt.Errorf("invalid filter param %q: too many of a single kind (limit %d)", key, maxSingleParameter)
			}
			x, err := ParseAt(value)
			if err != nil {
				return nil, fmt.Errorf("invalid filter param %q: invalid timespec %q: %w", key, value, err)
			}
			*a = append(*a, x)

		case 'T':
			a := &clause.NotAt
			if len(*a) >= maxSingleParameter {
				return nil, fmt.Errorf("invalid filter param %q: too many of a single kind (limit %d)", key, maxSingleParameter)
			}
			x, err := ParseAt(value)
			if err != nil {
				return nil, fmt.Errorf("invalid filter param %q: invalid timespec %q: %w", key, value, err)
			}
			*a = append(*a, x)

		default:
			return nil, fmt.Errorf("invalid filter param %q: unknown type %q", key, typ)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("invalid query string: %w", err)
	}

	return f, nil
}

func (f *Filter) String() string {
	var b strings.Builder

	json.NewEncoder(&b).Encode(f)
	// TODO: proper human-readable description

	return b.String()
}

func (f *Filter) Encode() string {
	var b strings.Builder

	if f.Date != nil {
		if b.Len() != 0 {
			b.WriteByte('&')
		}
		b.WriteString("fd=")
		if f.Date.IsZero() {
			b.WriteString("now")
		} else {
			b.WriteString(f.Date.Format("2006-01-02"))
		}
	}

	var idx int
	for _, clause := range f.Clause {
		if clause == nil {
			continue
		}
		add := func(typ byte, value string) {
			if b.Len() != 0 {
				b.WriteByte('&')
			}
			b.WriteByte('f')
			b.WriteString(strconv.Itoa(idx))
			b.WriteByte(typ)
			b.WriteByte('=')
			b.WriteString(value)
		}
		if clause.Exclude {
			add('e', "1")
		}
		for _, v := range clause.Facility {
			add('f', url.QueryEscape(v))
		}
		for _, v := range clause.NotFacility {
			add('F', url.QueryEscape(v))
		}
		for _, v := range clause.Activity {
			add('a', url.QueryEscape(v))
		}
		for _, v := range clause.NotActivity {
			add('A', url.QueryEscape(v))
		}
		for _, v := range clause.At {
			add('t', v.String())
		}
		for _, v := range clause.NotAt {
			add('T', v.String())
		}
		idx++
	}

	// TODO: canonical url encoding (elide empty clauses, sort params, etc)
	return b.String()
}

func (f *Filter) Apply(data ottrecidx.DataRef) ottrecidx.DataRef {
	mut := data.Mutate()

	if f.Date != nil {
		mut.FilterScheduleGroups(func(ref ottrecidx.ScheduleGroupRef) bool {
			return true // TODO
		})
	}

	for _, clause := range f.Clause {
		if clause == nil || clause.Exclude {
			continue
		}
		mut.FilterFacilities(func(ref ottrecidx.FacilityRef) bool {
			for _, clause := range f.Clause {
				if clause == nil || clause.Exclude {
					continue
				}
				var (
					inc = clause.Facility
					exc = clause.NotFacility
				)
				if hasInc, hasExc := len(inc) != 0, len(exc) != 0; hasInc || hasExc {
					s := normalizeText(ref.GetName(), false, true)
					if hasInc && !containsOneOf(s, inc) {
						continue
					}
					if hasExc && containsOneOf(s, exc) {
						continue
					}
				}
				return true
			}
			return false
		})
		mut.FilterActivities(func(ref ottrecidx.ActivityRef) bool {
			for _, clause := range f.Clause {
				if clause == nil || clause.Exclude {
					continue
				}
				var (
					inc = clause.Activity
					exc = clause.NotActivity
				)
				if hasInc, hasExc := len(inc) != 0, len(exc) != 0; hasInc || hasExc {
					s := normalizeText(ref.GetName(), false, true)
					if hasInc && !containsOneOf(s, inc) {
						continue
					}
					if hasExc && containsOneOf(s, exc) {
						continue
					}
				}
				return true
			}
			return false
		})
		mut.FilterTimes(func(ref ottrecidx.TimeRef) bool {
			for _, clause := range f.Clause {
				if clause == nil || clause.Exclude {
					continue
				}
				return true // TODO
			}
			return false
		})
		break
	}

	for _, clause := range f.Clause {
		if clause == nil || !clause.Exclude {
			continue
		}
		// TODO
	}

	mut.Elide()
	return mut.Data()
}

// TODO: function to emit warnings if Activity/NotActivity doesn't match any activity, same for Facility/NotFacility

// TODO: write tests for round-tripping, canonicalization, etc

func containsOneOf(s string, substr []string) bool {
	for _, substr := range substr {
		if strings.Contains(s, substr) {
			return true
		}
	}
	return false
}

func containsNoneOf(s string, substr []string) bool {
	for _, substr := range substr {
		if strings.Contains(s, substr) {
			return false
		}
	}
	return true
}

func takePrefix(s, cutset string) (prefix, rest string) {
	rest = strings.TrimLeft(s, cutset)
	prefix = s[:len(s)-len(rest)]
	return
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

// normalizeText performs various transformations on s:
//   - remove invisible characters
//   - collapse some kinds of consecutive whitespace (excluding newlines unless requested, but including nbsp)
//   - replace all kinds of dashes with "-"
//   - perform unicode NFKC normalization
//   - optionally lowercase the string
//   - remove leading and trailing whitespace
func normalizeText(s string, newlines, lower bool) string {
	// normalize the string
	s = norm.NFKC.String(s)

	// transform characters
	s = strings.Map(func(r rune) rune {

		// remove zero-width spaces
		switch r {
		case '\u200b', '\ufeff', '\u200d', '\u200c':
			return -1
		}

		// replace some whitespace for collapsing later
		switch r {
		case '\n':
			if newlines {
				return r
			}
			fallthrough
		case ' ', '\t', '\v', '\f', '\u00a0':
			return ' '
		}
		if unicode.Is(unicode.Zs, r) {
			return ' '
		}

		// replace smart punctuation
		switch r {
		case '“', '”', '‟':
			return '"'
		case '\u2018', '\u2019', '\u201b':
			return '\''
		case '\u2039':
			return '<'
		case '\u203a':
			return '>'
		}

		// normalize all kinds of dashes
		if unicode.Is(unicode.Pd, r) {
			return '-'
		}

		// remove invisible characters
		if !unicode.IsGraphic(r) {
			return -1
		}

		// lowercase (or not)
		if lower {
			return unicode.ToLower(r)
		}
		return r
	}, s)

	// collapse consecutive whitespace
	s = string(slices.CompactFunc([]rune(s), func(a, b rune) bool {
		return a == ' ' && a == b
	}))

	// remove leading/trailing whitespace
	return strings.TrimSpace(s)
}
