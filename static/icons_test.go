package static

import (
	"testing"
	"unicode"
)

func TestIcons(t *testing.T) {
	icons, err := iconRunes()
	if err != nil {
		t.Fatal(err)
	}

	// the font is a pinned embed, so the derived names are fixed; this was
	// verified against the upstream .codepoints file (4,264 names, every
	// codepoint the same glyph) before that file was dropped
	if len(icons) != 4264 {
		t.Errorf("got %d icons, want 4264", len(icons))
	}
	for name, want := range map[string]rune{
		"schedule":       '\ue192',
		"search":         '\ue8b6',
		"calendar_month": '\uebcc',
		"star":           '\ue838', // smallest of its legacy duplicate codepoints, not the official U+F09A
	} {
		if got := icons[name]; got != want {
			t.Errorf("icon %q = %U, want %U", name, got, want)
		}
	}

	// aliases are separate ligatures for the same glyph
	if icons["close"] != icons["clear"] {
		t.Errorf("close (%U) and clear (%U) should share a codepoint", icons["close"], icons["clear"])
	}

	// everything in the subset list resolves
	rs, err := subsetRunes()
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range rs {
		if !unicode.In(r, unicode.Co) {
			t.Errorf("icon %q = %U is not in a private use area", subsetIcons[i], r)
		}
	}

	// Icon returns the subset glyph and panics outside the subset
	if got := Icon("star"); got != string(icons["star"]) {
		t.Errorf("Icon(star) = %q, want %q", got, string(icons["star"]))
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("Icon should panic on an icon not in the subset list")
			}
		}()
		Icon("radar") // exists in the font, not in subsetIcons
	}()

	// icon(name) expands to a CSS string literal
	css, err := expandCSSIcons([]byte(`content: icon(schedule);`))
	if err != nil {
		t.Fatal(err)
	}
	if want := `content: "\e192";`; string(css) != want {
		t.Errorf("expandCSSIcons = %q, want %q", css, want)
	}
	if _, err := expandCSSIcons([]byte(`content: icon(radar);`)); err == nil {
		t.Error("expandCSSIcons should fail on an icon not in the subset list")
	}
}
