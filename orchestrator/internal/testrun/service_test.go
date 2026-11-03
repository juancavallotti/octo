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
	mu     sync.Mutex
	runs   map[string]Run
	seq    int
	latest []Run
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
func (f *fakeRepo) Latest(context.Context) ([]Run, error)                        { return f.latest, nil }

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
	mu   sync.Mutex
	plan deployment.TestPlan
	all  []deployment.Deployment
	// current is what Get returns, when a test needs the deployment changed.
	current   *deployment.Deployment
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

func (f *fakeDeployments) Get(_ context.Context, id string) (deployment.Deployment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.current != nil {
		return *f.current, nil
	}
	return deployment.Deployment{ID: id}, nil
}

func (f *fakeDeployments) List(context.Context) ([]deployment.Deployment, error) { return f.all, nil }

func (f *fakeDeployments) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rollouts
}

// fakeCluster reports one state for every run and remembers what it was asked to apply.
type fakeCluster struct {
	mu      sync.Mutex
	image   string
	state   kube.TestRunState
	logs    string
	applied []kube.TestRunSpec
}

func (f *fakeCluster) TestRunsEnabled() bool { return true }

func (f *fakeCluster) TestImage() string {
	if f.image != "" {
		return f.image
	}
	return "octo-devruntime:0.11.14"
}

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

func (f *fakeCluster) TestRunLogs(context.Context, string) ([]byte, error) {
	return []byte(f.logs), nil
}
func (f *fakeCluster) DeleteTestRun(context.Context, string) error { return nil }

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
	svc := NewService(newFakeRepo(), deps, nil, &fakeCluster{})
	if _, err := svc.Start(context.Background(), "dep-1", "", false, "u"); !errors.Is(err, ErrNoSuites) {
		t.Fatalf("err = %v, want ErrNoSuites", err)
	}
}

func TestStartShipsTheSnapshotAndRunsItsSuites(t *testing.T) {
	repo := newFakeRepo()
	cl := &fakeCluster{state: kube.TestRunState{Phase: kube.TestRunExited, ExitCode: 0, Pod: "p"}, logs: report}
	deps := &fakeDeployments{plan: plan(suite("orders_test.yaml"), snapshot.Resource{Name: "templates/a.tmpl", Content: "hi"})}
	svc := NewService(repo, deps, nil, cl, WithClock(time.Now, time.Millisecond))

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
			svc := NewService(newFakeRepo(), &fakeDeployments{}, nil, &fakeCluster{logs: tc.logs})
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
	svc := NewService(repo, deps, nil, cl)
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
	svc := NewService(repo, deps, nil, cl, WithClock(later, time.Millisecond))
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

// fakeSnapshots serves each snapshot's frozen files.
type fakeSnapshots map[string][]snapshot.Resource

func (f fakeSnapshots) ListResources(_ context.Context, id string) ([]snapshot.Resource, error) {
	return f[id], nil
}

func TestUpgradesMarksARunOlderThanTheLastRolloutStale(t *testing.T) {
	rolledOut := time.Now()
	dep := func(id string) deployment.Deployment {
		meta, _ := json.Marshal(deployment.Metadata{Name: id, RolledOutAt: &rolledOut})
		settings, _ := json.Marshal(deployment.Settings{SnapshotID: "s1"})
		return deployment.Deployment{ID: id, Metadata: meta, Settings: settings}
	}
	repo := newFakeRepo()
	repo.latest = []Run{
		{ID: "old", DeploymentID: "before", SnapshotID: "s1", CreatedAt: rolledOut.Add(-time.Minute)},
		{ID: "new", DeploymentID: "after", SnapshotID: "s1", CreatedAt: rolledOut.Add(time.Minute)},
	}
	deps := &fakeDeployments{all: []deployment.Deployment{dep("before"), dep("after")}}
	svc := NewService(repo, deps, fakeSnapshots{}, &fakeCluster{})

	_, got, err := svc.Upgrades(context.Background())
	if err != nil {
		t.Fatalf("Upgrades: %v", err)
	}
	for _, u := range got {
		if want := u.Name == "before"; u.LatestRunStale != want {
			t.Errorf("%s: stale = %v, want %v", u.Name, u.LatestRunStale, want)
		}
	}
}

func TestUpgrades(t *testing.T) {
	dep := func(id, snap, image string) deployment.Deployment {
		meta, _ := json.Marshal(deployment.Metadata{Name: id, RuntimeImage: image})
		settings, _ := json.Marshal(deployment.Settings{SnapshotID: snap})
		return deployment.Deployment{ID: id, Metadata: meta, Settings: settings}
	}
	deps := &fakeDeployments{all: []deployment.Deployment{
		dep("behind-tested", "s1", "octo-runtime:0.11.13"),
		dep("behind-untested", "s2", "octo-runtime:0.11.13"),
		dep("current", "s1", "octo-runtime:0.11.14"),
	}}
	snaps := fakeSnapshots{
		"s1": {suite("orders_test.yaml")},
		"s2": {{Name: "templates/a.tmpl"}},
	}
	svc := NewService(newFakeRepo(), deps, snaps, &fakeCluster{})

	current, got, err := svc.Upgrades(context.Background())
	if err != nil {
		t.Fatalf("Upgrades: %v", err)
	}
	if current != "0.11.14" || len(got) != 3 {
		t.Fatalf("current %q, %d upgrades", current, len(got))
	}
	want := map[string]struct {
		upgradeTo string
		suites    bool
	}{
		"behind-tested":   {"0.11.14", true},
		"behind-untested": {"0.11.14", false},
		"current":         {"", true},
	}
	for _, u := range got {
		w := want[u.Name]
		if u.UpgradeTo != w.upgradeTo || u.HasSuites != w.suites {
			t.Errorf("%s: upgradeTo %q hasSuites %v, want %q %v", u.Name, u.UpgradeTo, u.HasSuites, w.upgradeTo, w.suites)
		}
	}
}

func TestStartRefusesATestImageOfAnotherRelease(t *testing.T) {
	cl := &fakeCluster{image: "octo-devruntime:0.11.13"}
	svc := NewService(newFakeRepo(), &fakeDeployments{plan: plan(suite("orders_test.yaml"))}, nil, cl)
	if _, err := svc.Start(context.Background(), "dep-1", "", true, "u"); !errors.Is(err, ErrRuntimeMismatch) {
		t.Fatalf("err = %v, want ErrRuntimeMismatch", err)
	}
}

// A deployment rolled out while its suites ran is not rolled over by the run.
func TestCompleteLeavesADeploymentChangedDuringTheRun(t *testing.T) {
	repo := newFakeRepo()
	run, _ := repo.Create(context.Background(), Run{DeploymentID: "dep-1", SnapshotID: "snap-1", RolloutOnPass: true})
	later := run.CreatedAt.Add(time.Second)
	meta, _ := json.Marshal(deployment.Metadata{RolledOutAt: &later})
	deps := &fakeDeployments{current: &deployment.Deployment{ID: "dep-1", Metadata: meta}}
	svc := NewService(repo, deps, nil, &fakeCluster{logs: report})

	svc.complete(context.Background(), run, kube.TestRunState{Phase: kube.TestRunExited, Pod: "p"}, true)
	got := repo.get(run.ID)
	if deps.count() != 0 || got.Status != StatusPassed || !strings.Contains(got.Error, "Test it again") {
		t.Errorf("run = %+v, rollouts %d", got, deps.count())
	}
}

func TestIsStale(t *testing.T) {
	t0 := time.Now()
	at := func(d time.Duration) *time.Time { v := t0.Add(d); return &v }
	cases := []struct {
		name      string
		run       Run
		snapshot  string
		rolledOut *time.Time
		want      bool
	}{
		{"current", Run{SnapshotID: "s1", CreatedAt: t0}, "s1", at(-time.Minute), false},
		{"rolled out since", Run{SnapshotID: "s1", CreatedAt: t0}, "s1", at(time.Minute), true},
		{"another snapshot", Run{SnapshotID: "s2", CreatedAt: t0}, "s1", nil, true},
		{"its own rollout", Run{SnapshotID: "s1", CreatedAt: t0, RolledOutAt: at(time.Minute)}, "s1", at(time.Minute), false},
		{"a rollout after its own", Run{SnapshotID: "s1", CreatedAt: t0, RolledOutAt: at(time.Minute)}, "s1", at(2 * time.Minute), true},
	}
	for _, tc := range cases {
		if got := isStale(tc.run, tc.snapshot, tc.rolledOut); got != tc.want {
			t.Errorf("%s: stale = %v, want %v", tc.name, got, tc.want)
		}
	}
}
