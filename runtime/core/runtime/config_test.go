package runtime

import (
	"strings"
	"testing"

	"github.com/juancavallotti/eip-go/types"
)

func TestMergeConfigsConcatenates(t *testing.T) {
	merged, err := MergeConfigs([]types.Config{
		{
			Service:    types.ServiceConfig{Name: "svc"},
			Connectors: []types.ConnectorConfig{{Name: "a", Type: "http"}},
			Flows:      []types.FlowConfig{{Name: "f1"}},
		},
		{
			Connectors: []types.ConnectorConfig{{Name: "b", Type: "cron"}},
			Processors: []types.ProcessorConfig{{Name: "p1", Type: "log"}},
			Flows:      []types.FlowConfig{{Name: "f2"}},
		},
	})
	if err != nil {
		t.Fatalf("MergeConfigs: %v", err)
	}
	if merged.Service.Name != "svc" {
		t.Errorf("service name = %q, want svc", merged.Service.Name)
	}
	if len(merged.Connectors) != 2 || len(merged.Flows) != 2 || len(merged.Processors) != 1 {
		t.Errorf("unexpected merge: connectors=%d flows=%d processors=%d",
			len(merged.Connectors), len(merged.Flows), len(merged.Processors))
	}
}

func TestMergeConfigsRejectsDuplicates(t *testing.T) {
	tests := []struct {
		name    string
		configs []types.Config
		wantErr string
	}{
		{
			name: "duplicate flow",
			configs: []types.Config{
				{Flows: []types.FlowConfig{{Name: "dup"}}},
				{Flows: []types.FlowConfig{{Name: "dup"}}},
			},
			wantErr: "flow \"dup\"",
		},
		{
			name: "duplicate connector",
			configs: []types.Config{
				{Connectors: []types.ConnectorConfig{{Name: "c"}}},
				{Connectors: []types.ConnectorConfig{{Name: "c"}}},
			},
			wantErr: "connector \"c\"",
		},
		{
			name: "two services",
			configs: []types.Config{
				{Service: types.ServiceConfig{Name: "one"}},
				{Service: types.ServiceConfig{Name: "two"}},
			},
			wantErr: "service identity",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := MergeConfigs(tc.configs)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}
