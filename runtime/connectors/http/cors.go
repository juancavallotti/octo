package http

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// corsConfig is the resolved, request-ready form of corsSettings: origins are
// pre-split into an exact-match set plus a wildcard flag, and the header values
// are pre-joined. A nil *corsConfig means CORS is disabled and withCORS is a
// pass-through.
type corsConfig struct {
	anyOrigin        bool
	origins          map[string]struct{}
	allowMethods     string
	allowHeaders     string // empty => echo the requested headers on preflight
	exposeHeaders    string
	allowCredentials bool
	maxAge           string // seconds, as a header value; empty => omit
}

// defaultCORSMethods is used when a policy sets allowedOrigins but no methods.
var defaultCORSMethods = []string{
	http.MethodGet, http.MethodPost, http.MethodPut,
	http.MethodPatch, http.MethodDelete, http.MethodHead, http.MethodOptions,
}

// newCORSConfig resolves settings into a corsConfig, returning nil when no
// origins are configured (CORS off). An origin of "*" enables any-origin mode.
func newCORSConfig(set corsSettings) *corsConfig {
	if len(set.AllowedOrigins) == 0 {
		return nil
	}

	c := &corsConfig{
		origins:          make(map[string]struct{}, len(set.AllowedOrigins)),
		allowCredentials: set.AllowCredentials,
	}
	for _, o := range set.AllowedOrigins {
		o = strings.TrimSpace(o)
		if o == "" {
			continue
		}
		if o == "*" {
			c.anyOrigin = true
			continue
		}
		c.origins[o] = struct{}{}
	}
	// A policy with only blank/invalid origins is effectively disabled.
	if !c.anyOrigin && len(c.origins) == 0 {
		return nil
	}

	methods := set.AllowedMethods
	if len(methods) == 0 {
		methods = defaultCORSMethods
	}
	c.allowMethods = strings.Join(methods, ", ")
	c.allowHeaders = strings.Join(set.AllowedHeaders, ", ")
	c.exposeHeaders = strings.Join(set.ExposedHeaders, ", ")
	if d := time.Duration(set.MaxAge); d > 0 {
		c.maxAge = strconv.Itoa(int(d.Seconds()))
	}
	return c
}

// allowOriginFor returns the value to send in Access-Control-Allow-Origin for
// the given request Origin, and whether the origin is allowed at all. When
// credentials are enabled the concrete origin is echoed (never "*"), per spec.
func (c *corsConfig) allowOriginFor(origin string) (string, bool) {
	if _, ok := c.origins[origin]; ok {
		return origin, true
	}
	if c.anyOrigin {
		if c.allowCredentials {
			return origin, true
		}
		return "*", true
	}
	return "", false
}

// withCORS wraps next in the CORS middleware. When CORS is disabled it is a
// transparent pass-through. Otherwise every OPTIONS request is answered here
// with a 204 and never reaches a flow route (browsers send preflight as
// OPTIONS); non-OPTIONS requests get the CORS response headers for allowed
// origins and are forwarded to the flow.
func (c *Connector) withCORS(next http.Handler) http.Handler {
	if c.cors == nil {
		return next
	}
	cfg := c.cors
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Preflight (and any other OPTIONS) is terminated here so it never runs a
		// flow. Browsers never send a body-bearing OPTIONS, so a bare 204 is correct
		// even when the origin is not allowed.
		if r.Method == http.MethodOptions {
			cfg.writePreflight(w, r)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			cfg.applyActualHeaders(w.Header(), origin)
		}
		next.ServeHTTP(w, r)
	})
}

// applyActualHeaders adds the CORS response headers for a non-preflight request
// from origin, when that origin is allowed.
func (c *corsConfig) applyActualHeaders(h http.Header, origin string) {
	allowOrigin, ok := c.allowOriginFor(origin)
	if !ok {
		return
	}
	h.Set("Access-Control-Allow-Origin", allowOrigin)
	// Even in any-origin mode we vary on Origin because credentials mode echoes
	// the concrete origin, so the response is origin-dependent.
	h.Add("Vary", "Origin")
	if c.allowCredentials {
		h.Set("Access-Control-Allow-Credentials", "true")
	}
	if c.exposeHeaders != "" {
		h.Set("Access-Control-Expose-Headers", c.exposeHeaders)
	}
}

// writePreflight answers an OPTIONS request with a 204. When the request's Origin
// is allowed the response carries the full preflight header set; otherwise it is
// a bare 204 so the request still never reaches a flow.
func (c *corsConfig) writePreflight(w http.ResponseWriter, r *http.Request) {
	allowOrigin, ok := c.allowOriginFor(r.Header.Get("Origin"))
	if !ok {
		// Origin not allowed: a bare 204 so the request still never reaches a flow.
		w.WriteHeader(http.StatusNoContent)
		return
	}

	h := w.Header()
	h.Set("Access-Control-Allow-Origin", allowOrigin)
	h.Add("Vary", "Origin")
	if c.allowCredentials {
		h.Set("Access-Control-Allow-Credentials", "true")
	}
	h.Set("Access-Control-Allow-Methods", c.allowMethods)
	if c.allowHeaders != "" {
		h.Set("Access-Control-Allow-Headers", c.allowHeaders)
	} else if reqHeaders := r.Header.Get("Access-Control-Request-Headers"); reqHeaders != "" {
		h.Set("Access-Control-Allow-Headers", reqHeaders)
	}
	if c.maxAge != "" {
		h.Set("Access-Control-Max-Age", c.maxAge)
	}
	w.WriteHeader(http.StatusNoContent)
}
