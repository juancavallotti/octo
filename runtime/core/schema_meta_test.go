package core

import (
	"reflect"
	"testing"
)

type sampleSettings struct {
	Value string `json:"value"`
}

func TestSchemaRegistryPreservesRegistrationOrder(t *testing.T) {
	reg := NewSchemaRegistry()
	reg.AddBlock(BlockMeta{Type: "set-payload"})
	reg.AddBlock(BlockMeta{Type: "set-variable"})
	reg.AddBlock(BlockMeta{Type: "delete-variable"})

	blocks := reg.Blocks()
	got := []string{blocks[0].Type, blocks[1].Type, blocks[2].Type}
	want := []string{"set-payload", "set-variable", "delete-variable"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("registration order not preserved: got %v want %v", got, want)
	}
}

func TestExtensionSuppliesGroupAndIconDefaults(t *testing.T) {
	reg := NewSchemaRegistry()
	reg.AddExtension(ExtensionMeta{Group: "Slack", Icon: "Slack", SrcDir: "/pkg/slack"})
	reg.AddBlock(BlockMeta{Type: "slack-send-message", SrcDir: "/pkg/slack"})

	b := reg.Blocks()[0]
	if b.Group != "Slack" {
		t.Errorf("group not inherited from extension: got %q", b.Group)
	}
	if b.Icon != "Slack" {
		t.Errorf("icon not inherited from extension: got %q", b.Icon)
	}
}

func TestBlockValueOverridesExtensionDefault(t *testing.T) {
	reg := NewSchemaRegistry()
	reg.AddExtension(ExtensionMeta{Group: "Slack", Icon: "Slack", SrcDir: "/pkg/slack"})
	reg.AddBlock(BlockMeta{Type: "slack-verify-request", Group: "Security", Icon: "ShieldAlert", SrcDir: "/pkg/slack"})

	b := reg.Blocks()[0]
	if b.Group != "Security" {
		t.Errorf("block group should override extension: got %q", b.Group)
	}
	if b.Icon != "ShieldAlert" {
		t.Errorf("block icon should override extension: got %q", b.Icon)
	}
}

func TestExtensionScopedBySourceDirectory(t *testing.T) {
	reg := NewSchemaRegistry()
	reg.AddExtension(ExtensionMeta{Group: "Slack", SrcDir: "/pkg/slack"})
	reg.AddBlock(BlockMeta{Type: "log", SrcDir: "/pkg/logger"}) // different package

	if g := reg.Blocks()[0].Group; g != "" {
		t.Errorf("block in a different package should not inherit group, got %q", g)
	}
}

func TestConnectorInheritsExtensionIconAndSourceDir(t *testing.T) {
	reg := NewSchemaRegistry()
	reg.AddExtension(ExtensionMeta{Icon: "Slack", SrcDir: "/pkg/slack"})
	reg.AddConnector(ConnectorMeta{
		Type:    "slack",
		SrcDir:  "/pkg/slack",
		Sources: []SourceMeta{{Type: "slack-events"}},
	})

	c := reg.Connectors()[0]
	if c.Icon != "Slack" {
		t.Errorf("connector icon not inherited: got %q", c.Icon)
	}
	if c.Sources[0].SrcDir != "/pkg/slack" {
		t.Errorf("source SrcDir should default to connector's: got %q", c.Sources[0].SrcDir)
	}
}

func TestRegisterBlockMetaCapturesCallerDir(t *testing.T) {
	// The package-level Register* helpers capture the caller's source directory
	// via runtime.Caller; this test file's directory should be recorded.
	before := len(DefaultSchemaRegistry().Blocks())
	RegisterBlockMeta(BlockMeta{Type: "e2e.schema-meta-test", Config: reflect.TypeOf(sampleSettings{})})
	blocks := DefaultSchemaRegistry().Blocks()
	if len(blocks) != before+1 {
		t.Fatalf("expected one block added, got %d -> %d", before, len(blocks))
	}
	added := blocks[len(blocks)-1]
	if added.SrcDir == "" {
		t.Error("RegisterBlockMeta did not capture a caller source directory")
	}
}
