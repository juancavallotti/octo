package main

import (
	"strings"
	"testing"
)

// TestReadyBanner covers the startup box: every line is the same width once emoji
// are counted as the two columns terminals draw them in, and the counts it states
// are pluralised for what is actually serving.
func TestReadyBanner(t *testing.T) {
	t.Run("box lines align", func(t *testing.T) {
		lines := strings.Split(readyBanner(3, 2), "\n")
		if len(lines) != 4 {
			t.Fatalf("readyBanner lines = %d, want 4:\n%s", len(lines), strings.Join(lines, "\n"))
		}
		want := displayWidth(lines[0])
		for i, line := range lines {
			if got := displayWidth(line); got != want {
				t.Errorf("line %d width = %d, want %d: %q", i, got, want, line)
			}
		}
	})

	t.Run("states version and counts", func(t *testing.T) {
		got := readyBanner(3, 2)
		for _, want := range []string{Version, "3 flows · 2 connectors"} {
			if !strings.Contains(got, want) {
				t.Errorf("readyBanner missing %q:\n%s", want, got)
			}
		}
	})

	t.Run("singular counts", func(t *testing.T) {
		if got := readyBanner(1, 1); !strings.Contains(got, "1 flow · 1 connector serving") {
			t.Errorf("readyBanner(1, 1) not singular:\n%s", got)
		}
	})
}
