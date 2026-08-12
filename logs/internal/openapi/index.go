package openapi

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Operation is one route in the compact index: enough to decide whether to fetch
// its detail, and nothing else.
type Operation struct {
	OperationID string   `json:"operationId,omitempty"`
	Method      string   `json:"method"`
	Path        string   `json:"path"`
	Summary     string   `json:"summary,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

// Index returns every operation in the description as a flat, sorted list.
//
// It exists so a caller can find out what the API offers without reading the
// description itself. The full document is mostly schemas — a trace record alone is
// thirty fields — and this is a few hundred bytes. A client that reads the index
// first and then one filtered slice sees a fraction of what it would have had to
// read otherwise, which for a caller paying per token is the difference between
// usable and not.
//
// Sorted by path then method, so the output is stable across regenerations and a
// diff of two indexes reads as a list of route changes.
func Index(raw []byte) ([]Operation, error) {
	var doc struct {
		Paths map[string]map[string]struct {
			OperationID string   `json:"operationId"`
			Summary     string   `json:"summary"`
			Tags        []string `json:"tags"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("openapi: parse spec: %w", err)
	}

	out := make([]Operation, 0, len(doc.Paths))
	for path, operations := range doc.Paths {
		for method, op := range operations {
			out = append(out, Operation{
				OperationID: op.OperationID,
				Method:      strings.ToUpper(method),
				Path:        path,
				Summary:     op.Summary,
				Tags:        op.Tags,
			})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Method < out[j].Method
	})
	return out, nil
}
