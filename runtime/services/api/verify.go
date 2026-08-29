package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// Verification exists because this module publishes a contract and asks strangers
// to implement it.
//
// Without a checker, an implementer's first feedback is their flows behaving
// oddly through a runtime that can only say "unexpected status 500". This turns
// that into a checklist: point it at a server, and it says which features were
// declared, which of them actually work, and for each failure what the contract
// says should have happened.
//
// It runs against a scratch prefix it announces up front, so it is safe to point
// at a real deployment — though a staging one is still the better idea, since it
// does write.

// verifyPrefix scopes every key, subject and agent the harness touches, so its
// leftovers are recognizable and its writes cannot collide with real ones.
const verifyPrefix = "octo-verify/"

// CheckResult is one contract rule, checked.
type CheckResult struct {
	Feature Feature `json:"feature"`
	Name    string  `json:"name"`
	Passed  bool    `json:"passed"`
	Skipped bool    `json:"skipped"`
	// Detail says what happened when a check failed or was skipped: for a failure,
	// what the contract requires and what the server did instead.
	Detail string `json:"detail,omitempty"`
}

// VerifyReport is everything one run found.
type VerifyReport struct {
	URL            string         `json:"url"`
	SpecVersion    string         `json:"specVersion"`
	Implementation implementation `json:"implementation"`
	// Declared lists the features the discovery document claimed.
	Declared []string `json:"declared"`
	// ScratchPrefix is the namespace, subject and agent prefix everything this
	// run wrote sits under. It is reported rather than merely used, because
	// somebody pointing this at a live system deserves to know what it touched.
	ScratchPrefix string        `json:"scratchPrefix"`
	Checks        []CheckResult `json:"checks"`
}

// Failed reports whether any check failed, which is what the command's exit
// status is.
func (r VerifyReport) Failed() bool {
	for _, c := range r.Checks {
		if !c.Passed && !c.Skipped {
			return true
		}
	}
	return false
}

// Verify exercises a platform API against the published contract.
//
// Only declared features are checked. A feature you did not claim is not a
// failure — partial implementation is a first-class answer here — so it is
// reported as skipped and nothing is sent to it.
func Verify(ctx context.Context, cfg Config) (VerifyReport, error) {
	c, err := newClient(cfg)
	if err != nil {
		return VerifyReport{}, err
	}
	defer c.close()

	doc, err := fetchDiscovery(ctx, c, cfg)
	if err != nil {
		return VerifyReport{}, err
	}
	report := VerifyReport{
		URL:            cfg.BaseURL,
		SpecVersion:    doc.SpecVersion,
		Implementation: doc.Implementation,
		Declared:       supportedFeatures(doc),
		ScratchPrefix:  verifyPrefix,
	}
	report.Checks = append(report.Checks, checkDiscovery(doc)...)
	for _, suite := range verifySuites() {
		report.Checks = append(report.Checks, suite(ctx, c, cfg, doc)...)
	}
	return report, nil
}

// verifySuite is one feature's checks.
type verifySuite func(ctx context.Context, c *client, cfg Config, doc discoveryDocument) []CheckResult

func verifySuites() []verifySuite {
	return []verifySuite{verifyKV, verifyLeases, verifyQueues}
}

// checkDiscovery inspects the document itself, before anything is sent anywhere.
func checkDiscovery(doc discoveryDocument) []CheckResult {
	checks := []CheckResult{
		result(FeatureCore, "discovery answers", true, ""),
	}
	if doc.SpecVersion == specVersion {
		checks = append(checks, result(FeatureCore, "specVersion matches this runtime", true, ""))
	} else {
		checks = append(checks, result(FeatureCore, "specVersion matches this runtime", true,
			fmt.Sprintf("declared %q, this runtime speaks %q — a warning, not a failure",
				doc.SpecVersion, specVersion)))
	}

	// Not a rule, but the single most consequential thing a document can get
	// wrong, so it is surfaced as a check rather than buried in prose.
	f := doc.Features
	if !f.Leases.Supported && policyFor(FeatureLeases, f.Leases.Unsupported) == PolicyNoop {
		checks = append(checks, result(FeatureLeases, "opting out of leases is deliberate", true,
			"leases are unsupported with \"unsupported\": \"noop\", so every claim is granted. "+
				"That is correct for ONE instance and a silent correctness bug for more than one."))
	}
	if f.Secrets.Supported && !f.Secrets.EncryptedAtRest {
		checks = append(checks, result(FeatureSecrets, "secrets are encrypted at rest", false,
			"secrets.encryptedAtRest is not set, so values written to the *_secrets "+
				"namespaces may be stored in the clear"))
	}
	return checks
}

// verifyKV checks the version rules, which are the subtlest part of the contract
// and the part an implementation is most likely to get wrong.
func verifyKV(ctx context.Context, c *client, _ Config, doc discoveryDocument) []CheckResult {
	if !doc.Features.KV.Supported {
		return []CheckResult{skipped(FeatureKV, "kv")}
	}
	kv := newKVStore(c, doc.Features.KV)
	key := verifyPrefix + "kv"
	defer func() { _ = kv.Delete(ctx, core.NamespaceUser, key, 0) }()

	var out []CheckResult
	_, found, err := kv.Get(ctx, core.NamespaceUser, key+"-absent")
	out = append(out, check(FeatureKV, "reading an absent key is a miss, not an error",
		err == nil && !found, detail(err, "a 404 on a read must come back as 'nothing stored'")))

	version, err := kv.Set(ctx, core.NamespaceUser, key, []byte("one"), 0)
	out = append(out, check(FeatureKV, "version 0 creates",
		err == nil && version > 0, detail(err, "a create must return a positive version")))
	if err != nil {
		return out
	}

	entry, found, err := kv.Get(ctx, core.NamespaceUser, key)
	out = append(out, check(FeatureKV, "a read returns the value and its version",
		err == nil && found && string(entry.Value) == "one" && entry.Version == version,
		detail(err, "the version a write returns must be the version the next read reports")))

	_, err = kv.Set(ctx, core.NamespaceUser, key, []byte("two"), 0)
	out = append(out, check(FeatureKV, "version 0 over an existing key conflicts",
		errors.Is(err, core.ErrVersionConflict),
		detail(err, "0 means create; writing it over an existing key must answer 409")))

	_, err = kv.Set(ctx, core.NamespaceUser, key, []byte("two"), version+staleVersionOffset)
	out = append(out, check(FeatureKV, "a stale version conflicts",
		errors.Is(err, core.ErrVersionConflict),
		detail(err, "a version that is not the current one must answer 409, or a "+
			"concurrent update is silently lost")))

	err = kv.Delete(ctx, core.NamespaceUser, key+"-absent", 0)
	out = append(out, check(FeatureKV, "deleting an absent key succeeds",
		err == nil, detail(err, "erasure of something that is not there is what the caller asked for")))
	return out
}

// staleVersionOffset makes a version that is definitely not current.
const staleVersionOffset = 99

// verifyLeases checks that a claim is exclusive and that acquiring never blocks.
func verifyLeases(ctx context.Context, c *client, cfg Config, doc discoveryDocument) []CheckResult {
	if !doc.Features.Leases.Supported {
		return []CheckResult{skipped(FeatureLeases, "leases")}
	}
	first := newLeases(ctx, c, cfg.InstanceID, doc.Features.Leases)
	// A second identity, because a platform that keys claims on the holder would
	// otherwise pass an exclusivity check it should fail.
	second := newLeases(ctx, c, cfg.InstanceID+"-verify-other", doc.Features.Leases)
	name := verifyPrefix + "lease"

	lease, ok, err := first.Acquire(ctx, name, core.WithLeaseTTL(verifyLeaseTTL))
	out := []CheckResult{check(FeatureLeases, "a free name can be claimed",
		err == nil && ok, detail(err, "acquiring a name nobody holds must succeed"))}
	if err != nil || !ok {
		return out
	}
	// Close is idempotent, so this covers the early returns below without
	// double-releasing the claim the explicit Close already gave back.
	defer func() { _ = lease.Close() }()

	// Timed, because "never blocks" is the property, not just "eventually answers".
	started := time.Now()
	_, ok, err = second.Acquire(ctx, name, core.WithLeaseTTL(verifyLeaseTTL))
	elapsed := time.Since(started)
	out = append(out, check(FeatureLeases, "a held name is refused, not queued",
		err == nil && !ok, detail(err, "a second claim must answer acquired:false (or 409), "+
			"never wait for the holder to finish")))
	out = append(out, check(FeatureLeases, "acquiring answers immediately",
		elapsed < verifyAcquireBudget,
		fmt.Sprintf("the second claim took %s; a caller that cannot have a name goes and does "+
			"something else, so this must not block", elapsed.Round(time.Millisecond))))

	// Releasing is a check of its own, not a step on the way to one. A release
	// that failed would otherwise show up as the NEXT check failing, and the
	// implementer would go looking at the wrong route.
	releaseErr := lease.Close()
	out = append(out, check(FeatureLeases, "a claim can be released",
		releaseErr == nil, detail(releaseErr, "releasing a claim must succeed")))
	if releaseErr != nil {
		return out
	}

	regained, ok, err := first.Acquire(ctx, name, core.WithLeaseTTL(verifyLeaseTTL))
	out = append(out, check(FeatureLeases, "a released name can be claimed again",
		err == nil && ok, detail(err, "releasing must actually free the name")))
	if ok {
		_ = regained.Close()
	}
	return out
}

// Bounds for the lease checks.
const (
	verifyLeaseTTL      = 30 * time.Second
	verifyAcquireBudget = 3 * time.Second
)

// verifyQueues checks the long poll, which is where implementations go wrong most
// often and most expensively.
func verifyQueues(ctx context.Context, c *client, cfg Config, doc discoveryDocument) []CheckResult {
	if !doc.Features.Queues.Supported {
		return []CheckResult{skipped(FeatureQueues, "queues")}
	}
	q := newQueues(ctx, c, cfg.DeploymentID, doc.Features.Queues)
	subject := verifyPrefix + "queue"

	var out []CheckResult
	// An empty poll must hold the request open and then answer 204. A server that
	// answers instantly turns every subscription into a busy loop.
	started := time.Now()
	messages, err := q.receive(ctx, subject)
	elapsed := time.Since(started)
	out = append(out, check(FeatureQueues, "an empty poll answers 204, not an error",
		err == nil && len(messages) == 0,
		detail(err, "a poll with nothing to deliver must answer 204; that is the normal "+
			"state of an idle subject")))
	out = append(out, check(FeatureQueues, "an empty poll waits before answering",
		elapsed >= q.poll.timeout/2,
		fmt.Sprintf("the poll returned in %s of a declared %s window. A server that answers "+
			"immediately makes the runtime poll as fast as the network allows",
			elapsed.Round(time.Millisecond), q.poll.timeout)))

	msg, err := verifyMessage()
	if err != nil {
		return append(out, check(FeatureQueues, "publish and receive round-trip", false, err.Error()))
	}
	if err := q.Publish(ctx, subject, msg); err != nil {
		return append(out, check(FeatureQueues, "publish and receive round-trip", false,
			detail(err, "publishing to a subject must be accepted")))
	}
	delivered, err := q.receive(ctx, subject)
	roundTripped := err == nil && len(delivered) > 0
	out = append(out, check(FeatureQueues, "publish and receive round-trip", roundTripped,
		detail(err, "a published message must come back from a receive on the same subject")))
	if roundTripped {
		out = append(out, checkDeliveryShape(delivered[0], msg))
		q.settle(ctx, routeQueueAck, subject, delivered[0].DeliveryID, 0)
	}
	return out
}

// checkDeliveryShape confirms a delivery carried what it must: a handle to settle
// it, and the message unchanged.
func checkDeliveryShape(d delivery, sent types.Message) CheckResult {
	if d.DeliveryID == "" {
		return check(FeatureQueues, "a delivery carries the handle needed to settle it", false,
			"deliveryId was empty, so there is no way to acknowledge the message and it "+
				"would be redelivered forever")
	}
	got, err := decodeMessage(d.Message)
	if err != nil {
		return check(FeatureQueues, "a message crosses unchanged", false,
			detail(err, "the delivered envelope did not decode"))
	}
	if got.EventID != sent.EventID {
		return check(FeatureQueues, "a message crosses unchanged", false,
			fmt.Sprintf("eventId came back %q, sent %q — store and return the envelope whole",
				got.EventID, sent.EventID))
	}
	return result(FeatureQueues, "a message crosses unchanged", true, "")
}

// verifyMessage builds the probe message.
func verifyMessage() (types.Message, error) {
	msg, err := types.NewMessage("octo-verify")
	if err != nil {
		return types.Message{}, fmt.Errorf("build a probe message: %w", err)
	}
	msg.Body = map[string]any{"octoVerify": true}
	return *msg, nil
}

// check builds a result, using detail only when it failed.
func check(feature Feature, name string, passed bool, detail string) CheckResult {
	if passed {
		return result(feature, name, true, "")
	}
	return result(feature, name, false, detail)
}

func result(feature Feature, name string, passed bool, detail string) CheckResult {
	return CheckResult{Feature: feature, Name: name, Passed: passed, Detail: detail}
}

func skipped(feature Feature, name string) CheckResult {
	return CheckResult{
		Feature: feature, Name: name, Skipped: true,
		Detail: "not declared in the discovery document, so nothing was sent to it",
	}
}

// detail joins what the contract requires with what the server actually did.
func detail(err error, requirement string) string {
	if err == nil {
		return requirement
	}
	return requirement + "; got: " + err.Error()
}

// Format renders a report as the table the command prints.
func (r VerifyReport) Format() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Platform API at %s\n", r.URL)
	if r.Implementation.Name != "" {
		fmt.Fprintf(&b, "  implementation: %s %s\n", r.Implementation.Name, r.Implementation.Version)
	}
	fmt.Fprintf(&b, "  specVersion:    %s (this runtime speaks %s)\n", r.SpecVersion, specVersion)
	fmt.Fprintf(&b, "  declared:       %s\n", declaredList(r.Declared))
	fmt.Fprintf(&b, "  wrote under:    %s\n\n", r.ScratchPrefix)

	var passed, failed, skips int
	for _, c := range r.Checks {
		switch {
		case c.Skipped:
			skips++
			fmt.Fprintf(&b, "  SKIP  %-14s %s\n", c.Feature, c.Name)
		case c.Passed:
			passed++
			fmt.Fprintf(&b, "  ok    %-14s %s\n", c.Feature, c.Name)
			if c.Detail != "" {
				fmt.Fprintf(&b, "        %-14s note: %s\n", "", c.Detail)
			}
		default:
			failed++
			fmt.Fprintf(&b, "  FAIL  %-14s %s\n", c.Feature, c.Name)
			fmt.Fprintf(&b, "        %-14s %s\n", "", c.Detail)
		}
	}
	fmt.Fprintf(&b, "\n%d passed, %d failed, %d skipped\n", passed, failed, skips)
	return b.String()
}

// declaredList renders the declared features, saying so when there are none.
func declaredList(declared []string) string {
	if len(declared) == 0 {
		return "(nothing)"
	}
	return strings.Join(declared, " ")
}
