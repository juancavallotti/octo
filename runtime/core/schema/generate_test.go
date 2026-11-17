package schema

import (
	"reflect"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
)

type searchToolSample struct {
	MaxUses int `json:"maxUses" octo:"label=Max uses"`
}

type badToolSample struct {
	Broken func() `json:"broken" octo:"label=Broken"`
}

func TestGenerateEmitsProviderTools(t *testing.T) {
	reg := core.NewSchemaRegistry()
	reg.AddConnector(core.ConnectorMeta{
		Type:     "llm-sample",
		Category: "llm",
		ProviderTools: []core.ProviderToolMeta{
			{Type: "web_search", Label: "Web search", Settings: reflect.TypeFor[searchToolSample]()},
			{Type: "code_execution", Label: "Code execution"},
		},
	})
	reg.AddConnector(core.ConnectorMeta{Type: "http"})

	caps, err := Generate(reg)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	tools := caps.Connectors[0].ProviderTools
	if len(tools) != 2 {
		t.Fatalf("want 2 provider tools, got %d", len(tools))
	}
	if tools[0].Type != "web_search" || len(tools[0].Fields) != 1 || tools[0].Fields[0].Name != "maxUses" {
		t.Errorf("web_search spec: %+v", tools[0])
	}
	if tools[1].Fields == nil || len(tools[1].Fields) != 0 {
		t.Errorf("a settings-less tool emits an empty field list, got %+v", tools[1].Fields)
	}
	if caps.Connectors[1].ProviderTools != nil {
		t.Errorf("a connector with no catalogue omits it, got %+v", caps.Connectors[1].ProviderTools)
	}
}

func TestGenerateLocatesBadProviderToolSettings(t *testing.T) {
	reg := core.NewSchemaRegistry()
	reg.AddConnector(core.ConnectorMeta{
		Type:          "llm-sample",
		ProviderTools: []core.ProviderToolMeta{{Type: "broken", Settings: reflect.TypeFor[badToolSample]()}},
	})
	_, err := Generate(reg)
	if err == nil || !strings.Contains(err.Error(), `provider tool "broken"`) {
		t.Fatalf("want an error naming the tool, got %v", err)
	}
}
