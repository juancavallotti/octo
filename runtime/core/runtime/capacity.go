package runtime

import (
	"fmt"
	"log/slog"

	"github.com/juancavallotti/octo/runtime/core"
)

// capacityBound is one component a flow drives whose concurrency is capped
// independently of the flow's workers.
type capacityBound struct {
	component string
	capacity  core.Capacity
}

// capacityCheck collects, while a flow is built, the capacity of every connector
// its blocks bind to and of its source. Blocks resolve their connectors in their
// factories, so wrapping the lookup they are handed sees every binding without
// the runtime reading any block's settings.
type capacityCheck struct {
	bounds []capacityBound
	seen   map[string]bool
}

func newCapacityCheck() *capacityCheck {
	return &capacityCheck{seen: make(map[string]bool)}
}

// lookup wraps a connector lookup so each connector a block resolves is recorded.
func (c *capacityCheck) lookup(
	next func(string) (core.Connector, bool),
) func(string) (core.Connector, bool) {
	return func(name string) (core.Connector, bool) {
		conn, ok := next(name)
		if ok {
			c.add(name, conn)
		}
		return conn, ok
	}
}

// add records component when it reports a bounded capacity. A connector bound by
// several blocks is recorded once.
func (c *capacityCheck) add(component string, v any) {
	reporter, ok := v.(core.CapacityReporter)
	if !ok || c.seen[component] {
		return
	}
	c.seen[component] = true
	if capacity := reporter.Capacity(); capacity.Limit > 0 {
		c.bounds = append(c.bounds, capacityBound{component: component, capacity: capacity})
	}
}

// overCapacity returns the components whose capacity is below the flow's
// workers: the excess workers queue on the component rather than running, which
// otherwise surfaces only as latency.
func (bf *boundFlow) overCapacity() []capacityBound {
	var over []capacityBound
	for _, bound := range bf.capacity {
		if bf.workers > bound.capacity.Limit {
			over = append(over, bound)
		}
	}
	return over
}

// warnCapacity logs one warning per component the flow's workers outnumber.
func (bf *boundFlow) warnCapacity() {
	for _, bound := range bf.overCapacity() {
		slog.Warn("flow workers exceed connector capacity",
			"flow", bf.name, "connector", bound.component,
			"workers", bf.workers, "workersOrigin", workersOrigin(bf.workersDerived),
			bound.capacity.Setting, bound.capacity.Limit,
			"fix", fmt.Sprintf("raise %s to %d, or set the flow's workers to %d",
				bound.capacity.Setting, bf.workers, bound.capacity.Limit))
	}
}
