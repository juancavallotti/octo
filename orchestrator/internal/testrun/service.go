package testrun

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/juancavallotti/octo/orchestrator/internal/deployment"
	"github.com/juancavallotti/octo/orchestrator/internal/kube"
	"github.com/juancavallotti/octo/orchestrator/internal/projectfile"
	"github.com/juancavallotti/octo/orchestrator/internal/snapshot"
)

const (
	// pollInterval paces the watcher reading a run's Job.
	pollInterval = 3 * time.Second
	// grace is how long past its deadline a run may stay running before it is
	// treated as abandoned: Kubernetes kills it at the deadline, and this covers
	// the time to notice.
	grace = time.Minute
	// stepTimeout bounds each cluster or database call the watcher makes.
	stepTimeout = 15 * time.Second
	// rolloutTimeout bounds the rollout a passing run asked for.
	rolloutTimeout = 30 * time.Second
	// listLimit is how many of a deployment's runs a list returns.
	listLimit = 20
)

// repository is the persistence surface the service needs. *Repo satisfies it.
type repository interface {
	Create(ctx context.Context, run Run) (Run, error)
	Get(ctx context.Context, deploymentID, id string) (Run, error)
	ListByDeployment(ctx context.Context, deploymentID string, limit int) ([]Run, error)
	Latest(ctx context.Context) ([]Run, error)
	ListRunningSince(ctx context.Context, t time.Time) ([]Run, error)
	Finish(ctx context.Context, id string, res Result) (bool, error)
	RecordRollout(ctx context.Context, id string, at *time.Time, rolloutErr string) error
}

// deployments is the slice of the deployment service a test run needs.
// *deployment.Service satisfies it.
type deployments interface {
	TestPlan(ctx context.Context, id, snapshotID string) (deployment.TestPlan, error)
	Rollout(
		ctx context.Context, id, snapshotID string, env map[string]deployment.EnvBinding, tracing *bool, runner *string,
	) (deployment.Deployment, error)
	CurrentRuntime() string
	List(ctx context.Context) ([]deployment.Deployment, error)
}

// snapshots reads the files frozen under a snapshot. *snapshot.Service
// satisfies it.
type snapshots interface {
	ListResources(ctx context.Context, snapshotID string) ([]snapshot.Resource, error)
}

// cluster is the Kubernetes surface a test run drives. *kube.Client satisfies it.
type cluster interface {
	TestRunsEnabled() bool
	ApplyTestRun(ctx context.Context, spec kube.TestRunSpec) error
	TestRunState(ctx context.Context, runID string) (kube.TestRunState, error)
	TestRunLogs(ctx context.Context, pod string) ([]byte, error)
	DeleteTestRun(ctx context.Context, runID string) error
}

// Service starts test runs and records how they end.
type Service struct {
	repo        repository
	deployments deployments
	snapshots   snapshots
	cluster     cluster
	now         func() time.Time
	poll        time.Duration
}

// Option configures a Service.
type Option func(*Service)

// WithClock overrides the service's clock and poll interval, for tests.
func WithClock(now func() time.Time, poll time.Duration) Option {
	return func(s *Service) {
		s.now = now
		s.poll = poll
	}
}

// NewService returns a Service. cluster may be nil, in which case every run is
// refused with ErrUnavailable.
func NewService(repo repository, deps deployments, snaps snapshots, cl cluster, opts ...Option) *Service {
	s := &Service{repo: repo, deployments: deps, snapshots: snaps, cluster: cl, now: time.Now, poll: pollInterval}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Enabled reports whether this install can run tests.
func (s *Service) Enabled() bool {
	return s.cluster != nil && s.cluster.TestRunsEnabled()
}

// Start runs deployment id's suites from snapshotID (the snapshot it runs now,
// when empty) against the runtime release the install deploys, and returns the
// running run.
//
// The run is watched from here until it ends, under ctx's values but not its
// cancellation, so the credential of whoever started it is what a rollout on
// pass is performed with.
func (s *Service) Start(ctx context.Context, id, snapshotID string, rolloutOnPass bool, createdBy string) (Run, error) {
	if !s.Enabled() {
		return Run{}, ErrUnavailable
	}
	plan, err := s.deployments.TestPlan(ctx, id, snapshotID)
	if err != nil {
		return Run{}, err
	}
	files := make(map[string]string, len(plan.Resources))
	var suites []string
	for _, res := range plan.Resources {
		files[res.Name] = res.Content
		if projectfile.IsSuite(res.Name) {
			suites = append(suites, res.Name)
		}
	}
	if len(suites) == 0 {
		return Run{}, ErrNoSuites
	}

	run, err := s.repo.Create(ctx, Run{
		DeploymentID:   plan.DeploymentID,
		SnapshotID:     plan.SnapshotID,
		RuntimeVersion: s.deployments.CurrentRuntime(),
		RolloutOnPass:  rolloutOnPass,
		CreatedBy:      createdBy,
	})
	if err != nil {
		return Run{}, err
	}
	if err := s.cluster.ApplyTestRun(ctx, kube.TestRunSpec{
		ID:            run.ID,
		DeploymentID:  plan.DeploymentID,
		IntegrationID: plan.IntegrationID,
		Definition:    plan.Definition,
		Files:         files,
		Suites:        suites,
		Env:           plan.Env,
		SecretEnv:     plan.SecretEnv,
	}); err != nil {
		if _, ferr := s.repo.Finish(ctx, run.ID, Result{Status: StatusErrored, Error: err.Error()}); ferr != nil {
			slog.ErrorContext(ctx, "record a test run that did not start", "run", run.ID, "error", ferr)
		}
		return Run{}, fmt.Errorf("start the test run: %w", err)
	}

	go s.watch(context.WithoutCancel(ctx), run)
	return run, nil
}

// Get returns one of a deployment's test runs.
func (s *Service) Get(ctx context.Context, deploymentID, id string) (Run, error) {
	return s.repo.Get(ctx, deploymentID, id)
}

// List returns a deployment's most recent test runs, newest first.
func (s *Service) List(ctx context.Context, deploymentID string) ([]Run, error) {
	return s.repo.ListByDeployment(ctx, deploymentID, listLimit)
}

// Latest returns each deployment's most recent test run, keyed by deployment id.
func (s *Service) Latest(ctx context.Context) (map[string]Run, error) {
	runs, err := s.repo.Latest(ctx)
	if err != nil {
		return nil, err
	}
	ret := make(map[string]Run, len(runs))
	for _, r := range runs {
		ret[r.DeploymentID] = r
	}
	return ret, nil
}

// Sweep finishes the runs nobody is watching: those still running well past
// their deadline, whose watcher went with the replica that started it. Such a
// run is finished from what its Job says, but a rollout it asked for is not
// performed, since the credential to perform it went with the watcher.
func (s *Service) Sweep(ctx context.Context) (int, error) {
	runs, err := s.repo.ListRunningSince(ctx, s.now().Add(-(kube.TestRunDeadline + 2*grace)))
	if err != nil {
		return 0, err
	}
	ret := 0
	for _, run := range runs {
		state, err := s.cluster.TestRunState(ctx, run.ID)
		if err != nil {
			slog.ErrorContext(ctx, "sweep a test run", "run", run.ID, "error", err)
			continue
		}
		if s.complete(ctx, run, state, false) {
			ret++
		}
	}
	return ret, nil
}

// watch follows run until its Job ends, then finishes it.
func (s *Service) watch(ctx context.Context, run Run) {
	give := run.CreatedAt.Add(kube.TestRunDeadline + grace)
	ticker := time.NewTicker(s.poll)
	defer ticker.Stop()
	for range ticker.C {
		step, cancel := context.WithTimeout(ctx, stepTimeout)
		state, err := s.cluster.TestRunState(step, run.ID)
		cancel()
		if err != nil {
			slog.Error("watch a test run", "run", run.ID, "error", err)
			continue
		}
		if state.Phase == kube.TestRunActive && s.now().Before(give) {
			continue
		}
		s.complete(ctx, run, state, true)
		return
	}
}

// complete records the verdict state implies for run and, when this call was the
// one to record it and the run passed, performs the rollout it asked for.
// Reports whether this call recorded the verdict.
func (s *Service) complete(ctx context.Context, run Run, state kube.TestRunState, canRollout bool) bool {
	step, cancel := context.WithTimeout(ctx, stepTimeout)
	res := s.verdict(step, run, state)
	won, err := s.repo.Finish(step, run.ID, res)
	cancel()
	if err != nil {
		slog.Error("finish a test run", "run", run.ID, "error", err)
		return false
	}
	if !won {
		return false
	}
	slog.Info("test run finished", "run", run.ID, "deployment", run.DeploymentID, "status", res.Status)
	if res.Status == StatusPassed && run.RolloutOnPass {
		s.rollout(ctx, run, canRollout)
	}
	return true
}

// verdict turns what the cluster says about a run into the result it records.
//
// dolphin's exit code is the verdict when it exited: 0 every case passed, 1 a
// case failed, anything else the suites could not be run. A run that never got
// that far errored, and one Kubernetes killed timed out.
func (s *Service) verdict(ctx context.Context, run Run, state kube.TestRunState) Result {
	switch state.Phase {
	case kube.TestRunExited:
		logs, err := s.cluster.TestRunLogs(ctx, state.Pod)
		if err != nil {
			return Result{Status: StatusErrored, Error: fmt.Sprintf("dolphin exited %d and its output could not be read: %v", state.ExitCode, err)}
		}
		report, rerr := extractReport(logs)
		if rerr != nil {
			return Result{Status: StatusErrored, Error: fmt.Sprintf("dolphin exited %d without a report: %s", state.ExitCode, logTail(logs))}
		}
		switch state.ExitCode {
		case 0:
			return Result{Status: StatusPassed, Report: report}
		case 1:
			return Result{Status: StatusFailed, Report: report}
		default:
			return Result{Status: StatusErrored, Report: report, Error: "a suite or the integration's config could not be run"}
		}
	case kube.TestRunDeadlineExceeded:
		return Result{Status: StatusTimedOut, Error: fmt.Sprintf("the run took longer than %s", kube.TestRunDeadline)}
	case kube.TestRunFailedToStart:
		return Result{Status: StatusErrored, Error: "the test pod did not start: " + state.Reason}
	case kube.TestRunGone:
		return Result{Status: StatusErrored, Error: "the test run's job was gone before its result was read"}
	default:
		if err := s.cluster.DeleteTestRun(ctx, run.ID); err != nil {
			slog.Error("delete an abandoned test run", "run", run.ID, "error", err)
		}
		return Result{Status: StatusTimedOut, Error: "the run never finished, and was abandoned"}
	}
}

// rollout rolls a passing run's deployment onto the snapshot it tested, keeping
// the deployment's env and tracing, and records how that went.
func (s *Service) rollout(ctx context.Context, run Run, canRollout bool) {
	var at *time.Time
	msg := ""
	if !canRollout {
		msg = "the suites passed, but the rollout was not performed: the orchestrator replica " +
			"that started this run stopped before it finished. Roll the deployment out by hand."
	} else {
		rctx, cancel := context.WithTimeout(ctx, rolloutTimeout)
		_, err := s.deployments.Rollout(rctx, run.DeploymentID, run.SnapshotID, nil, nil, nil)
		cancel()
		if err != nil {
			msg = "the suites passed, but the rollout failed: " + err.Error()
		} else {
			t := s.now()
			at = &t
		}
	}
	step, cancel := context.WithTimeout(ctx, stepTimeout)
	defer cancel()
	if err := s.repo.RecordRollout(step, run.ID, at, msg); err != nil {
		slog.Error("record a test run's rollout", "run", run.ID, "error", errors.Join(err, errors.New(msg)))
	}
}
