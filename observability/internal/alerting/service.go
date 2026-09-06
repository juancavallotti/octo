package alerting

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// MaxWatches bounds an installation.
//
// The same reasoning behind podstats.MaxSelectedSeries: refusing the next one is
// better than answering slowly, because a tick that cannot finish inside its
// interval degrades every watch rather than the one that was added last.
const MaxWatches = 200

// watchStore is what the service needs from persistence, declared here where it
// is consumed. It overlaps the runner's own interface deliberately — the two
// callers need different subsets, and naming one union would make each of them
// depend on methods it never calls.
type watchStore interface {
	Create(ctx context.Context, w Watch, createdBy string) (Watch, error)
	Update(ctx context.Context, w Watch, updatedBy string) (Watch, error)
	Get(ctx context.Context, id string) (Watch, error)
	Delete(ctx context.Context, id string) error
	List(ctx context.Context) ([]Due, error)
	Count(ctx context.Context) (int, error)
	Retire(ctx context.Context, watchID, reason string, at time.Time) error
	Mute(ctx context.Context, watchID string, until time.Time) error
	Acknowledge(ctx context.Context, incidentID, userID string, at time.Time) error
	Incidents(ctx context.Context, f IncidentFilter) ([]Incident, error)
	History(ctx context.Context, f HistoryFilter) ([]EvaluationRecord, error)
}

// previewer evaluates a watch without recording anything. *Runner satisfies it.
type previewer interface {
	Preview(ctx context.Context, w Watch) (Evaluation, error)
}

// Service is the CRUD and validation layer between the API and the store.
//
// Validating actions is injected rather than imported, because the package that
// knows how to build an action imports this one. The seam is one function, and it
// is the same function the dispatcher validates with — a second implementation
// would drift from the thing it was validating, and the drift would surface as an
// action that saved cleanly and never delivered.
type Service struct {
	store    watchStore
	preview  previewer
	validate func(Watch) error
	now      func() time.Time
}

// NewService wires the service. validateActions may be nil in a process that
// performs none.
func NewService(store watchStore, preview previewer, validateActions func(Watch) error) *Service {
	if validateActions == nil {
		validateActions = func(Watch) error { return nil }
	}
	return &Service{store: store, preview: preview, validate: validateActions, now: time.Now}
}

// Create validates and stores a new watch.
func (s *Service) Create(ctx context.Context, w Watch, userID string) (Watch, error) {
	if err := s.check(w); err != nil {
		return Watch{}, err
	}
	count, err := s.store.Count(ctx)
	if err != nil {
		return Watch{}, err
	}
	if count >= MaxWatches {
		return Watch{}, fmt.Errorf("alerting: %w: this installation already has %d",
			ErrTooManyWatches, count)
	}
	return s.store.Create(ctx, normalize(w), userID)
}

// Update replaces a watch's definition.
//
// Disabling one retires it in the same call: an episode that outlives the watch
// that can resolve it stays open forever, and somebody switching a noisy watch
// off means to stop hearing about it rather than to freeze its last incident on
// the dashboard.
func (s *Service) Update(ctx context.Context, w Watch, userID string) (Watch, error) {
	if err := s.check(w); err != nil {
		return Watch{}, err
	}
	updated, err := s.store.Update(ctx, normalize(w), userID)
	if err != nil {
		return Watch{}, err
	}
	// Retired whenever the saved watch is disabled, rather than only on the
	// enabled-to-disabled transition. Retiring an already-quiet watch is a no-op
	// on both rows, and keying it to the transition would leave an episode open
	// forever on a watch disabled by any path this call did not observe.
	if !updated.Enabled {
		if err := s.store.Retire(ctx, updated.ID, ClosedDisabled, s.now()); err != nil {
			return Watch{}, err
		}
	}
	return updated, nil
}

// Get reads one watch.
func (s *Service) Get(ctx context.Context, id string) (Watch, error) {
	return s.store.Get(ctx, id)
}

// List returns every watch with its current state.
func (s *Service) List(ctx context.Context) ([]Due, error) {
	return s.store.List(ctx)
}

// Delete removes a watch, closing any episode first so nothing is left pointing
// at a watch that no longer exists.
func (s *Service) Delete(ctx context.Context, id string) error {
	if err := s.store.Retire(ctx, id, ClosedDeleted, s.now()); err != nil {
		return err
	}
	return s.store.Delete(ctx, id)
}

// Preview evaluates a definition now and reports what it would have decided,
// without recording anything or telling anybody.
//
// It takes a whole watch rather than an id on purpose: the editor's use is to
// judge a definition that has not been saved, which is the only time the question
// is worth asking.
func (s *Service) Preview(ctx context.Context, w Watch) (Evaluation, error) {
	if s.preview == nil {
		return Evaluation{}, fmt.Errorf("alerting: this process does not evaluate watches")
	}
	if err := s.check(w); err != nil {
		return Evaluation{}, err
	}
	return s.preview.Preview(ctx, normalize(w))
}

// Mute suppresses a watch's notifications until the given time, without stopping
// its evaluation: the history stays complete and its incident still resolves.
func (s *Service) Mute(ctx context.Context, id string, until time.Time) error {
	if _, err := s.store.Get(ctx, id); err != nil {
		return err
	}
	return s.store.Mute(ctx, id, until)
}

// Acknowledge marks an open incident as seen.
func (s *Service) Acknowledge(ctx context.Context, incidentID, userID string) error {
	return s.store.Acknowledge(ctx, incidentID, userID, s.now())
}

// Incidents lists episodes.
func (s *Service) Incidents(ctx context.Context, f IncidentFilter) ([]Incident, error) {
	return s.store.Incidents(ctx, f)
}

// History pages the evaluation log.
func (s *Service) History(ctx context.Context, f HistoryFilter) ([]EvaluationRecord, error) {
	return s.store.History(ctx, f)
}

// check runs every validation a definition has to pass, in one place, so saving
// and previewing cannot disagree about what is acceptable.
func (s *Service) check(w Watch) error {
	if _, err := Build(normalize(w)); err != nil {
		return err
	}
	if err := s.validate(w); err != nil {
		return err
	}
	return searchNeedsAScope(w)
}

// searchNeedsAScope refuses a log search that would scan the whole table.
//
// The log indexes serve a deployment and a window; a substring predicate filters
// on top of them. Bounded by a deployment that is affordable, and unbounded it is
// a sequential scan of every log line in the installation, once a minute, forever.
// Refused at save time rather than at evaluation, while somebody is looking.
func searchNeedsAScope(w Watch) error {
	for _, c := range w.Conditions {
		if c.Source != SourceLogs || strings.TrimSpace(c.Scope.Search) == "" {
			continue
		}
		if c.Scope.DeploymentID == "" && c.Scope.AppName == "" {
			return fmt.Errorf(
				"alerting: %w: condition %q searches log messages, which needs a deployment or app to scope it",
				ErrInvalidParams, c.ID)
		}
	}
	return nil
}

// normalize trims what a form submits, so two watches differing only in trailing
// whitespace are not two watches.
func normalize(w Watch) Watch {
	w.Name = strings.TrimSpace(w.Name)
	w.Description = strings.TrimSpace(w.Description)
	if w.Severity == "" {
		w.Severity = SeverityWarning
	}
	if w.OnNoData == "" {
		w.OnNoData = NoDataOK
	}
	if w.Combinator == "" {
		w.Combinator = CombineAll
	}
	return w
}

// The severities a watch may carry. A closed set, because they order an incident
// list and a free-text one orders nothing.
const (
	SeverityInfo     = "info"
	SeverityWarning  = "warning"
	SeverityCritical = "critical"
)

// ValidSeverity reports whether s is one this platform knows.
func ValidSeverity(s string) bool {
	switch s {
	case SeverityInfo, SeverityWarning, SeverityCritical:
		return true
	default:
		return false
	}
}
