package pinecone

import (
	"testing"

	"github.com/juancavallotti/octo/runtime/types"
)

func TestResolveDeleteMode(t *testing.T) {
	cases := []struct {
		name string
		cfg  deleteSettings
		want deleteMode
		err  bool
	}{
		{"none set", deleteSettings{}, 0, true},
		{"ids and filter set", deleteSettings{IDs: "body.ids", Filter: "body.filter"}, 0, true},
		{"ids and deleteAll set", deleteSettings{IDs: "body.ids", DeleteAll: true}, 0, true},
		{"all three set", deleteSettings{IDs: "x", Filter: "y", DeleteAll: true}, 0, true},
		{"ids only", deleteSettings{IDs: "body.ids"}, deleteByIDs, false},
		{"filter only", deleteSettings{Filter: "body.filter"}, deleteByFilter, false},
		{"deleteAll only", deleteSettings{DeleteAll: true}, deleteAllInNamespace, false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			mode, err := resolveDeleteMode(tt.cfg)
			if tt.err {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveDeleteMode: %v", err)
			}
			if mode != tt.want {
				t.Errorf("mode = %v, want %v", mode, tt.want)
			}
		})
	}
}

func TestNewDeleteValidation(t *testing.T) {
	deps := depsWith("pc", &Connector{})

	if _, err := newDelete(types.Settings{"ids": "body.ids"}, deps); err == nil {
		t.Error("expected error when connector is missing")
	}
	if _, err := newDelete(types.Settings{"connector": "pc"}, deps); err == nil {
		t.Error("expected error when no delete mode is set")
	}
	if _, err := newDelete(types.Settings{"connector": "pc", "ids": "x", "filter": "y"}, deps); err == nil {
		t.Error("expected error when more than one delete mode is set")
	}
	if _, err := newDelete(types.Settings{"connector": "nope", "deleteAll": true}, deps); err == nil {
		t.Error("expected error when connector is not configured")
	}
	if _, err := newDelete(types.Settings{"connector": "pc", "ids": "body.("}, deps); err == nil {
		t.Error("expected error compiling a malformed ids expression")
	}
}

func TestNewDeleteAllCompilesNoExpression(t *testing.T) {
	proc, err := newDelete(types.Settings{"connector": "pc", "deleteAll": true}, depsWith("pc", &Connector{}))
	if err != nil {
		t.Fatalf("newDelete: %v", err)
	}
	p, ok := proc.(*deleteProcessor)
	if !ok {
		t.Fatalf("proc is %T, want *deleteProcessor", proc)
	}
	if p.mode != deleteAllInNamespace || p.ids != nil || p.filter != nil {
		t.Errorf("processor = %+v", p)
	}
}
