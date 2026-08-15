package engine

import (
	"strings"
	"testing"
)

func TestParseURITemplateRejects(t *testing.T) {
	cases := []struct {
		name     string
		template string
		wants    string
	}{
		{"no variable at all", "contacts://contact", "has no {variable}"},
		{"unclosed brace", "contacts://contact/{id", "unclosed {"},
		{"empty placeholder", "contacts://contact/{}", "empty {} placeholder"},
		{"name is not an identifier", "contacts://contact/{contact-id}", "must be a letter or underscore"},
		{"name starts with a digit", "contacts://contact/{1st}", "must be a letter or underscore"},
		{"duplicate name", "contacts://{id}/x/{id}", "more than once"},
		{"adjacent placeholders", "contacts://{a}{b}", "nothing between them"},
		{"nested brace", "contacts://{a{b}}", "nested {"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseURITemplate(tc.template)
			if err == nil {
				t.Fatalf("parseURITemplate(%q) should have failed", tc.template)
			}
			if !strings.Contains(err.Error(), tc.wants) {
				t.Errorf("error %q should mention %q", err, tc.wants)
			}
		})
	}
}

func TestURITemplateMatch(t *testing.T) {
	cases := []struct {
		name     string
		template string
		uri      string
		want     map[string]any
	}{
		{
			name: "one trailing variable", template: "contacts://contact/{id}",
			uri: "contacts://contact/42", want: map[string]any{"id": "42"},
		},
		{
			// The non-greedy rule: {id} stops at the next literal instead of
			// swallowing "/notes" too.
			name: "a variable stops at the next literal", template: "contacts://contact/{id}/notes",
			uri: "contacts://contact/42/notes", want: map[string]any{"id": "42"},
		},
		{
			name: "two variables", template: "octo://{kind}/{name}",
			uri: "octo://city/paris", want: map[string]any{"kind": "city", "name": "paris"},
		},
		{
			// Level-1 expansion percent-encodes everything outside the unreserved
			// set, so matching has to decode on the way back.
			name: "the value is percent-decoded", template: "octo://city/{name}",
			uri: "octo://city/S%C3%A3o%20Paulo", want: map[string]any{"name": "São Paulo"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tpl, err := parseURITemplate(tc.template)
			if err != nil {
				t.Fatalf("parseURITemplate: %v", err)
			}
			got, ok := tpl.match(tc.uri)
			if !ok {
				t.Fatalf("%q should match %q", tc.uri, tc.template)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("match = %v, want %v", got, tc.want)
			}
			for name, want := range tc.want {
				if got[name] != want {
					t.Errorf("match[%q] = %v, want %v", name, got[name], want)
				}
			}
		})
	}
}

func TestURITemplateDoesNotMatch(t *testing.T) {
	cases := []struct {
		name     string
		template string
		uri      string
	}{
		{"a different scheme", "contacts://contact/{id}", "octo://contact/42"},
		{"a different path", "contacts://contact/{id}", "contacts://other/42"},
		// An empty expansion would make the bare prefix a member of the family,
		// and it is not one.
		{"an empty value", "contacts://contact/{id}", "contacts://contact/"},
		{"trailing junk", "contacts://contact/{id}/notes", "contacts://contact/42/notes/extra"},
		{"a missing trailing literal", "contacts://contact/{id}/notes", "contacts://contact/42"},
		{"only the prefix", "contacts://contact/{id}", "contacts://contact"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tpl, err := parseURITemplate(tc.template)
			if err != nil {
				t.Fatalf("parseURITemplate: %v", err)
			}
			if values, ok := tpl.match(tc.uri); ok {
				t.Errorf("%q should not match %q, matched with %v", tc.uri, tc.template, values)
			}
		})
	}
}
