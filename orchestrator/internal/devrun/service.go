package devrun

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/juancavallotti/octo/orchestrator/internal/deployment"
	"github.com/juancavallotti/octo/orchestrator/internal/integration"
	"github.com/juancavallotti/octo/orchestrator/internal/kube"
	"github.com/juancavallotti/octo/orchestrator/internal/resource"
)

const (
	// devRunTokenBytes is the entropy behind the bundle-pull token, matching
	// apikey's. The plaintext lives only in the pod's env; the orchestrator keeps
	// its hash on the workload.
	devRunTokenBytes = 32
	// touchInterval is how stale the last-activity annotation has to be before a
	// reload rewrites it. Reap granularity is measured in tens of minutes, so a
	// minute's precision is free — and this turns a burst of debounced saves into one
	// API-server write instead of one per save.
	touchInterval = time.Minute
	// DefaultIdleTimeout is how long a dev run survives without being reloaded. It
	// follows an editing session, so the bound is "did anyone touch this in the last
	// hour", not a resource budget.
	DefaultIdleTimeout = time.Hour
)

// devRunCluster is everything the service needs from Kubernetes. It is declared
// here, in the consumer, and satisfied structurally by *kube.Client — so a service
// test drives a hand-written fake with no cluster, and the package never grows a
// second state store to be tempted by.
type devRunCluster interface {
	DevRunsEnabled() bool
	ApplyDevRun(ctx context.Context, spec kube.DevRunSpec) error
	ReconcileDevRunService(ctx context.Context, spec kube.DevRunSpec) error
	PublishDevRunEndpoint(ctx context.Context, spec kube.DevRunSpec) error
	WithdrawDevRunEndpoint(ctx context.Context, devRunID string) error
	DeleteDevRun(ctx context.Context, devRunID string) error
	TouchDevRun(ctx context.Context, devRunID string, at time.Time) error
	SetDevRunHost(ctx context.Context, devRunID, host string) error
	ListDevRuns(ctx context.Context, sel kube.DevRunSelector) ([]kube.DevRun, error)
	PodLogs(ctx context.Context, podName, container string, follow bool, tail int64) (io.ReadCloser, error)
	SidecarURL(devRunID string) string
	ExternalHost(subdomain string) string
	ExternalURL(subdomain string) string
}

// integrationStore reads the integration a dev run runs. *integration.Service
// satisfies it.
type integrationStore interface {
	Get(ctx context.Context, id string) (integration.Integration, error)
}

// resourceStore reads an integration's live (working-copy) resources — the ones a
// dev run gets, as opposed to a deployment's frozen snapshot. *resource.Service
// satisfies it.
type resourceStore interface {
	ListByIntegration(ctx context.Context, integrationID string) ([]resource.Resource, error)
}

// sidecarCommander commands one dev run's sidecar. Address and bearer are arguments
// rather than state because both are per-run and derived on demand.
type sidecarCommander interface {
	Reload(ctx context.Context, baseURL, token string) error
	Status(ctx context.Context, baseURL, token string) (json.RawMessage, error)
}

// Service holds dev-run lifecycle logic. Its state seam is the cluster: there is no
// repository, because there is no table (see the package doc).
type Service struct {
	cluster      devRunCluster
	integrations integrationStore
	resources    resourceStore
	sidecars     sidecarCommander

	hashSecret  []byte
	idleTimeout time.Duration
	// now is injectable so the reaper and the touch-throttle are testable without
	// sleeping, matching run-host's reapIdle.
	now func() time.Time
}

// Option customizes a Service at construction.
type Option func(*Service)

// WithIdleTimeout overrides how long a dev run survives without a reload.
func WithIdleTimeout(d time.Duration) Option {
	return func(s *Service) {
		if d > 0 {
			s.idleTimeout = d
		}
	}
}

// WithClock overrides the clock, for tests.
func WithClock(now func() time.Time) Option {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

// withSidecars overrides the sidecar commander, for tests. Unexported: production
// has exactly one implementation and nothing should be able to swap it from outside.
func withSidecars(c sidecarCommander) Option {
	return func(s *Service) { s.sidecars = c }
}

// NewService returns a Service. cluster may be nil, in which case every operation
// reports ErrUnavailable — the caller should not register the routes then.
//
// hashSecret keys every derivation the feature makes. An empty one disables dev runs
// outright rather than silently falling back to an unkeyed hash, because an unkeyed
// hash makes every dev run's public hostname a pure function of two guessable values
// — and the hostname is the only thing guarding it.
func NewService(
	cluster devRunCluster,
	integrations integrationStore,
	resources resourceStore,
	hashSecret []byte,
	opts ...Option,
) *Service {
	s := &Service{
		cluster:      cluster,
		integrations: integrations,
		resources:    resources,
		sidecars:     newSidecarClient(),
		hashSecret:   hashSecret,
		idleTimeout:  DefaultIdleTimeout,
		now:          time.Now,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Enabled reports whether dev runs can be served at all.
func (s *Service) Enabled() bool {
	return s != nil && s.cluster != nil && len(s.hashSecret) > 0 && s.cluster.DevRunsEnabled()
}

// IdleTimeout is how long a dev run survives without being reloaded. Exported for the
// startup log, which is where an operator reads back what their configuration resolved
// to — the setting is otherwise invisible until a run they were using disappears.
func (s *Service) IdleTimeout() time.Duration { return s.idleTimeout }

// Ensure starts a dev run for (userID, integrationID), or attaches to the one
// already running it.
//
// Idempotent because the workload's name is derived: a second editor tab clicking
// Run computes the same name, and AlreadyExists from the API server is "attached",
// not a failure. That is the whole of the concurrency control — there is no row to
// check first and so no window between checking and creating.
func (s *Service) Ensure(ctx context.Context, userID, integrationID string) (EnsureResult, error) {
	if !s.Enabled() {
		return EnsureResult{}, ErrUnavailable
	}
	if userID == "" {
		return EnsureResult{}, ErrUserRequired
	}
	if integrationID == "" {
		return EnsureResult{}, fmt.Errorf("%w: a dev run needs a saved integration to run", ErrIntegrationNotFound)
	}
	integ, err := s.integrations.Get(ctx, integrationID)
	if err != nil {
		return EnsureResult{}, fmt.Errorf("%w: %w", ErrIntegrationNotFound, err)
	}

	spec, err := s.specFor(ctx, userID, integ)
	if err != nil {
		return EnsureResult{}, err
	}

	created := true
	if err := s.cluster.ApplyDevRun(ctx, spec); err != nil {
		if !errors.Is(err, kube.ErrDevRunExists) {
			return EnsureResult{}, fmt.Errorf("devrun: start: %w", err)
		}
		// Attaching. The existing pod keeps the dev-run token it was created with —
		// spec.DevRunToken was minted for a create that did not happen and is simply
		// dropped — so reconcile the objects that CAN have gone stale since, which is
		// the endpoint: the definition may have gained or lost its HTTP_PORT while the
		// pod was running.
		created = false
		if err := s.reconcileEndpoint(ctx, spec); err != nil {
			return EnsureResult{}, err
		}
	}

	run := s.specResult(spec)
	// Prefer the cluster's own account of the run when it has one — it carries the real
	// creation time and live pod status. A create this call just made will NOT be there:
	// the listing comes from an informer cache that is milliseconds behind the write.
	// That is not an error to report, because the state we would be missing is the state
	// we just authored; reading it back as authoritative is what would turn a successful
	// Run into a spurious "not found".
	if live, err := s.get(ctx, kube.DevRunSelector{UserID: userID}, spec.ID); err == nil {
		run = live
	}
	slog.Info("dev run ensured",
		"devRunId", spec.ID, "integrationId", integrationID, "created", created,
		"networked", spec.Networked, "host", run.Host)
	return EnsureResult{DevRun: run, Created: created}, nil
}

// specResult is the dev run as the spec describes it, for reporting a create that the
// informer cache has not caught up with yet. Everything in it is either derived or was
// just written by this call, so it needs no read to be accurate — except the live pod
// status, which for a workload created moments ago is empty because there is not one
// yet.
func (s *Service) specResult(spec kube.DevRunSpec) DevRun {
	run := DevRun{
		ID:            spec.ID,
		UserID:        spec.UserID,
		IntegrationID: spec.IntegrationID,
		LastActivity:  spec.LastActivity,
		CreatedAt:     spec.LastActivity,
	}
	if spec.Networked {
		run.Host = s.cluster.ExternalHost(spec.Subdomain)
		run.TestURL = s.cluster.ExternalURL(spec.Subdomain)
	}
	return run
}

// specFor builds the pod spec for one dev run: derived identity, a freshly minted
// bundle-pull token, and whether the definition serves HTTP.
func (s *Service) specFor(ctx context.Context, userID string, integ integration.Integration) (kube.DevRunSpec, error) {
	id := deriveID(s.hashSecret, userID, integ.ID)
	subdomain := deriveLabel(s.hashSecret, userID, integ.ID)
	networked := deployment.Exposable(integ.Definition)

	if networked {
		if err := s.assertHostFree(ctx, id, subdomain); err != nil {
			return kube.DevRunSpec{}, err
		}
	}

	token, err := mintToken()
	if err != nil {
		return kube.DevRunSpec{}, err
	}
	return kube.DevRunSpec{
		ID:            id,
		UserID:        userID,
		IntegrationID: integ.ID,
		DevRunToken:   token,
		TokenHash:     hashToken(token),
		SidecarToken:  deriveSidecarToken(s.hashSecret, userID, integ.ID),
		Networked:     networked,
		Subdomain:     subdomain,
		LastActivity:  s.now(),
	}, nil
}

// assertHostFree refuses a run whose derived public host is already published by a
// different dev run.
//
// At 36^8 with a secret key this is unreachable, but "unreachable" is not a safety
// property and the failure mode is severe: a silent collision routes one user's
// public traffic to another user's pod. So it is refused with the conflict named,
// and deliberately not resolved by advancing to a second label — a resolved
// collision would have to be *remembered* to stay stable, and remembering it is the
// storage this design does without.
func (s *Service) assertHostFree(ctx context.Context, devRunID, subdomain string) error {
	host := s.cluster.ExternalHost(subdomain)
	if host == "" {
		return nil
	}
	runs, err := s.cluster.ListDevRuns(ctx, kube.DevRunSelector{})
	if err != nil {
		return fmt.Errorf("devrun: check host: %w", err)
	}
	for _, run := range runs {
		if run.Host == host && run.ID != devRunID {
			return fmt.Errorf("%w: %s is published by dev run %s", ErrHostTaken, host, run.ID)
		}
	}
	return nil
}

// Stop tears down a dev run: its endpoint, Service and Deployment.
//
// That is genuinely all of it. A dev run writes no kv_store rows (the standalone
// runtime build has no KV provider) and no database rows of any kind, so there is
// nothing to clean up, and its identity is a derivation, so nothing has to be
// preserved for the next Run to find. A Run is always a clean slate — the same
// property a fresh local child process has, here for free rather than from a
// teardown step that could be forgotten.
func (s *Service) Stop(ctx context.Context, userID, devRunID string) error {
	if !s.Enabled() {
		return ErrUnavailable
	}
	scope, err := ownerScope(userID)
	if err != nil {
		return err
	}
	if _, err := s.get(ctx, scope, devRunID); err != nil {
		return err
	}
	return s.stop(ctx, devRunID, "stopped by user")
}

// stop is the unauthorised teardown every path funnels into, so the reason a dev run
// ended is logged exactly once and in one place.
func (s *Service) stop(ctx context.Context, devRunID, reason string) error {
	if err := s.cluster.DeleteDevRun(ctx, devRunID); err != nil {
		return fmt.Errorf("devrun: stop: %w", err)
	}
	slog.Info("dev run stopped", "devRunId", devRunID, "reason", reason)
	return nil
}

// Reload makes a dev run pick up the integration's current saved state.
//
// Two things happen before the sidecar is told anything. The last-activity
// annotation is bumped, which is what keeps the reaper off it; and the endpoint is
// reconciled, because a save can flip an integration between serving HTTP and not.
// The order matters in one direction: publish *before* forwarding, so a public host
// never goes live pointing at a runtime that is not yet listening.
func (s *Service) Reload(ctx context.Context, userID, devRunID string) error {
	if !s.Enabled() {
		return ErrUnavailable
	}
	scope, err := ownerScope(userID)
	if err != nil {
		return err
	}
	run, err := s.get(ctx, scope, devRunID)
	if err != nil {
		return err
	}
	return s.reload(ctx, run)
}

// reload is the unauthorised reload every path funnels into: the user's explicit
// "reload now", and the save-triggered notification.
func (s *Service) reload(ctx context.Context, run DevRun) error {
	integ, err := s.integrations.Get(ctx, run.IntegrationID)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrIntegrationNotFound, err)
	}
	spec := kube.DevRunSpec{
		ID:            run.ID,
		UserID:        run.UserID,
		IntegrationID: run.IntegrationID,
		Networked:     deployment.Exposable(integ.Definition),
		Subdomain:     deriveLabel(s.hashSecret, run.UserID, run.IntegrationID),
	}
	if spec.Networked != (run.Host != "") {
		if err := s.reconcileEndpoint(ctx, spec); err != nil {
			return err
		}
	}
	if err := s.touch(ctx, run); err != nil {
		return err
	}
	return s.sidecars.Reload(ctx, s.cluster.SidecarURL(run.ID), deriveSidecarToken(s.hashSecret, run.UserID, run.IntegrationID))
}

// reconcileEndpoint brings a dev run's Service and external endpoint in line with
// whether its definition currently serves HTTP.
//
// The Service exists for every dev run (it is how the sidecar is reached), so
// flipping to networked only adds a port to it; flipping away leaves the port and
// withdraws the endpoint, because a Service port pointing at nothing is inert while
// a published hostname pointing at nothing is a 502 someone has to explain.
func (s *Service) reconcileEndpoint(ctx context.Context, spec kube.DevRunSpec) error {
	if !spec.Networked {
		if err := s.cluster.WithdrawDevRunEndpoint(ctx, spec.ID); err != nil {
			return fmt.Errorf("devrun: withdraw endpoint: %w", err)
		}
		if err := s.cluster.SetDevRunHost(ctx, spec.ID, ""); err != nil {
			return fmt.Errorf("devrun: clear host: %w", err)
		}
		return nil
	}
	if err := s.cluster.ReconcileDevRunService(ctx, spec); err != nil {
		return fmt.Errorf("devrun: reconcile service: %w", err)
	}
	if err := s.cluster.PublishDevRunEndpoint(ctx, spec); err != nil {
		return fmt.Errorf("devrun: publish endpoint: %w", err)
	}
	if err := s.cluster.SetDevRunHost(ctx, spec.ID, s.cluster.ExternalHost(spec.Subdomain)); err != nil {
		return fmt.Errorf("devrun: record host: %w", err)
	}
	return nil
}

// touch records that the dev run was just used, but only when the recorded value is
// stale enough to be worth a write. A 2-second-debounced editor saving in a burst
// would otherwise patch the API server once per save to store a timestamp read at
// hour granularity.
func (s *Service) touch(ctx context.Context, run DevRun) error {
	now := s.now()
	if now.Sub(run.LastActivity) < touchInterval {
		return nil
	}
	if err := s.cluster.TouchDevRun(ctx, run.ID, now); err != nil {
		return fmt.Errorf("devrun: touch: %w", err)
	}
	return nil
}

// Bundle returns the workspace bundle for a dev run: the integration's saved
// definition, with the dev-env resource declared, plus its live resources.
//
// Authorised by the presented token, not by a user. The integration id comes off the
// *workload's* label rather than from the request, so a dev pod can only ever fetch
// the integration it was created to run — and because the token's hash lives on that
// workload, a token whose pod is gone authorises nothing at all.
func (s *Service) Bundle(ctx context.Context, devRunID, token string) (Bundle, error) {
	run, err := s.authorize(ctx, devRunID, token)
	if err != nil {
		return Bundle{}, err
	}
	integ, err := s.integrations.Get(ctx, run.IntegrationID)
	if err != nil {
		return Bundle{}, fmt.Errorf("%w: %w", ErrIntegrationNotFound, err)
	}
	stored, err := s.resources.ListByIntegration(ctx, run.IntegrationID)
	if err != nil {
		return Bundle{}, fmt.Errorf("devrun: list resources: %w", err)
	}
	files := make([]Resource, 0, len(stored))
	for _, r := range stored {
		files = append(files, Resource{Name: r.Name, Kind: r.Kind, Content: r.Content})
	}
	return buildBundle(integ.Definition, files), nil
}

// Expire tears a dev run down at its own sidecar's request, which is what a sidecar
// does when its bundle pull 404s — its integration was deleted, or the run was torn
// down and this pod outlived it.
//
// Token-authorised and separate from Stop's route on purpose: DELETE /devruns/{id}
// authorises a *user* action, and a pod holding a dev-run token is not a user.
func (s *Service) Expire(ctx context.Context, devRunID, token string) error {
	run, err := s.authorize(ctx, devRunID, token)
	if err != nil {
		return err
	}
	return s.stop(ctx, run.ID, "expired by its sidecar")
}

// authorize resolves a dev run from an id and the token its sidecar presented,
// comparing against the hash recorded on the workload.
//
// A dev run whose workload carries no token hash authorises nothing: that is a
// workload from before the convention, or one written wrong, and treating an absent
// hash as "no check needed" is how an authorisation bug ships silently.
func (s *Service) authorize(ctx context.Context, devRunID, token string) (DevRun, error) {
	if !s.Enabled() {
		return DevRun{}, ErrUnavailable
	}
	run, err := s.get(ctx, kube.DevRunSelector{}, devRunID)
	if err != nil {
		return DevRun{}, err
	}
	if run.tokenHash == "" || token == "" || !constantTimeEqual(run.tokenHash, hashToken(token)) {
		return DevRun{}, ErrUnauthorized
	}
	return run, nil
}

// Status reports one dev run's live state: the cluster's answer, plus the sidecar's
// own view folded in when it answers promptly.
//
// The cluster half is the one that matters and it comes from the informer cache. The
// sidecar half is fetched best-effort with a short timeout and left out on failure —
// it carries which generation is actually applied, which is the only way a user can
// tell "my save reached the pod" from "my save is still in Postgres", so it is worth
// one in-cluster hop per poll.
func (s *Service) Status(ctx context.Context, userID, devRunID string) (DevRun, error) {
	if !s.Enabled() {
		return DevRun{}, ErrUnavailable
	}
	scope, err := ownerScope(userID)
	if err != nil {
		return DevRun{}, err
	}
	run, err := s.get(ctx, scope, devRunID)
	if err != nil {
		return DevRun{}, err
	}
	sidecar, err := s.sidecars.Status(ctx, s.cluster.SidecarURL(run.ID),
		deriveSidecarToken(s.hashSecret, run.UserID, run.IntegrationID))
	if err != nil {
		// Expected while a pod is still starting, so this is debug rather than warn: the
		// cluster status already reports that it is not ready, and logging a warning per
		// poll during a normal startup is how a log stops being read.
		slog.Debug("dev run sidecar status unavailable", "devRunId", run.ID, "error", err)
		return run, nil
	}
	run.Sidecar = sidecar
	return run, nil
}

// PodLogs opens a stream of the dev run's runtime logs, replaying the last tail lines
// and — with follow — tailing until the caller closes the stream or the request goes
// away.
//
// This is the whole log path: the runtime container writes to stdout and the
// orchestrator streams the pod, so a dev run needs no log shipping, no ring buffer in
// a BFF replica, and no per-run state anywhere. Replay-then-follow comes free from
// Kubernetes, which is the other half of why the buffer this replaces is gone.
func (s *Service) PodLogs(
	ctx context.Context, userID, devRunID string, follow bool, tail int64,
) (io.ReadCloser, error) {
	if !s.Enabled() {
		return nil, ErrUnavailable
	}
	scope, err := ownerScope(userID)
	if err != nil {
		return nil, err
	}
	run, err := s.get(ctx, scope, devRunID)
	if err != nil {
		return nil, err
	}
	pod := runtimePod(run.Detail)
	if pod == "" {
		return nil, ErrNotStarted
	}
	// The container has to be named: a dev-run pod has two, and Kubernetes rejects an
	// ambiguous log request. The runtime's is the one anyone asking for "the logs"
	// means; the sidecar's own diagnostics are on its /diagnostics route.
	return s.cluster.PodLogs(ctx, pod, kube.RuntimeContainer, follow, tail)
}

// runtimePod picks which of a dev run's pods to read.
//
// There is normally exactly one — a dev run has a single replica — but a restart
// briefly leaves two, and the logs a developer wants are from the one that is actually
// serving. So prefer a ready pod, and fall back to the first: a crash-looping run has
// no ready pod at all, and that is precisely when its logs are worth reading.
func runtimePod(st kube.Status) string {
	for _, p := range st.Pods {
		if p.Ready {
			return p.Name
		}
	}
	if len(st.Pods) > 0 {
		return st.Pods[0].Name
	}
	return ""
}

// ListByUser answers "what am I running?" — and, with integrationID set, the
// editor's Run-vs-Attach question.
//
// This is the query that replaces a table, and it is a label lookup against a synced
// informer cache: no database, no API call, and no way for the answer to disagree
// with what is actually running.
func (s *Service) ListByUser(ctx context.Context, userID, integrationID string) ([]DevRun, error) {
	if !s.Enabled() {
		return nil, ErrUnavailable
	}
	scope, err := ownerScope(userID)
	if err != nil {
		return nil, err
	}
	scope.IntegrationID = integrationID
	runs, err := s.cluster.ListDevRuns(ctx, scope)
	if err != nil {
		return nil, fmt.Errorf("devrun: list: %w", err)
	}
	out := make([]DevRun, 0, len(runs))
	for _, run := range runs {
		out = append(out, s.toDevRun(run))
	}
	return out, nil
}

// NotifyIntegrationChanged reloads every dev run for an integration whose stored
// state just changed. It is the save→reload seam, and it is wired into the write
// path rather than into the editor so that every writer is covered at once — the
// editor's save, the integrations list, MCP's update_flow, any API-key client.
//
// Best-effort by contract, and the caller must never fail a write because of it. A
// save that succeeded with a reload that did not is a stale running app, which the
// status surface already reports; a save rolled back because a pod was unreachable
// is data loss. It returns an error only so a caller can log it.
//
// The common case — nobody is running this integration — costs no I/O at all,
// because the listing comes from the informer cache.
func (s *Service) NotifyIntegrationChanged(ctx context.Context, integrationID string) error {
	if !s.Enabled() || integrationID == "" {
		return nil
	}
	runs, err := s.cluster.ListDevRuns(ctx, kube.DevRunSelector{IntegrationID: integrationID})
	if err != nil {
		return fmt.Errorf("devrun: notify: list: %w", err)
	}
	var errs []error
	for _, run := range runs {
		if err := s.reload(ctx, s.toDevRun(run)); err != nil {
			errs = append(errs, fmt.Errorf("dev run %s: %w", run.ID, err))
		}
	}
	return errors.Join(errs...)
}

// ReapIdle stops every dev run that has not been reloaded within the idle timeout,
// returning how many it collected.
//
// This is also the only thing that collects a dev run whose integration was deleted
// or whose tab was simply abandoned. There is no delete hook for either: nobody is
// watching an abandoned run, and a deleted integration's run is torn down sooner
// anyway, the moment anything tries to reload it and its sidecar's pull 404s.
func (s *Service) ReapIdle(ctx context.Context) (int, error) {
	if !s.Enabled() {
		return 0, nil
	}
	runs, err := s.cluster.ListDevRuns(ctx, kube.DevRunSelector{})
	if err != nil {
		return 0, fmt.Errorf("devrun: reap: list: %w", err)
	}
	cutoff := s.now().Add(-s.idleTimeout)
	reaped := 0
	var errs []error
	for _, run := range runs {
		// A workload with no last-activity annotation falls back to its creation time.
		// The alternative readings are both wrong in a way that matters: treat it as
		// zero and a bug in the annotation reaps live runs instantly; treat it as now
		// and the same bug leaks pods forever.
		last := run.LastActivity
		if last.IsZero() {
			last = run.CreatedAt
		}
		if !last.Before(cutoff) {
			continue
		}
		if err := s.stop(ctx, run.ID, "idle"); err != nil {
			errs = append(errs, err)
			continue
		}
		reaped++
	}
	return reaped, errors.Join(errs...)
}

// ownerScope is the selector that confines a user-facing operation to the caller's own
// dev runs.
//
// It exists so that the one thing every such operation must not get wrong is decided
// once. An empty user id in kube.DevRunSelector means "do not filter by user", which
// for a listing is merely wrong and for Stop or Reload is one user acting on another's
// pod — so the empty case is refused here rather than remembered at five call sites.
func ownerScope(userID string) (kube.DevRunSelector, error) {
	if userID == "" {
		return kube.DevRunSelector{}, ErrUserRequired
	}
	return kube.DevRunSelector{UserID: userID}, nil
}

// get returns one dev run within the given selector's scope.
//
// A run outside the scope — another user's — reads as ErrNotFound rather than a
// distinct "forbidden", so probing an id cannot tell the two apart. Same shape as
// the resource module, where a resource is only ever addressed within its
// integration.
func (s *Service) get(ctx context.Context, sel kube.DevRunSelector, devRunID string) (DevRun, error) {
	runs, err := s.cluster.ListDevRuns(ctx, sel)
	if err != nil {
		return DevRun{}, fmt.Errorf("devrun: get: %w", err)
	}
	for _, run := range runs {
		if run.ID == devRunID {
			return s.toDevRun(run), nil
		}
	}
	return DevRun{}, ErrNotFound
}

// toDevRun maps a cluster workload to the domain type, filling in the one thing the
// cluster does not carry: the host as a URL.
func (s *Service) toDevRun(run kube.DevRun) DevRun {
	out := DevRun{
		ID:            run.ID,
		UserID:        run.UserID,
		IntegrationID: run.IntegrationID,
		Host:          run.Host,
		LastActivity:  run.LastActivity,
		CreatedAt:     run.CreatedAt,
		Detail:        run.Status,
		tokenHash:     run.TokenHash,
	}
	if run.Host != "" {
		out.TestURL = s.cluster.ExternalURL(deriveLabel(s.hashSecret, run.UserID, run.IntegrationID))
	}
	return out
}

// mintToken returns a random bundle-pull token. Random rather than derived, because
// the orchestrator can verify it against the workload's annotation — which means a
// leaked one stops working the instant the pod does. See deriveSidecarToken for why
// the other direction cannot do this.
func mintToken() (string, error) {
	buf := make([]byte, devRunTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("devrun: mint token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// hashToken returns the hex SHA-256 of a token. The same function records it and
// checks it, so the two always agree.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// constantTimeEqual compares two hex digests without leaking, through timing, how
// long a shared prefix they have. Both operands are fixed-length hex here so the
// length check reveals nothing.
func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
