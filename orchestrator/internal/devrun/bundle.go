package devrun

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// devEnvResource is the resource an integration's dev credentials live in. It
	// mirrors run-host's DEV_ENV_RESOURCE (packages/run-host/src/resources.ts): the
	// editor's Dev .env panel already saves it through the ordinary resource API, so
	// it arrives in the bundle like any other env file and only its *declaration*
	// has to be arranged for.
	devEnvResource = ".env.dev"
	// resourcesKey and envKey are the definition path the dev-env resource is
	// declared at: resources.env[].
	resourcesKey = "resources"
	envKey       = "env"
	// generationLen is how much of the content digest a generation marker shows. Six
	// bytes is plenty to distinguish revisions in a log line and short enough to read.
	generationLen = 12
)

// buildBundle assembles what a dev run needs on disk from the integration's saved
// definition and its live resources.
//
// The dev-env resource is *appended last* to resources.env, so its values override
// every other env resource and every declared default — the same precedence the
// local runner gives it, and the reason a developer can point a saved integration at
// their own credentials without editing it.
func buildBundle(definition string, resources []Resource) Bundle {
	out := Bundle{
		Definition: injectDevEnvResource(definition),
		Resources:  resources,
	}
	out.Generation = generationOf(out)
	return out
}

// generationOf is a content digest over everything in the bundle: the definition
// plus each resource's name, kind and content.
//
// A digest rather than a timestamp, deliberately. The obvious marker — the latest
// last_updated across the integration and its resources — moves *backwards* when a
// resource is deleted, so it can repeat a value the sidecar has already seen and
// report a real change as a no-op. A digest changes if and only if the content does.
func generationOf(b Bundle) string {
	h := sha256.New()
	// hash.Hash's Write never fails; the lengths are mixed in so that concatenation
	// cannot forge a collision between, say _a_ resource named "ab" and one named "a"
	// whose content starts with "b".
	write := func(s string) {
		_, _ = fmt.Fprintf(h, "%d:%s", len(s), s)
	}
	write(b.Definition)
	for _, r := range b.Resources {
		write(r.Name)
		write(r.Kind)
		write(r.Content)
	}
	return hex.EncodeToString(h.Sum(nil))[:generationLen]
}

// injectDevEnvResource declares the dev-env resource in a definition's
// resources.env list, appending it last (so it wins) and leaving an existing
// declaration where the author put it.
//
// It edits the parsed *node tree* rather than round-tripping through a map, which
// keeps comments, key order and scalar styles intact. That matters more here than it
// did for the local runner: a definition is full of CEL expressions whose quoting a
// re-render could legitimately change, and a definition the developer cannot
// recognise in a log or an error message is a definition they cannot debug.
//
// A malformed definition is returned untouched. The runtime validates the whole
// document at load time and reports it far better than this could — and a dev run
// whose YAML does not parse has a more pressing problem than its env precedence.
func injectDevEnvResource(definition string) string {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(definition), &doc); err != nil {
		return definition
	}
	// An empty or comment-only document has no content to edit. Synthesising a
	// mapping here would turn "nothing" into a config, which is not this function's
	// call to make.
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return definition
	}
	root := doc.Content[0]

	resources := mappingValue(root, resourcesKey)
	if resources == nil {
		return definition
	}
	env := sequenceValue(resources, envKey)
	if env == nil {
		return definition
	}
	for _, item := range env.Content {
		if item.Kind == yaml.ScalarNode && strings.TrimSpace(item.Value) == devEnvResource {
			return definition
		}
	}
	env.Content = append(env.Content, &yaml.Node{
		Kind:  yaml.ScalarNode,
		Tag:   "!!str",
		Value: devEnvResource,
	})

	rendered, err := yaml.Marshal(&doc)
	if err != nil {
		return definition
	}
	return string(rendered)
}

// mappingValue returns the mapping stored at key in a mapping node, creating an
// empty one when the key is absent. It returns nil when the key exists but holds
// something that is not a mapping — a definition where `resources:` is a string is
// broken, and quietly replacing the author's value would hide that.
func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if found := childValue(node, key); found != nil {
		if found.Kind != yaml.MappingNode {
			return nil
		}
		return found
	}
	value := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
	return value
}

// sequenceValue returns the sequence stored at key in a mapping node, creating an
// empty one when the key is absent, and nil when it holds something else. An
// explicitly null value (`env:` with nothing under it) counts as absent, which is
// the one YAML shape a hand-written definition reaches by accident.
func sequenceValue(node *yaml.Node, key string) *yaml.Node {
	if found := childValue(node, key); found != nil {
		switch {
		case found.Kind == yaml.SequenceNode:
			return found
		case found.Kind == yaml.ScalarNode && found.Tag == "!!null":
			found.Kind = yaml.SequenceNode
			found.Tag = "!!seq"
			found.Value = ""
			return found
		default:
			return nil
		}
	}
	value := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
	return value
}

// childValue returns the value node for key in a mapping node, or nil. A mapping's
// Content alternates key, value, key, value.
func childValue(node *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}
