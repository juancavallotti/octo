package runtime

import (
	"context"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

type plainConnector struct{}

func (plainConnector) Start(context.Context, types.ConnectorConfig) error { return nil }
func (plainConnector) Stop(context.Context) error                         { return nil }

type boundedConnector struct {
	plainConnector

	capacity core.Capacity
}

func (c boundedConnector) Capacity() core.Capacity { return c.capacity }

func TestCapacityCheckRecordsBoundedConnectorsOnce(t *testing.T) {
	connectors := map[string]core.Connector{
		"db":    boundedConnector{capacity: core.Capacity{Limit: 16, Setting: "maxOpenConns"}},
		"mongo": boundedConnector{capacity: core.Capacity{Setting: "maxPoolSize"}},
		"log":   plainConnector{},
	}
	check := newCapacityCheck()
	lookup := check.lookup(func(name string) (core.Connector, bool) {
		c, ok := connectors[name]
		return c, ok
	})
	for _, name := range []string{"db", "db", "mongo", "log", "missing"} {
		lookup(name)
	}

	if len(check.bounds) != 1 {
		t.Fatalf("bounds = %+v, want only the bounded db connector", check.bounds)
	}
	if got := check.bounds[0]; got.component != "db" || got.capacity.Limit != 16 {
		t.Errorf("bound = %+v, want db at 16", got)
	}
}

func TestOverCapacity(t *testing.T) {
	bounds := []capacityBound{
		{component: "pool", capacity: core.Capacity{Limit: 16, Setting: "maxOpenConns"}},
		{component: "listeners", capacity: core.Capacity{Limit: 32, Setting: "listeners"}},
	}
	cases := []struct {
		name    string
		workers int
		want    int
	}{
		{"within every capacity", 16, 0},
		{"above the smaller one", 32, 1},
		{"above both", 64, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bf := &boundFlow{name: "orders", workers: tc.workers, capacity: bounds}
			if got := bf.overCapacity(); len(got) != tc.want {
				t.Errorf("overCapacity() at %d workers = %+v, want %d", tc.workers, got, tc.want)
			}
		})
	}
}
