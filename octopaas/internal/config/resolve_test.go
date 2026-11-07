package config

import "testing"

func TestResolveEnvironment(t *testing.T) {
	f := &File{Current: "work", Profiles: map[string]Profile{
		"work":  {URL: "https://a", Environment: "staging"},
		"other": {URL: "https://b"},
	}}
	cases := []struct {
		name       string
		o          Overrides
		env        map[string]string
		want       string
		wantSource Source
	}{
		{"the profile's", Overrides{}, nil, "staging", FromConfig},
		{"the environment variable over the profile", Overrides{}, map[string]string{EnvEnvironment: "qa"}, "qa", FromEnv},
		{"the flag over both", Overrides{Environment: "prod"}, map[string]string{EnvEnvironment: "qa"}, "prod", FromFlag},
		{"the default when nothing names one", Overrides{Profile: "other"}, nil, "", FromDefault},
		{"octo is the default", Overrides{Environment: "octo"}, nil, "", FromFlag},
		{"a blank variable names nothing", Overrides{}, map[string]string{EnvEnvironment: " "}, "staging", FromConfig},
		{"a blank flag names nothing", Overrides{Environment: "  "}, nil, "staging", FromConfig},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := Resolve(f, tc.o, func(k string) string { return tc.env[k] })
			if err != nil {
				t.Fatal(err)
			}
			if r.Environment != tc.want || r.EnvironmentSource != tc.wantSource {
				t.Errorf("environment = %q (%s), want %q (%s)", r.Environment, r.EnvironmentSource, tc.want, tc.wantSource)
			}
		})
	}
}

// A named default goes on the wire, so a key bound elsewhere is refused instead
// of acting in its own; an unnamed one sends nothing.
func TestEnvironmentHeader(t *testing.T) {
	cases := []struct {
		name string
		r    Resolved
		want string
	}{
		{"nothing named", Resolved{EnvironmentSource: FromDefault}, ""},
		{"the default named", Resolved{EnvironmentSource: FromFlag}, DefaultEnvironment},
		{"another named", Resolved{Environment: "stage", EnvironmentSource: FromEnv}, "stage"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.r.EnvironmentHeader(); got != tc.want {
				t.Errorf("header = %q, want %q", got, tc.want)
			}
		})
	}
}

// A profile that chose the default names it, so the choice reaches the platform
// rather than reading as no choice at all.
func TestAProfileThatChoseTheDefaultNamesIt(t *testing.T) {
	f := &File{Current: "p", Profiles: map[string]Profile{"p": {URL: "https://a", Environment: DefaultEnvironment}}}
	r, err := Resolve(f, Overrides{}, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if r.Environment != "" || r.EnvironmentSource != FromConfig || r.EnvironmentHeader() != DefaultEnvironment {
		t.Errorf("resolved %q (%s), header %q; want the default, named from the config", r.Environment, r.EnvironmentSource, r.EnvironmentHeader())
	}
}
