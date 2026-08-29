package api

import (
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/juancavallotti/octo/runtime/services"
)

// Environment variables this module reads.
//
// The prefix is OCTO_PLATFORM_API_ rather than OCTO_API_ because "the octo API"
// already means the orchestrator's API to anyone working in this repo. The module
// is named api; the thing it talks to is the platform API.
//
// OCTO_DEPLOYMENT_ID is deliberately reused rather than reinvented: it is the same
// identifier the k8s module already reads, so a deployment that sets it once gets
// consistent scoping from whichever module it runs.
const (
	envURL             = "OCTO_PLATFORM_API_URL"
	envToken           = "OCTO_PLATFORM_API_TOKEN"      //nolint:gosec // a variable name, not a credential
	envTokenFile       = "OCTO_PLATFORM_API_TOKEN_FILE" //nolint:gosec // a variable name, not a credential
	envHeaders         = "OCTO_PLATFORM_API_HEADERS"
	envCAFile          = "OCTO_PLATFORM_API_CA_FILE"
	envClientCertFile  = "OCTO_PLATFORM_API_CLIENT_CERT_FILE"
	envClientKeyFile   = "OCTO_PLATFORM_API_CLIENT_KEY_FILE"
	envTimeout         = "OCTO_PLATFORM_API_TIMEOUT"
	envLongTimeout     = "OCTO_PLATFORM_API_LONG_TIMEOUT"
	envStartup         = "OCTO_PLATFORM_API_STARTUP"
	envDiscoveryBudget = "OCTO_PLATFORM_API_DISCOVERY_BUDGET"
	envDeploymentID    = "OCTO_DEPLOYMENT_ID"
	envInstanceID      = "OCTO_INSTANCE_ID"
	envPodName         = "POD_NAME"
)

// The schemes this module speaks. Plaintext is allowed to loopback, where a
// sidecar's bytes never leave the pod, and to anywhere at all when there is no
// credential to protect.
const (
	schemeHTTPS = "https"
	schemeHTTP  = "http"
)

// Startup policy: what happens when discovery never answers.
const (
	// StartupRequire refuses to start. A runtime that cannot reach its platform is
	// not a working runtime, and failing at startup is how the other modules behave.
	StartupRequire = "require"
	// StartupDegrade starts anyway, with every feature unsupported. For a
	// deployment that would rather serve its non-platform flows than not serve at
	// all.
	StartupDegrade = "degrade"
)

// Defaults.
const (
	defaultTimeout         = 10 * time.Second
	defaultLongTimeout     = 60 * time.Second
	defaultDiscoveryBudget = 30 * time.Second
)

// Config is the module's resolved environment.
type Config struct {
	BaseURL   string
	Token     string
	TokenFile string
	Headers   map[string]string

	CAFile         string
	ClientCertFile string
	ClientKeyFile  string

	Timeout     time.Duration
	LongTimeout time.Duration

	Startup         string
	DiscoveryBudget time.Duration

	DeploymentID string
	InstanceID   string
}

// loadConfig reads the environment. It fails on a missing or unusable base URL,
// and on a credential this runtime would have to send in the clear; every other
// setting has a defensible default.
func loadConfig() (Config, error) {
	base, err := platformURL(services.EnvString(envURL, ""))
	if err != nil {
		return Config{}, err
	}
	headers, err := parseHeaders(services.EnvString(envHeaders, ""))
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		BaseURL:         strings.TrimRight(base.String(), "/"),
		Token:           services.EnvString(envToken, ""),
		TokenFile:       services.EnvString(envTokenFile, ""),
		Headers:         headers,
		CAFile:          services.EnvString(envCAFile, ""),
		ClientCertFile:  services.EnvString(envClientCertFile, ""),
		ClientKeyFile:   services.EnvString(envClientKeyFile, ""),
		Timeout:         envDuration(envTimeout, defaultTimeout),
		LongTimeout:     envDuration(envLongTimeout, defaultLongTimeout),
		Startup:         startupPolicy(),
		DiscoveryBudget: envDuration(envDiscoveryBudget, defaultDiscoveryBudget),
		DeploymentID:    services.EnvString(envDeploymentID, ""),
		InstanceID:      instanceID(),
	}
	if err := requireEncryptedTransport(base, cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// platformURL validates the base URL and strips what must never be there.
//
// The parse is not pedantry. This URL is logged at startup and again on every
// discovery retry, so a credential embedded in it — https://user:pass@host, the
// shape a copied curl command leaves behind — would be written to the log of
// every runtime that used it. Rejecting is better than redacting: a URL carrying
// userinfo means somebody believes that is how this authenticates, and quietly
// dropping it would leave them with a runtime that cannot log in and no reason
// why.
//
// Query and fragment go for a duller reason: every route appends its own query,
// so anything here would be silently dropped rather than sent.
func platformURL(raw string) (*url.URL, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		return nil, fmt.Errorf("api: %s is required: it is the base URL of the platform API "+
			"this runtime delegates every platform capability to", envURL)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("api: %s is not a URL: %w", envURL, err)
	}
	switch {
	case parsed.Scheme != schemeHTTP && parsed.Scheme != schemeHTTPS:
		return nil, fmt.Errorf("api: %s must be %s or %s, not %q",
			envURL, schemeHTTP, schemeHTTPS, parsed.Scheme)
	case parsed.Host == "":
		return nil, fmt.Errorf("api: %s names no host", envURL)
	case parsed.User != nil:
		return nil, fmt.Errorf("api: %s must not carry credentials in the URL; this runtime "+
			"logs the URL, so they would end up in the log. Use %s, %s or %s instead",
			envURL, envToken, envTokenFile, envHeaders)
	case parsed.RawQuery != "" || parsed.Fragment != "":
		return nil, fmt.Errorf("api: %s must not carry a query or fragment; every route appends "+
			"its own query, so anything here would be dropped rather than sent", envURL)
	}
	return parsed, nil
}

// requireEncryptedTransport refuses to send a credential over plaintext HTTP to
// anywhere but loopback.
//
// The sidecar deployment is exactly plaintext HTTP to loopback, so that stays
// allowed: those bytes never leave the pod, and requiring TLS there would mean
// issuing a certificate for 127.0.0.1 to protect a hop that has no network on it.
// Anywhere else, a bearer token over http:// is a credential on the wire in the
// clear, and no deployment wants that badly enough to be given it silently.
//
// A plaintext endpoint with NO credential is left alone: it is somebody's private
// network, the contract carries no secrets of its own, and refusing it would only
// push them into setting a token they do not need.
func requireEncryptedTransport(base *url.URL, cfg Config) error {
	if cfg.Token == "" && cfg.TokenFile == "" && len(cfg.Headers) == 0 {
		return nil
	}
	if base.Scheme == schemeHTTPS || isLoopback(base.Hostname()) {
		return nil
	}
	return fmt.Errorf("api: refusing to send credentials to %s over plaintext %s. "+
		"Use %s, or point %s at loopback for a sidecar, or unset %s/%s/%s",
		base.Host, schemeHTTP, schemeHTTPS, envURL, envToken, envTokenFile, envHeaders)
}

// isLoopback reports whether a host is this machine, where plaintext is fine.
func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// startupPolicy resolves the startup behavior, defaulting to require.
func startupPolicy() string {
	v := strings.ToLower(strings.TrimSpace(services.EnvString(envStartup, StartupRequire)))
	if v == StartupRequire || v == StartupDegrade {
		return v
	}
	slog.Warn("api: ignoring unrecognized startup policy",
		"var", envStartup, "value", v, "default", StartupRequire)
	return StartupRequire
}

// instanceID identifies this replica to the platform, for leases and leader
// election.
//
// POD_NAME is the fallback rather than a separate variable so a k8s Deployment
// that already projects the downward API gets working leases with no extra
// wiring. Cloud Run and a plain container get the hostname, which is per-instance
// there; the pid disambiguates two runtimes sharing one hostname.
func instanceID() string {
	if v := services.EnvString(envInstanceID, ""); v != "" {
		return v
	}
	if v := services.EnvString(envPodName, ""); v != "" {
		return v
	}
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "octo"
	}
	return fmt.Sprintf("%s-%d", host, os.Getpid())
}

// envDuration reads a Go duration string, falling back on anything unparseable —
// the same posture as services.EnvInt, and for the same reason: a typo in a
// deployment's environment should not be why a runtime will not start.
func envDuration(name string, fallback time.Duration) time.Duration {
	raw := services.EnvString(name, "")
	if raw == "" {
		return fallback
	}
	v, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil || v <= 0 {
		slog.Warn("api: ignoring unparseable duration",
			"var", name, "value", raw, "default", fallback)
		return fallback
	}
	return v
}

// parseHeaders reads the extra-header list: "Name: value" pairs separated by
// newlines or commas.
//
// This is the escape hatch that keeps the auth model small. Bearer tokens are
// first-class because they are what most platforms use, but an API key header, a
// tenant id, or a service-mesh header would each otherwise need its own variable.
// One list covers all of them.
func parseHeaders(raw string) (map[string]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	out := map[string]string{}
	for _, line := range strings.FieldsFunc(raw, func(r rune) bool { return r == '\n' || r == ',' }) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name, value, found := strings.Cut(line, ":")
		name = strings.TrimSpace(name)
		if !found || name == "" {
			return nil, fmt.Errorf("api: %s entry %q is not a \"Name: value\" pair", envHeaders, line)
		}
		out[name] = strings.TrimSpace(value)
	}
	return out, nil
}
