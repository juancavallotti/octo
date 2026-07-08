package schema

import (
	"reflect"
	"testing"
)

type inner struct {
	Kind  string `json:"kind" octo:"label=Kind,type=enum,enum=a|b"`
	Token string `json:"token" octo:"label=Token,showIf=kind=a"`
}

type walkerSample struct {
	Str      string            `json:"str" octo:"label=Str,required"`
	Expr     string            `json:"expr" octo:"label=Expr,type=cel"`
	Num      int               `json:"num" octo:"label=Num,default=5"`
	Flag     bool              `json:"flag" octo:"label=Flag,default=true"`
	Ptr      *bool             `json:"ptr" octo:"label=Ptr,default=false"`
	Tags     []string          `json:"tags" octo:"label=Tags"`
	Headers  map[string]string `json:"headers" octo:"label=Headers"`
	Conn     string            `json:"conn" octo:"label=Conn,ref=connector:http-client"`
	Auth     inner             `json:"auth" octo:"label=Auth,type=object"`
	Ignored  string            `json:"ignored"`                 // no octo tag -> excluded
	Excluded string            `json:"-" octo:"label=Excluded"` // json:"-" -> excluded
}

func fieldByName(fields []Field, name string) (Field, bool) {
	for _, f := range fields {
		if f.Name == name {
			return f, true
		}
	}
	return Field{}, false
}

func TestFieldsOfCoversEveryPath(t *testing.T) {
	fields, err := fieldsOf(reflect.TypeOf(walkerSample{}))
	if err != nil {
		t.Fatalf("fieldsOf: %v", err)
	}

	// Untagged and json:"-" fields are excluded; order is declaration order.
	gotNames := make([]string, len(fields))
	for i, f := range fields {
		gotNames[i] = f.Name
	}
	wantNames := []string{"str", "expr", "num", "flag", "ptr", "tags", "headers", "conn", "auth"}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("field set/order = %v, want %v", gotNames, wantNames)
	}

	str, _ := fieldByName(fields, "str")
	if str.Type != "string" || !str.Required {
		t.Errorf("str: got type=%q required=%v", str.Type, str.Required)
	}
	if expr, _ := fieldByName(fields, "expr"); expr.Type != "cel" {
		t.Errorf("expr: explicit type override not applied: %q", expr.Type)
	}
	if num, _ := fieldByName(fields, "num"); num.Type != "number" || num.Default != int64(5) {
		t.Errorf("num: got type=%q default=%v(%T)", num.Type, num.Default, num.Default)
	}
	if flag, _ := fieldByName(fields, "flag"); flag.Type != "boolean" || flag.Default != true {
		t.Errorf("flag: got type=%q default=%v", flag.Type, flag.Default)
	}
	if ptr, _ := fieldByName(fields, "ptr"); ptr.Type != "boolean" || ptr.Default != false {
		t.Errorf("ptr (*bool): got type=%q default=%v", ptr.Type, ptr.Default)
	}
	if tags, _ := fieldByName(fields, "tags"); tags.Type != "string-list" {
		t.Errorf("tags: inferred type = %q, want string-list", tags.Type)
	}
	if h, _ := fieldByName(fields, "headers"); h.Type != "string-map" {
		t.Errorf("headers: inferred type = %q, want string-map", h.Type)
	}
	conn, _ := fieldByName(fields, "conn")
	if conn.Ref == nil || conn.Ref.Kind != "connector" || conn.Ref.ConnectorType != "http-client" {
		t.Errorf("conn: ref = %+v", conn.Ref)
	}

	auth, _ := fieldByName(fields, "auth")
	if auth.Type != "object" || len(auth.Fields) != 2 {
		t.Fatalf("auth: type=%q, nested fields=%d", auth.Type, len(auth.Fields))
	}
	if auth.Fields[0].Type != "enum" || !reflect.DeepEqual(auth.Fields[0].Enum, []string{"a", "b"}) {
		t.Errorf("auth.kind: type=%q enum=%v", auth.Fields[0].Type, auth.Fields[0].Enum)
	}
	tok := auth.Fields[1]
	if tok.ShowIf == nil || tok.ShowIf.Field != "kind" || tok.ShowIf.Equals != "a" {
		t.Errorf("auth.token showIf = %+v", tok.ShowIf)
	}
}

type badKind struct {
	Ch chan int `json:"ch" octo:"label=Ch"`
}

func TestFieldsOfErrorsOnUninferableType(t *testing.T) {
	if _, err := fieldsOf(reflect.TypeOf(badKind{})); err == nil {
		t.Fatal("expected an error for an uninferable Go type, got nil")
	}
}

type badTag struct {
	X string `json:"x" octo:"label=X,bogus=1"`
}

func TestFieldsOfErrorsOnUnknownClause(t *testing.T) {
	if _, err := fieldsOf(reflect.TypeOf(badTag{})); err == nil {
		t.Fatal("expected an error for an unknown octo clause, got nil")
	}
}

func TestHumanizeFallbackLabel(t *testing.T) {
	if got := humanize("statusVar"); got != "Status var" {
		t.Errorf("humanize(statusVar) = %q, want %q", got, "Status var")
	}
}

func TestMarshalIsIndentedWithTrailingNewline(t *testing.T) {
	out, err := Marshal(Capabilities{Blocks: []Block{}, Connectors: []Connector{}})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if len(out) == 0 || out[len(out)-1] != '\n' {
		t.Error("expected a trailing newline")
	}
	want := "{\n  \"blocks\": [],\n  \"connectors\": []\n}\n"
	if string(out) != want {
		t.Errorf("Marshal empty catalogue =\n%q\nwant\n%q", out, want)
	}
}
