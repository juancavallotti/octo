// The "aggregate" block: many messages in, one out. It is the other half of what
// `foreach mode: map` fuses, and the first block in the runtime that holds state
// between invocations — which is what lets it combine messages that arrive over
// time (batch 100 webhook events; every event in a 5s window) rather than only
// elements of one body.
//
// Group state lives in core.KV under optimistic concurrency, so any replica can
// fold into a group and two replicas can never each hold half of one. The one
// condition that fires without a message arriving — the timeout — needs a timer
// instead, and that timer is gated on core.LeaderElection so a group is reaped
// once per cluster rather than once per replica. Both services behave correctly
// in the standalone module too (an in-process map and a permanent leader), so
// there is no second code path for single-process runs.
package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

// How an aggregate combines messages.
const (
	aggregateStrategyAppend     = "append"
	aggregateStrategyExpression = "expression"
)

// What an aggregate does when it is already holding maxGroups groups.
const (
	// aggregateOverflowFail is the default: the message errors into the flow's
	// error chain, where it is visible and recoverable. The alternatives both lose
	// something quietly, so neither is a good default.
	aggregateOverflowFail = "fail"
	// aggregateOverflowComplete releases the group that has been open the longest,
	// which emits a partial group.
	aggregateOverflowComplete = "complete"
	// aggregateOverflowDrop discards the message.
	aggregateOverflowDrop = "drop"
)

// When the timeout is measured from.
const (
	aggregateTimeoutFromFirst = "first"
	aggregateTimeoutFromLast  = "last"
)

// Why a group completed, reported to the flow that receives it.
const (
	aggregateReasonSize       = "size"
	aggregateReasonTimeout    = "timeout"
	aggregateReasonExpression = "expression"
	aggregateReasonOverflow   = "overflow"
)

// Variables carried by an aggregated message.
const (
	varAggregateKey    = "aggregateKey"
	varAggregateCount  = "aggregateCount"
	varAggregateReason = "aggregateReason"
	// varGroup is the accumulated state an aggregate's expressions see. It is a
	// message variable rather than a new CEL activation root on purpose: it keeps
	// expr.CompileMessage the single seam for message expressions, and it matches
	// how every other block hands context to an expression (foreach's vars.<as>,
	// handle-errors' vars.error, validate's vars.validationErrors).
	varGroup = "group"
)

// Defaults that make split -> aggregate work with no configuration. Both are
// ordinary expressions rather than special cases in the code, so overriding
// either is the documented way to aggregate messages that never came from a
// split. The size default guards with has() because a message from anywhere else
// simply has no groupSize.
const (
	defaultCorrelation    = "vars." + varGroupID
	defaultCompletionSize = "has(vars." + varGroupSize + ") ? vars." + varGroupSize + " : -1"
	defaultMaxGroups      = 1000
)

// aggregateWriteAttempts bounds the optimistic read-modify-write loop, matching
// the object-write block's budget. A conflict means another replica folded into
// the same group first, so the retry re-reads and folds onto its result.
//
// The budget is small because it only ever has to win against *other replicas*:
// folds racing within this process are serialized by foldLocks first.
const aggregateWriteAttempts = 5

// maxPositionedIndex bounds how far a positioned message may pad the appended
// accumulator. A group large enough to reach it should be folding with
// strategy: expression anyway — append rewrites the whole group on every
// message, so its cost is quadratic in stored bytes long before this.
const maxPositionedIndex = 100_000

// foldStripes is how many mutexes serialize in-process folds. Contention on one
// group is the normal case here, not the exception — every element of a split
// folds into the same group, and a flow runs them concurrently by design. Left
// to the optimistic loop alone, a wide fan-in would burn its whole retry budget
// losing to its own siblings and fail messages that nothing was actually wrong
// with. Striping keeps the lock table bounded; a hash collision only costs two
// unrelated groups a little serialization.
const foldStripes = 64

// stripeSeed is the multiplier of the classic string hash the stripe is picked
// with. Any small odd prime spreads hex digests evenly enough for this.
const stripeSeed = 31

// foldLocks serializes folds into the same group within this process.
type foldLocks [foldStripes]sync.Mutex

// lock takes the stripe for a group and returns its release.
func (l *foldLocks) lock(hash string) func() {
	mu := &l[stripeOf(hash)]
	mu.Lock()
	return mu.Unlock
}

// stripeOf picks a group's stripe from its (hex-encoded) hash.
func stripeOf(hash string) int {
	var sum uint32
	for i := 0; i < len(hash); i++ {
		sum = sum*stripeSeed + uint32(hash[i])
	}
	return int(sum % foldStripes)
}

// Reaper tick bounds. The interval tracks the configured timeout so a short
// window is not checked at a lazy cadence, but never busier than a second nor
// lazier than half a minute.
const (
	minReapInterval = time.Second
	maxReapInterval = 30 * time.Second
)

var errGroupContended = errors.New("aggregate: group contended by concurrent writers")

// groupState is one in-flight group as it is stored. Everything an expression or
// a completion check needs is here, so a replica that has never seen the group
// before can pick it up from a single read.
type groupState struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
	// Size is how many messages the group expects, or sizeUnknown until an
	// arriving message reveals it. Completing on a count rather than on a
	// last-marker is what makes the result independent of arrival order, and
	// elements of a split routinely arrive out of order because they run
	// concurrently.
	Size int `json:"size"`
	Acc  any `json:"acc"`
	// CorrelationID of the group's first message, inherited by the aggregated one:
	// the caller's correlation chain is not the aggregator's to break.
	CorrelationID string `json:"correlationId,omitempty"`

	FirstAt  int64 `json:"firstAt"`
	LastAt   int64 `json:"lastAt"`
	Deadline int64 `json:"deadline,omitempty"`
}

// groupIndex lists the open groups. core.KV has no scan or list, so the reaper
// cannot discover expired groups without an index of its own — and because it
// names every open group, it doubles as the ledger that bounds how many there
// are.
type groupIndex struct {
	Groups map[string]groupEntry `json:"groups"`
}

// groupEntry is what the index remembers about one group: enough to answer both
// questions asked of it without reading the group itself.
type groupEntry struct {
	// FirstAt is when the group's slot was claimed. The sweep does not need it,
	// but the overflow policy does, and it is the only ordering that always
	// exists: Deadline is 0 for every group of an aggregate with no
	// completionTimeout, so ordering by it would have picked whichever group the
	// map happened to hand over first rather than the oldest.
	FirstAt int64 `json:"firstAt"`
	// Deadline is when the group times out, 0 when it has none — which, while the
	// sweep is running, also marks a slot claimed for a group that never appeared.
	Deadline int64 `json:"deadline,omitempty"`
}

// aggregate combines messages into groups and continues the flow once per
// completed group.
type aggregate struct {
	storeKey  string
	leaderKey string

	correlation    *expr.Program
	fold           *expr.Program
	completionSize *expr.Program
	completionExpr *expr.Program
	appendMode     bool

	timeout      time.Duration
	timeoutFrom  string
	maxGroups    int
	onOverflow   string
	buildRespone *Flow
	env          map[string]any

	cont core.Continuation

	// folds serializes concurrent folds into the same group on this replica, so
	// the optimistic loop only has to resolve genuine cross-replica races.
	folds foldLocks
	// index does the same for the group index, which is a single hot key every
	// new group and every open group's write-back touches. Without it a burst of
	// new groups would spend its whole retry budget losing to its own siblings —
	// and losing that race refuses a message, where the fold's equivalent only
	// delays a timeout.
	//
	// It is always taken after a fold stripe, never before: releasing a group
	// takes that group's stripe and then rewrites the index.
	index sync.Mutex

	lease    core.Leadership
	done     chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

func (a *aggregate) SetContinuation(c core.Continuation) { a.cont = c }

// StateKey reports the identity this block's stored groups live under, so the
// runtime can refuse a config where two blocks would share one.
func (a *aggregate) StateKey() string { return a.storeKey }

// aggregateBlock builds an aggregate from its config.
//
//nolint:ireturn // builders intentionally return the MessageProcessor interface
func (b *builder) aggregateBlock(cfg types.BlockConfig) (core.MessageProcessor, error) {
	if err := allowSlots(cfg, blockKindAggregate,
		"storeKey", "correlation", "strategy", "expression", "completionSize", "completionTimeout",
		"completionExpression", "timeoutFrom", "maxGroups", "onOverflow", "buildResponse"); err != nil {
		return nil, err
	}
	if err := requireRootChain(b, blockKindAggregate); err != nil {
		return nil, err
	}

	storeKey := cfg.StoreKey
	if storeKey == "" {
		storeKey = b.path
	}
	maxGroups := cfg.MaxGroups
	if maxGroups <= 0 {
		maxGroups = defaultMaxGroups
	}

	a := &aggregate{
		storeKey: storeKey,
		// Derived from the same identity the state is stored under, and that
		// matters: campaigning under anything else would let two generations of a
		// reloaded flow hold two leases over one set of groups and reap them twice.
		leaderKey: "aggregate_" + storeKey,
		maxGroups: maxGroups,
		env:       expr.EnvActivation(b.deps.Env),
		done:      make(chan struct{}),
	}

	if err := a.compileExpressions(b, cfg); err != nil {
		return nil, err
	}
	if err := a.resolveCompletion(cfg); err != nil {
		return nil, err
	}

	var err error
	if a.buildRespone, err = buildTakeoverResponse(b, cfg.BuildResponse); err != nil {
		return nil, err
	}
	return a, nil
}

// compileExpressions compiles the block's four CEL settings, applying the
// defaults that make split -> aggregate work with no configuration.
func (a *aggregate) compileExpressions(b *builder, cfg types.BlockConfig) error {
	correlation := cfg.Correlation
	if correlation == "" {
		correlation = defaultCorrelation
	}
	var err error
	if a.correlation, err = expr.CompileMessage(b.deps.Resources, correlation); err != nil {
		return fmt.Errorf("aggregate correlation: %w", err)
	}

	completionSize := cfg.CompletionSize
	if completionSize == "" {
		completionSize = defaultCompletionSize
	}
	if a.completionSize, err = expr.CompileMessage(b.deps.Resources, completionSize); err != nil {
		return fmt.Errorf("aggregate completionSize: %w", err)
	}

	if cfg.CompletionExpression != "" {
		if a.completionExpr, err = expr.CompileMessage(b.deps.Resources, cfg.CompletionExpression); err != nil {
			return fmt.Errorf("aggregate completionExpression: %w", err)
		}
	}
	return a.resolveStrategy(b, cfg)
}

// resolveStrategy picks how messages are combined. The two strategies each own
// exactly one setting, so declaring the other one is a mistake worth naming
// rather than ignoring.
func (a *aggregate) resolveStrategy(b *builder, cfg types.BlockConfig) error {
	switch cfg.Strategy {
	case "", aggregateStrategyAppend:
		a.appendMode = true
		if cfg.Expression != "" {
			return errors.New("aggregate block in append strategy must not declare an expression")
		}
	case aggregateStrategyExpression:
		if cfg.Expression == "" {
			return errors.New("aggregate block in expression strategy requires an expression")
		}
		fold, err := expr.CompileMessage(b.deps.Resources, cfg.Expression)
		if err != nil {
			return fmt.Errorf("aggregate expression: %w", err)
		}
		a.fold = fold
	default:
		return fmt.Errorf("aggregate block strategy %q must be %q or %q",
			cfg.Strategy, aggregateStrategyAppend, aggregateStrategyExpression)
	}
	return nil
}

// resolveCompletion validates the timeout and overflow settings, which are
// enumerations rather than expressions.
func (a *aggregate) resolveCompletion(cfg types.BlockConfig) error {
	if cfg.CompletionTimeout != "" {
		timeout, err := time.ParseDuration(cfg.CompletionTimeout)
		if err != nil {
			return fmt.Errorf("aggregate completionTimeout: %w", err)
		}
		if timeout <= 0 {
			return errors.New("aggregate completionTimeout must be positive")
		}
		a.timeout = timeout
	}

	switch cfg.TimeoutFrom {
	case "", aggregateTimeoutFromFirst:
		a.timeoutFrom = aggregateTimeoutFromFirst
	case aggregateTimeoutFromLast:
		a.timeoutFrom = aggregateTimeoutFromLast
	default:
		return fmt.Errorf("aggregate block timeoutFrom %q must be %q or %q",
			cfg.TimeoutFrom, aggregateTimeoutFromFirst, aggregateTimeoutFromLast)
	}

	switch cfg.OnOverflow {
	case "", aggregateOverflowFail:
		a.onOverflow = aggregateOverflowFail
	case aggregateOverflowComplete, aggregateOverflowDrop:
		a.onOverflow = cfg.OnOverflow
	default:
		return fmt.Errorf("aggregate block onOverflow %q must be one of %q, %q, %q",
			cfg.OnOverflow, aggregateOverflowFail, aggregateOverflowComplete, aggregateOverflowDrop)
	}
	return nil
}

// Start acquires leadership and begins the reaper, but only for an aggregate
// that can time out: with no timeout there is nothing to fire on a timer and so
// nothing to elect a leader for.
//
// Leadership gates the reaper's work rather than this startup, following the
// cron source: every replica campaigns and stands by, so whichever one holds the
// key sweeps and the rest take over the moment it stops.
func (a *aggregate) Start(ctx context.Context) error {
	if a.timeout <= 0 {
		return nil
	}
	lease, err := core.RuntimeServicesFromContext(ctx).LeaderElection().Acquire(ctx, a.leaderKey)
	if err != nil {
		return fmt.Errorf("aggregate: acquire leadership for %q: %w", a.leaderKey, err)
	}
	a.lease = lease
	a.wg.Add(1)
	go a.reap(ctx)
	return nil
}

// Stop quiets the reaper and waits for an in-flight sweep, so nothing tries to
// resume work after the flow's pool is torn down, then releases the key for
// another replica.
func (a *aggregate) Stop(context.Context) error {
	a.stopOnce.Do(func() { close(a.done) })
	a.wg.Wait()
	if a.lease != nil {
		return a.lease.Close()
	}
	return nil
}

// Process folds the message into its group and hands the caller an answer. The
// message goes no further down this chain: the blocks after this one run once
// per completed group, through the continuation, not once per message.
func (a *aggregate) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	key, err := a.correlation.EvalString(expr.MessageActivation(msg, a.env))
	if err != nil {
		return nil, fmt.Errorf(
			"aggregate correlation: %w (set the correlation expression when messages do not come from a split)",
			err)
	}
	if key == "" {
		return nil, errors.New("aggregate correlation expression evaluated to empty")
	}

	dropped, err := a.foldMessage(ctx, msg, key)
	if err != nil {
		return nil, err
	}
	if dropped {
		return nil, nil
	}
	return applyTakeover(ctx, msg, a.buildRespone)
}

// foldMessage adds the message to its group, releasing the group when a
// condition is met. It is the optimistic read-modify-write the object-write
// block established: a version conflict means another replica got there first,
// so re-read and fold onto its result rather than overwrite it. That is what
// makes the group correct across replicas without any of them coordinating.
//
// dropped reports that an overflow policy discarded the message.
func (a *aggregate) foldMessage(ctx context.Context, msg *types.Message, key string) (dropped bool, err error) {
	hash := hashedKey(key)

	// Admission comes first, and outside the stripe lock. That ordering is
	// load-bearing: under onOverflow: complete, claiming a slot may evict another
	// group, and evicting one takes the evicted group's stripe. Stripes are shared
	// by design, so holding this group's across an eviction would deadlock outright
	// whenever the two collide.
	admitted, claimed, err := a.reserve(ctx, hash)
	if err != nil {
		return false, err
	}
	if !admitted {
		return true, nil
	}

	completed, reason, err := a.foldLocked(ctx, msg, key, hash)
	if err != nil {
		// A slot claimed for a group the fold never went on to create would be held
		// forever: there is no group behind it, so nothing completes it and no
		// deadline expires it.
		if claimed {
			a.forgetGroup(ctx, hash)
		}
		return false, err
	}
	if completed == nil {
		return false, nil
	}
	return false, a.deliver(ctx, hash, completed, reason)
}

// foldLocked is the fold itself, holding the group's stripe for the whole
// optimistic loop. It is the read-modify-write the object-write block
// established: a version conflict means another replica got there first, so
// re-read and fold onto its result rather than overwrite it. That is what makes
// the group correct across replicas without any of them coordinating.
//
// A completed group is returned rather than delivered, and the reason it
// completed with it. Delivering it here would run the whole downstream flow
// under the stripe — resume is caller-runs, so the tail can execute on this very
// goroutine — and every unrelated group hashing to the same stripe would queue
// behind it.
func (a *aggregate) foldLocked(
	ctx context.Context, msg *types.Message, key, hash string,
) (*groupState, string, error) {
	kv := core.RuntimeServicesFromContext(ctx).KV()
	stateKey := a.groupStateKey(hash)

	defer a.folds.lock(hash)()

	for range aggregateWriteAttempts {
		entry, found, err := kv.Get(ctx, core.NamespaceSystem, stateKey)
		if err != nil {
			return nil, "", fmt.Errorf("aggregate: read group %q: %w", key, err)
		}

		state, err := a.loadGroup(msg, key, entry, found)
		if err != nil {
			return nil, "", err
		}

		reason, complete, err := a.applyMessage(msg, &state)
		if err != nil {
			return nil, "", err
		}

		var conflict bool
		if complete {
			conflict, err = a.commitComplete(ctx, hash, stateKey, entry, found)
		} else {
			conflict, err = a.commitOpen(ctx, hash, stateKey, entry, &state)
		}
		if conflict {
			// Another writer got there first; re-read and fold onto their result
			// rather than overwrite it.
			continue
		}
		if err != nil {
			return nil, "", fmt.Errorf("aggregate: group %q: %w", key, err)
		}
		if complete {
			return &state, reason, nil
		}
		return nil, "", nil
	}
	return nil, "", fmt.Errorf("%w: %q after %d attempts", errGroupContended, key, aggregateWriteAttempts)
}

// deliver continues the flow with a completed group, putting the group back if
// the continuation will not take it.
//
// The group is out of the store before this runs, and has to be: a message
// arriving during the handover must open a fresh group rather than fold into one
// already on its way out. So a refused delivery is the one case where the
// accumulated messages exist nowhere but in this call, and writing them back is
// the only alternative to losing them with the error.
func (a *aggregate) deliver(ctx context.Context, hash string, state *groupState, reason string) error {
	if err := a.release(ctx, state, reason); err != nil {
		a.restore(ctx, hash, state)
		return err
	}
	return nil
}

// restore puts a group back after a failed delivery, so what it holds can still
// be retried or reaped.
//
// It is a create, not a write: if a message has already opened a new group under
// the same key, that group is the live one and merging into it is not this
// call's business. Losing the old one is then real, so it is logged with what it
// was holding rather than passed over.
func (a *aggregate) restore(ctx context.Context, hash string, state *groupState) {
	defer a.folds.lock(hash)()

	encoded, err := json.Marshal(state)
	if err == nil {
		kv := core.RuntimeServicesFromContext(ctx).KV()
		_, err = kv.Set(ctx, core.NamespaceSystem, a.groupStateKey(hash), encoded, 0)
	}
	if err != nil {
		slog.Error("aggregate could not put back a group whose delivery failed",
			"storeKey", a.storeKey, "key", state.Key, "count", state.Count, "error", err)
		return
	}
	a.trackGroup(ctx, hash, state)
}

// loadGroup decodes the stored group, or opens a new one. Its slot in the index
// has already been claimed by the time this runs.
func (a *aggregate) loadGroup(
	msg *types.Message, key string, entry core.Entry, found bool,
) (groupState, error) {
	state := groupState{Key: key, Size: sizeUnknown}
	if found {
		if err := json.Unmarshal(entry.Value, &state); err != nil {
			return state, fmt.Errorf("aggregate: decode group %q: %w", key, err)
		}
		return state, nil
	}
	state.CorrelationID = msg.CorrelationID
	state.FirstAt = nowNanos()
	return state, nil
}

// commitComplete takes a group out of the store once a condition has satisfied
// it, leaving the caller to deliver it. A group that completed on its very first
// message was never stored, so there is nothing to delete — but its slot was
// still claimed, so the index is cleared either way.
func (a *aggregate) commitComplete(
	ctx context.Context, hash, stateKey string, entry core.Entry, stored bool,
) (conflict bool, err error) {
	if stored {
		kv := core.RuntimeServicesFromContext(ctx).KV()
		if err := kv.Delete(ctx, core.NamespaceSystem, stateKey, entry.Version); err != nil {
			if errors.Is(err, core.ErrVersionConflict) {
				return true, nil
			}
			return false, fmt.Errorf("release: %w", err)
		}
	}
	a.forgetGroup(ctx, hash)
	return false, nil
}

// commitOpen writes back a group that is still waiting for more messages.
func (a *aggregate) commitOpen(
	ctx context.Context, hash, stateKey string, entry core.Entry, state *groupState,
) (conflict bool, err error) {
	encoded, err := json.Marshal(state)
	if err != nil {
		return false, fmt.Errorf("encode: %w", err)
	}
	kv := core.RuntimeServicesFromContext(ctx).KV()
	if _, err := kv.Set(ctx, core.NamespaceSystem, stateKey, encoded, entry.Version); err != nil {
		if errors.Is(err, core.ErrVersionConflict) {
			return true, nil
		}
		return false, fmt.Errorf("write: %w", err)
	}
	a.trackGroup(ctx, hash, state)
	return false, nil
}

// applyMessage folds one message into the group and decides whether the group is
// now complete.
//
// The order the expressions see the group in is fixed and load-bearing: the fold
// runs against the group *before* this message, because it is computing the
// accumulator that includes it; the completion checks run *after*, because they
// are answering "are we done now".
func (a *aggregate) applyMessage(msg *types.Message, state *groupState) (string, bool, error) {
	acc, err := a.accumulate(msg, state)
	if err != nil {
		return "", false, err
	}

	state.Acc = acc
	state.Count++
	state.LastAt = nowNanos()
	state.Deadline = a.deadline(state)

	if err := a.learnSize(msg, state); err != nil {
		return "", false, err
	}
	if state.Size >= 0 && state.Count >= state.Size {
		return aggregateReasonSize, true, nil
	}
	if a.completionExpr != nil {
		done, err := evalCondition(a.completionExpr, a.withGroup(msg, state), a.env)
		if err != nil {
			return "", false, fmt.Errorf("aggregate completionExpression: %w", err)
		}
		if done {
			return aggregateReasonExpression, true, nil
		}
	}
	return "", false, nil
}

// accumulate produces the group's new accumulator.
func (a *aggregate) accumulate(msg *types.Message, state *groupState) (any, error) {
	if a.appendMode {
		return placeInOrder(state.Acc, msg, state), nil
	}
	value, err := a.fold.Eval(expr.MessageActivation(a.withGroup(msg, state), a.env))
	if err != nil {
		return nil, fmt.Errorf("aggregate expression: %w", err)
	}
	// Normalize to decoded-JSON kinds, so an accumulator behaves the same on the
	// fold that produced it as on the one that reads it back out of the store.
	return normalizeJSON(value)
}

// placeInOrder adds a message's body to the appended accumulator.
//
// A message from a split carries its position, and is put there rather than on
// the end. Elements run concurrently and finish in whatever order their work
// takes, so appending on arrival would re-join a split into a different order
// every run — the same input yielding a differently-shuffled array each time.
// The position is already on the message; honoring it costs nothing and makes
// the result deterministic.
//
// A message with no position — events batched off a source, which were never a
// collection to begin with — is appended, since arrival is the only order it
// has. So is one whose position is not plausible for this group: groupIndex is
// an ordinary message variable that anything upstream can set, and padding to
// whatever it names would let one message allocate a slice of that size under
// the fold lock and then encode it into the store.
//
// A group that completes without every position filled (a timeout with an
// element still missing) leaves a null in the gap rather than a shorter array.
// That matches how foreach's map mode reports a dropped iteration, and it says
// *which* element is missing instead of quietly renumbering the rest.
func placeInOrder(acc any, msg *types.Message, state *groupState) []any {
	items, _ := acc.([]any)

	index, positioned := msg.Variables.Int(varGroupIndex)
	if !positioned || index < 0 || index >= positionLimit(msg, state) {
		return append(items, msg.Body)
	}
	for len(items) <= index {
		items = append(items, nil)
	}
	items[index] = msg.Body
	return items
}

// positionLimit is the exclusive bound on a position the accumulator will pad
// to: the group's expected size where it is known, and a fixed ceiling where it
// is not. The ceiling still applies to a known size, since that size is a
// message variable too.
//
// The group's own size is preferred over the message's because it is sticky —
// learned once and not re-read from every arrival — but on the first message of
// a group it has not been learned yet, which is why the message's is consulted
// at all.
func positionLimit(msg *types.Message, state *groupState) int {
	if state.Size > 0 {
		return min(state.Size, maxPositionedIndex)
	}
	if size, ok := msg.Variables.Int(varGroupSize); ok && size > 0 {
		return min(size, maxPositionedIndex)
	}
	return maxPositionedIndex
}

// learnSize records the group's expected total the first time a message reveals
// it. It is sticky: a size learned once is not un-learned by a later message
// that does not carry one.
func (a *aggregate) learnSize(msg *types.Message, state *groupState) error {
	if state.Size >= 0 {
		return nil
	}
	value, err := a.completionSize.Eval(expr.MessageActivation(a.withGroup(msg, state), a.env))
	if err != nil {
		return fmt.Errorf("aggregate completionSize: %w", err)
	}
	if size, ok := asInt(value); ok && size > 0 {
		state.Size = size
	}
	return nil
}

// withGroup returns the message an expression is evaluated against: a scoped copy
// carrying the group under vars.group. Scoped rather than cloned because the copy
// never leaves this goroutine, so there is no reason to pay for the body.
func (a *aggregate) withGroup(msg *types.Message, state *groupState) *types.Message {
	scoped := msg.Scoped()
	scoped.Variables.Set(varGroup, state.activation())
	return scoped
}

// activation is the group as CEL sees it. ageMs and idleMs are pre-computed
// because they are what a predicate actually asks for; the timestamps are there
// for the cases they are not.
func (g *groupState) activation() map[string]any {
	now := nowNanos()
	return map[string]any{
		"key":     g.Key,
		"count":   g.Count,
		"size":    g.Size,
		"acc":     g.Acc,
		"firstAt": time.Unix(0, g.FirstAt).UTC().Format(time.RFC3339),
		"lastAt":  time.Unix(0, g.LastAt).UTC().Format(time.RFC3339),
		"ageMs":   (now - g.FirstAt) / int64(time.Millisecond),
		"idleMs":  (now - g.LastAt) / int64(time.Millisecond),
	}
}

// deadline is when this group times out, or 0 when it has no timeout. Measuring
// from the last message makes it a sliding idle window rather than a fixed one.
func (a *aggregate) deadline(state *groupState) int64 {
	if a.timeout <= 0 {
		return 0
	}
	from := state.FirstAt
	if a.timeoutFrom == aggregateTimeoutFromLast {
		from = state.LastAt
	}
	return from + int64(a.timeout)
}

// release builds the aggregated message and continues the flow with it.
//
// The group variables are deliberately not carried over: they described the group
// that just closed, and forwarding them would make a second aggregation stage put
// every message in a batch of one. The key survives as vars.aggregateKey.
func (a *aggregate) release(ctx context.Context, state *groupState, reason string) error {
	out, err := types.NewMessage(state.CorrelationID)
	if err != nil {
		return err
	}
	out.Body = state.Acc
	out.Variables.Set(varAggregateKey, state.Key)
	out.Variables.Set(varAggregateCount, state.Count)
	out.Variables.Set(varAggregateReason, reason)

	if err := a.cont.Resume(ctx, out); err != nil {
		return fmt.Errorf("aggregate: continue with group %q: %w", state.Key, err)
	}
	return nil
}

// reserve claims this group's slot in the index before the group is opened, and
// is what makes maxGroups a cap rather than a suggestion. Reading the count and
// writing the group afterwards would let every message in a burst of new groups
// see the same room, all of which only one of them was entitled to: the folds of
// two different groups are not serialized against each other, by design. Here
// the check and the claim are one compare-and-swap, so exactly maxGroups of them
// win and the rest meet the overflow policy.
//
// admitted is false when the policy discarded the message. claimed reports that
// this call is the one that put the group in the index, so a fold that then
// fails can give the slot back.
func (a *aggregate) reserve(ctx context.Context, hash string) (admitted, claimed bool, err error) {
	for range aggregateWriteAttempts {
		claimed, full, err := a.claimSlot(ctx, hash)
		if err != nil {
			return false, false, err
		}
		if !full {
			return true, claimed, nil
		}
		// At capacity. The policy runs with the index lock released, because
		// releasing a group takes that group's fold stripe and rewrites the index
		// itself. If it makes room, go back and claim what opened up.
		admitted, err := a.overflow(ctx)
		if err != nil || !admitted {
			return false, false, err
		}
	}
	return false, false, fmt.Errorf("%w: group index of %q", errGroupContended, a.storeKey)
}

// claimSlot puts the group in the index when it is not already there and there
// is room for it, reporting which of the two happened. full means the block is
// at capacity and the caller must apply the overflow policy.
func (a *aggregate) claimSlot(ctx context.Context, hash string) (claimed, full bool, err error) {
	a.index.Lock()
	defer a.index.Unlock()

	for range aggregateWriteAttempts {
		index, version, err := a.readIndex(ctx)
		if err != nil {
			return false, false, err
		}
		if _, held := index.Groups[hash]; held {
			// Already counted — an open group, or one whose slot an earlier message
			// claimed. Either way there is nothing to admit.
			return false, false, nil
		}
		if len(index.Groups) >= a.maxGroups {
			return false, true, nil
		}

		index.Groups[hash] = groupEntry{FirstAt: nowNanos()}
		conflict, err := a.writeIndex(ctx, index, version)
		if err != nil {
			return false, false, err
		}
		if !conflict {
			return true, false, nil
		}
	}
	return false, false, fmt.Errorf("%w: group index of %q", errGroupContended, a.storeKey)
}

// overflow applies the policy for a block already holding maxGroups groups. It
// reports whether the message may go on to open a group.
func (a *aggregate) overflow(ctx context.Context) (bool, error) {
	switch a.onOverflow {
	case aggregateOverflowDrop:
		slog.Warn("aggregate at capacity: message dropped",
			"storeKey", a.storeKey, "maxGroups", a.maxGroups)
		return false, nil
	case aggregateOverflowComplete:
		index, _, err := a.readIndex(ctx)
		if err != nil {
			return false, err
		}
		if err := a.releaseOldest(ctx, index); err != nil {
			return false, err
		}
		return true, nil
	default:
		return false, fmt.Errorf(
			"aggregate: %d groups open (maxGroups); raise maxGroups, narrow the correlation, or set onOverflow",
			a.maxGroups)
	}
}

// releaseOldest completes the group that has been open the longest to make room.
// It emits a partial group, which is why it is not the default policy.
//
// Ordering is by when the group opened rather than by when it expires, because
// an aggregate that completes on size or on a predicate has no deadlines at all
// and every entry would compare equal — leaving the choice to Go's randomized
// map iteration. Ties break on the hash so the victim is at least deterministic.
func (a *aggregate) releaseOldest(ctx context.Context, index groupIndex) error {
	var (
		oldest  string
		firstAt int64
	)
	for hash, entry := range index.Groups {
		if oldest == "" || entry.FirstAt < firstAt || (entry.FirstAt == firstAt && hash < oldest) {
			oldest, firstAt = hash, entry.FirstAt
		}
	}
	if oldest == "" {
		return nil
	}
	return a.completeStored(ctx, oldest, aggregateReasonOverflow)
}

// reap runs the timeout sweep on the leader. The tick is what makes a
// time-capped group possible at all: it is the only thing in the runtime that
// fires when no message arrives.
func (a *aggregate) reap(ctx context.Context) {
	defer a.wg.Done()

	ticker := time.NewTicker(a.reapInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-a.done:
			return
		case <-ticker.C:
			a.sweep(ctx)
		}
	}
}

// reapInterval checks often enough to honor the configured window without
// hammering the store for a long one.
func (a *aggregate) reapInterval() time.Duration {
	interval := a.timeout / 2
	if interval < minReapInterval {
		return minReapInterval
	}
	if interval > maxReapInterval {
		return maxReapInterval
	}
	return interval
}

// sweep completes every group past its deadline. It is skipped entirely while
// this replica does not hold the key, so a group is reaped once across the
// cluster rather than once per replica.
func (a *aggregate) sweep(ctx context.Context) {
	if a.lease != nil && !a.lease.IsLeader() {
		return
	}
	index, _, err := a.readIndex(ctx)
	if err != nil {
		slog.Error("aggregate sweep failed to read the group index", "storeKey", a.storeKey, "error", err)
		return
	}

	now := nowNanos()
	for hash, entry := range index.Groups {
		if entry.Deadline == 0 {
			// This aggregate has a timeout, so every group it opens gets a deadline.
			// An entry without one is a slot claimed for a group that never appeared.
			a.reclaimStale(ctx, hash)
			continue
		}
		if entry.Deadline > now {
			continue
		}
		if err := a.completeStored(ctx, hash, aggregateReasonTimeout); err != nil {
			slog.Error("aggregate failed to complete a timed-out group",
				"storeKey", a.storeKey, "error", err)
		}
	}
}

// reclaimStale gives back a slot held by no group: a claim whose fold died with
// the process, or an entry whose removal ran out of retries. Left alone it would
// count against maxGroups forever, since there is nothing behind it to complete
// or expire.
//
// The group is checked rather than assumed absent, so a real group — one carried
// over from a generation of the config that had no completionTimeout, and so no
// deadline — is left where it is. A claim that is merely in flight may be
// reclaimed here, which costs nothing: the fold re-tracks the group when it
// commits, moments later.
func (a *aggregate) reclaimStale(ctx context.Context, hash string) {
	defer a.folds.lock(hash)()

	kv := core.RuntimeServicesFromContext(ctx).KV()
	_, found, err := kv.Get(ctx, core.NamespaceSystem, a.groupStateKey(hash))
	if err != nil || found {
		return
	}
	a.forgetGroup(ctx, hash)
}

// completeStored releases a group read straight from the store, for the paths
// that have no message in hand: the timeout sweep and the overflow policy.
func (a *aggregate) completeStored(ctx context.Context, hash, reason string) error {
	state, err := a.takeStored(ctx, hash)
	if err != nil || state == nil {
		return err
	}
	// Delivered with the stripe released, for the reason foldLocked gives.
	return a.deliver(ctx, hash, state, reason)
}

// takeStored lifts a group out of the store, or reports that there is nothing to
// take. The delete is version-checked, so a group another replica completed
// first is simply gone and this call does nothing.
func (a *aggregate) takeStored(ctx context.Context, hash string) (*groupState, error) {
	kv := core.RuntimeServicesFromContext(ctx).KV()
	stateKey := a.groupStateKey(hash)

	// Take the same stripe a fold would, so this cannot delete a group out from
	// under a message that is mid-fold on this replica.
	defer a.folds.lock(hash)()

	entry, found, err := kv.Get(ctx, core.NamespaceSystem, stateKey)
	if err != nil {
		return nil, fmt.Errorf("aggregate: read group: %w", err)
	}
	if !found {
		a.forgetGroup(ctx, hash)
		return nil, nil
	}
	var state groupState
	if err := json.Unmarshal(entry.Value, &state); err != nil {
		return nil, fmt.Errorf("aggregate: decode group: %w", err)
	}
	if err := kv.Delete(ctx, core.NamespaceSystem, stateKey, entry.Version); err != nil {
		if errors.Is(err, core.ErrVersionConflict) {
			return nil, nil
		}
		return nil, fmt.Errorf("aggregate: release group: %w", err)
	}
	a.forgetGroup(ctx, hash)
	return &state, nil
}

// groupStateKey and indexKey namespace everything this block stores under its
// storeKey, which is what keeps two aggregate blocks — and two generations of a
// reloaded flow — out of each other's groups.
func (a *aggregate) groupStateKey(hash string) string {
	return "aggregate/" + hashedKey(a.storeKey) + "/g/" + hash
}

func (a *aggregate) indexKey() string {
	return "aggregate/" + hashedKey(a.storeKey) + "/index"
}

func (a *aggregate) readIndex(ctx context.Context) (groupIndex, int64, error) {
	kv := core.RuntimeServicesFromContext(ctx).KV()
	entry, found, err := kv.Get(ctx, core.NamespaceSystem, a.indexKey())
	if err != nil {
		return groupIndex{}, 0, fmt.Errorf("aggregate: read group index: %w", err)
	}
	index := groupIndex{Groups: map[string]groupEntry{}}
	if found {
		if err := json.Unmarshal(entry.Value, &index); err != nil {
			return groupIndex{}, 0, fmt.Errorf("aggregate: decode group index: %w", err)
		}
		if index.Groups == nil {
			index.Groups = map[string]groupEntry{}
		}
	}
	return index, entry.Version, nil
}

// writeIndex stores the index at the version it was read at, reporting a lost
// race rather than treating it as a failure.
func (a *aggregate) writeIndex(ctx context.Context, index groupIndex, version int64) (conflict bool, err error) {
	encoded, err := json.Marshal(index)
	if err != nil {
		return false, fmt.Errorf("aggregate: encode group index: %w", err)
	}
	kv := core.RuntimeServicesFromContext(ctx).KV()
	if _, err := kv.Set(ctx, core.NamespaceSystem, a.indexKey(), encoded, version); err != nil {
		if errors.Is(err, core.ErrVersionConflict) {
			return true, nil
		}
		return false, fmt.Errorf("aggregate: write group index: %w", err)
	}
	return false, nil
}

// trackGroup and forgetGroup keep the reaper's index in step with the groups.
// A failure to update it is logged rather than surfaced: the message has already
// been folded successfully, so failing the flow would misreport what happened.
// The cost of a stale entry is bounded — a missing one delays a timeout until
// the group is written again, a leftover one resolves on the next sweep.
func (a *aggregate) trackGroup(ctx context.Context, hash string, state *groupState) {
	// The reservation carried the moment the slot was claimed; correct it to when
	// the group actually opened, so the overflow policy orders by the group's own
	// clock rather than by an approximation of it.
	want := groupEntry{FirstAt: state.FirstAt, Deadline: state.Deadline}
	a.updateIndex(ctx, func(index *groupIndex) bool {
		if entry, ok := index.Groups[hash]; ok && entry == want {
			return false
		}
		index.Groups[hash] = want
		return true
	})
}

func (a *aggregate) forgetGroup(ctx context.Context, hash string) {
	a.updateIndex(ctx, func(index *groupIndex) bool {
		if _, ok := index.Groups[hash]; !ok {
			return false
		}
		delete(index.Groups, hash)
		return true
	})
}

// updateIndex applies mutate under the same optimistic loop the groups use.
func (a *aggregate) updateIndex(ctx context.Context, mutate func(*groupIndex) bool) {
	a.index.Lock()
	defer a.index.Unlock()

	for range aggregateWriteAttempts {
		index, version, err := a.readIndex(ctx)
		if err != nil {
			slog.Error("aggregate group index", "storeKey", a.storeKey, "error", err)
			return
		}
		if !mutate(&index) {
			return
		}
		conflict, err := a.writeIndex(ctx, index, version)
		if err != nil {
			slog.Error("aggregate group index", "storeKey", a.storeKey, "error", err)
			return
		}
		if conflict {
			continue
		}
		return
	}
	slog.Warn("aggregate group index contended; a timeout may be delayed", "storeKey", a.storeKey)
}

func nowNanos() int64 { return time.Now().UnixNano() }

// asInt coerces a CEL result to an int, accepting the numeric shapes an
// expression or a JSON round-trip can produce.
func asInt(value any) (int, bool) {
	switch n := value.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case uint64:
		if n > math.MaxInt {
			return 0, false
		}
		return int(n), true
	case float64:
		if n == float64(int(n)) {
			return int(n), true
		}
	}
	return 0, false
}

// normalizeJSON round-trips a value through JSON so it holds decoded-JSON kinds,
// matching what the message body contract promises and what the value will look
// like once it has been through the store.
func normalizeJSON(value any) (any, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode value: %w", err)
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("decode value: %w", err)
	}
	return decoded, nil
}
