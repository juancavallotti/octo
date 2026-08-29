package api

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// verifyConfig points the harness at a fake.
func verifyConfig(f *fake) Config {
	return Config{
		BaseURL:         f.url(),
		Timeout:         defaultTimeout,
		LongTimeout:     defaultLongTimeout,
		Startup:         StartupRequire,
		DiscoveryBudget: defaultDiscoveryBudget,
		InstanceID:      "verify-instance",
		DeploymentID:    "verify-deployment",
	}
}

// fullBackend starts a fake implementing everything the harness checks,
// correctly — including holding an empty poll open, which is the behaviour the
// harness is checking for and which a fake that answered instantly would fail.
func fullBackend(t *testing.T, doc discoveryDocument) *fake {
	t.Helper()
	f := newFake(t, doc)
	newKVBackend().install(f)
	newLeaseBackend().install(f)
	queues := newQueueBackend()
	queues.pollDelay = time.Duration(doc.Features.Queues.PollTimeoutSeconds) * time.Second
	queues.install(f)
	return f
}

// fastVerify shortens the poll so the empty-poll check does not wait twenty
// seconds. The check itself asserts the server waited MOST of the window, so a
// short window still proves the behaviour.
func fastVerify() discoveryDocument {
	doc := fullDiscovery()
	doc.Features.Queues.PollTimeoutSeconds = 1
	return doc
}

// A correct implementation passes everything. This is what makes the harness
// trustworthy: run it against the reference implementation and it must be silent.
func TestVerifyPassesAgainstACorrectImplementation(t *testing.T) {
	f := fullBackend(t, fastVerify())

	report, err := Verify(t.Context(), verifyConfig(f))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if report.Failed() {
		t.Fatalf("a correct implementation failed checks:\n%s", report.Format())
	}
	if len(report.Checks) < 10 {
		t.Fatalf("only %d checks ran; the harness is not exercising much", len(report.Checks))
	}
}

// And the other half: a harness that never fails is not a harness. Each case
// breaks one contract rule and asserts the specific check that catches it.
func TestVerifyCatchesBrokenImplementations(t *testing.T) {
	cases := []struct {
		name   string
		breaks func(*fake)
		want   string
	}{
		{
			name: "a create over an existing key returns 200 instead of 409",
			breaks: func(f *fake) {
				f.breaks("PUT "+pathKVEntry, func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set(headerVersion, "1")
					w.WriteHeader(http.StatusOK)
				})
			},
			want: "version 0 over an existing key conflicts",
		},
		{
			name: "an empty poll answers immediately instead of waiting",
			breaks: func(f *fake) {
				f.breaks("POST /v1/queues/{subject}/receive",
					func(w http.ResponseWriter, _ *http.Request) {
						w.WriteHeader(http.StatusNoContent)
					})
			},
			want: "an empty poll waits before answering",
		},
		{
			name: "an empty poll answers 404 instead of 204",
			breaks: func(f *fake) {
				f.breaks("POST /v1/queues/{subject}/receive",
					func(w http.ResponseWriter, _ *http.Request) {
						http.Error(w, "nothing here", http.StatusNotFound)
					})
			},
			want: "an empty poll answers 204, not an error",
		},
		{
			name: "a held lease is granted twice",
			breaks: func(f *fake) {
				f.breaks("POST /v1/leases/acquire", func(w http.ResponseWriter, _ *http.Request) {
					writeJSON(w, acquireResponse{Acquired: true, LeaseID: "always-granted"})
				})
			},
			want: "a held name is refused, not queued",
		},
		{
			name: "a delivery carries no id to settle it with",
			breaks: func(f *fake) {
				f.breaks("POST /v1/queues/{subject}/receive",
					func(w http.ResponseWriter, _ *http.Request) {
						writeJSON(w, receiveResponse{Messages: []delivery{{Message: messageWire{}}}})
					})
			},
			want: "a delivery carries the handle needed to settle it",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := fullBackend(t, fastVerify())
			// The override wins over the backend's own handler, so the
			// implementation is correct in every way except the one under test.
			tc.breaks(f)

			report, err := Verify(t.Context(), verifyConfig(f))
			if err != nil {
				t.Fatalf("Verify: %v", err)
			}
			if !report.Failed() {
				t.Fatalf("the harness passed a broken implementation:\n%s", report.Format())
			}
			if !failedCheck(report, tc.want) {
				t.Fatalf("expected %q to fail; got:\n%s", tc.want, report.Format())
			}
		})
	}
}

// failedCheck reports whether a named check failed.
func failedCheck(report VerifyReport, name string) bool {
	for _, c := range report.Checks {
		if c.Name == name && !c.Passed && !c.Skipped {
			return true
		}
	}
	return false
}

// A feature nobody declared is skipped, not failed: partial implementation is a
// first-class answer, and a harness that punished it would push people toward
// declaring things they have not written.
func TestVerifySkipsUndeclaredFeatures(t *testing.T) {
	doc := discoveryDocument{SpecVersion: specVersion}
	doc.Features.KV.Supported = true
	f := newFake(t, doc)
	newKVBackend().install(f)

	report, err := Verify(t.Context(), verifyConfig(f))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if report.Failed() {
		t.Fatalf("a storage-only implementation failed checks:\n%s", report.Format())
	}
	if !strings.Contains(report.Format(), "SKIP") {
		t.Fatalf("nothing was skipped:\n%s", report.Format())
	}
}

// Granting leases while declaring them unsupported is the one thing worth saying
// out loud even when nothing failed, because it is silent and it is a correctness
// bug on more than one instance.
func TestVerifyNotesTheLeaseOptOut(t *testing.T) {
	doc := discoveryDocument{SpecVersion: specVersion}
	doc.Features.Leases.Unsupported = string(PolicyNoop)
	f := newFake(t, doc)

	report, err := Verify(t.Context(), verifyConfig(f))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !strings.Contains(report.Format(), "correctness bug") {
		t.Fatalf("the lease opt-out was not surfaced:\n%s", report.Format())
	}
}

// The report reads as a checklist, which is the whole point of it.
func TestVerifyReportFormat(t *testing.T) {
	f := fullBackend(t, fastVerify())
	report, err := Verify(t.Context(), verifyConfig(f))
	if err != nil {
		t.Fatal(err)
	}
	out := report.Format()
	for _, want := range []string{"Platform API at", "declared:", "passed,", "ok    "} {
		if !strings.Contains(out, want) {
			t.Errorf("the report is missing %q:\n%s", want, out)
		}
	}
}

// Somebody pointing this at a live system deserves to know what it wrote. The
// prefix is reported rather than merely used, because both the docs and the CLI
// help promise it is.
func TestVerifyReportsWhatItWroteUnder(t *testing.T) {
	f := fullBackend(t, fastVerify())
	report, err := Verify(t.Context(), verifyConfig(f))
	if err != nil {
		t.Fatal(err)
	}
	if report.ScratchPrefix != verifyPrefix {
		t.Fatalf("scratchPrefix = %q, want %q", report.ScratchPrefix, verifyPrefix)
	}
	if !strings.Contains(report.Format(), verifyPrefix) {
		t.Fatalf("the report does not say what it wrote under:\n%s", report.Format())
	}
}
