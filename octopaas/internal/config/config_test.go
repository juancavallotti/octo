package config

import (
	"os"
	"strings"
	"testing"
)

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func twoProfiles() *File {
	return &File{
		Current: "prod",
		Profiles: map[string]Profile{
			"prod":  {URL: "https://api.octopaas.dev"},
			"local": {URL: "http://localhost:8095/", APIKey: "octo_local"},
		},
	}
}

// The precedence the guide documents: flag, then env, then the file. Each case
// names who should win and checks that it did, and that we can say why.
func TestResolvePrecedence(t *testing.T) {

	cases := []struct {
		name        string
		o           Overrides
		env         map[string]string
		wantProfile string
		wantFrom    Source
		wantURL     string
		wantKey     string
	}{
		{name: "the file's current profile by default", wantProfile: "prod", wantFrom: FromConfig, wantURL: "https://api.octopaas.dev"},
		{name: "OCTOPAAS_PROFILE beats current", env: map[string]string{EnvProfile: "local"}, wantProfile: "local", wantFrom: FromEnv, wantURL: "http://localhost:8095", wantKey: "octo_local"},
		{name: "--profile beats OCTOPAAS_PROFILE", o: Overrides{Profile: "prod"}, env: map[string]string{EnvProfile: "local"}, wantProfile: "prod", wantFrom: FromFlag, wantURL: "https://api.octopaas.dev"},
		{name: "OCTOPAAS_URL and OCTOPAAS_KEY override the chosen profile's fields", env: map[string]string{EnvProfile: "local", EnvURL: "https://ci.example.com", EnvKey: "octo_ci"}, wantProfile: "local", wantFrom: FromEnv, wantURL: "https://ci.example.com", wantKey: "octo_ci"},
		{name: "--url beats OCTOPAAS_URL", o: Overrides{URL: "https://flag.example.com"}, env: map[string]string{EnvURL: "https://env.example.com"}, wantProfile: "prod", wantFrom: FromConfig, wantURL: "https://flag.example.com"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {

			got, err := Resolve(twoProfiles(), c.o, env(c.env))
			if err != nil {
				t.Fatalf("Resolve should succeed: %v", err)
			}
			if got.Profile != c.wantProfile || got.ProfileSource != c.wantFrom {
				t.Errorf("profile = %s (from %s), want %s (from %s)", got.Profile, got.ProfileSource, c.wantProfile, c.wantFrom)
			}
			if got.URL != c.wantURL {
				t.Errorf("url = %q, want %q", got.URL, c.wantURL)
			}
			if got.APIKey != c.wantKey {
				t.Errorf("key = %q, want %q", got.APIKey, c.wantKey)
			}
		})
	}
}

// A typo in a profile name must never quietly run a command against prod.
func TestResolveRefusesAnUnknownProfile(t *testing.T) {

	for _, c := range []struct {
		name string
		o    Overrides
		env  map[string]string
	}{
		{name: "from the flag", o: Overrides{Profile: "staging"}},
		{name: "from the environment", env: map[string]string{EnvProfile: "staging"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := Resolve(twoProfiles(), c.o, env(c.env))
			if err == nil || !strings.Contains(err.Error(), `no profile "staging"`) {
				t.Fatalf("an unknown profile should be refused by name, got %v", err)
			}
		})
	}
}

// CI has no config worth speaking of: the environment carries everything, and a
// current profile that does not exist is not the job's problem.
func TestResolveEnvironmentAloneIsEnough(t *testing.T) {

	f := &File{Current: "gone", Profiles: map[string]Profile{}}
	got, err := Resolve(f, Overrides{}, env(map[string]string{EnvURL: "https://api.example.com", EnvKey: "octo_ci"}))
	if err != nil {
		t.Fatalf("URL and key from the environment should be enough: %v", err)
	}
	if got.URL != "https://api.example.com" || got.KeySource != FromEnv {
		t.Errorf("got %+v", got)
	}
}

// The first run writes a file a person can read, and only they can read it,
// because it may come to hold an API key.
func TestLoadBootstrapsAPrivateFile(t *testing.T) {

	home := t.TempDir() + "/octopaas"
	f, created, err := Load(home)
	if err != nil || !created {
		t.Fatalf("the first Load should create the file: created=%v err=%v", created, err)
	}
	if f.Current != DefaultProfile || f.Profiles[DefaultProfile].URL != DefaultURL {
		t.Errorf("a fresh config should point the default profile at %s, got %+v", DefaultURL, f)
	}

	info, err := os.Stat(Path(home))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("config.yaml should be 0600, is %o", info.Mode().Perm())
	}
	data, _ := os.ReadFile(Path(home))
	if !strings.HasPrefix(string(data), "# octopaas configuration.") {
		t.Errorf("config.yaml should open with its guidance, got:\n%s", data)
	}

	_, created, err = Load(home)
	if err != nil || created {
		t.Fatalf("a second Load should read what the first wrote: created=%v err=%v", created, err)
	}
}

func TestMaskKey(t *testing.T) {

	if got := MaskKey("octo_abcdefghijklmnopqrstuvwxyz"); got != "octo_abcd…wxyz" {
		t.Errorf("MaskKey = %q", got)
	}
	if got := MaskKey("short"); strings.Contains(got, "s") {
		t.Errorf("a short key should be hidden entirely, got %q", got)
	}
}
