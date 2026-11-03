package observability

import (
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/juancavallotti/octo/runtime/services"
)

// queueCollector reports the backlog in front of each running flow's workers.
// It reads the generation's input channels at scrape time rather than being fed
// per message, so it costs nothing on the hot path and stays true when every
// worker is blocked and no message is moving — the moment it matters most.
//
// It holds the generation rather than one gauge per flow, so a reload swaps what
// it reads and a flow the new config dropped simply stops being reported: there is
// no per-flow registration to undo. Between generations it holds nothing.
type queueCollector struct {
	flows    atomic.Pointer[services.RunningFlows]
	depth    *prometheus.Desc
	capacity *prometheus.Desc
}

func newQueueCollector() *queueCollector {
	return &queueCollector{
		depth: prometheus.NewDesc(prometheus.BuildFQName(namespace, "", "flow_queue_depth"),
			"Messages a flow's source has handed over that no worker has picked up yet. "+
				"At capacity, the source is blocked on its next message.",
			[]string{labelFlow}, nil),
		capacity: prometheus.NewDesc(prometheus.BuildFQName(namespace, "", "flow_queue_capacity"),
			"How many messages a flow's buffer holds before its source blocks.",
			[]string{labelFlow}, nil),
	}
}

// observe replaces the generation being read; nil reports no queues.
func (c *queueCollector) observe(flows services.RunningFlows) {
	if flows == nil {
		c.flows.Store(nil)
		return
	}
	c.flows.Store(&flows)
}

func (c *queueCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.depth
	ch <- c.capacity
}

func (c *queueCollector) Collect(ch chan<- prometheus.Metric) {
	flows := c.flows.Load()
	if flows == nil {
		return
	}
	for _, queue := range (*flows).Queues() {
		ch <- prometheus.MustNewConstMetric(c.depth, prometheus.GaugeValue, float64(queue.Depth), queue.Flow)
		ch <- prometheus.MustNewConstMetric(c.capacity, prometheus.GaugeValue, float64(queue.Capacity), queue.Flow)
	}
}
