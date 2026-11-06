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
