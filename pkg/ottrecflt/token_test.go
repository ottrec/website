package ottrecflt

import "testing"

func TestTokenString(t *testing.T) {
	seen := map[string]token{}
	for kind := range maxToken {
		str := kind.String()
		if str == "" {
			t.Errorf("token(%d): missing string", kind)
			continue
		}
		if other, dup := seen[str]; dup {
			t.Errorf("token(%d): duplicate string %q (seen for %d)", kind, str, other)
			continue
		}
		seen[str] = kind
	}
}
