package config

import (
	"fmt"
	"strings"
)

// Source says where a resolved setting came from, so `auth status` and
// `profile list` can tell you why octopaas is doing what it is doing.
type Source string

// The places a setting can come from, strongest first.
const (
	FromFlag    Source = "flag"
	FromEnv     Source = "env"
	FromConfig  Source = "config"
	FromDefault Source = "default"
)

// Overrides are what the command line asked for. Empty means not asked.
type Overrides struct {
	Profile     string
	URL         string
	Environment string
}

// Resolved is the platform a command talks to and how it authenticates.
type Resolved struct {
	Profile       string
	ProfileSource Source
	URL           string
	URLSource     Source
	// APIKey is empty when the command should use the profile's OAuth session.
	APIKey    string
	KeySource Source
	// Environment is the one the command acts in, "" for the default.
	Environment       string
	EnvironmentSource Source
}

// Resolve picks the profile, URL and key a command uses. Flags beat the
// environment, which beats the file.
//
// The profile is chosen first: --profile, then OCTOPAAS_PROFILE, then the file's
// current one. OCTOPAAS_URL and OCTOPAAS_KEY then override that profile's fields
// rather than replacing the profile, so a CI job with only those two set still
// works against a machine with no config at all.
func Resolve(f *File, o Overrides, getenv func(string) string) (Resolved, error) {

	var ret Resolved

	//we name the profile before we look at it, so a typo is an error and never
	//a silent fallback onto whatever happens to be current.
	switch {
	case o.Profile != "":
		ret.Profile, ret.ProfileSource = o.Profile, FromFlag
	case getenv(EnvProfile) != "":
		ret.Profile, ret.ProfileSource = getenv(EnvProfile), FromEnv
	case f.Current != "":
		ret.Profile, ret.ProfileSource = f.Current, FromConfig
	default:
		ret.Profile, ret.ProfileSource = DefaultProfile, FromDefault
	}

	p, known := f.Profiles[ret.Profile]
	if !known && ret.ProfileSource != FromDefault && (ret.ProfileSource != FromConfig || !envOnly(getenv)) {
		return Resolved{}, fmt.Errorf("there is no profile %q (from %s); you have: %s", ret.Profile, ret.ProfileSource, list(f))
	}

	switch {
	case o.URL != "":
		ret.URL, ret.URLSource = o.URL, FromFlag
	case getenv(EnvURL) != "":
		ret.URL, ret.URLSource = getenv(EnvURL), FromEnv
	case p.URL != "":
		ret.URL, ret.URLSource = p.URL, FromConfig
	default:
		ret.URL, ret.URLSource = DefaultURL, FromDefault
	}
	ret.URL = strings.TrimRight(ret.URL, "/")

	switch {
	case getenv(EnvKey) != "":
		ret.APIKey, ret.KeySource = getenv(EnvKey), FromEnv
	case p.APIKey != "":
		ret.APIKey, ret.KeySource = p.APIKey, FromConfig
	}

	//a blank value names nothing: it must not outrank the profile and then trim
	//down to the default.
	switch {
	case strings.TrimSpace(o.Environment) != "":
		ret.Environment, ret.EnvironmentSource = o.Environment, FromFlag
	case strings.TrimSpace(getenv(EnvEnvironment)) != "":
		ret.Environment, ret.EnvironmentSource = getenv(EnvEnvironment), FromEnv
	case p.Environment != "":
		ret.Environment, ret.EnvironmentSource = p.Environment, FromConfig
	default:
		ret.EnvironmentSource = FromDefault
	}
	ret.Environment = NormalizeEnvironment(ret.Environment)
	return ret, nil
}

// EnvironmentHeader is what a request names as its environment: nothing when
// none was named, so a key bound to one environment acts in its own, and the
// default spelled out when it was named, so that key is refused rather than
// quietly acting in its own.
func (r Resolved) EnvironmentHeader() string {
	if r.EnvironmentSource == FromDefault {
		return ""
	}
	return DisplayEnvironment(r.Environment)
}

// NormalizeEnvironment spells the default environment as the empty string,
// whichever way it was named.
func NormalizeEnvironment(environment string) string {
	environment = strings.TrimSpace(environment)
	if environment == DefaultEnvironment {
		return ""
	}
	return environment
}

// DisplayEnvironment names an environment for a person, spelling the default.
func DisplayEnvironment(environment string) string {
	if environment == "" {
		return DefaultEnvironment
	}
	return environment
}

// envOnly is the CI shape: everything a command needs is in the environment, so
// a config whose current profile is gone does not stand in the way.
func envOnly(getenv func(string) string) bool {
	return getenv(EnvURL) != "" && getenv(EnvKey) != ""
}

func list(f *File) string {

	names := f.Names()
	if len(names) == 0 {
		return "none yet (add one with `octopaas profile add`)"
	}
	return strings.Join(names, ", ")
}

// MaskKey shows enough of an API key to recognise it and not enough to use it.
func MaskKey(key string) string {

	if len(key) <= 12 {
		return strings.Repeat("•", len(key))
	}
	return key[:9] + "…" + key[len(key)-4:]
}
