package runtime

import (
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// Queues reads each started flow's input channel, so its capacity is the buffer
// the flow was built with; before the flows start there is nothing to read.
func TestQueuesReportStartedFlows(t *testing.T) {
	cfg := types.Config{Flows: []types.FlowConfig{{Name: "orders", Workers: 2, Buffer: 16}}}
	svc := NewService(cfg, core.DefaultRegistry(), WithInvokeMode())
	if got := svc.Queues(); got != nil {
		t.Fatalf("Queues before start = %v, want none", got)
	}

	stop := startService(t, svc)
	defer stop()

	got := svc.Queues()
	if len(got) != 1 || got[0] != (types.FlowQueue{Flow: "orders", Depth: 0, Capacity: 16}) {
		t.Errorf("Queues = %v, want orders with an empty buffer of 16", got)
	}
}
