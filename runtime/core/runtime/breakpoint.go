package runtime

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/juancavallotti/octo/types"
)

// Block chain names addressable with a bracket. The first two are a flow's own
// chains; the rest are the branches of a composite block. A composite's list-valued
// branches (a fork's branches, a switch's cases, an ai-router's routes, an
// ai-agent's tools) are addressed by the member's own name instead, or by its index.
const (
	branchProcess  = "process"
	branchError    = "error"
	branchThen     = "then"
	branchElse     = "else"
	branchBody     = "body"
	branchDefault  = "default"
	branchOnReject = "onReject"
)

// breakpointBlockType is the block type the injector wraps a target in. It matches
// the engine's internal breakpoint kind, which the block builder recognizes.
const breakpointBlockType = "breakpoint"

// address is a parsed breakpoint address: the flow to break in, which of its two
// chains to start from, and the path of steps down to the target block.
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

// injectBreakpoint wraps the block named by addr in an implicit breakpoint block,
// so the engine records the message there and halts the flow.
//
// It mutates cfg in place: the flow tree is a web of slices and *FlowConfig
// pointers, so a "copy" would alias the caller's backing arrays anyway. That is
// safe because a breakpoint is invoke-only, on a config the invoking process loaded
// for itself and throws away. Call it exactly once per config.
func injectBreakpoint(cfg *types.Config, addr string) error {
	parsed, err := parseAddress(addr)
	if err != nil {
		return err
	}

	flow, err := findFlow(cfg, parsed.flow)
	if err != nil {
		return err
	}
	chain, err := flowChain(flow, parsed.chain)
	if err != nil {
		return err
	}

	// Walk down to the chain holding the target, descending one composite per step.
	for _, hop := range parsed.steps[:len(parsed.steps)-1] {
		block, err := selectBlock(*chain, hop.block, addr)
		if err != nil {
			return err
		}
		if chain, err = branchChain(block, hop.branch); err != nil {
			return fmt.Errorf("breakpoint %q: block %q: %w", addr, hop.block, err)
		}
	}

	target, err := selectBlock(*chain, parsed.steps[len(parsed.steps)-1].block, addr)
	if err != nil {
		return err
	}
	*target = wrapInBreakpoint(*target)
	return nil
}

// wrapInBreakpoint returns the breakpoint block that wraps target. The wrapper
// takes the target's label so a block error is reported against the same name it
// would have been without the breakpoint.
func wrapInBreakpoint(target types.BlockConfig) types.BlockConfig {
	return types.BlockConfig{
		Type:    breakpointBlockType,
		Name:    blockLabel(target),
		Process: []types.BlockConfig{target},
	}
}

// blockLabel is how a block is identified in errors and addresses: its name, else
// its type, else the processor it refs.
func blockLabel(cfg types.BlockConfig) string {
	switch {
	case cfg.Name != "":
		return cfg.Name
	case cfg.Type != "":
		return cfg.Type
	default:
		return cfg.Ref
	}
}

// parseAddress splits an address into its flow, chain, and steps.
func parseAddress(addr string) (address, error) {
	if strings.TrimSpace(addr) == "" {
		return address{}, fmt.Errorf("breakpoint address is empty")
	}

	segments := strings.Split(addr, ".")
	if len(segments) < 2 {
		return address{}, fmt.Errorf(
			"breakpoint %q: address must name a flow and at least one block, as <flow>.<block>", addr)
	}

	flow, chain, err := splitBracket(segments[0])
	if err != nil {
		return address{}, fmt.Errorf("breakpoint %q: %w", addr, err)
	}
	if flow == "" {
		return address{}, fmt.Errorf("breakpoint %q: address must start with a flow name", addr)
	}
	if chain == "" {
		chain = branchProcess
	}

	steps, err := parseSteps(addr, segments[1:])
	if err != nil {
		return address{}, err
	}
	return address{flow: flow, chain: chain, steps: steps}, nil
}

// parseSteps parses the block segments of an address. Only the last step names the
// target; every earlier one must say which branch of its composite to descend into.
func parseSteps(addr string, segments []string) ([]step, error) {
	steps := make([]step, 0, len(segments))
	for i, segment := range segments {
		block, branch, err := splitBracket(segment)
		if err != nil {
			return nil, fmt.Errorf("breakpoint %q: %w", addr, err)
		}
		if block == "" {
			return nil, fmt.Errorf("breakpoint %q: segment %d names no block", addr, i+1)
		}

		last := i == len(segments)-1
		switch {
		case !last && branch == "":
			return nil, fmt.Errorf(
				"breakpoint %q: block %q is on the way to the target, so it needs a branch, as %s[<branch>]",
				addr, block, block)
		case last && branch != "":
			return nil, fmt.Errorf(
				"breakpoint %q: the target block %q must not name a branch; address a block inside it instead",
				addr, block)
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
func findFlow(cfg *types.Config, name string) (*types.FlowConfig, error) {
	for i := range cfg.Flows {
		if cfg.Flows[i].Name == name {
			return &cfg.Flows[i], nil
		}
	}

	names := make([]string, 0, len(cfg.Flows))
	for i := range cfg.Flows {
		names = append(names, cfg.Flows[i].Name)
	}
	return nil, fmt.Errorf("breakpoint: no flow named %q (flows: %s)", name, strings.Join(names, ", "))
}

// flowChain returns the flow's process chain, or its error chain when the address
// asked for it.
func flowChain(flow *types.FlowConfig, chain string) (*[]types.BlockConfig, error) {
	switch chain {
	case branchProcess:
		return &flow.Process, nil
	case branchError:
		return &flow.Error, nil
	default:
		return nil, fmt.Errorf(
			"breakpoint: flow %q has no chain %q (want %q or %q)", flow.Name, chain, branchProcess, branchError)
	}
}

// selectBlock finds the one block in chain that the selector names. A block is
// selected by its name, else its type, else the processor it refs — so an unnamed
// block is still addressable, as long as it is the only one of its kind here.
func selectBlock(chain []types.BlockConfig, selector, addr string) (*types.BlockConfig, error) {
	for _, match := range []func(types.BlockConfig) string{
		func(b types.BlockConfig) string { return b.Name },
		func(b types.BlockConfig) string { return b.Type },
		func(b types.BlockConfig) string { return b.Ref },
	} {
		var found []int
		for i := range chain {
			if match(chain[i]) == selector && selector != "" {
				found = append(found, i)
			}
		}
		switch len(found) {
		case 0:
			continue
		case 1:
			return &chain[found[0]], nil
		default:
			return nil, fmt.Errorf(
				"breakpoint %q: %q matches %d blocks in the same chain; give the one you want a unique name",
				addr, selector, len(found))
		}
	}

	labels := make([]string, 0, len(chain))
	for i := range chain {
		labels = append(labels, blockLabel(chain[i]))
	}
	return nil, fmt.Errorf(
		"breakpoint %q: no block %q in that chain (blocks: %s)", addr, selector, strings.Join(labels, ", "))
}

// branchChain returns the block chain of the named branch of a composite, so the
// caller can descend into it. It is the single source of truth for what a composite
// exposes to an address: the reserved names below, plus — for the composites whose
// branches are a list — each member's own name, or its index.
func branchChain(block *types.BlockConfig, branch string) (*[]types.BlockConfig, error) {
	if chain, ok := reservedBranch(block, branch); ok {
		return chain, nil
	}
	if chain, ok := listBranch(block, branch); ok {
		return chain, nil
	}
	return nil, fmt.Errorf("has no branch %q (branches: %s)", branch, strings.Join(branchNames(block), ", "))
}

// reservedOrder lists the reserved branch names in the order they are reported, so
// the "branches: …" hint on a bad address is stable.
var reservedOrder = []string{
	branchProcess, branchError, branchThen, branchElse, branchBody, branchDefault, branchOnReject,
}

// reservedBranches maps each branch name every composite spells the same way to the
// chain it holds on a given block. A getter returns nil when the block does not have
// that slot set, so a slot this block does not use reports as "no such branch"
// rather than resolving to an empty chain.
var reservedBranches = map[string]func(*types.BlockConfig) *[]types.BlockConfig{
	branchProcess:  func(b *types.BlockConfig) *[]types.BlockConfig { return ownChain(&b.Process) },
	branchError:    func(b *types.BlockConfig) *[]types.BlockConfig { return ownChain(&b.Error) },
	branchThen:     func(b *types.BlockConfig) *[]types.BlockConfig { return subChain(b.Then) },
	branchElse:     func(b *types.BlockConfig) *[]types.BlockConfig { return subChain(b.Else) },
	branchBody:     func(b *types.BlockConfig) *[]types.BlockConfig { return subChain(b.Body) },
	branchDefault:  func(b *types.BlockConfig) *[]types.BlockConfig { return subChain(b.Default) },
	branchOnReject: func(b *types.BlockConfig) *[]types.BlockConfig { return subChain(b.OnReject) },
}

// subChain returns a sub-flow's block chain, or nil when the slot is unset.
func subChain(flow *types.FlowConfig) *[]types.BlockConfig {
	if flow == nil {
		return nil
	}
	return &flow.Process
}

// ownChain returns a block's own chain slot, or nil when it is empty.
func ownChain(chain *[]types.BlockConfig) *[]types.BlockConfig {
	if len(*chain) == 0 {
		return nil
	}
	return chain
}

// reservedBranch resolves the branches every composite names the same way.
func reservedBranch(block *types.BlockConfig, branch string) (*[]types.BlockConfig, bool) {
	get, ok := reservedBranches[branch]
	if !ok {
		return nil, false
	}
	chain := get(block)
	return chain, chain != nil
}

// listMember is one member of a composite whose branches are a list, reduced to what
// an address needs: the name it is addressed by and the chain to descend into.
type listMember struct {
	name  string
	chain *[]types.BlockConfig
}

// listMembers flattens the composites whose branches are a list — a fork's branches,
// a switch's cases, an ai-router's routes, an ai-agent's (or mcp-router's) tools —
// into one view. A block only ever populates one of those slots, so the flattened
// order is that slot's order and an index selector means what it looks like.
func listMembers(block *types.BlockConfig) []listMember {
	members := make([]listMember, 0,
		len(block.Branches)+len(block.Cases)+len(block.Routes)+len(block.Tools))
	for i := range block.Branches {
		members = append(members, listMember{block.Branches[i].Name, &block.Branches[i].Process})
	}
	for i := range block.Cases {
		members = append(members, listMember{block.Cases[i].Flow.Name, &block.Cases[i].Flow.Process})
	}
	for i := range block.Routes {
		members = append(members, listMember{block.Routes[i].Name, &block.Routes[i].Process})
	}
	for i := range block.Tools {
		members = append(members, listMember{block.Tools[i].Name, &block.Tools[i].Process})
	}
	return members
}

// listBranch resolves a list-valued branch by the member's name, or by its index
// when it has none.
func listBranch(block *types.BlockConfig, branch string) (*[]types.BlockConfig, bool) {
	index, isIndex := parseIndex(branch)
	for i, member := range listMembers(block) {
		if member.name == branch || (isIndex && index == i) {
			return member.chain, true
		}
	}
	return nil, false
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
func branchNames(block *types.BlockConfig) []string {
	var names []string
	for _, name := range reservedOrder {
		if reservedBranches[name](block) != nil {
			names = append(names, name)
		}
	}
	for i, member := range listMembers(block) {
		names = append(names, memberName(member.name, i))
	}
	if len(names) == 0 {
		return []string{"none: this is a leaf block"}
	}
	return names
}

// memberName is how a member of a list-valued branch is addressed: its name, or its
// index when it has none.
func memberName(name string, index int) string {
	if name != "" {
		return name
	}
	return strconv.Itoa(index)
}
