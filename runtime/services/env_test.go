package services

import "testing"

// A typo in a deployment's environment must not be the reason a runtime will not
// start: an unreadable value falls back to the documented default and says so in
// the log. These cases pin that, and the conversational spellings a human will
// reach for in a compose file or a Helm value.

func TestEnvBool(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		set      bool
		fallback bool
		want     bool
	}{
		{name: "unset falls back", fallback: true, want: true},
		{name: "empty falls back", value: "", set: true, fallback: true, want: true},
		{name: "false", value: "false", set: true, fallback: true, want: false},
		{name: "0", value: "0", set: true, fallback: true, want: false},
		{name: "off", value: "off", set: true, fallback: true, want: false},
		{name: "no", value: "no", set: true, fallback: true, want: false},
		{name: "OFF is case-insensitive", value: "OFF", set: true, fallback: true, want: false},
		{name: "true", value: "true", set: true, fallback: false, want: true},
		{name: "on", value: "on", set: true, fallback: false, want: true},
		{name: "yes", value: "yes", set: true, fallback: false, want: true},
		{name: "whitespace is trimmed", value: "  off  ", set: true, fallback: true, want: false},
		{name: "garbage falls back rather than failing", value: "maybe", set: true, fallback: true, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const name = "OCTO_TEST_BOOL"
			if tt.set {
				t.Setenv(name, tt.value)
			}
			if got := EnvBool(name, tt.fallback); got != tt.want {
				t.Errorf("EnvBool(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}
