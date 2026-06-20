package deployment

import (
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// envHTTPPort and envHTTPHost are the env vars an integration declares to bind
	// a runtime HTTP listener. Declaring HTTP_PORT (with a numeric default) is what
	// makes an integration externally exposable; HTTP_HOST is optional.
	envHTTPPort = "HTTP_PORT"
	envHTTPHost = "HTTP_HOST"
	// bindAllHost is supplied as HTTP_HOST so the runtime binds all interfaces,
	// which is required for the pod to be reachable through its Service.
	bindAllHost = "0.0.0.0"
)

// envDecl is the minimal slice of the runtime config the orchestrator parses: the
// env declarations. Parsed locally (rather than importing the runtime module) to
// keep the orchestrator decoupled from the runtime's full schema.
type envDecl struct {
	Env []struct {
		Name    string  `yaml:"name"`
		Default *string `yaml:"default"`
	} `yaml:"env"`
}

// resolveRuntimeEnv inspects an integration definition for an HTTP_PORT (and
// optional HTTP_HOST) env declaration. It returns the resolved listen port (0
// when none is declared or it has no usable numeric default), the env vars the
// orchestrator supplies into the pod, and whether the integration is externally
// exposable (a usable HTTP_PORT was found). A malformed definition resolves to
// the zero, internal-only result rather than an error: the runtime validates the
// full document at load time.
func resolveRuntimeEnv(definition string) (port int, env map[string]string, exposable bool) {
	var decl envDecl
	if err := yaml.Unmarshal([]byte(definition), &decl); err != nil {
		return 0, nil, false
	}
	var hasPort, hasHost bool
	for _, e := range decl.Env {
		switch strings.TrimSpace(e.Name) {
		case envHTTPPort:
			if e.Default != nil {
				if p, err := strconv.Atoi(strings.TrimSpace(*e.Default)); err == nil && p > 0 && p <= 65535 {
					port = p
					hasPort = true
				}
			}
		case envHTTPHost:
			hasHost = true
		}
	}
	if !hasPort {
		return 0, nil, false
	}
	env = map[string]string{envHTTPPort: strconv.Itoa(port)}
	if hasHost {
		env[envHTTPHost] = bindAllHost
	}
	return port, env, true
}
