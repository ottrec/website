package esbuild

import (
	"strings"
	"testing"
)

func TestESBuild(t *testing.T) {
	t.Run("Invalid", func(t *testing.T) {
		if _, err := Transform("test.ts", `const x: = 1`); err == nil {
			t.Errorf("expected error")
		} else if !strings.Contains(err.Error(), "test.ts") {
			t.Errorf("incorrect error: %v", err)
		}
	})
	t.Run("Simple", func(t *testing.T) {
		if res, err := Transform("test.ts", `export {}; const greeting: string = "hi"; console.log(greeting ?? "");`); err != nil {
			t.Errorf("unexpected error: %v", err)
		} else if !strings.HasPrefix(res, "(()=>{") || strings.Contains(res, ":string") || strings.Contains(res, ": string") {
			t.Errorf("incorrect result: %q", res)
		}
	})
}
