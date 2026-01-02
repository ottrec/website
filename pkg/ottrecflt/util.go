package ottrecflt

import (
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// newFuzzyWordMatcher returns a function which creates a function which matches
// q against a string using [fuzzyWordMatchFunc], where matches are done for
// string prefixes ignoring case/periods/diacritics, and replacing dashes with
// spaces. The returned functions must not be used concurrently.
//
// Note: It's worth the complexity to optimize this since it will be called many
// times while evaluating filters.
func newFuzzyWordMatcher() func(string) func(string) bool {
	// slow path (contains non-ascii, e.g., unicode or accents)
	norm := transform.Chain( // note: not safe for concurrent usage
		runes.Map(func(r rune) rune {
			if unicode.Is(unicode.Pd, r) {
				return ' ' // all kinds of dashes to spaces
			}
			return r
		}),
		norm.NFKD,                          // decompose, canonicalize
		runes.Remove(runes.In(unicode.Mn)), // remove nonspacing marks
		runes.Map(unicode.ToLower),         // to lowercase
		runes.Remove(runes.Predicate(func(r rune) bool {
			return r == '.' // remove periods
		})),
		norm.NFC, // compose
	)

	// fast path (only ascii so we just need to make it lowercase and remove periods)
	asciiNorm1 := strings.NewReplacer("-", " ", ".", "")
	asciiNorm := func(s string) string { return strings.ToLower(asciiNorm1.Replace(s)) }

	return func(q string) func(string) bool {
		// normalize the query (we don't need to worry about whitespace since cutWords will do that)
		if isASCII(q) {
			q = asciiNorm(q) // fast path
		} else {
			if t, _, err := transform.String(norm, q); err == nil {
				q = t
			}
		}

		return func(s string) bool {
			// try fast path (only ascii lowercase, compare the rest as-is)
			if fuzzyWordMatchFunc(s, q, func(sw, qw string) bool {
				if len(sw) >= len(qw) {
					for i := range len(qw) {
						sc, qc := sw[i], qw[i]
						if 'A' <= sc && sc <= 'Z' {
							sc = sc - 'A' + 'a'
						}
						if sc != qc {
							return false
						}
					}
					return true
				}
				return false
			}) {
				return true
			}
			if isASCII(s) {
				s = asciiNorm(s)
			} else {
				if t, _, err := transform.String(norm, s); err == nil {
					s = t
				}
			}
			return fuzzyWordMatchFunc(s, q, strings.HasPrefix)
		}
	}
}

// fuzzyWordMatchFunc returns true if all words in q have a fn(sWord, qWord) of
// all words in q in the same order (words in s can be skipped).
func fuzzyWordMatchFunc(s, q string, fn func(sw, qw string) bool) bool {
	for {
		qw, qr, ok := cutWord(q)
		if !ok {
			break // no words left in query, we've matched everything
		}
		q = qr

		for {
			sw, sr, ok := cutWord(s)
			if !ok {
				return false // no words left in the string, but words left in query
			}
			s = sr

			if fn(sw, qw) {
				break // we've found a matching word in the string, continue with the next query word
			}
		}
	}
	return true
}

// isASCII returns true if all characters in s are ASCII.
func isASCII(s string) bool {
	for i := range len(s) {
		if s[i] > utf8.RuneSelf {
			return false
		}
	}
	return true
}

// cutWord discards any leading whitespace, then returns the next sequence of
// non-whitespace characters and anything after, if any. If no non-whitespace
// characters are found, (s, "", false) is returned. Whitespace is any character
// for which [unicode.IsSpace] returns true.
func cutWord(s string) (word, rest string, ok bool) {
	if i := strings.IndexFunc(s, func(r rune) bool {
		return !unicode.IsSpace(r)
	}); i == -1 {
		return s, "", false
	} else if i != 0 {
		s = s[i:]
	}
	j := strings.IndexFunc(s, unicode.IsSpace)
	if j == -1 {
		j = len(s)
	}
	return s[:j], s[j:], true
}

// distanceBetween uses the Haversine formula to get the distance (kilometers)
// between two coordinates (degrees).
func distanceBetween(lat1, lng1, lat2, lng2 float64) float64 {
	const earthRadiusKm = 6371

	lat1 = deg2rad(lat1)
	lng1 = deg2rad(lng1)
	lat2 = deg2rad(lat2)
	lng2 = deg2rad(lng2)

	dlat := lat2 - lat1
	dlng := lng2 - lng1

	a := math.Pow(math.Sin(dlat/2), 2) + math.Cos(lat1)*math.Cos(lat2)*math.Pow(math.Sin(dlng/2), 2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	d := c * earthRadiusKm

	return d
}

// deg2rad converts d from degrees to radians.
func deg2rad(d float64) float64 {
	return d * math.Pi / 180
}
