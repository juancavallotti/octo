package core

// Capacity is how many messages a component can serve at once, and the setting
// that changes it. A Limit of zero means unbounded.
type Capacity struct {
	Limit int
	// Setting names the knob an operator raises to lift Limit, e.g. maxOpenConns.
	Setting string
}

// CapacityReporter is an optional capability a Connector or MessageSource may
// provide when its concurrency is bounded independently of the workers of the
// flows that drive it — a connection pool, a fixed set of listeners. Callers
// type-assert for it; a connector reports after Start, a source once built.
type CapacityReporter interface {
	Capacity() Capacity
}
