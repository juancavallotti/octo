package testrun

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/juancavallotti/octo/orchestrator/internal/deployment"
	"github.com/juancavallotti/octo/orchestrator/internal/kube"
	"github.com/juancavallotti/octo/orchestrator/internal/snapshot"
)

// fakeRepo keeps runs in memory, with Finish as conditional as the real one.
type fakeRepo struct {
	mu   sync.Mutex
	runs map[string]Run
	seq  int
}

func newFakeRepo() *fakeRepo { return &fakeRepo{runs: map[string]Run{}} }

func (f *fakeRepo) Create(_ context.Context, run Run) (Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	run.ID = "run-" + string(rune('0'+f.seq))
	run.Status = StatusRunning
	run.CreatedAt = time.Now()
	f.runs[run.ID] = run
	return run, nil
}

func (f *fakeRepo) Get(_ context.Context, deploymentID, id string) (Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.runs[id]
	if !ok || r.DeploymentID != deploymentID {
		return Run{}, ErrNotFound
	}
	return r, nil
}

func (f *fakeRepo) ListByDeployment(context.Context, string, int) ([]Run, error) { return nil, nil }
func (f *fakeRepo) Latest(context.Context) ([]Run, error)                        { return nil, nil }

func (f *fakeRepo) ListRunningSince(_ context.Context, t time.Time) ([]Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var ret []Run
	for _, r := range f.runs {
		if r.Status == StatusRunning && r.CreatedAt.Before(t) {
			ret = append(ret, r)
		}
	}
	return ret, nil
}

func (f *fakeRepo) Finish(_ context.Context, id string, res Result) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.runs[id]
	if r.Status != StatusRunning {
		return false, nil
	}
	r.Status, r.Report, r.Error = res.Status, res.Report, res.Error
	f.runs[id] = r
	return true, nil
}

func (f *fakeRepo) RecordRollout(_ context.Context, id string, at *time.Time, msg string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.runs[id]
	r.RolledOutAt, r.Error = at, msg
	f.runs[id] = r
	return nil
}

func (f *fakeRepo) get(id string) Run {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.runs[id]
}

// fakeDeployments hands out one plan and counts rollouts.
type fakeDeployments struct {
	mu        sync.Mutex
	plan      deployment.TestPlan
	rollouts  int
	rolloutTo string
}

func (f *fakeDeployments) TestPlan(context.Context, string, string) (deployment.TestPlan, error) {
	return f.plan, nil
}

func (f *fakeDeployments) Rollout(
	_ context.Context, _, snapshotID string, _ map[string]deployment.EnvBinding, _ *bool, _ *string,
) (deployment.Deployment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rollouts++
	f.rolloutTo = snapshotID
	return deployment.Deployment{}, nil
}

func (f *fakeDeployments) CurrentRuntime() string { return "0.11.14" }

func (f *fakeDeployments) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rollouts
}

// fakeCluster reports one state for every run and remembers what it was asked to apply.
type fakeCluster struct {
	mu      sync.Mutex
	state   kube.TestRunState
	logs    string
	applied []kube.TestRunSpec
}

func (f *fakeCluster) TestRunsEnabled() bool { return true }

func (f *fakeCluster) ApplyTestRun(_ context.Context, spec kube.TestRunSpec) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.applied = append(f.applied, spec)
	return nil
}

func (f *fakeCluster) TestRunState(context.Context, string) (kube.TestRunState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state, nil
}

func (f *fakeCluster) TestRunLogs(context.Context, string) ([]byte, error) { return []byte(f.logs), nil }
func (f *fakeCluster) DeleteTestRun(context.Context, string) error         { return nil }

func plan(resources ...snapshot.Resource) deployment.TestPlan {
	return deployment.TestPlan{
		DeploymentID: "dep-1", IntegrationID: "int-1", SnapshotID: "snap-1",
		Definition: "flows: []", Resources: resources,
	}
}

func suite(name string) snapshot.Resource {
	return snapshot.Resource{Name: ".octo/tests/" + name, Kind: "template", Content: "flow: orders"}
}

func TestStartRefusesAnIntegrationWithoutSuites(t *testing.T) {
	deps := &fakeDeployments{plan: plan(snapshot.Resource{Name: "templates/a.tmpl"})}
	svc := NewService(newFakeRepo(), deps, &fakeCluster{})
	if _, err := svc.Start(context.Background(), "dep-1", "", false, "u"); !errors.Is(err, ErrNoSuites) {
		t.Fatalf("err = %v, want ErrNoSuites", err)
	}
}

func TestStartShipsTheSnapshotAndRunsItsSuites(t *testing.T) {
	repo := newFakeRepo()
	cl := &fakeCluster{state: kube.TestRunState{Phase: kube.TestRunExited, ExitCode: 0, Pod: "p"}, logs: report}
	deps := &fakeDeployments{plan: plan(suite("orders_test.yaml"), snapshot.Resource{Name: "templates/a.tmpl", Content: "hi"})}
	svc := NewService(repo, deps, cl, WithClock(time.Now, time.Millisecond))

	run, err := svc.Start(context.Background(), "dep-1", "", true, "u")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if run.RuntimeVersion != "0.11.14" || run.CreatedBy != "u" {
		t.Errorf("run = %+v", run)
	}
	spec := cl.applied[0]
	if len(spec.Suites) != 1 || spec.Suites[0] != ".octo/tests/orders_test.yaml" {
		t.Errorf("suites = %v", spec.Suites)
	}
	if spec.Files["templates/a.tmpl"] != "hi" || spec.Definition != "flows: []" {
		t.Errorf("files = %v, definition %q", spec.Files, spec.Definition)
	}

	waitFor(t, func() bool { return repo.get(run.ID).RolledOutAt != nil })
	if deps.rolloutTo != "snap-1" {
		t.Errorf("rolled out to %q, want snap-1", deps.rolloutTo)
	}
}

func TestVerdict(t *testing.T) {
	cases := []struct {
		name  string
		state kube.TestRunState
		logs  string
		want  Status
	}{
		{"passed", kube.TestRunState{Phase: kube.TestRunExited, ExitCode: 0}, report, StatusPassed},
		{"failed", kube.TestRunState{Phase: kube.TestRunExited, ExitCode: 1}, report, StatusFailed},
		{"suite errored", kube.TestRunState{Phase: kube.TestRunExited, ExitCode: 2}, report, StatusErrored},
		{"exited without a report", kube.TestRunState{Phase: kube.TestRunExited, ExitCode: 0}, "boom", StatusErrored},
		{"deadline", kube.TestRunState{Phase: kube.TestRunDeadlineExceeded}, "", StatusTimedOut},
		{"never started", kube.TestRunState{Phase: kube.TestRunFailedToStart, Reason: "ErrImagePull"}, "", StatusErrored},
		{"job gone", kube.TestRunState{Phase: kube.TestRunGone}, "", StatusErrored},
		{"abandoned", kube.TestRunState{Phase: kube.TestRunActive}, "", StatusTimedOut},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewService(newFakeRepo(), &fakeDeployments{}, &fakeCluster{logs: tc.logs})
			got := svc.verdict(context.Background(), Run{ID: "r"}, tc.state)
			if got.Status != tc.want {
				t.Errorf("status = %s, want %s (error %q)", got.Status, tc.want, got.Error)
			}
			if tc.want == StatusPassed && !json.Valid(got.Report) {
				t.Errorf("report = %s", got.Report)
			}
		})
	}
}

// Two replicas finishing the same passing run: one verdict, one rollout.
func TestCompleteRollsOutOnce(t *testing.T) {
	repo := newFakeRepo()
	deps := &fakeDeployments{}
	cl := &fakeCluster{logs: report}
	svc := NewService(repo, deps, cl)
	run, _ := repo.Create(context.Background(), Run{DeploymentID: "dep-1", SnapshotID: "snap-1", RolloutOnPass: true})
	state := kube.TestRunState{Phase: kube.TestRunExited, ExitCode: 0, Pod: "p"}

	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			svc.complete(context.Background(), run, state, true)
		}()
	}
	wg.Wait()
	if n := deps.count(); n != 1 {
		t.Errorf("rollouts = %d, want 1", n)
	}
}

func TestSweepFinishesWithoutRollingOut(t *testing.T) {
	repo := newFakeRepo()
	deps := &fakeDeployments{}
	cl := &fakeCluster{state: kube.TestRunState{Phase: kube.TestRunExited, ExitCode: 0, Pod: "p"}, logs: report}
	later := func() time.Time { return time.Now().Add(time.Hour) }
	svc := NewService(repo, deps, cl, WithClock(later, time.Millisecond))
	run, _ := repo.Create(context.Background(), Run{DeploymentID: "dep-1", SnapshotID: "snap-1", RolloutOnPass: true})

	n, err := svc.Sweep(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("Sweep = %d, %v", n, err)
	}
	got := repo.get(run.ID)
	if got.Status != StatusPassed || deps.count() != 0 || !strings.Contains(got.Error, "by hand") {
		t.Errorf("run = %+v, rollouts %d", got, deps.count())
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
