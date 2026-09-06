package retention

import "time"

const (
	// settingsKey is this feature's row in site_settings.
	settingsKey = "retention"

	// maxDays bounds a window at ten years. It is not a storage limit — it is
	// there so a mistyped paste is refused at the form rather than stored as a
	// policy that will never delete anything and that nobody will look at again.
	maxDays = 3650
)

// stored is the jsonb shape in site_settings.value.
//
// Days rather than a Go duration: retention is set and reasoned about in whole
// days, and a duration string would let someone store 36h, which reads as a day
// and a half and prunes on neither boundary.
type stored struct {
	LogsDays   int       `json:"logsDays"`
	TracesDays int       `json:"tracesDays"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// Policy is the read model. Zero on an axis means keep forever, which is both
// the default and what this installation did before the feature existed — so an
// absent row and an unconfigured policy are the same thing by construction.
type Policy struct {
	LogsDays   int
	TracesDays int
	UpdatedAt  *time.Time
}

// Update is one save. Both fields are always supplied: the form submits the
// whole policy, so a partial update is not a state this needs to represent.
type Update struct {
	LogsDays   int
	TracesDays int
}

// Enabled reports whether the policy would delete anything at all.
func (p Policy) Enabled() bool {
	return p.LogsDays > 0 || p.TracesDays > 0
}

// toPolicy maps the stored row to the read model.
func (s stored) toPolicy() Policy {
	out := Policy{LogsDays: s.LogsDays, TracesDays: s.TracesDays}
	if !s.UpdatedAt.IsZero() {
		t := s.UpdatedAt
		out.UpdatedAt = &t
	}
	return out
}

// validateUpdate checks a save before anything is written.
func validateUpdate(u Update) error {
	if !validDays(u.LogsDays) || !validDays(u.TracesDays) {
		return ErrInvalidDays
	}
	return nil
}

// validDays reports whether d is a window this will store. Zero is valid and
// means keep forever; negative is not, because "delete records newer than now"
// is not a retention policy anyone means to write.
func validDays(d int) bool {
	return d >= 0 && d <= maxDays
}
