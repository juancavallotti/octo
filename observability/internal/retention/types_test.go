package retention

import (
	"errors"
	"testing"
	"time"
)

func TestValidateUpdate(t *testing.T) {
	cases := []struct {
		name string
		up   Update
		want error
	}{
		{"both unset keeps everything", Update{}, nil},
		{"ordinary windows", Update{LogsDays: 30, TracesDays: 7}, nil},
		{"one axis off", Update{LogsDays: 30}, nil},
		{"the upper bound itself", Update{LogsDays: maxDays, TracesDays: maxDays}, nil},
		{"beyond the bound", Update{LogsDays: maxDays + 1}, ErrInvalidDays},
		{"negative logs", Update{LogsDays: -1}, ErrInvalidDays},
		{"negative traces", Update{TracesDays: -1}, ErrInvalidDays},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateUpdate(c.up)
			if !errors.Is(err, c.want) {
				t.Fatalf("validateUpdate(%+v) = %v, want %v", c.up, err, c.want)
			}
		})
	}
}

// An absent row decodes to the zero value, and the zero value must mean "keep
// everything". This is the property that makes installing the feature a no-op
// until an admin opts in, so it is worth pinning rather than assuming.
func TestZeroPolicyKeepsEverything(t *testing.T) {
	var s stored
	p := s.toPolicy()
	if p.Enabled() {
		t.Fatal("the zero policy reports itself enabled")
	}
	if p.UpdatedAt != nil {
		t.Fatalf("UpdatedAt = %v, want nil for a row that was never written", p.UpdatedAt)
	}
}

func TestPolicyEnabledOnEitherAxis(t *testing.T) {
	if !(Policy{LogsDays: 1}).Enabled() {
		t.Fatal("a logs-only policy reports itself disabled")
	}
	if !(Policy{TracesDays: 1}).Enabled() {
		t.Fatal("a traces-only policy reports itself disabled")
	}
}

func TestToPolicyCarriesUpdatedAt(t *testing.T) {
	when := time.Date(2026, 8, 11, 3, 0, 0, 0, time.UTC)
	p := stored{LogsDays: 30, TracesDays: 7, UpdatedAt: when}.toPolicy()

	if p.LogsDays != 30 || p.TracesDays != 7 {
		t.Fatalf("days = (%d, %d), want (30, 7)", p.LogsDays, p.TracesDays)
	}
	if p.UpdatedAt == nil || !p.UpdatedAt.Equal(when) {
		t.Fatalf("UpdatedAt = %v, want %v", p.UpdatedAt, when)
	}
}
