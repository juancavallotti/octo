package main

import (
	"slices"
	"testing"
)

// The parsing is small, and every case here is one the chart can actually emit:
// an empty list renders an empty string, and a values file with a trailing comma
// or a stray space renders exactly that. A blank surviving the split becomes a
// LocalObjectReference naming a Secret called "", which the kubelet reports as
// missing on every pod the orchestrator deploys — a failure whose message names
// no Secret at all.
func TestImagePullSecretsConfig(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want []string
	}{
		{"unset", "", nil},
		{"one", "regcred", []string{"regcred"}},
		{"several", "regcred,mirror-creds", []string{"regcred", "mirror-creds"}},
		{"spaces", " regcred , mirror-creds ", []string{"regcred", "mirror-creds"}},
		{"trailing comma", "regcred,", []string{"regcred"}},
		{"only separators", " , ", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("RUNTIME_IMAGE_PULL_SECRETS", tt.env)
			got := imagePullSecretsConfig()
			// Nilness is asserted on its own because slices.Equal(nil, []string{})
			// is true, so the nil cases above would pass against an empty slice —
			// which is the one distinction this function's contract makes.
			if !slices.Equal(got, tt.want) || (got == nil) != (tt.want == nil) {
				t.Errorf("imagePullSecretsConfig() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
