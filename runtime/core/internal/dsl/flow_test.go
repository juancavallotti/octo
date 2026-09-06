package dsl

import (
	"reflect"
	"testing"

	"github.com/juancavallotti/octo/runtime/types"
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
						// Nested chains stay as the document spelled them; the block
						// types them when it decodes its own settings.
						Settings: types.Settings{
							"process": []any{map[string]any{"type": "transform", "name": "normalize"}},
							"error":   []any{map[string]any{"type": "deadletter"}},
						},
					},
					{
						Type: "fork",
						Name: "notify-and-audit",
						Settings: types.Settings{"branches": []any{
							map[string]any{"name": "notify", "process": []any{map[string]any{"type": "email"}}},
							map[string]any{"name": "audit", "process": []any{map[string]any{"type": "log"}}},
						}},
					},
				},
			},
		},
	}

	if !reflect.DeepEqual(config, want) {
		t.Errorf("Parse() =\n%#v\nwant\n%#v", config, want)
	}
}
