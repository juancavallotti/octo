package podstats

import "testing"

// The keys are the published contract with the writer, so they are asserted as
// literals rather than rebuilt from the same helpers that produce them.
func TestKeys(t *testing.T) {
	const dep, pod = "dep-1", "octo-dep-1-abc"

	for name, tc := range map[string]struct{ got, want string }{
		"pods":   {PodsKey(dep), "octo:stats:v0:dep-1:pods"},
		"meta":   {MetaKey(dep, pod), "octo:stats:v0:dep-1:octo-dep-1-abc:meta"},
		"dict":   {DictKey(dep, pod, 3), "octo:stats:v0:dep-1:octo-dep-1-abc:dict:3"},
		"live":   {LiveKey(dep, pod), "octo:stats:v0:dep-1:octo-dep-1-abc:live"},
		"rollup": {RollupKey(dep, pod), "octo:stats:v0:dep-1:octo-dep-1-abc:rollup"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s key = %q, want %q", name, tc.got, tc.want)
		}
	}
}

func TestTierKeySelectsTheList(t *testing.T) {
	const dep, pod = "dep-1", "p"

	if got := TierKey(dep, pod, TierRollup); got != RollupKey(dep, pod) {
		t.Errorf("rollup tier key = %q, want the rollup list", got)
	}
	if got := TierKey(dep, pod, TierLive); got != LiveKey(dep, pod) {
		t.Errorf("live tier key = %q, want the live list", got)
	}
	// Auto is resolved before a key is ever built; if one is asked for anyway,
	// the full-resolution tier is the safe reading.
	if got := TierKey(dep, pod, TierAuto); got != LiveKey(dep, pod) {
		t.Errorf("auto tier key = %q, want the live list", got)
	}
}
