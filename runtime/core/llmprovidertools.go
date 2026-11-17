package core

import (
	"encoding/json"
	"reflect"

	"github.com/juancavallotti/octo/runtime/types"
)

// LLMProviderTool is one server-side tool a request enables: a capability the
// provider runs itself during the turn — web search, web fetch, code execution —
// as opposed to an LLMTool, which the model asks the caller to run.
//
// Type names the tool in the connector's own catalogue and Settings carries its
// configuration. Both are opaque here: only the connector that declared the type
// knows what it means or how to decode it.
type LLMProviderTool struct {
	Type     string
	Settings types.Settings
}

// LLMProviderTools is the optional half of a provider connector that runs
// server-side tools. A connector that does not implement it runs none, and a
// block configured to enable one against it fails to build.
type LLMProviderTools interface {
	// ValidateProviderTools reports whether every tool is one this connector runs
	// and its settings decode. The error names the offending tool and the types
	// the connector accepts.
	//
	// It is the build-time check, not the only one: a connector still refuses a
	// tool it cannot translate when the call is made, because a caller is free
	// not to ask first.
	ValidateProviderTools(tools []LLMProviderTool) error
}

// LLMOpaqueTurn is an assistant turn exactly as one provider produced it, for the
// turns a portable LLMMessage cannot express without loss — server-side tool
// calls and their results, interleaved with the text they ground.
//
// Content is the provider's own encoding and is never inspected, merged or
// constructed outside the connector that wrote it. A connector of the same
// Provider family echoes it in place of the portable fields; any other connector
// ignores it and reads the portable fields, which remain populated.
type LLMOpaqueTurn struct {
	// Provider is the vendor family that wrote Content, in the vocabulary of
	// LLMProvider.Provider.
	Provider string
	// Content is the turn's provider-native content, verbatim.
	Content json.RawMessage
}

// OpaqueFor returns the turn's opaque content when it was written by provider,
// and nil otherwise.
func (m LLMMessage) OpaqueFor(provider string) json.RawMessage {
	if m.Opaque == nil || m.Opaque.Provider != provider {
		return nil
	}
	return m.Opaque.Content
}

// ProviderToolMeta is the editor-facing description of one server-side tool a
// connector runs. Settings is the zero-value settings struct whose `octo`/`json`
// tags describe the tool's fields; nil means the tool takes none.
type ProviderToolMeta struct {
	Type        string
	Label       string
	Description string
	Settings    reflect.Type
}
