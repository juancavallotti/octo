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

// An absent row decodes to the zero value, and on the two streams anybody may
// need to produce months later that must still mean "keep everything" — the
// property that makes installing retention a no-op until an admin opts in.
//
// The alerting axis is deliberately not like that, and this pins the difference:
// an unwritten row resolves to the default window rather than to zero, because an
// evaluation log left to grow forever is not a decision anybody would have made
// on purpose.
func TestZeroPolicyKeepsTheTwoEvidenceStreamsForever(t *testing.T) {
	var s stored
	p := s.toPolicy()
	if p.LogsDays != 0 || p.TracesDays != 0 {
		t.Fatalf("policy = %+v, want both evidence windows unset", p)
	}
	if p.AlertsDays != defaultAlertDays {
		t.Fatalf("AlertsDays = %d, want the %d-day default", p.AlertsDays, defaultAlertDays)
	}
	if !p.Enabled() {
		t.Fatal("an unconfigured policy prunes nothing, so the alert history would grow forever")
	}
	if p.UpdatedAt != nil {
		t.Fatalf("UpdatedAt = %v, want nil for a row that was never written", p.UpdatedAt)
	}
}

// A stored zero is different from an absent key, and means what it means
// everywhere else: keep it forever.
func TestAnExplicitZeroAlertWindowIsHonoured(t *testing.T) {
	zero := 0
	p := stored{AlertsDays: &zero}.toPolicy()
	if p.AlertsDays != 0 {
		t.Fatalf("AlertsDays = %d, want the zero that was written", p.AlertsDays)
	}
	if p.Enabled() {
		t.Fatal("a policy that keeps all three streams reports itself enabled")
	}
}

func TestPolicyEnabledOnEitherAxis(t *testing.T) {
	if !(Policy{LogsDays: 1}).Enabled() {
		t.Fatal("a logs-only policy reports itself disabled")
	}
	if !(Policy{TracesDays: 1}).Enabled() {
		t.Fatal("a traces-only policy reports itself disabled")
	}
	if !(Policy{AlertsDays: 1}).Enabled() {
		t.Fatal("an alerts-only policy reports itself disabled")
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
