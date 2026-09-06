package runtime

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/schema"
	"github.com/juancavallotti/octo/runtime/types"
)

// address is a parsed block address: the flow the block is in, which of that flow's
// two chains to start from, and the path of steps down to the block.
//
// The grammar is `<flow>[<chain>].<block>[<branch>].<block>…`, e.g.
// `orders.checkHeader[else].api-call-1`. Every step but the last names a composite
// and the branch of it to descend into; the last step names the target block. The
// flow's optional bracket selects its error chain (`orders[error].notify`).
type address struct {
	flow  string
	chain string
	steps []step
}

// step is one hop of an address: the block to select, and — for every step but the
// last — the branch of that block to descend into.
type step struct {
	block  string
	branch string
}

// resolver walks an address down a config. It carries the kind of debug feature that
// asked — "breakpoint", "spy", "mock" — because every failure here is a bad address
// the user typed, and the message has to name the flag it came from.
type resolver struct {
	kind string
	addr string
	// defs indexes the config's named processors, so a block declared by ref can
	// be looked up by the type it resolves to.
	defs map[string]string
}

// block is a block as the resolver sees it: the generic map a block decodes to.
//
// The resolver walks a generic tree rather than typed structs because a block's
// sub-flows are the block's own business — they live in its settings, typed by
// the settings struct only that block knows. What the resolver knows is which
// keys of that struct hold chains, read from the block's schema (see
// schema.Branches), and that is enough to descend through any block, including
// one registered from outside the runtime tree.
type block map[string]any

// rewriteTarget resolves the block addr names and replaces it with what rewrite
// returns: a breakpoint or a spy wraps the block it finds, a mock replaces it.
//
// It mutates cfg in place. That is safe because the debug features are
// invoke-only, on a config the invoking process loaded for itself and throws
// away. The chain the target sits in is walked as a generic tree and written
// back typed, so a chain injected into twice — a spy on a fork and a breakpoint
// inside one of its branches — reads the first injection on the way to the
// second.
func rewriteTarget(
	cfg *types.Config, kind, addr string, rewrite func(target types.BlockConfig) types.BlockConfig,
) error {
	r := newResolver(cfg, kind, addr)
	parsed, err := r.parseAddress()
	if err != nil {
		return err
	}
	flow, err := r.findFlow(cfg, parsed.flow)
	if err != nil {
		return err
	}
	chain, err := r.flowChain(flow, parsed.chain)
	if err != nil {
		return err
	}

	root, err := toGeneric(*chain)
	if err != nil {
		return err
	}
	parent, index, err := r.locate(root, parsed.steps)
	if err != nil {
		return err
	}
	target, err := fromGeneric(parent[index])
	if err != nil {
		return err
	}
	replacement, err := toGeneric(rewrite(target))
	if err != nil {
		return err
	}
	parent[index] = replacement[0]

	typed, err := fromGenericChain(root)
	if err != nil {
		return err
	}
	*chain = typed
	return nil
}

// resolveTarget returns a copy of the block addr names, for a caller that wants
// to look at it rather than rewrite it.
func resolveTarget(cfg *types.Config, kind, addr string) (types.BlockConfig, error) {
	var found types.BlockConfig
	err := rewriteTarget(cfg, kind, addr, func(target types.BlockConfig) types.BlockConfig {
		found = target
		return target
	})
	return found, err
}

// newResolver indexes the config's processors for ref resolution.
func newResolver(cfg *types.Config, kind, addr string) resolver {
	defs := make(map[string]string, len(cfg.Processors))
	for _, p := range cfg.Processors {
		defs[p.Name] = p.Type
	}
	return resolver{kind: kind, addr: addr, defs: defs}
}

// locate walks the steps down from root and returns the chain holding the target
// and the target's index in it.
func (r resolver) locate(root []any, steps []step) ([]any, int, error) {
	chain := root
	for _, hop := range steps[:len(steps)-1] {
		index, err := r.selectBlock(chain, hop.block)
		if err != nil {
			return nil, 0, err
		}
		next, err := r.branchChain(unwrapDebug(asBlock(chain[index])), hop.branch)
		if err != nil {
			return nil, 0, fmt.Errorf("%s %q: block %q: %w", r.kind, r.addr, hop.block, err)
		}
		chain = next
	}
	index, err := r.selectBlock(chain, steps[len(steps)-1].block)
	if err != nil {
		return nil, 0, err
	}
	return chain, index, nil
}

// toGeneric converts a typed chain — or one block — to the generic tree the
// resolver walks. It goes through JSON because that is the bridge every block's
// settings already cross, so a sub-flow written in Go and one decoded from a
// document arrive here in the same shape.
func toGeneric(chain ...any) ([]any, error) {
	var value any = chain
	if len(chain) == 1 {
		value = chain[0]
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode flow: %w", err)
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, fmt.Errorf("decode flow: %w", err)
	}
	if list, ok := generic.([]any); ok {
		return list, nil
	}
	return []any{generic}, nil
}

// fromGeneric types one block back.
func fromGeneric(value any) (types.BlockConfig, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return types.BlockConfig{}, fmt.Errorf("encode block: %w", err)
	}
	var cfg types.BlockConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return types.BlockConfig{}, fmt.Errorf("decode block: %w", err)
	}
	return cfg, nil
}

// fromGenericChain types a chain back.
func fromGenericChain(chain []any) ([]types.BlockConfig, error) {
	raw, err := json.Marshal(chain)
	if err != nil {
		return nil, fmt.Errorf("encode flow: %w", err)
	}
	var typed []types.BlockConfig
	if err := json.Unmarshal(raw, &typed); err != nil {
		return nil, fmt.Errorf("decode flow: %w", err)
	}
	return typed, nil
}

// asBlock reads a chain element as a block. Anything that is not a map — which
// a valid config never produces — reads as an empty block, and fails to match
// any selector.
func asBlock(value any) block {
	if m, ok := value.(map[string]any); ok {
		return m
	}
	return block{}
}

// str reads a string-valued key.
func (b block) str(key string) string {
	s, _ := b[key].(string)
	return s
}

// slot reads a settings key wherever the block spelled it: under `settings`, or
// at the top level beside `type`. A block decoded from a document folds the two
// together, but a nested block deeper than the root chain arrives here as it
// was written, so both spellings have to be read.
func (b block) slot(name string) (any, bool) {
	if settings, ok := b["settings"].(map[string]any); ok {
		if v, found := settings[name]; found {
			return v, true
		}
	}
	v, found := b[name]
	return v, found
}

// unwrapDebug peels the debug wrappers an earlier injection put around a block, so a
// second address can still descend through it. Spying a fork and then addressing a
// block inside one of its branches would otherwise find the spy — which has no
// branches — where the fork used to be.
//
// It peels on the way down only, never at the target: an address that lands on an
// already-wrapped block must resolve to the wrapper's slot, so the next injection
// wraps around what is already there rather than replacing it.
func unwrapDebug(b block) block {
	for isDebugWrapper(b) {
		inner, ok := b.slot(core.BranchProcess)
		chain, isChain := inner.([]any)
		if !ok || !isChain || len(chain) != 1 {
			return b
		}
		b = asBlock(chain[0])
	}
	return b
}

// isDebugWrapper reports whether the block is one the debug injectors added.
func isDebugWrapper(b block) bool {
	return b.str("type") == breakpointBlockType || b.str("type") == spyBlockType
}

// blockLabel is how a block is identified in errors and addresses: its name, else
// its type, else the processor it refs.
func blockLabel(cfg types.BlockConfig) string {
	return core.BlockLabel(cfg.Name, cfg.Type, cfg.Ref)
}

// parseAddress splits the address into its flow, chain, and steps.
func (r resolver) parseAddress() (address, error) {
	if strings.TrimSpace(r.addr) == "" {
		return address{}, fmt.Errorf("%s address is empty", r.kind)
	}

	segments := strings.Split(r.addr, ".")
	if len(segments) < 2 {
		return address{}, fmt.Errorf(
			"%s %q: address must name a flow and at least one block, as <flow>.<block>", r.kind, r.addr)
	}

	flow, chain, err := splitBracket(segments[0])
	if err != nil {
		return address{}, fmt.Errorf("%s %q: %w", r.kind, r.addr, err)
	}
	if flow == "" {
		return address{}, fmt.Errorf("%s %q: address must start with a flow name", r.kind, r.addr)
	}
	if chain == "" {
		chain = core.BranchProcess
	}

	steps, err := r.parseSteps(segments[1:])
	if err != nil {
		return address{}, err
	}
	return address{flow: flow, chain: chain, steps: steps}, nil
}

// parseSteps parses the block segments of an address. Only the last step names the
// target; every earlier one must say which branch of its composite to descend into.
func (r resolver) parseSteps(segments []string) ([]step, error) {
	steps := make([]step, 0, len(segments))
	for i, segment := range segments {
		block, branch, err := splitBracket(segment)
		if err != nil {
			return nil, fmt.Errorf("%s %q: %w", r.kind, r.addr, err)
		}
		if block == "" {
			return nil, fmt.Errorf("%s %q: segment %d names no block", r.kind, r.addr, i+1)
		}

		last := i == len(segments)-1
		switch {
		case !last && branch == "":
			return nil, fmt.Errorf(
				"%s %q: block %q is on the way to the target, so it needs a branch, as %s[<branch>]",
				r.kind, r.addr, block, block)
		case last && branch != "":
			return nil, fmt.Errorf(
				"%s %q: the target block %q must not name a branch; address a block inside it instead",
				r.kind, r.addr, block)
		}
		steps = append(steps, step{block: block, branch: branch})
	}
	return steps, nil
}

// splitBracket splits "name[bracket]" into its parts. bracket is empty when the
// segment carries none.
func splitBracket(segment string) (name, bracket string, err error) {
	open := strings.IndexByte(segment, '[')
	if open < 0 {
		if strings.ContainsRune(segment, ']') {
			return "", "", fmt.Errorf("segment %q has a closing bracket with no opening one", segment)
		}
		return segment, "", nil
	}
	if !strings.HasSuffix(segment, "]") {
		return "", "", fmt.Errorf("segment %q is missing its closing bracket", segment)
	}

	bracket = segment[open+1 : len(segment)-1]
	if bracket == "" {
		return "", "", fmt.Errorf("segment %q has an empty bracket", segment)
	}
	if strings.ContainsAny(bracket, "[]") {
		return "", "", fmt.Errorf("segment %q has nested brackets; use one branch per segment", segment)
	}
	return segment[:open], bracket, nil
}

// findFlow returns the named flow from the config.
func (r resolver) findFlow(cfg *types.Config, name string) (*types.FlowConfig, error) {
	for i := range cfg.Flows {
		if cfg.Flows[i].Name == name {
			return &cfg.Flows[i], nil
		}
	}

	names := make([]string, 0, len(cfg.Flows))
	for i := range cfg.Flows {
		names = append(names, cfg.Flows[i].Name)
	}
	return nil, fmt.Errorf("%s: no flow named %q (flows: %s)", r.kind, name, strings.Join(names, ", "))
}

// flowChain returns the flow's process chain, or its error chain when the address
// asked for it.
func (r resolver) flowChain(flow *types.FlowConfig, chain string) (*[]types.BlockConfig, error) {
	switch chain {
	case core.BranchProcess:
		return &flow.Process, nil
	case core.BranchError:
		return &flow.Error, nil
	default:
		return nil, fmt.Errorf(
			"%s: flow %q has no chain %q (want %q or %q)", r.kind, flow.Name, chain, core.BranchProcess, core.BranchError)
	}
}

// selectBlock finds the one block in chain that the selector names. A block is
// selected by its name, else its type, else the processor it refs — so an unnamed
// block is still addressable, as long as it is the only one of its kind here.
func (r resolver) selectBlock(chain []any, selector string) (int, error) {
	for _, key := range []string{"name", "type", "ref"} {
		var found []int
		for i := range chain {
			if asBlock(chain[i]).str(key) == selector && selector != "" {
				found = append(found, i)
			}
		}
		switch len(found) {
		case 0:
			continue
		case 1:
			return found[0], nil
		default:
			return 0, fmt.Errorf(
				"%s %q: %q matches %d blocks in the same chain; give the one you want a unique name",
				r.kind, r.addr, selector, len(found))
		}
	}

	labels := make([]string, 0, len(chain))
	for i := range chain {
		b := asBlock(chain[i])
		labels = append(labels, core.BlockLabel(b.str("name"), b.str("type"), b.str("ref")))
	}
	return 0, fmt.Errorf(
		"%s %q: no block %q in that chain (blocks: %s)", r.kind, r.addr, selector, strings.Join(labels, ", "))
}

// effectiveType is the block's type after its ref is resolved: what its schema is
// registered under.
func (r resolver) effectiveType(b block) string {
	if t := b.str("type"); t != "" {
		return t
	}
	return r.defs[b.str("ref")]
}

// branchChain returns the block chain of the named branch of a composite, so the
// caller can descend into it. The block's schema says which of its settings hold
// chains: the named slots resolve by their own name, the list-valued slots by
// each member's name, or its index.
func (r resolver) branchChain(b block, branch string) ([]any, error) {
	branches, err := schema.Branches(core.DefaultSchemaRegistry(), r.effectiveType(b))
	if err != nil {
		return nil, err
	}
	if branches != nil {
		for _, name := range branches.Named {
			if name != branch {
				continue
			}
			if chain, ok := namedChain(b, name); ok {
				return chain, nil
			}
		}
		for _, slot := range branches.ByMember {
			if chain, ok := memberChain(b, slot, branch); ok {
				return chain, nil
			}
		}
	}
	return nil, fmt.Errorf("has no branch %q (branches: %s)", branch, strings.Join(branchNames(b, branches), ", "))
}

// namedChain reads the chain a named slot holds: a bare block list, or a sub-flow
// whose `process` is one. An absent or empty slot reads as "no such branch"
// rather than as an empty chain.
func namedChain(b block, name string) ([]any, bool) {
	value, ok := b.slot(name)
	if !ok {
		return nil, false
	}
	if flow, isFlow := value.(map[string]any); isFlow {
		value = flow[core.BranchProcess]
	}
	chain, isChain := value.([]any)
	if !isChain || len(chain) == 0 {
		return nil, false
	}
	return chain, true
}

// memberChain reads the chain of one member of a list-valued slot, selected by
// the member's own name or by its index in the slot.
func memberChain(b block, slot, branch string) ([]any, bool) {
	index, isIndex := parseIndex(branch)
	for i, member := range members(b, slot) {
		if member.str("name") == branch || (isIndex && index == i) {
			chain, ok := member[core.BranchProcess].([]any)
			return chain, ok
		}
	}
	return nil, false
}

// members reads a list-valued slot's entries.
func members(b block, slot string) []block {
	value, ok := b.slot(slot)
	list, isList := value.([]any)
	if !ok || !isList {
		return nil
	}
	out := make([]block, 0, len(list))
	for _, entry := range list {
		out = append(out, asBlock(entry))
	}
	return out
}

// parseIndex reports whether branch is a non-negative integer index.
func parseIndex(branch string) (int, bool) {
	index, err := strconv.Atoi(branch)
	if err != nil || index < 0 {
		return 0, false
	}
	return index, true
}

// branchNames lists the branches a block actually exposes, for the error message
// when an address names one it does not have.
func branchNames(b block, branches *schema.AddressBranches) []string {
	var names []string
	if branches != nil {
		for _, name := range branches.Named {
			if _, ok := namedChain(b, name); ok {
				names = append(names, name)
			}
		}
		for _, slot := range branches.ByMember {
			for i, member := range members(b, slot) {
				names = append(names, core.MemberBranch(member.str("name"), i))
			}
		}
	}
	if len(names) == 0 {
		return []string{"none: this is a leaf block"}
	}
	return names
}
