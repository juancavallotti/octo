package dsl

import (
	"reflect"
	"testing"

	"github.com/juancavallotti/octo/types"
)

func TestParseFlowsWithComposites(t *testing.T) {
	const data = `
service:
  name: orders
connectors:
  - name: orders-kafka
    type: kafka
flows:
  - name: ingest-orders
    workers: 8
    buffer: 128
    source:
      connector: orders-kafka
      type: topic
      settings:
        topic: orders
    process:
      - type: validate
        settings:
          schema: order.schema.json
      - type: handle-errors
        name: persist
        process:
          - type: transform
            name: normalize
        error:
          - type: deadletter
      - type: fork
        name: notify-and-audit
        branches:
          - name: notify
            process:
              - type: email
          - name: audit
            process:
              - type: log
`

	config, err := Parse([]byte(data))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	want := types.Config{
		Service: types.ServiceConfig{Name: "orders"},
		Connectors: []types.ConnectorConfig{
			{Name: "orders-kafka", Type: "kafka"},
		},
		Flows: []types.FlowConfig{
			{
				Name:    "ingest-orders",
				Workers: 8,
				Buffer:  128,
				Source: &types.SourceConfig{
					Connector: "orders-kafka",
					Type:      "topic",
					Settings:  map[string]any{"topic": "orders"},
				},
				Process: []types.BlockConfig{
					{
						Type:     "validate",
						Settings: map[string]any{"schema": "order.schema.json"},
					},
					{
						Type: "handle-errors",
						Name: "persist",
						Process: []types.BlockConfig{
							{Type: "transform", Name: "normalize"},
						},
						Error: []types.BlockConfig{
							{Type: "deadletter"},
						},
					},
					{
						Type: "fork",
						Name: "notify-and-audit",
						Branches: []types.FlowConfig{
							{
								Name:    "notify",
								Process: []types.BlockConfig{{Type: "email"}},
							},
							{
								Name:    "audit",
								Process: []types.BlockConfig{{Type: "log"}},
							},
						},
					},
				},
			},
		},
	}

	if !reflect.DeepEqual(config, want) {
		t.Errorf("Parse() =\n%#v\nwant\n%#v", config, want)
	}
}
