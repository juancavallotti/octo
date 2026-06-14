package core

import (
	"log/slog"
	"testing"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want slog.Level
	}{
		{name: "empty defaults to info", in: "", want: slog.LevelInfo},
		{name: "debug", in: "debug", want: slog.LevelDebug},
		{name: "case-insensitive", in: "INFO", want: slog.LevelInfo},
		{name: "warning alias", in: "warning", want: slog.LevelWarn},
		{name: "error", in: "error", want: slog.LevelError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseLevel(tt.in)
			if err != nil {
				t.Fatalf("ParseLevel(%q): %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("ParseLevel(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseLevelRejectsUnknown(t *testing.T) {
	if _, err := ParseLevel("loud"); err == nil {
		t.Fatal("expected an error for an unknown level")
	}
}
