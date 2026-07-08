package schema

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"unicode"
)

// fieldsOf reflects a settings struct into a slice of Field specs, in struct
// declaration order (which is the editor field order). A field is included only
// when it carries a non-empty `octo` tag (opt-in), letting a struct keep runtime
// fields out of the schema simply by leaving them untagged.
func fieldsOf(t reflect.Type) ([]Field, error) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("expected a struct, got %s", t.Kind())
	}
	out := make([]Field, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		field, include, err := fieldOf(t.Field(i))
		if err != nil {
			return nil, err
		}
		if include {
			out = append(out, field)
		}
	}
	return out, nil
}

// fieldOf reflects a single struct field into a Field spec. include is false when
// the field opts out of the schema (untagged, or json:"-"); the returned Field is
// then meaningless. An anonymous tagged field, a malformed tag, or an uninferable
// type is an error.
func fieldOf(sf reflect.StructField) (Field, bool, error) {
	octo := sf.Tag.Get("octo")
	if strings.TrimSpace(octo) == "" {
		return Field{}, false, nil // opt-in: untagged fields are not part of the schema
	}
	if sf.Anonymous {
		return Field{}, false, fmt.Errorf("anonymous field %s carries an octo tag; flattening is unsupported", sf.Name)
	}
	name := jsonName(sf)
	if name == "-" {
		return Field{}, false, nil
	}
	ft, err := parseOctoTag(octo)
	if err != nil {
		return Field{}, false, fmt.Errorf("field %s: %w", sf.Name, err)
	}
	ftype, err := resolveType(ft.typ, sf.Type)
	if err != nil {
		return Field{}, false, fmt.Errorf("field %s: %w", sf.Name, err)
	}

	field := Field{
		Name:        name,
		Label:       ft.label,
		Type:        ftype,
		Required:    ft.required,
		Enum:        ft.enum,
		Description: ft.desc,
		Ref:         ft.ref,
		ShowIf:      ft.showIf,
	}
	if field.Label == "" {
		field.Label = humanize(name)
	}
	if ft.hasDefault {
		def, derr := typedDefault(ft.defaultRaw, ftype)
		if derr != nil {
			return Field{}, false, fmt.Errorf("field %s: %w", sf.Name, derr)
		}
		field.Default = def
	}
	if ftype == TypeObject {
		nested, nerr := fieldsOf(sf.Type)
		if nerr != nil {
			return Field{}, false, fmt.Errorf("field %s: %w", sf.Name, nerr)
		}
		field.Fields = nested
	}
	return field, true, nil
}

// jsonName returns the field's serialized name from its json tag (first
// comma-segment), falling back to the Go field name. A json:"-" tag yields "-",
// which the caller treats as "skip".
func jsonName(sf reflect.StructField) string {
	tag := sf.Tag.Get("json")
	if tag == "" {
		return sf.Name
	}
	name, _, _ := strings.Cut(tag, ",")
	if name == "" {
		return sf.Name
	}
	return name
}

// resolveType returns the explicit FieldType when the tag names one, else infers
// it from the Go type. An unknown explicit type or an uninferable Go type is an
// error, so a missing `type=` override fails at generation.
func resolveType(explicit string, t reflect.Type) (FieldType, error) {
	if explicit != "" {
		ft := FieldType(explicit)
		if !validFieldTypes[ft] {
			return "", fmt.Errorf("unknown field type %q", explicit)
		}
		return ft, nil
	}
	return inferType(t)
}

// inferType maps a Go type to a FieldType for the mechanical cases. Semantic
// types (cel, enum, and all the flow/list slot types) and mis-inferring named
// types (e.g. a duration whose kind is int64) must instead declare `type=`.
func inferType(t reflect.Type) (FieldType, error) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Bool:
		return TypeBoolean, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return TypeNumber, nil
	case reflect.String:
		return TypeString, nil
	case reflect.Slice:
		if t.Elem().Kind() == reflect.String {
			return "string-list", nil
		}
	case reflect.Map:
		if t.Key().Kind() == reflect.String && t.Elem().Kind() == reflect.String {
			return "string-map", nil
		}
	case reflect.Struct:
		return TypeObject, nil
	}
	return "", fmt.Errorf("cannot infer a field type from Go type %s; add an explicit octo type=", t)
}

// typedDefault converts a default= clause string to the JSON-native value the
// field type implies, so booleans and numbers serialize unquoted. Typing follows
// the resolved FieldType (not the Go kind), so a duration declared type=string
// keeps its "30s" form rather than failing to parse as a number.
func typedDefault(raw string, ftype FieldType) (any, error) {
	switch ftype {
	case TypeBoolean:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("default %q is not a boolean: %w", raw, err)
		}
		return b, nil
	case TypeNumber:
		if i, err := strconv.ParseInt(raw, 10, 64); err == nil {
			return i, nil
		}
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, fmt.Errorf("default %q is not a number: %w", raw, err)
		}
		return f, nil
	default:
		return raw, nil
	}
}

// humanize turns a field name into a fallback label ("statusVar" -> "Status
// var"). It is only used when a field omits label=; migrated fields set explicit
// labels, so this stays a best-effort fallback.
func humanize(name string) string {
	if name == "" {
		return ""
	}
	var b strings.Builder
	for i, r := range name {
		switch {
		case i == 0:
			b.WriteRune(unicode.ToUpper(r))
		case unicode.IsUpper(r):
			b.WriteByte(' ')
			b.WriteRune(unicode.ToLower(r))
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
