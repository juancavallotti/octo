package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeAPI serves the routes the upgrade commands call, recording what reached it.
type fakeAPI struct {
	mu        sync.Mutex
	started   []map[string]any
	rollouts  []map[string]any
	reads     int
	verdict   string
	deployRow map[string]any
	// what the run reads back with, for a run that asked to roll out
	rolloutOnPass bool
	rolledOutAt   any
	runError      string
	// what reached an integration's routes, which create must not touch first
	integrationCalls []string
	// when set, every route answers 403 with this message
	forbidden string
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	write := func(status int, v any) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	body := map[string]any{}
	_ = json.NewDecoder(r.Body).Decode(&body)

	switch {
	case f.forbidden != "":
		write(403, map[string]any{"error": f.forbidden})
	case r.URL.Path == "/v1/upgrades":
		write(200, map[string]any{"runtimeVersion": "0.11.14", "deployments": []any{
			map[string]any{"deploymentId": "d1", "name": "billing", "tag": "v1.0.0", "runtimeVersion": "0.11.13", "upgradeTo": "0.11.14", "hasSuites": true},
			map[string]any{"deploymentId": "d2", "name": "timer", "runtimeVersion": "0.11.13", "upgradeTo": "0.11.14", "hasSuites": false, "pinned": true},
		}})
	case r.URL.Path == "/v1/environments":
		// What a key bound to stage is shown: its own environment and no other.
		write(200, []any{map[string]any{"name": "stage", "displayName": "Staging", "namespace": "octo-stage", "default": false}})
	case r.URL.Path == "/v1/runtimes":
		write(200, map[string]any{"current": "0.11.14", "versions": []any{"0.11.14", "0.11.13"}})
	case r.URL.Path == "/v1/integrations" && r.Method == http.MethodGet:
		write(200, []any{map[string]any{"id": "i1", "name": "billing"}})
	case strings.HasPrefix(r.URL.Path, "/v1/integrations/i1/"):
		f.integrationCalls = append(f.integrationCalls, r.Method+" "+r.URL.Path)
		write(500, map[string]any{"error": "the fake does not deploy"})
	case r.URL.Path == "/v1/deployments" && r.Method == http.MethodGet:
		write(200, []any{f.deployRow})
	case r.URL.Path == "/v1/deployments/d1" && r.Method == http.MethodGet:
		write(200, f.deployRow)
	case r.URL.Path == "/v1/deployments/d1/testruns" && r.Method == http.MethodPost:
		f.started = append(f.started, body)
		write(202, map[string]any{"id": "r1", "deploymentId": "d1", "status": "running", "runtimeVersion": "0.11.14"})
	case r.URL.Path == "/v1/deployments/d1/testruns/r1":
		f.reads++
		write(200, map[string]any{"id": "r1", "deploymentId": "d1", "status": f.verdict, "runtimeVersion": "0.11.14",
			"rolloutOnPass": f.rolloutOnPass, "rolledOutAt": f.rolledOutAt, "error": f.runError,
			"report": map[string]any{"totals": map[string]any{"cases": 1, "failed": 1}, "suites": []any{
				map[string]any{"path": "t_test.yaml", "cases": []any{map[string]any{"name": "it prices", "status": f.verdict, "summary": "want USD"}}},
			}}})
	case r.URL.Path == "/v1/deployments/d1/rollout":
		f.rollouts = append(f.rollouts, body)
		write(200, f.deployRow)
	default:
		write(404, map[string]any{"error": "not found: " + r.Method + " " + r.URL.Path})
	}
}

// run executes octopaas against api with args, returning its status and output.
func run(t *testing.T, api *fakeAPI, args ...string) (int, string) {

	t.Helper()
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	env := map[string]string{
		"OCTOPAAS_URL":  srv.URL,
		"OCTOPAAS_KEY":  "octo_test",
		"OCTOPAAS_HOME": t.TempDir(),
	}
	var out, errOut bytes.Buffer
	app := &App{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errOut, Getenv: func(k string) string { return env[k] }}
	status := Execute(context.Background(), app, args)
	return status, out.String() + errOut.String()
}

func TestOutdatedListsBehindAndUntested(t *testing.T) {

	status, out := run(t, &fakeAPI{}, "deployments", "outdated", "--plain")
	if status != 0 {
		t.Fatalf("status %d: %s", status, out)
	}
	for _, want := range []string{"billing", "0.11.13 → 0.11.14", "not tested on 0.11.14", "pinned", "No test suites", "timer"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestTestWaitsForTheVerdictAndFailsOnAFailure(t *testing.T) {

	api := &fakeAPI{verdict: "failed", deployRow: map[string]any{"id": "d1", "name": "billing"}}
	status, out := run(t, api, "deployments", "test", "billing", "--upgrade", "--plain")
	if status != 1 {
		t.Fatalf("status %d, want 1 for failing suites: %s", status, out)
	}
	if len(api.started) != 1 || api.started[0]["rolloutOnPass"] != true {
		t.Errorf("started = %v, want one run with rolloutOnPass", api.started)
	}
	if !strings.Contains(out, "it prices") || !strings.Contains(out, "want USD") {
		t.Errorf("the failing case is not reported:\n%s", out)
	}
}

func TestTestPasses(t *testing.T) {

	api := &fakeAPI{verdict: "passed", deployRow: map[string]any{"id": "d1", "name": "billing"}}
	if status, out := run(t, api, "deployments", "test", "billing", "--plain"); status != 0 {
		t.Fatalf("status %d: %s", status, out)
	}
	if api.started[0]["rolloutOnPass"] != false {
		t.Errorf("a plain test asked for a rollout: %v", api.started[0])
	}
}

// A pinned deployment is upgraded by moving its pin; it stays pinned.
func TestUpgradeKeepsAPinPinned(t *testing.T) {

	api := &fakeAPI{deployRow: map[string]any{
		"id": "d1", "name": "billing", "snapshotId": "s1", "status": "running",
		"replicas": 1, "readyReplicas": 1, "desiredReplicas": 1,
		"upgradeTo": "0.11.14", "runtimePin": "0.11.13",
	}}
	if status, out := run(t, api, "deployments", "upgrade", "billing", "--no-wait", "--plain"); status != 0 {
		t.Fatalf("status %d: %s", status, out)
	}
	if len(api.rollouts) != 1 || api.rollouts[0]["snapshotId"] != nil || api.rollouts[0]["runtimeVersion"] != "0.11.14" {
		t.Errorf("rollouts = %v, want no version and the pin moved to 0.11.14", api.rollouts)
	}
}

// The platform will not roll a pinned deployment out on a pass, so the run is
// started without asking and the pin is moved once it passes.
func TestUpgradeWithTestsMovesAPinAfterAPass(t *testing.T) {

	api := &fakeAPI{verdict: "passed", deployRow: map[string]any{
		"id": "d1", "name": "billing", "status": "running",
		"replicas": 1, "readyReplicas": 1, "desiredReplicas": 1,
		"upgradeTo": "0.11.14", "runtimePin": "0.11.13",
	}}
	if status, out := run(t, api, "deployments", "upgrade", "billing", "--test", "--plain"); status != 0 {
		t.Fatalf("status %d: %s", status, out)
	}
	if len(api.started) != 1 || api.started[0]["rolloutOnPass"] != false {
		t.Errorf("started = %v, want one run that does not roll out", api.started)
	}
	if len(api.rollouts) != 1 || api.rollouts[0]["runtimeVersion"] != "0.11.14" {
		t.Errorf("rollouts = %v, want the pin moved to 0.11.14", api.rollouts)
	}
}

// A failing run moves nothing.
func TestUpgradeWithTestsLeavesAPinOnAFailure(t *testing.T) {

	api := &fakeAPI{verdict: "failed", deployRow: map[string]any{
		"id": "d1", "name": "billing", "upgradeTo": "0.11.14", "runtimePin": "0.11.13",
	}}
	if status, out := run(t, api, "deployments", "upgrade", "billing", "--test", "--plain"); status != 1 {
		t.Fatalf("status %d, want 1: %s", status, out)
	}
	if len(api.rollouts) != 0 {
		t.Errorf("rollouts = %v, want none after failing suites", api.rollouts)
	}
}

func TestOutdatedJSONListsOnlyTheOutdated(t *testing.T) {

	api := &fakeAPI{}
	status, out := run(t, api, "deployments", "outdated", "-o", "json")
	if status != 0 {
		t.Fatalf("status %d: %s", status, out)
	}
	var view struct {
		Deployments []struct {
			Name string `json:"name"`
		} `json:"deployments"`
	}
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatalf("decode %q: %v", out, err)
	}
	if len(view.Deployments) != 2 {
		t.Errorf("deployments = %v, want the two outdated ones", view.Deployments)
	}
}

// A release the platform does not support is refused before a draft is frozen.
func TestCreateRefusesAnUnsupportedRuntimeFirst(t *testing.T) {

	api := &fakeAPI{}
	status, out := run(t, api, "deployments", "create", "billing", "--runtime", "0.9.0", "--plain")
	if status == 0 || !strings.Contains(out, "0.11.14, 0.11.13") {
		t.Fatalf("status %d, want a refusal listing the releases: %s", status, out)
	}
	if len(api.integrationCalls) != 0 {
		t.Errorf("reached %v before refusing", api.integrationCalls)
	}
}

// A passing run whose rollout was not made is not a success for --upgrade.
func TestTestUpgradeFailsWhenTheRolloutWasNotMade(t *testing.T) {

	api := &fakeAPI{verdict: "passed", rolloutOnPass: true, runError: "the deployment was rolled out while the run was going",
		deployRow: map[string]any{"id": "d1", "name": "billing"}}
	status, out := run(t, api, "deployments", "test", "billing", "--upgrade", "--plain")
	if status != 1 || !strings.Contains(out, "not upgraded") {
		t.Fatalf("status %d, want 1 with not upgraded: %s", status, out)
	}
}
