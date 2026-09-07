package retention

import "time"

const (
	// settingsKey is this feature's row in site_settings.
	settingsKey = "retention"

	// maxDays bounds a window at ten years. It is not a storage limit — it is
	// there so a mistyped paste is refused at the form rather than stored as a
	// policy that will never delete anything and that nobody will look at again.
	maxDays = 3650

	// defaultAlertDays is how long the alerting history is kept when nobody has
	// said.
	//
	// The other two axes default to keeping everything, and this one deliberately
	// does not. Logs and traces are evidence somebody may need to produce months
	// later; an evaluation log is diagnostic and its value decays in days, while
	// its volume does not — a hundred one-minute watches write about 144,000 rows
	// a day whether or not anything happens. Inheriting "unset means forever"
	// would leave every installation quietly accumulating that table after the
	// upgrade, which is a decision nobody would have made on purpose.
	defaultAlertDays = 14
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

	// AlertsDays is a pointer so that "never configured" and "configured to keep
	// forever" stay different facts. A row written before alerting existed has no
	// key at all and gets the default; somebody who deliberately sets zero gets
	// what every other axis means by it. A plain int would collapse the two and
	// silently switch the default off for every existing installation on upgrade.
	AlertsDays *int `json:"alertsDays,omitempty"`
}

// Policy is the read model. Zero on an axis means keep forever, which is both
// the default and what this installation did before the feature existed — so an
// absent row and an unconfigured policy are the same thing by construction.
type Policy struct {
	LogsDays   int
	TracesDays int
	// AlertsDays is resolved: an unconfigured policy reads back as the default
	// rather than as zero, so every reader sees the window that is actually in
	// force. Zero here means the same as it does on the other two axes — keep
	// forever — and it only appears when somebody asked for it.
	AlertsDays int
	UpdatedAt  *time.Time
}

// Update is one save. Both fields are always supplied: the form submits the
// whole policy, so a partial update is not a state this needs to represent.
type Update struct {
	LogsDays   int
	TracesDays int
	AlertsDays int
}

// Enabled reports whether the policy would delete anything at all.
func (p Policy) Enabled() bool {
	return p.LogsDays > 0 || p.TracesDays > 0 || p.AlertsDays > 0
}

// toPolicy maps the stored row to the read model.
func (s stored) toPolicy() Policy {
	out := Policy{LogsDays: s.LogsDays, TracesDays: s.TracesDays, AlertsDays: defaultAlertDays}
	if s.AlertsDays != nil {
		out.AlertsDays = *s.AlertsDays
	}
	if !s.UpdatedAt.IsZero() {
		t := s.UpdatedAt
		out.UpdatedAt = &t
	}
	return out
}

// validateUpdate checks a save before anything is written.
func validateUpdate(u Update) error {
	if !validDays(u.LogsDays) || !validDays(u.TracesDays) || !validDays(u.AlertsDays) {
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
