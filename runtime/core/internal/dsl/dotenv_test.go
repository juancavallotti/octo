package dsl

import (
	"strings"
	"testing"
)

func TestParseDotEnv(t *testing.T) {
	data := []byte(`
# a comment
DB_HOST=localhost
export API_KEY = secret
QUOTED="quoted value"
SINGLE='single value'
EMPTY=
SPACED  =  trimmed

`)
	values, err := ParseDotEnv(data)
	if err != nil {
		t.Fatalf("ParseDotEnv: %v", err)
	}

	want := map[string]string{
		"DB_HOST": "localhost",
		"API_KEY": "secret",
		"QUOTED":  "quoted value",
		"SINGLE":  "single value",
		"EMPTY":   "",
		"SPACED":  "trimmed",
	}
	if len(values) != len(want) {
		t.Fatalf("parsed %d vars, want %d: %v", len(values), len(want), values)
	}
	for k, v := range want {
		if got := values[k]; got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
}

func TestParseDotEnvErrors(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		wantErr string
	}{
		{name: "missing equals", data: "NOT_AN_ASSIGNMENT", wantErr: "missing '='"},
		{name: "empty name", data: "=value", wantErr: "empty variable name"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseDotEnv([]byte(tc.data)); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}
