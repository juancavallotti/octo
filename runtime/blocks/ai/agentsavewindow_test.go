package ai

import (
	"context"
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// slowLLM answers every turn, taking its time over it — enough time that a save
// deadline started before the turn loop would be measurably nearer than one
// started at the save.
type slowLLM struct{ delay time.Duration }

func (s *slowLLM) Start(context.Context, types.ConnectorConfig) error { return nil }
func (s *slowLLM) Stop(context.Context) error                         { return nil }
func (s *slowLLM) Complete(_ context.Context, _ core.LLMRequest) (*core.LLMResponse, error) {
	time.Sleep(s.delay)
	return endTurnResp("answered"), nil
}

// deadlineKV records the deadline carried by the context each Set was made on.
type deadlineKV struct {
	*fakeKV
	deadlines []time.Time
}

func (s *deadlineKV) Set(
	ctx context.Context, namespace, key string, value []byte, expectedVersion int64,
) (int64, error) {
	if at, ok := ctx.Deadline(); ok {
		s.deadlines = append(s.deadlines, at)
	} else {
		s.deadlines = append(s.deadlines, time.Time{})
	}
	return s.fakeKV.Set(ctx, namespace, key, value, expectedVersion)
}

// kvServices swaps one KV into an otherwise ordinary set of fake services.
type kvServices struct {
	core.RuntimeServices
	kv core.KV
}

//nolint:ireturn // satisfies the RuntimeServices interface
func (s kvServices) KV() core.KV { return s.kv }

// A run that takes longer than memorySaveTimeout still saves its transcript.
//
// The regression this pins cost a real conversation. The save context was derived
// once, before the turn loop, so its deadline covered the run as well as the save
// — and an agent that spends more than memorySaveTimeout calling tools (which is
// an ordinary interactive run, not a pathological one) reached its save with the
// clock already expired, stored nothing, and reported the loss as a single
// warning. The symptom was an agent that remembered short exchanges and forgot
// every long one.
//
// Asserted on the deadline rather than by outliving memorySaveTimeout, which
// would mean a two-minute test. The turn takes a measurable slice of time, so a
// deadline that started before it is that much nearer than one started at the
// save; the tolerance is what separates the two without being a race.
func TestAgentSavesAfterARunLongerThanTheSaveTimeout(t *testing.T) {
	const turn = 400 * time.Millisecond
	const tolerance = 200 * time.Millisecond

	base := newFakeKV()
	rec := &deadlineKV{fakeKV: base}
	ctx := core.ContextWithRuntimeServices(
		context.Background(), kvServices{RuntimeServices: fakeServices{Store: base}, kv: rec})

	agent := mustBuildAI(t, agentRegistry(new([]any)), depsLLM(&slowLLM{delay: turn}),
		memoryAgentConfig(`"slow"`))

	start := time.Now()
	if _, err := agent.Process(ctx, aiMessage(t)); err != nil {
		t.Fatalf("process: %v", err)
	}
	if elapsed := time.Since(start); elapsed < turn {
		t.Fatalf("the run finished in %v, faster than the turn it was meant to take", elapsed)
	}

	if len(rec.deadlines) == 0 {
		t.Fatal("the transcript was never written")
	}
	// The clock has to have started at the save: a deadline set before the turn
	// loop would already be `turn` closer than this.
	for i, at := range rec.deadlines {
		if at.IsZero() {
			t.Fatalf("save %d ran on a context with no deadline at all", i)
		}
		if left := time.Until(at); left < memorySaveTimeout-tolerance {
			t.Errorf("save %d had %v left of a %v budget: the deadline was started before the run, not at the save",
				i, left, memorySaveTimeout)
		}
	}

	stored, err := loadMemory(ctx, core.NamespaceUser, "slow")
	if err != nil {
		t.Fatalf("load memory: %v", err)
	}
	if !hasAssistantText(stored.Messages, "answered") {
		t.Fatalf("the run's answer is not in the stored transcript: %+v", stored.Messages)
	}
}
