package devrun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/juancavallotti/octo/orchestrator/internal/integration"
	"github.com/juancavallotti/octo/orchestrator/internal/kube"
	"github.com/juancavallotti/octo/orchestrator/internal/resource"
)

// The definition of a networked integration: it declares HTTP_PORT with a usable
// default, which is what makes it exposable.
const networkedDefinition = "service:\n  name: orders\nenv:\n  - name: HTTP_PORT\n    default: \"9999\"\n"

// The definition of a cron-driven integration: no HTTP_PORT, so no public host.
const internalDefinition = "service:\n  name: nightly\n"

// fakeCluster stands in for *kube.Client. It keeps the dev runs it was told to create
// and honours the label selector, because "a run you do not own reads as not found"
// is a property of the selector and a fake that ignored it would not test it.
type fakeCluster struct {
	runs       map[string]kube.DevRun
	baseDomain string
	// calls records every mutating call in order, so an ordering requirement — publish
	// the endpoint before telling the sidecar to reload — is checkable rather than
	// merely intended.
	calls   []string
	listErr error
	// hideFromListings makes ListDevRuns report nothing, standing in for an informer
	// cache that has not caught up with a write.
	hideFromListings bool
	applyErr         error
	// lastSpec is the spec of the most recent Apply, so the derived identity a caller
	// computed can be inspected.
	lastSpec kube.DevRunSpec
	// logs is what a pod-log stream yields; logsErr fails the open instead.
	logs    string
	logsErr error
}

func newFakeCluster() *fakeCluster {
	return &fakeCluster{runs: map[string]kube.DevRun{}, baseDomain: "apps.example.com"}
}

func (f *fakeCluster) record(format string, args ...any) {
	f.calls = append(f.calls, fmt.Sprintf(format, args...))
}

func (f *fakeCluster) DevRunsEnabled() bool { return true }

func (f *fakeCluster) ApplyDevRun(_ context.Context, spec kube.DevRunSpec) error {
	f.record("apply %s", spec.ID)
	f.lastSpec = spec
	if f.applyErr != nil {
		return f.applyErr
	}
	if _, exists := f.runs[spec.ID]; exists {
		return kube.ErrDevRunExists
	}
	run := kube.DevRun{
		ID:            spec.ID,
		UserID:        spec.UserID,
		IntegrationID: spec.IntegrationID,
		TokenHash:     spec.TokenHash,
		LastActivity:  spec.LastActivity,
		CreatedAt:     spec.LastActivity,
	}
	if spec.Networked {
		run.Host = f.ExternalHost(spec.Subdomain)
	}
	f.runs[spec.ID] = run
	return nil
}

func (f *fakeCluster) ReconcileDevRunService(_ context.Context, spec kube.DevRunSpec) error {
	f.record("reconcile-service %s", spec.ID)
	return nil
}

func (f *fakeCluster) PublishDevRunEndpoint(_ context.Context, spec kube.DevRunSpec) error {
	f.record("publish %s", spec.ID)
	return nil
}

func (f *fakeCluster) WithdrawDevRunEndpoint(_ context.Context, devRunID string) error {
	f.record("withdraw %s", devRunID)
	return nil
}

func (f *fakeCluster) DeleteDevRun(_ context.Context, devRunID string) error {
	f.record("delete %s", devRunID)
	delete(f.runs, devRunID)
	return nil
}

func (f *fakeCluster) TouchDevRun(_ context.Context, devRunID string, at time.Time) error {
	f.record("touch %s", devRunID)
	run := f.runs[devRunID]
	run.LastActivity = at
	f.runs[devRunID] = run
	return nil
}

func (f *fakeCluster) SetDevRunHost(_ context.Context, devRunID, host string) error {
	f.record("set-host %s=%q", devRunID, host)
	run := f.runs[devRunID]
	run.Host = host
	f.runs[devRunID] = run
	return nil
}

func (f *fakeCluster) ListDevRuns(_ context.Context, sel kube.DevRunSelector) ([]kube.DevRun, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	if f.hideFromListings {
		return nil, nil
	}
	var out []kube.DevRun
	for _, run := range f.runs {
		if sel.UserID != "" && run.UserID != sel.UserID {
			continue
		}
		if sel.IntegrationID != "" && run.IntegrationID != sel.IntegrationID {
			continue
		}
		out = append(out, run)
	}
	return out, nil
}

// PodLogs stands in for the pod-log stream, recording exactly what was asked for. The
// container name is part of that record on purpose: a dev-run pod has two containers
// and Kubernetes rejects a log request that does not say which.
func (f *fakeCluster) PodLogs(
	_ context.Context, podName, container string, follow bool, tail int64,
) (io.ReadCloser, error) {
	f.record("pod-logs %s/%s follow=%t tail=%d", podName, container, follow, tail)
	if f.logsErr != nil {
		return nil, f.logsErr
	}
	return io.NopCloser(strings.NewReader(f.logs)), nil
}

// withPods gives a dev run live pods, which is what the cluster reports once the
// workload has actually been scheduled.
func (f *fakeCluster) withPods(devRunID string, pods ...kube.PodStatus) {
	run := f.runs[devRunID]
	run.Status = kube.Status{Phase: kube.StatusRunning, DesiredReplicas: 1, Pods: pods}
	for _, p := range pods {
		if p.Ready {
			run.Status.ReadyReplicas = 1
		}
	}
	f.runs[devRunID] = run
}

func (f *fakeCluster) SidecarURL(devRunID string) string {
	return "http://octo-dev-" + devRunID + ".octo:8099"
}

func (f *fakeCluster) ExternalHost(subdomain string) string {
	if f.baseDomain == "" || subdomain == "" {
		return ""
	}
	return subdomain + "." + f.baseDomain
}

func (f *fakeCluster) ExternalURL(subdomain string) string {
	if host := f.ExternalHost(subdomain); host != "" {
		return "https://" + host
	}
	return ""
}

// fakeIntegrations serves definitions by id.
type fakeIntegrations struct {
	byID map[string]integration.Integration
}

func (f fakeIntegrations) Get(_ context.Context, id string) (integration.Integration, error) {
	integ, ok := f.byID[id]
	if !ok {
		return integration.Integration{}, errors.New("no such integration")
	}
	return integ, nil
}

// fakeResources serves an integration's live resources.
type fakeResources struct {
	byIntegration map[string][]resource.Resource
	err           error
}

func (f fakeResources) ListByIntegration(_ context.Context, id string) ([]resource.Resource, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.byIntegration[id], nil
}

// fakeSidecar records the commands it was given.
type fakeSidecar struct {
	cluster *fakeCluster
	err     error
	status  json.RawMessage
	reloads []string
}

func (f *fakeSidecar) Reload(_ context.Context, baseURL, token string) error {
	f.reloads = append(f.reloads, baseURL+" "+token)
	if f.cluster != nil {
		f.cluster.record("sidecar-reload %s", baseURL)
	}
	return f.err
}

func (f *fakeSidecar) Status(_ context.Context, _, _ string) (json.RawMessage, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.status, nil
}

// testService wires a Service over fakes, with the clock pinned so the touch throttle
// and the reaper are deterministic.
func testService(t *testing.T, cluster *fakeCluster, defs map[string]string) (*Service, *fakeSidecar) {
	t.Helper()
	integs := fakeIntegrations{byID: map[string]integration.Integration{}}
	for id, definition := range defs {
		integs.byID[id] = integration.Integration{ID: id, Name: id, Definition: definition}
	}
	sidecar := &fakeSidecar{cluster: cluster}
	svc := NewService(cluster, integs, fakeResources{}, secret,
		WithClock(func() time.Time { return testNow }),
		withSidecars(sidecar))
	return svc, sidecar
}

var testNow = time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)

func TestEnsureCreates(t *testing.T) {
	cluster := newFakeCluster()
	svc, _ := testService(t, cluster, map[string]string{"int-1": networkedDefinition})

	got, err := svc.Ensure(context.Background(), "u1", "int-1")
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if !got.Created {
		t.Error("Created = false, want true for a first Run")
	}
	if want := deriveID(secret, "u1", "int-1"); got.ID != want {
		t.Errorf("id = %q, want the derived %q", got.ID, want)
	}
	wantHost := deriveLabel(secret, "u1", "int-1") + ".apps.example.com"
	if got.Host != wantHost {
		t.Errorf("host = %q, want %q", got.Host, wantHost)
	}
	if got.TestURL != "https://"+wantHost {
		t.Errorf("testURL = %q, want https://%s", got.TestURL, wantHost)
	}
	if !cluster.lastSpec.Networked {
		t.Error("spec.Networked = false for a definition declaring HTTP_PORT")
	}
	if cluster.lastSpec.TokenHash != hashToken(cluster.lastSpec.DevRunToken) {
		t.Error("spec.TokenHash is not the hash of spec.DevRunToken")
	}
	if want := deriveSidecarToken(secret, "u1", "int-1"); cluster.lastSpec.SidecarToken != want {
		t.Error("spec.SidecarToken is not the derived token; a second replica could not reload this run")
	}
}

// TestEnsureSurvivesAStaleCache is the case a real cluster hits every time: the dev-run
// listing comes from an informer cache that is milliseconds behind the create, so the
// workload this call just made is not in it yet. Reading it back as authoritative would
// turn every successful Run into "not found".
func TestEnsureSurvivesAStaleCache(t *testing.T) {
	cluster := newFakeCluster()
	svc, _ := testService(t, cluster, map[string]string{"int-1": networkedDefinition})
	// A cache that never sees the write at all is the strongest version of behind.
	cluster.hideFromListings = true

	got, err := svc.Ensure(context.Background(), "u1", "int-1")
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if !got.Created {
		t.Error("Created = false, want true")
	}
	if want := deriveID(secret, "u1", "int-1"); got.ID != want {
		t.Errorf("id = %q, want %q", got.ID, want)
	}
	wantHost := deriveLabel(secret, "u1", "int-1") + ".apps.example.com"
	if got.Host != wantHost || got.TestURL != "https://"+wantHost {
		t.Errorf("host = %q, testURL = %q; want %q and its https URL", got.Host, got.TestURL, wantHost)
	}
}

// TestEnsureIsIdempotent is the two-tabs case: the second Run attaches rather than
// failing, because the workload name is derived and the API server arbitrates.
func TestEnsureIsIdempotent(t *testing.T) {
	cluster := newFakeCluster()
	svc, _ := testService(t, cluster, map[string]string{"int-1": networkedDefinition})

	first, err := svc.Ensure(context.Background(), "u1", "int-1")
	if err != nil {
		t.Fatalf("first Ensure: %v", err)
	}
	second, err := svc.Ensure(context.Background(), "u1", "int-1")
	if err != nil {
		t.Fatalf("second Ensure: %v", err)
	}
	if second.Created {
		t.Error("Created = true on the second Run, want false (attached)")
	}
	if first.ID != second.ID {
		t.Errorf("ids differ: %q then %q; two tabs must share one pod", first.ID, second.ID)
	}
	if first.TestURL != second.TestURL {
		t.Errorf("test URLs differ: %q then %q", first.TestURL, second.TestURL)
	}
}

// TestEnsureNonNetworkedPublishesNothing: a cron integration gets a pod and a Service
// (the sidecar has to be reachable) but no public host.
func TestEnsureNonNetworkedPublishesNothing(t *testing.T) {
	cluster := newFakeCluster()
	svc, _ := testService(t, cluster, map[string]string{"int-1": internalDefinition})

	got, err := svc.Ensure(context.Background(), "u1", "int-1")
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if got.Host != "" || got.TestURL != "" {
		t.Errorf("host = %q, testURL = %q; want neither for a definition with no HTTP_PORT", got.Host, got.TestURL)
	}
	if cluster.lastSpec.Networked {
		t.Error("spec.Networked = true for a definition with no HTTP_PORT")
	}
}

// TestEnsureRefusesATakenHost: a collision is refused with the conflict named rather
// than resolved, because resolving it would need storage to stay stable.
func TestEnsureRefusesATakenHost(t *testing.T) {
	cluster := newFakeCluster()
	// A different user's live run already publishing the host u1/int-1 would derive.
	cluster.runs["other"] = kube.DevRun{
		ID:            "other",
		UserID:        "u2",
		IntegrationID: "int-9",
		Host:          deriveLabel(secret, "u1", "int-1") + ".apps.example.com",
	}
	svc, _ := testService(t, cluster, map[string]string{"int-1": networkedDefinition})

	_, err := svc.Ensure(context.Background(), "u1", "int-1")
	if !errors.Is(err, ErrHostTaken) {
		t.Fatalf("Ensure error = %v, want ErrHostTaken", err)
	}
	if got := err.Error(); !strings.Contains(got, "other") {
		t.Errorf("error %q does not name the conflicting run", got)
	}
}

func TestEnsureRequiresASavedIntegration(t *testing.T) {
	cluster := newFakeCluster()
	svc, _ := testService(t, cluster, map[string]string{"int-1": networkedDefinition})

	for name, args := range map[string][2]string{
		"no integration id": {"u1", ""},
		"unknown id":        {"u1", "nope"},
	} {
		if _, err := svc.Ensure(context.Background(), args[0], args[1]); !errors.Is(err, ErrIntegrationNotFound) {
			t.Errorf("%s: error = %v, want ErrIntegrationNotFound", name, err)
		}
	}
}

// TestEveryUserOperationNeedsAUser is the guard on the one mistake the label selector
// makes easy: an empty user id means "do not filter by user", so an operation that let
// one through would address every user's dev runs — and two of these operations delete
// or rewrite what a pod is running.
func TestEveryUserOperationNeedsAUser(t *testing.T) {
	cluster := newFakeCluster()
	svc, _ := testService(t, cluster, map[string]string{"int-1": networkedDefinition})
	run, err := svc.Ensure(context.Background(), "u1", "int-1")
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	ctx := context.Background()

	for name, call := range map[string]func() error{
		"Ensure": func() error { _, err := svc.Ensure(ctx, "", "int-1"); return err },
		"Status": func() error { _, err := svc.Status(ctx, "", run.ID); return err },
		"Stop":   func() error { return svc.Stop(ctx, "", run.ID) },
		"Reload": func() error { return svc.Reload(ctx, "", run.ID) },
		"List":   func() error { _, err := svc.ListByUser(ctx, "", ""); return err },
		"PodLogs": func() error {
			_, err := svc.PodLogs(ctx, "", run.ID, false, 0)
			return err
		},
	} {
		if err := call(); !errors.Is(err, ErrUserRequired) {
			t.Errorf("%s with no user id: error = %v, want ErrUserRequired", name, err)
		}
	}
	if _, still := cluster.runs[run.ID]; !still {
		t.Error("an unscoped call reached the cluster: the dev run was deleted")
	}
}

// TestPodLogsReadsTheRuntimeContainer: the container has to be named, because a dev-run
// pod has two and Kubernetes rejects an ambiguous request. The ready pod is preferred
// so a restart in progress does not serve the logs of the pod that is going away.
func TestPodLogsReadsTheRuntimeContainer(t *testing.T) {
	cluster := newFakeCluster()
	cluster.logs = "listening on :8080\n"
	svc, _ := testService(t, cluster, map[string]string{"int-1": networkedDefinition})
	run, err := svc.Ensure(context.Background(), "u1", "int-1")
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	cluster.withPods(run.ID,
		kube.PodStatus{Name: "octo-dev-old", Phase: "Running"},
		kube.PodStatus{Name: "octo-dev-new", Phase: "Running", Ready: true})

	stream, err := svc.PodLogs(context.Background(), "u1", run.ID, true, 100)
	if err != nil {
		t.Fatalf("PodLogs: %v", err)
	}
	defer func() { _ = stream.Close() }()
	body, err := io.ReadAll(stream)
	if err != nil {
		t.Fatalf("read stream: %v", err)
	}
	if string(body) != cluster.logs {
		t.Errorf("stream = %q, want %q", body, cluster.logs)
	}
	want := "pod-logs octo-dev-new/" + kube.RuntimeContainer + " follow=true tail=100"
	if !containsCall(cluster.calls, want) {
		t.Errorf("calls = %v, want one matching %q", cluster.calls, want)
	}
}

// TestPodLogsBeforeThereIsAPod: the workload exists but nothing has been scheduled yet,
// which is a wait rather than a "no such run" — the caller should retry, not start it
// again.
func TestPodLogsBeforeThereIsAPod(t *testing.T) {
	cluster := newFakeCluster()
	svc, _ := testService(t, cluster, map[string]string{"int-1": networkedDefinition})
	run, err := svc.Ensure(context.Background(), "u1", "int-1")
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if _, err := svc.PodLogs(context.Background(), "u1", run.ID, false, 0); !errors.Is(err, ErrNotStarted) {
		t.Errorf("error = %v, want ErrNotStarted", err)
	}
}

// TestPodLogsIsScopedToTheOwner: another user's logs are as unreachable as their run.
func TestPodLogsIsScopedToTheOwner(t *testing.T) {
	cluster := newFakeCluster()
	svc, _ := testService(t, cluster, map[string]string{"int-1": networkedDefinition})
	run, err := svc.Ensure(context.Background(), "u1", "int-1")
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	cluster.withPods(run.ID, kube.PodStatus{Name: "octo-dev-pod", Ready: true})

	if _, err := svc.PodLogs(context.Background(), "u2", run.ID, false, 0); !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

// containsCall reports whether calls holds one exactly equal to want.
func containsCall(calls []string, want string) bool {
	for _, call := range calls {
		if call == want {
			return true
		}
	}
	return false
}

// TestStopIsScopedToTheOwner: another user's run reads as not found, so probing an id
// cannot distinguish "not yours" from "not running".
func TestStopIsScopedToTheOwner(t *testing.T) {
	cluster := newFakeCluster()
	svc, _ := testService(t, cluster, map[string]string{"int-1": networkedDefinition})
	run, err := svc.Ensure(context.Background(), "u1", "int-1")
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	if err := svc.Stop(context.Background(), "u2", run.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Stop as another user = %v, want ErrNotFound", err)
	}
	if _, exists := cluster.runs[run.ID]; !exists {
		t.Fatal("the run was deleted by a user who does not own it")
	}
	if err := svc.Stop(context.Background(), "u1", run.ID); err != nil {
		t.Fatalf("Stop as the owner: %v", err)
	}
	if _, exists := cluster.runs[run.ID]; exists {
		t.Fatal("the run survived its owner stopping it")
	}
}

// TestReloadPublishesBeforeForwarding is the ordering requirement: a public host must
// never go live pointing at a runtime nobody has told to load the config yet.
func TestReloadPublishesBeforeForwarding(t *testing.T) {
	cluster := newFakeCluster()
	// The run starts non-networked, then the definition gains an HTTP_PORT.
	svc, sidecar := testService(t, cluster, map[string]string{"int-1": internalDefinition})
	run, err := svc.Ensure(context.Background(), "u1", "int-1")
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	svc.integrations = fakeIntegrations{byID: map[string]integration.Integration{
		"int-1": {ID: "int-1", Definition: networkedDefinition},
	}}
	cluster.calls = nil

	if err := svc.Reload(context.Background(), "u1", run.ID); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	publish, forward := indexOf(cluster.calls, "publish "+run.ID), indexOf(cluster.calls, "sidecar-reload "+cluster.SidecarURL(run.ID))
	if publish < 0 {
		t.Fatalf("endpoint was not published on the flip to networked; calls: %v", cluster.calls)
	}
	if forward < 0 {
		t.Fatalf("the sidecar was not told to reload; calls: %v", cluster.calls)
	}
	if publish > forward {
		t.Errorf("published after forwarding; calls: %v", cluster.calls)
	}
	if got := cluster.runs[run.ID].Host; got == "" {
		t.Error("the host annotation was not recorded, so the next reload will flip again")
	}
	if len(sidecar.reloads) != 1 {
		t.Errorf("sidecar reloads = %d, want 1", len(sidecar.reloads))
	}
}

// TestReloadWithdrawsWhenNoLongerNetworked is the reverse flip: an HTTP_PORT removed
// from a definition should stop the public host answering, not leave it pointed at a
// runtime that is not listening.
func TestReloadWithdrawsWhenNoLongerNetworked(t *testing.T) {
	cluster := newFakeCluster()
	svc, _ := testService(t, cluster, map[string]string{"int-1": networkedDefinition})
	run, err := svc.Ensure(context.Background(), "u1", "int-1")
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	svc.integrations = fakeIntegrations{byID: map[string]integration.Integration{
		"int-1": {ID: "int-1", Definition: internalDefinition},
	}}
	cluster.calls = nil

	if err := svc.Reload(context.Background(), "u1", run.ID); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if indexOf(cluster.calls, "withdraw "+run.ID) < 0 {
		t.Fatalf("endpoint was not withdrawn; calls: %v", cluster.calls)
	}
	if got := cluster.runs[run.ID].Host; got != "" {
		t.Errorf("host annotation = %q, want it cleared", got)
	}
}

// TestReloadThrottlesTheActivityWrite: a burst of debounced saves must not become one
// API-server write per save to store a timestamp read at hour granularity.
func TestReloadThrottlesTheActivityWrite(t *testing.T) {
	cluster := newFakeCluster()
	svc, _ := testService(t, cluster, map[string]string{"int-1": networkedDefinition})
	run, err := svc.Ensure(context.Background(), "u1", "int-1")
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	// The run was just created, so its activity stamp is fresh: no write.
	cluster.calls = nil
	if err := svc.Reload(context.Background(), "u1", run.ID); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if i := indexOf(cluster.calls, "touch "+run.ID); i >= 0 {
		t.Errorf("a fresh run was touched anyway; calls: %v", cluster.calls)
	}

	// Move the clock past the throttle and the write happens.
	svc.now = func() time.Time { return testNow.Add(2 * touchInterval) }
	cluster.calls = nil
	if err := svc.Reload(context.Background(), "u1", run.ID); err != nil {
		t.Fatalf("Reload after the throttle: %v", err)
	}
	if indexOf(cluster.calls, "touch "+run.ID) < 0 {
		t.Errorf("a stale run was not touched; calls: %v", cluster.calls)
	}
}

func TestBundleAuthorizesByTokenHash(t *testing.T) {
	cluster := newFakeCluster()
	svc, _ := testService(t, cluster, map[string]string{"int-1": networkedDefinition})
	svc.resources = fakeResources{byIntegration: map[string][]resource.Resource{
		"int-1": {{Name: ".env.dev", Kind: "env", Content: "TOKEN=x"}},
	}}
	run, err := svc.Ensure(context.Background(), "u1", "int-1")
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	token := cluster.lastSpec.DevRunToken

	got, err := svc.Bundle(context.Background(), run.ID, token)
	if err != nil {
		t.Fatalf("Bundle with the right token: %v", err)
	}
	if len(got.Resources) != 1 || got.Resources[0].Content != "TOKEN=x" {
		t.Errorf("resources = %+v, want the integration's live resources", got.Resources)
	}
	if got.Generation == "" {
		t.Error("generation is empty")
	}

	for name, presented := range map[string]string{
		"wrong token": "not-the-token",
		"empty token": "",
	} {
		if _, err := svc.Bundle(context.Background(), run.ID, presented); !errors.Is(err, ErrUnauthorized) {
			t.Errorf("%s: error = %v, want ErrUnauthorized", name, err)
		}
	}
}

// TestBundleRejectsAWorkloadWithNoTokenHash: an absent hash must not read as "no check
// needed", which is how an authorisation bug ships silently.
func TestBundleRejectsAWorkloadWithNoTokenHash(t *testing.T) {
	cluster := newFakeCluster()
	cluster.runs["legacy"] = kube.DevRun{ID: "legacy", UserID: "u1", IntegrationID: "int-1"}
	svc, _ := testService(t, cluster, map[string]string{"int-1": networkedDefinition})

	if _, err := svc.Bundle(context.Background(), "legacy", "anything"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("error = %v, want ErrUnauthorized", err)
	}
}

// TestBundleTakesTheIntegrationFromTheWorkload: the id comes off the label, not the
// request, so a dev pod cannot fetch another integration's bundle.
func TestBundleTakesTheIntegrationFromTheWorkload(t *testing.T) {
	cluster := newFakeCluster()
	svc, _ := testService(t, cluster, map[string]string{
		"int-1": "service:\n  name: mine\n",
		"int-2": "service:\n  name: theirs\n",
	})
	run, err := svc.Ensure(context.Background(), "u1", "int-1")
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	got, err := svc.Bundle(context.Background(), run.ID, cluster.lastSpec.DevRunToken)
	if err != nil {
		t.Fatalf("Bundle: %v", err)
	}
	if !strings.Contains(got.Definition, "mine") || strings.Contains(got.Definition, "theirs") {
		t.Errorf("bundle definition = %q, want int-1's", got.Definition)
	}
}

func TestExpireTearsTheRunDown(t *testing.T) {
	cluster := newFakeCluster()
	svc, _ := testService(t, cluster, map[string]string{"int-1": networkedDefinition})
	run, err := svc.Ensure(context.Background(), "u1", "int-1")
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	if err := svc.Expire(context.Background(), run.ID, "wrong"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Expire with a wrong token = %v, want ErrUnauthorized", err)
	}
	if err := svc.Expire(context.Background(), run.ID, cluster.lastSpec.DevRunToken); err != nil {
		t.Fatalf("Expire: %v", err)
	}
	if _, exists := cluster.runs[run.ID]; exists {
		t.Fatal("the run survived expiry")
	}
}

func TestListByUser(t *testing.T) {
	cluster := newFakeCluster()
	svc, _ := testService(t, cluster, map[string]string{
		"int-1": networkedDefinition,
		"int-2": internalDefinition,
	})
	for _, args := range [][2]string{{"u1", "int-1"}, {"u1", "int-2"}, {"u2", "int-1"}} {
		if _, err := svc.Ensure(context.Background(), args[0], args[1]); err != nil {
			t.Fatalf("Ensure(%v): %v", args, err)
		}
	}

	mine, err := svc.ListByUser(context.Background(), "u1", "")
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	if len(mine) != 2 {
		t.Errorf("got %d runs for u1, want 2", len(mine))
	}
	one, err := svc.ListByUser(context.Background(), "u1", "int-2")
	if err != nil {
		t.Fatalf("ListByUser narrowed: %v", err)
	}
	if len(one) != 1 || one[0].IntegrationID != "int-2" {
		t.Errorf("narrowed listing = %+v, want just int-2", one)
	}
}

// TestNotifyIntegrationChangedReloadsEveryRun is the save→reload seam: the trigger is
// at the write, so every writer is covered.
func TestNotifyIntegrationChangedReloadsEveryRun(t *testing.T) {
	cluster := newFakeCluster()
	svc, sidecar := testService(t, cluster, map[string]string{
		"int-1": networkedDefinition,
		"int-2": networkedDefinition,
	})
	for _, args := range [][2]string{{"u1", "int-1"}, {"u2", "int-1"}, {"u1", "int-2"}} {
		if _, err := svc.Ensure(context.Background(), args[0], args[1]); err != nil {
			t.Fatalf("Ensure(%v): %v", args, err)
		}
	}

	if err := svc.NotifyIntegrationChanged(context.Background(), "int-1"); err != nil {
		t.Fatalf("NotifyIntegrationChanged: %v", err)
	}
	// Both users editing int-1 get reloaded; the int-2 run does not.
	if len(sidecar.reloads) != 2 {
		t.Errorf("reloads = %d, want 2 (one per int-1 run)", len(sidecar.reloads))
	}
}

// TestNotifyIntegrationChangedIsQuietWhenNothingRuns is the common case, and it must
// cost nothing and report nothing.
func TestNotifyIntegrationChangedIsQuietWhenNothingRuns(t *testing.T) {
	cluster := newFakeCluster()
	svc, sidecar := testService(t, cluster, map[string]string{"int-1": networkedDefinition})

	if err := svc.NotifyIntegrationChanged(context.Background(), "int-1"); err != nil {
		t.Fatalf("NotifyIntegrationChanged with nothing running = %v, want nil", err)
	}
	if len(sidecar.reloads) != 0 {
		t.Errorf("reloads = %d, want 0", len(sidecar.reloads))
	}
}

// TestNotifyIntegrationChangedReportsButDoesNotHideFailures: the error is returned so
// a caller can log it. The caller's contract — never fail the write — is enforced at
// the call site, in integration.Service.notifyChanged.
func TestNotifyIntegrationChangedReportsFailures(t *testing.T) {
	cluster := newFakeCluster()
	svc, sidecar := testService(t, cluster, map[string]string{"int-1": networkedDefinition})
	if _, err := svc.Ensure(context.Background(), "u1", "int-1"); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	sidecar.err = ErrSidecarUnreachable

	if err := svc.NotifyIntegrationChanged(context.Background(), "int-1"); err == nil {
		t.Fatal("NotifyIntegrationChanged reported success while the sidecar was unreachable")
	}
}

func TestReapIdle(t *testing.T) {
	cluster := newFakeCluster()
	svc, _ := testService(t, cluster, map[string]string{"int-1": networkedDefinition})
	svc.idleTimeout = time.Hour

	cluster.runs["fresh"] = kube.DevRun{ID: "fresh", UserID: "u1", LastActivity: testNow.Add(-time.Minute)}
	cluster.runs["idle"] = kube.DevRun{ID: "idle", UserID: "u1", LastActivity: testNow.Add(-2 * time.Hour)}
	// No annotation at all: the fallback is the creation time, so an old one is reaped
	// and a new one is not. Reading a missing annotation as zero would reap live runs;
	// reading it as now would leak pods forever.
	cluster.runs["unstamped-old"] = kube.DevRun{ID: "unstamped-old", UserID: "u1", CreatedAt: testNow.Add(-3 * time.Hour)}
	cluster.runs["unstamped-new"] = kube.DevRun{ID: "unstamped-new", UserID: "u1", CreatedAt: testNow.Add(-time.Minute)}

	reaped, err := svc.ReapIdle(context.Background())
	if err != nil {
		t.Fatalf("ReapIdle: %v", err)
	}
	if reaped != 2 {
		t.Errorf("reaped = %d, want 2", reaped)
	}
	for _, id := range []string{"fresh", "unstamped-new"} {
		if _, exists := cluster.runs[id]; !exists {
			t.Errorf("%s was reaped but is not idle", id)
		}
	}
	for _, id := range []string{"idle", "unstamped-old"} {
		if _, exists := cluster.runs[id]; exists {
			t.Errorf("%s is idle but survived the reap", id)
		}
	}
}

// TestStatusToleratesAnUnreachableSidecar: the cluster already answered the question
// that matters, so a pod that is still starting must not fail a status poll.
func TestStatusToleratesAnUnreachableSidecar(t *testing.T) {
	cluster := newFakeCluster()
	svc, sidecar := testService(t, cluster, map[string]string{"int-1": networkedDefinition})
	run, err := svc.Ensure(context.Background(), "u1", "int-1")
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	sidecar.err = ErrSidecarUnreachable

	got, err := svc.Status(context.Background(), "u1", run.ID)
	if err != nil {
		t.Fatalf("Status = %v, want the cluster's answer anyway", err)
	}
	if got.ID != run.ID {
		t.Errorf("id = %q, want %q", got.ID, run.ID)
	}
	if got.Sidecar != nil {
		t.Errorf("Sidecar = %s, want nil when the sidecar did not answer", got.Sidecar)
	}

	sidecar.err = nil
	sidecar.status = json.RawMessage(`{"reload":{"generation":"abc"}}`)
	got, err = svc.Status(context.Background(), "u1", run.ID)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if string(got.Sidecar) != `{"reload":{"generation":"abc"}}` {
		t.Errorf("Sidecar = %s, want the sidecar's json passed through", got.Sidecar)
	}
}

// TestClusterFailuresSurface: a cluster that cannot be read or written must not look
// like "nothing is running", which is the reading that would silently start a second
// pod for an integration that already has one.
func TestClusterFailuresSurface(t *testing.T) {
	cluster := newFakeCluster()
	cluster.applyErr = errors.New("apiserver said no")
	svc, _ := testService(t, cluster, map[string]string{"int-1": networkedDefinition})

	if _, err := svc.Ensure(context.Background(), "u1", "int-1"); err == nil {
		t.Fatal("Ensure reported success while the create failed")
	}

	cluster.applyErr = nil
	cluster.listErr = errors.New("cache not synced")
	if _, err := svc.ListByUser(context.Background(), "u1", ""); err == nil {
		t.Error("ListByUser reported success while the listing failed")
	}
	if _, err := svc.ReapIdle(context.Background()); err == nil {
		t.Error("ReapIdle reported success while the listing failed")
	}
	if err := svc.NotifyIntegrationChanged(context.Background(), "int-1"); err == nil {
		t.Error("NotifyIntegrationChanged reported success while the listing failed")
	}
}

// TestDisabledWithoutASecret: an unkeyed hash would make every dev-run host a pure
// function of two guessable values, so no secret means no feature rather than a
// weaker one.
func TestDisabledWithoutASecret(t *testing.T) {
	svc := NewService(newFakeCluster(), fakeIntegrations{}, fakeResources{}, nil)
	if svc.Enabled() {
		t.Fatal("Enabled() = true with no hash secret")
	}
	if _, err := svc.Ensure(context.Background(), "u1", "int-1"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Ensure = %v, want ErrUnavailable", err)
	}
	if _, err := svc.ListByUser(context.Background(), "u1", ""); !errors.Is(err, ErrUnavailable) {
		t.Errorf("ListByUser = %v, want ErrUnavailable", err)
	}
}

// TestDisabledWithoutACluster: nil cluster, same posture.
func TestDisabledWithoutACluster(t *testing.T) {
	svc := NewService(nil, fakeIntegrations{}, fakeResources{}, secret)
	if svc.Enabled() {
		t.Fatal("Enabled() = true with no cluster")
	}
	if err := svc.NotifyIntegrationChanged(context.Background(), "int-1"); err != nil {
		t.Errorf("NotifyIntegrationChanged = %v, want nil (nothing to notify)", err)
	}
	if n, err := svc.ReapIdle(context.Background()); err != nil || n != 0 {
		t.Errorf("ReapIdle = (%d, %v), want (0, nil)", n, err)
	}
}

func indexOf(calls []string, want string) int {
	for i, c := range calls {
		if c == want {
			return i
		}
	}
	return -1
}
