package static

import (
	"encoding/binary"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sync"

	"github.com/pgaskin/go-hbsubset"
)

// Material Symbols icons are referenced by name: stylesheets use icon(name)
// in place of a raw content escape (expanded by compileCSS), and templates use
// [Icon]. Both fail loudly on icons not in subsetIcons, since the served font
// would render them as tofu.
//
// The name to codepoint table is derived from the embedded font itself rather
// than the upstream .codepoints file: the GSUB ligature rules (letter glyph
// sequence to icon glyph) are the authoritative source of names, and cmap maps
// the icon glyphs back to codepoints. Aliases (e.g. clear/close) are separate
// ligatures for the same glyph, and some glyphs have several legacy
// codepoints; the smallest is used consistently for both the subset and the
// emitted glyphs, so they always agree.

// Icon returns the glyph for a Material Symbols icon, for use as element text
// (with a glyph base class like .material-symbols-outlined). It panics if the
// icon is not in subsetIcons.
func Icon(name string) string {
	r, err := iconRune(name)
	if err != nil {
		panic(err)
	}
	return string(r)
}

// iconRune resolves an icon name to its codepoint, failing if the icon is not
// in subsetIcons.
func iconRune(name string) (rune, error) {
	if !slices.Contains(subsetIcons, name) {
		return 0, fmt.Errorf("icon %q is not in the subset list", name)
	}
	icons, err := iconRunes()
	if err != nil {
		return 0, err
	}
	r, ok := icons[name]
	if !ok {
		return 0, fmt.Errorf("icon %q does not exist in the font", name)
	}
	return r, nil
}

// subsetRunes resolves subsetIcons to the codepoints given to the subsetter.
func subsetRunes() ([]rune, error) {
	rs := make([]rune, len(subsetIcons))
	for i, name := range subsetIcons {
		r, err := iconRune(name)
		if err != nil {
			return nil, err
		}
		rs[i] = r
	}
	return rs, nil
}

var cssIconRef = regexp.MustCompile(`icon\(([a-z0-9_]+)\)`)

// expandCSSIcons replaces icon(name) references with CSS strings containing
// the icon's codepoint. Unknown or non-subset names are a compile error.
func expandCSSIcons(data []byte) ([]byte, error) {
	var rerr error
	out := cssIconRef.ReplaceAllFunc(data, func(m []byte) []byte {
		name := string(m[len("icon(") : len(m)-1])
		r, err := iconRune(name)
		if err != nil {
			rerr = err
			return m
		}
		return fmt.Appendf(nil, `"\%x"`, r)
	})
	return out, rerr
}

// iconRunes maps every icon name in the embedded Material Symbols font to its
// codepoint.
var iconRunes = sync.OnceValues(func() (map[string]rune, error) {
	data, err := res.ReadFile("fonts/materialsymbolsoutlined.ttf")
	if err != nil {
		return nil, err
	}
	face, err := hbsubset.NewFace(data, 0)
	if err != nil {
		return nil, err
	}
	letter := make(map[uint32]byte) // glyphs of the name letters a-z0-9_
	code := make(map[uint32]rune)   // smallest codepoint per glyph
	for r, g := range face.GlyphMapping() {
		if r == '_' || 'a' <= r && r <= 'z' || '0' <= r && r <= '9' {
			letter[g] = byte(r)
		}
		if c, ok := code[g]; !ok || r < c {
			code[g] = r
		}
	}
	gsub, err := face.Table(hbsubset.MakeTag("GSUB"))
	if err != nil {
		return nil, fmt.Errorf("get GSUB table: %w", err)
	}
	return gsubLigatureNames(gsub, letter, code)
})

// gsubLigatureNames extracts each icon's name and codepoint from the GSUB
// ligature rules (lookup type 4, in this font always wrapped in type 7
// extension lookups): a ligature substitutes a sequence of letter glyphs, the
// name, with the icon glyph. Ligatures whose components aren't all name
// letters or whose glyph has no codepoint are skipped.
func gsubLigatureNames(gsub []byte, letter map[uint32]byte, code map[uint32]rune) (_ map[string]rune, err error) {
	// the font is a trusted embed, so rather than bounds-checking every read,
	// let a malformed table panic and convert it to an error
	defer func() {
		if recover() != nil {
			err = errors.New("malformed GSUB table")
		}
	}()
	u16 := func(off int) int { return int(binary.BigEndian.Uint16(gsub[off:])) }
	u32 := func(off int) int { return int(binary.BigEndian.Uint32(gsub[off:])) }

	names := make(map[string]rune)

	// Ligature: ligGlyph, compCount, comps[compCount-1] (first comes from the
	// coverage table)
	ligature := func(off int, first uint32) {
		name := make([]byte, 0, 32)
		c, ok := letter[first]
		if !ok {
			return
		}
		name = append(name, c)
		for i := range u16(off+2) - 1 {
			if c, ok = letter[uint32(u16(off+4+i*2))]; !ok {
				return
			}
			name = append(name, c)
		}
		if r, ok := code[uint32(u16(off))]; ok {
			names[string(name)] = r
		}
	}

	// Coverage: the first glyph of the ligature set at each index
	coverage := func(off int) []uint32 {
		var glyphs []uint32
		switch u16(off) {
		case 1: // glyph list
			for i := range u16(off + 2) {
				glyphs = append(glyphs, uint32(u16(off+4+i*2)))
			}
		case 2: // glyph ranges
			for i := range u16(off + 2) {
				for g := u16(off + 4 + i*6); g <= u16(off+6+i*6); g++ {
					glyphs = append(glyphs, uint32(g))
				}
			}
		}
		return glyphs
	}

	// LigatureSubst format 1: coverage, then a LigatureSet per covered glyph
	ligatureSubst := func(off int) {
		if u16(off) != 1 {
			return
		}
		firsts := coverage(off + u16(off+2))
		for i := range min(u16(off+4), len(firsts)) {
			set := off + u16(off+6+i*2)
			for j := range u16(set) {
				ligature(set+u16(set+2+j*2), firsts[i])
			}
		}
	}

	lookups := u16(8) // lookup list offset
	for i := range u16(lookups) {
		lookup := lookups + u16(lookups+2+i*2)
		for j := range u16(lookup + 4) {
			sub := lookup + u16(lookup+6+j*2)
			switch u16(lookup) { // lookup type
			case 4:
				ligatureSubst(sub)
			case 7: // extension: format, wrapped type, u32 offset
				if u16(sub+2) == 4 {
					ligatureSubst(sub + u32(sub+4))
				}
			}
		}
	}
	return names, nil
}
