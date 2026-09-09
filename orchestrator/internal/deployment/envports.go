package deployment

import (
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// envHTTPPort and envHTTPHost are the env vars a runtime HTTP listener binds to.
	// An integration may declare HTTP_PORT (with a numeric default) or leave its http
	// connector to read it off the environment; either way, binding an HTTP source is
	// what makes it externally exposable. HTTP_HOST is optional.
	envHTTPPort = "HTTP_PORT"
	envHTTPHost = "HTTP_HOST"
	// envObservabilityURL is supplied by the orchestrator to deployments granted the
	// observability API, so a binding may not target it: a deployment whose pod
	// carries OBSERVABILITY_URL while its record says it was never granted the API is a
	// record that lies, and the record is the thing a future access model reads.
	envObservabilityURL = "OBSERVABILITY_URL"
	// bindAllHost is supplied as HTTP_HOST so the runtime binds all interfaces,
	// which is required for the pod to be reachable through its Service.
	bindAllHost = "0.0.0.0"
)

// envDecl is the minimal slice of the runtime config the orchestrator parses: the
// env declarations. Parsed locally (rather than importing the runtime module) to
// keep the orchestrator decoupled from the runtime's full schema.
type envDecl struct {
	Env []struct {
		Name     string  `yaml:"name"`
		Default  *string `yaml:"default"`
		Required bool    `yaml:"required"`
	} `yaml:"env"`
}

// EnvVarDecl is one environment variable an integration declares, surfaced to the
// deploy modal so it can prompt the operator to fill it (with a literal value or a
// cluster secret). The orchestrator-managed HTTP_PORT/HTTP_HOST are never included.
type EnvVarDecl struct {
	Name     string
	Default  string
	Required bool
}

// declaredEnvVars lists the environment variables an integration declares, sorted
// by name and excluding the orchestrator-managed HTTP_PORT/HTTP_HOST. A malformed
// definition yields no vars (the runtime validates the full document at load time).
func declaredEnvVars(definition string) []EnvVarDecl {
	var decl envDecl
	if err := yaml.Unmarshal([]byte(definition), &decl); err != nil {
		return nil
	}
	out := make([]EnvVarDecl, 0, len(decl.Env))
	for _, e := range decl.Env {
		name := strings.TrimSpace(e.Name)
		if name == "" || name == envHTTPPort || name == envHTTPHost || name == envObservabilityURL {
			continue
		}
		d := ""
		if e.Default != nil {
			d = *e.Default
		}
		out = append(out, EnvVarDecl{Name: name, Default: d, Required: e.Required})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// providedEnvKeys is the set of env var names a deploy's bindings supply — those
// carrying a literal value or a secret reference. An empty binding provides
// nothing.
func providedEnvKeys(bindings map[string]EnvBinding) map[string]struct{} {
	keys := make(map[string]struct{}, len(bindings))
	for name, b := range bindings {
		if b.Value != "" || b.Secret != "" {
			keys[name] = struct{}{}
		}
	}
	return keys
}

// parseDotEnvKeys extracts the variable names from .env-style content; values are
// ignored, since only presence matters for the required-var check. It mirrors the
// runtime's dotenv reader loosely — `KEY=VALUE` lines with an optional `export `
// prefix, skipping blanks and `#` comments.
func parseDotEnvKeys(content string) map[string]struct{} {
	keys := map[string]struct{}{}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}
		if key := strings.TrimSpace(line[:eq]); key != "" {
			keys[key] = struct{}{}
		}
	}
	return keys
}

// missingRequiredEnv returns the names of required env vars the definition
// declares that provided does not cover, sorted. A declared default does NOT
// satisfy a required var (matching the runtime), so only an explicitly provided
// binding or an env-resource key counts.
func missingRequiredEnv(definition string, provided map[string]struct{}) []string {
	var missing []string
	for _, ev := range declaredEnvVars(definition) {
		if !ev.Required {
			continue
		}
		if _, ok := provided[ev.Name]; !ok {
			missing = append(missing, ev.Name)
		}
	}
	sort.Strings(missing)
	return missing
}

// Exposable reports whether a definition serves HTTP on a port this platform can
// wire, and so can be reached over it.
//
// Exported for the dev-run service, which asks the same question of a live definition
// to decide whether to publish a public host. It shares resolveRuntimeEnv rather than
// re-parsing because the rules are subtler than they look — see wiresInjectedPort —
// and a second copy would answer differently on exactly the definitions that matter.
func Exposable(definition string) bool {
	_, _, exposable := resolveRuntimeEnv(definition)
	return exposable
}

// resolveRuntimeEnv works out what an integration listens on. It returns the
// resolved listen port (0 when nothing here can reach it), the env vars the
// orchestrator supplies into the pod, and whether the integration is externally
// exposable.
//
// Exposability is not "does it declare HTTP_PORT". The orchestrator does not read
// the port so much as CHOOSE it — it injects HTTP_PORT and points a Service at what
// it injected — so the only question that matters is whether the listener will take
// that value (wiresInjectedPort). A declaration that nothing reads leaves a pod
// serving a port the Service does not target; an HTTP source with no declaration at
// all is wired perfectly well, because the http connector reads HTTP_PORT itself.
//
// The port injected is the declared default when there is a usable one — a definition
// that names a port should see that port in its pod — and the runtime's own default
// otherwise. A malformed definition resolves to the zero, internal-only result rather
// than an error: the runtime validates the full document at load time.
func resolveRuntimeEnv(definition string) (port int, env map[string]string, exposable bool) {
	var decl envDecl
	if err := yaml.Unmarshal([]byte(definition), &decl); err != nil {
		return 0, nil, false
	}
	declared := map[string]bool{}
	port = defaultImplicitPort
	for _, e := range decl.Env {
		name := strings.TrimSpace(e.Name)
		declared[name] = true
		if name != envHTTPPort || e.Default == nil {
			continue
		}
		if p, err := strconv.Atoi(strings.TrimSpace(*e.Default)); err == nil && p > 0 && p <= 65535 {
			port = p
		}
	}
	if !wiresInjectedPort(definition, declared) {
		return 0, nil, false
	}
	// Both halves of the address are supplied, always: the port the listener is to
	// bind, and the bind-all host the pod needs to be reachable through its Service.
	// A definition that pins either of those to something else is not exposable in
	// the first place, so there is nothing here to override that anyone chose.
	return port, map[string]string{envHTTPPort: strconv.Itoa(port), envHTTPHost: bindAllHost}, true
}

// httpConnectorType is the runtime type of the connector that owns an HTTP
// listener, both as a connector's declared `type` and as the `type` of the
// sources it exposes. A source names it in one of three ways, all of which end up
// at the same connector (runtime connectorSet.resolveConnector): binding a
// configured instance by name, naming the type itself where an instance name goes
// (the editor's fallback), or leaving the binding empty and letting the source
// type resolve it.
const httpConnectorType = "http"

// defaultImplicitPort mirrors the port the http connector falls back to when nothing
// supplies HTTP_PORT (runtime/connectors/http: defaultPort). The orchestrator injects
// a port either way, so this is only the number it picks for a definition that names
// none — chosen to match what the same document does when run by hand.
const defaultImplicitPort = 8080

// httpDecl is the second slice of the definition this file parses: the connectors
// declared and the sources flows bind to, enough to answer whether an injected
// address reaches anything, without importing the runtime's full schema.
type httpDecl struct {
	Connectors []struct {
		Name     string `yaml:"name"`
		Type     string `yaml:"type"`
		Settings struct {
			// Host and Port are read untyped because a definition may write either
			// as a literal, as a number, or as an unresolved `${VAR}` reference.
			Host any `yaml:"host"`
			Port any `yaml:"port"`
		} `yaml:"settings"`
	} `yaml:"connectors"`
	Flows []struct {
		Source *struct {
			Connector string `yaml:"connector"`
			Type      string `yaml:"type"`
		} `yaml:"source"`
	} `yaml:"flows"`
}

// wiresInjectedPort reports whether an injected HTTP_PORT/HTTP_HOST actually reaches
// a listener that serves routes. Everything below is a way for that to fail, and each
// one is a pod that comes up "networked" with a Service pointing at nothing:
//
//   - Nothing binds an HTTP source. A listener with no routes answers 404, and a
//     definition with no HTTP source at all listens only because a configured
//     connector does — neither is an endpoint worth publishing, however loudly the
//     env block declares HTTP_PORT.
//   - The connector pins its own port (`port: 9000`, or `port: 0` for an
//     OS-assigned one). Settings beat the environment in the runtime, so the
//     injected value is ignored and the pod serves a port nothing routes to.
//   - The connector takes its port from some OTHER variable (`port: ${API_PORT}`),
//     which resolves to that variable's value, not to what was injected.
//   - The connector reads `${HTTP_PORT}` but the env block never declares it, which
//     is a load error in the runtime: the pod does not come up at all.
//   - The connector pins a host that is not bind-all (`host: 127.0.0.1`). The port
//     is right and the pod is still unreachable through its Service.
//   - The binding is ambiguous — two configured http connectors and a source that
//     names neither — which the runtime refuses to start.
//   - Two configured http connectors are both left to the environment. They receive
//     the same injected port and the second one's Start fails on "address already in
//     use", taking the whole run down with it.
//
// What is left is the shape the platform can wire: one HTTP source, reached through a
// connector whose address is the environment's to decide.
func wiresInjectedPort(definition string, declared map[string]bool) bool {
	var decl httpDecl
	if err := yaml.Unmarshal([]byte(definition), &decl); err != nil {
		return false
	}

	// The configured http instances, by name, each saying whether its address is the
	// environment's to decide.
	envBound := map[string]bool{}
	var names []string
	var envBoundCount int
	for _, c := range decl.Connectors {
		if strings.TrimSpace(c.Type) != httpConnectorType {
			continue
		}
		name := strings.TrimSpace(c.Name)
		names = append(names, name)
		envBound[name] = takesEnvPort(c.Settings.Port, declared) && takesEnvHost(c.Settings.Host, declared)
		if envBound[name] {
			envBoundCount++
		}
	}
	// Two of them racing for the injected port is not a wiring question but a run
	// that dies on startup, whichever one a source happens to bind.
	if envBoundCount > 1 {
		return false
	}

	for _, f := range decl.Flows {
		if f.Source == nil {
			continue
		}
		bind := strings.TrimSpace(f.Source.Connector)
		// An explicit binding to a configured instance wins, whatever its type.
		if bind != "" {
			if bound, ok := envBound[bind]; ok {
				return bound
			}
			// Not an instance name: it only resolves if it names the type itself.
			if bind != httpConnectorType {
				continue
			}
		} else if strings.TrimSpace(f.Source.Type) != httpConnectorType {
			continue
		}
		// The source wants an http connector without naming an instance: a lone
		// configured one binds implicitly, several are ambiguous and the runtime
		// refuses to start, none means a default instance off the environment.
		switch len(names) {
		case 0:
			return true
		case 1:
			return envBound[names[0]]
		default:
			return false
		}
	}
	return false
}

// takesEnvPort reports whether a connector leaves its listen port to the environment:
// either it sets none (the runtime then reads HTTP_PORT itself) or it substitutes
// HTTP_PORT, which resolves to the injected value because the OS environment outranks
// a declared default. A reference only resolves if the variable is declared — an
// undeclared one is a load error, not a listener — and any other value, literal or
// otherwise, is the definition deciding the port for itself.
func takesEnvPort(raw any, declared map[string]bool) bool {
	return takesEnvVar(raw, envHTTPPort, declared)
}

// takesEnvHost reports whether a connector's bind address is one the platform can
// reach: left to the environment (unset or `${HTTP_HOST}`), or pinned to bind-all
// itself, which is what the orchestrator would have injected anyway. A pinned
// loopback or a single interface is a pod that serves only itself.
func takesEnvHost(raw any, declared map[string]bool) bool {
	if s, ok := raw.(string); ok && strings.TrimSpace(s) == bindAllHost {
		return true
	}
	return takesEnvVar(raw, envHTTPHost, declared)
}

// takesEnvVar is the shared shape of both questions: an absent setting leaves the
// runtime to read the variable itself, and an exact `${NAME}` reference to a declared
// variable resolves to what was injected. Anything else pins the value.
func takesEnvVar(raw any, name string, declared map[string]bool) bool {
	switch v := raw.(type) {
	case nil:
		return true
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return true
		}
		return s == "${"+name+"}" && declared[name]
	default:
		return false
	}
}
