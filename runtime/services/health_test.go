package services

import "testing"

func TestNewHealthStartsNotReady(t *testing.T) {
	h := NewHealth()
	if got := h.State(); got != StateStarting {
		t.Fatalf("State() = %q, want %q", got, StateStarting)
	}
	if h.Ready() {
		t.Fatal("Ready() = true on a fresh gate, want false")
	}
}

func TestHealthReadyOnlyInStateReady(t *testing.T) {
	tests := []struct {
		state State
		ready bool
	}{
		{StateStarting, false},
		{StateReady, true},
		{StateReloading, false},
		{StateDraining, false},
		{StateStopped, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			h := NewHealth()
			h.SetState(tt.state)
			if got := h.State(); got != tt.state {
				t.Fatalf("State() = %q, want %q", got, tt.state)
			}
			if got := h.Ready(); got != tt.ready {
				t.Fatalf("Ready() = %v, want %v", got, tt.ready)
			}
		})
	}
}

// A generation cycle: the gate must go back to ready after a reload, and a drain
// must not be recoverable by anything the CLI does afterwards except a new state.
func TestHealthTransitions(t *testing.T) {
	h := NewHealth()
	for _, state := range []State{StateReady, StateReloading, StateReady, StateDraining, StateStopped} {
		h.SetState(state)
		if got := h.State(); got != state {
			t.Fatalf("after SetState(%q), State() = %q", state, got)
		}
	}
}

// The zero value is not how a gate is built, but reading one must not panic:
// a hosted service holding a *Health races the CLI's first SetState only in
// tests, and a nil-pointer deref there would be a confusing failure.
func TestHealthZeroValueReadsAsStarting(t *testing.T) {
	var h Health
	if got := h.State(); got != StateStarting {
		t.Fatalf("zero Health State() = %q, want %q", got, StateStarting)
	}
	if h.Ready() {
		t.Fatal("zero Health Ready() = true, want false")
	}
}
