package observability

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/juancavallotti/octo/runtime/services"
	"github.com/juancavallotti/octo/runtime/types"
)

// liveQueues answers from a slice the test changes between scrapes, the way a
// running flow's channel fills and drains between them.
type liveQueues struct{ queues []types.FlowQueue }

func (l *liveQueues) Queues() []types.FlowQueue { return l.queues }

const queueHeader = `
	# HELP octo_flow_queue_capacity How many messages a flow's buffer holds before its source blocks.
	# TYPE octo_flow_queue_capacity gauge
`

func TestQueueGaugesReadTheRunningFlowsAtScrapeTime(t *testing.T) {
	m := newMetrics(services.NewHealth())
	live := &liveQueues{queues: []types.FlowQueue{{Flow: "orders", Depth: 0, Capacity: 64}}}
	m.queues.observe(live)

	live.queues[0].Depth = 12
	const want = queueHeader + `
	octo_flow_queue_capacity{flow="orders"} 64
	# HELP octo_flow_queue_depth Messages a flow's source has handed over that no worker has picked up yet. At capacity, the source is blocked on its next message.
	# TYPE octo_flow_queue_depth gauge
	octo_flow_queue_depth{flow="orders"} 12
	`
	if err := testutil.CollectAndCompare(m.queues, strings.NewReader(want)); err != nil {
		t.Error(err)
	}
}

// A reload must not leave the previous generation's flows behind: the stopped
// generation is withdrawn, and the next one replaces it.
func TestQueueGaugesFollowTheGeneration(t *testing.T) {
	m := newMetrics(services.NewHealth())
	m.queues.observe(&liveQueues{queues: []types.FlowQueue{{Flow: "orders"}, {Flow: "audit"}}})

	m.queues.observe(nil)
	if got := testutil.CollectAndCount(m.queues); got != 0 {
		t.Errorf("series between generations = %d, want none", got)
	}

	m.queues.observe(&liveQueues{queues: []types.FlowQueue{{Flow: "orders", Capacity: 8}}})
	const want = queueHeader + `
	octo_flow_queue_capacity{flow="orders"} 8
	`
	if err := testutil.CollectAndCompare(m.queues, strings.NewReader(want), "octo_flow_queue_capacity"); err != nil {
		t.Error(err)
	}
}
