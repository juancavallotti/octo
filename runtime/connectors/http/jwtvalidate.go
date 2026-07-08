// This file provides the "jwt-validate" block: an HTTP-request auth filter. It
// verifies a bearer JWT against an OIDC provider and, on failure, stops the flow
// with a configurable response (401 by default) so the rest of the chain never
// runs — the "filter" pattern. On success it injects the verified claims into
// vars so downstream blocks can authorize on them.
//
// The token is read from a request-header variable (default "Authorization",
// stripping a "Bearer " prefix); the http route source must be configured to
// copy that header into vars (source `headers: [Authorization]`).
//
// Signing keys are resolved one of three ways (the `mode` setting):
//   - "discover" (default): OIDC discovery from the configured issuer
//     (<issuer>/.well-known/openid-configuration) -> its JWKS.
//   - "jwks": fetch the JWKS directly from `jwksUrl` (cached/refreshed).
//   - "inline": a PEM/JWK public key supplied via `publicKey`, or loaded from a
//     resource named by `publicKeyResource`.
//
// Key material is resolved lazily on first request (sync.Once) so a provider that
// is briefly unreachable at boot does not fail flow construction; inline mode
// needs no network.
package http

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/types"
)

func init() {
	core.MustRegisterBlock("jwt-validate", newJWTValidate)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:     "jwt-validate",
		Label:    "JWT Validate",
		Category: "processor",
		Group:    "Flow Control",
		Icon:     "ShieldAlert",
		Description: "Filter: verify a bearer JWT against an OIDC provider. On failure it stops " +
			"the flow with a configurable response (401 by default) so the rest of the chain " +
			"never runs; on success it injects the verified claims into vars (claimsVar, default " +
			"`jwt`) and the subject into vars.sub. Reads the token from a request-header variable " +
			"— the http route source must copy that header into vars (source headers: " +
			"[Authorization]).",
		Config: reflect.TypeFor[jwtValidateSettings](),
	})
}

const (
	// defaultTokenHeader is the request-header variable the token is read from.
	defaultTokenHeader = "Authorization"
	// bearerPrefix is stripped (case-insensitively) from the token header value.
	bearerPrefix = "bearer "
	// defaultClaimsVar is the variable the verified claims are stored under.
	defaultClaimsVar = "jwt"
	// defaultRejectStatus is the response status used when verification fails.
	// (httpStatusVar is defined in source.go, the same package.)
	defaultRejectStatus = 401

	modeDiscover = "discover"
	modeJWKS     = "jwks"
	modeInline   = "inline"
)

// jwtValidateSettings is the jwt-validate block's typed configuration. Field
// order follows the editor's capability schema (which differs from the runtime
// struct's original order); decoding is by json name, so order is presentational.
type jwtValidateSettings struct {
	// How signing keys are resolved: discover (OIDC discovery from issuer), jwks
	// (fetch jwksUrl directly), or inline (publicKey / publicKeyResource).
	Mode string `json:"mode" octo:"label=Key mode,type=enum,enum=discover|jwks|inline,default=discover"`
	// Expected token issuer (iss). Required for discover mode (its discovery URL).
	// For jwks/inline it is the expected iss, checked when set.
	Issuer string `json:"issuer" octo:"label=Issuer"`
	// Expected token audience (aud). When empty the audience check is skipped.
	Audience string `json:"audience" octo:"label=Audience"`
	// Accepted signing algorithms, e.g. RS256. When empty go-oidc's defaults apply.
	Algorithms []string `json:"algorithms" octo:"label=Algorithms"`
	// JWKS endpoint for mode jwks (cached and refreshed).
	JWKSURL string `json:"jwksUrl" octo:"label=JWKS URL"`
	// Literal PEM public key or certificate for mode inline. Set exactly one of
	// publicKey / publicKeyResource.
	PublicKey string `json:"publicKey" octo:"label=Public key (PEM)"`
	// Resource id whose content is the PEM/JWK public key, for mode inline. Set
	// exactly one of publicKey / publicKeyResource.
	PublicKeyResource string `json:"publicKeyResource" octo:"label=Public key resource,ref=template"`
	// Variable holding the bearer token; a `Bearer ` prefix is stripped. The http
	// source must copy this header into vars.
	TokenHeader string `json:"tokenHeader" octo:"label=Token variable,default=Authorization"`
	// Variable the verified claims map is stored under on success (the subject is
	// also set on vars.sub).
	ClaimsVar string `json:"claimsVar" octo:"label=Claims variable,default=jwt"`
	// HTTP status of the built-in rejection response when verification fails.
	// Defaults to 401.
	RejectStatus int `json:"rejectStatus" octo:"label=Reject status"`
	// Protected-resource-metadata URL (RFC 9728) advertised in the WWW-Authenticate
	// challenge as resource_metadata="…". Set this to make the endpoint an
	// MCP-compliant protected resource.
	ResourceMetadataURL string `json:"resourceMetadataUrl" octo:"label=Resource metadata URL"`
	// By default, when an issuer is configured, a rejection sets a
	// WWW-Authenticate: Bearer challenge in vars (list "WWW-Authenticate" in the
	// route source's responseHeaders to emit it). Check to opt out.
	DisableWWWAuthenticate bool `json:"disableWwwAuthenticate" octo:"label=Disable WWW-Authenticate,default=false"`
}

// jwtValidate verifies a bearer JWT and, on failure, rejects the request. The
// verifier is built lazily (once) because discover/jwks modes reach the network.
type jwtValidate struct {
	tokenHeader  string
	claimsVar    string
	rejectStatus int
	// wwwAuthenticate, issuer, resourceMetadataURL drive the WWW-Authenticate
	// challenge set on a rejection (see challenge).
	wwwAuthenticate     bool
	issuer              string
	resourceMetadataURL string

	once     sync.Once
	verifier *oidc.IDTokenVerifier
	buildErr error
	build    func(ctx context.Context) (*oidc.IDTokenVerifier, error)
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newJWTValidate(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg jwtValidateSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}

	mode := cfg.Mode
	if mode == "" {
		mode = modeDiscover
	}

	// Pre-resolve inline key material at build time (no network) so config errors
	// surface early; discover/jwks are deferred to first request.
	build, err := verifierBuilder(mode, cfg, deps)
	if err != nil {
		return nil, fmt.Errorf("jwt-validate: %w", err)
	}

	block := &jwtValidate{
		tokenHeader:         orDefaultStr(cfg.TokenHeader, defaultTokenHeader),
		claimsVar:           orDefaultStr(cfg.ClaimsVar, defaultClaimsVar),
		rejectStatus:        cfg.RejectStatus,
		wwwAuthenticate:     !cfg.DisableWWWAuthenticate && cfg.Issuer != "",
		issuer:              cfg.Issuer,
		resourceMetadataURL: cfg.ResourceMetadataURL,
		build:               build,
	}
	if block.rejectStatus == 0 {
		block.rejectStatus = defaultRejectStatus
	}
	return block, nil
}

// verifierConfig builds the oidc verification config shared by all modes.
func verifierConfig(cfg jwtValidateSettings) *oidc.Config {
	oc := &oidc.Config{
		ClientID:             cfg.Audience,
		SupportedSigningAlgs: cfg.Algorithms,
	}
	if cfg.Audience == "" {
		oc.SkipClientIDCheck = true
	}
	return oc
}

// verifierBuilder returns a closure that constructs the verifier, validating the
// static parts of the config now and deferring any network to when it is called.
func verifierBuilder(
	mode string,
	cfg jwtValidateSettings,
	deps core.BlockDeps,
) (func(ctx context.Context) (*oidc.IDTokenVerifier, error), error) {
	switch mode {
	case modeDiscover:
		if cfg.Issuer == "" {
			return nil, errors.New("discover mode requires an issuer")
		}
		return func(ctx context.Context) (*oidc.IDTokenVerifier, error) {
			provider, err := oidc.NewProvider(ctx, cfg.Issuer)
			if err != nil {
				return nil, fmt.Errorf("oidc discovery: %w", err)
			}
			return provider.Verifier(verifierConfig(cfg)), nil
		}, nil

	case modeJWKS:
		if cfg.JWKSURL == "" {
			return nil, errors.New("jwks mode requires a jwksUrl")
		}
		return func(ctx context.Context) (*oidc.IDTokenVerifier, error) {
			keySet := oidc.NewRemoteKeySet(ctx, cfg.JWKSURL)
			return oidc.NewVerifier(cfg.Issuer, keySet, issuerAwareConfig(cfg)), nil
		}, nil

	case modeInline:
		keys, err := inlineKeys(cfg, deps)
		if err != nil {
			return nil, err
		}
		keySet := &oidc.StaticKeySet{PublicKeys: keys}
		verifier := oidc.NewVerifier(cfg.Issuer, keySet, issuerAwareConfig(cfg))
		return func(context.Context) (*oidc.IDTokenVerifier, error) {
			return verifier, nil
		}, nil

	default:
		return nil, fmt.Errorf("unknown mode %q (want discover, jwks, or inline)", mode)
	}
}

// issuerAwareConfig is the verifier config for modes that build their own verifier
// (jwks/inline): when no issuer is configured the iss check is skipped.
func issuerAwareConfig(cfg jwtValidateSettings) *oidc.Config {
	oc := verifierConfig(cfg)
	if cfg.Issuer == "" {
		oc.SkipIssuerCheck = true
	}
	return oc
}

// inlineKeys resolves the inline public key(s) from the literal PEM/JWK or a
// resource, requiring exactly one source.
func inlineKeys(cfg jwtValidateSettings, deps core.BlockDeps) ([]crypto.PublicKey, error) {
	if (cfg.PublicKey == "") == (cfg.PublicKeyResource == "") {
		return nil, errors.New("inline mode requires exactly one of publicKey or publicKeyResource")
	}
	pemData := cfg.PublicKey
	if cfg.PublicKeyResource != "" {
		if deps.Resources == nil {
			return nil, errors.New("inline mode publicKeyResource needs a resource loader")
		}
		content, err := deps.Resources.Load(context.Background(), core.ResourceKindTemplate, cfg.PublicKeyResource)
		if err != nil {
			return nil, fmt.Errorf("load publicKeyResource %q: %w", cfg.PublicKeyResource, err)
		}
		pemData = string(content)
	}
	key, err := parsePublicKey(pemData)
	if err != nil {
		return nil, err
	}
	return []crypto.PublicKey{key}, nil
}

// parsePublicKey parses a PEM-encoded RSA/ECDSA public key (PKIX or a certificate).
func parsePublicKey(pemData string) (crypto.PublicKey, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(pemData)))
	if block == nil {
		return nil, errors.New("publicKey is not valid PEM")
	}
	switch block.Type {
	case "CERTIFICATE":
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse certificate: %w", err)
		}
		return cert.PublicKey, nil
	default:
		key, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse public key: %w", err)
		}
		switch key.(type) {
		case *rsa.PublicKey, *ecdsa.PublicKey:
			return key, nil
		default:
			return nil, fmt.Errorf("unsupported public key type %T", key)
		}
	}
}

// Process verifies the bearer token. On success it injects the claims and passes
// the message through; on any failure it configures a reject response and stops
// the flow.
func (j *jwtValidate) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	verifier, err := j.verifierFor(ctx)
	if err != nil {
		// A key-resolution failure is an operational error, not a bad token.
		return nil, fmt.Errorf("jwt-validate: %w", err)
	}

	token := bearerToken(msg, j.tokenHeader)
	if token == "" {
		// Routine: an unauthenticated request — also the OAuth discovery handshake,
		// which expects the 401 + challenge — so this is not warn-worthy. A token
		// that is present but fails below is a real failure and warns.
		slog.DebugContext(ctx, "jwt-validate: no bearer token", "correlationID", msg.CorrelationID)
		return j.reject(msg, false), nil
	}

	idToken, err := verifier.Verify(ctx, token)
	if err != nil {
		// A present-but-invalid token is a real auth failure worth surfacing; the
		// go-oidc error names the cause (expired, wrong audience, bad signature, ...).
		j.logReject(ctx, msg, "verification failed", err)
		return j.reject(msg, true), nil
	}

	var claims map[string]any
	if err := idToken.Claims(&claims); err != nil {
		j.logReject(ctx, msg, "claims decode failed", err)
		return j.reject(msg, true), nil
	}
	msg.Variables.Set(j.claimsVar, claims)
	msg.Variables.Set("sub", idToken.Subject)
	return msg, nil
}

// logReject warns that a present-but-invalid token was rejected, naming the
// reason and the underlying error (expired, wrong audience, bad signature, ...).
// A missing token is not logged here — it is routine (see Process).
func (j *jwtValidate) logReject(ctx context.Context, msg *types.Message, reason string, err error) {
	slog.WarnContext(ctx, "jwt-validate: token rejected",
		"reason", reason, "error", err, "correlationID", msg.CorrelationID)
}

// verifierFor builds the verifier once and reuses it across requests.
func (j *jwtValidate) verifierFor(ctx context.Context) (*oidc.IDTokenVerifier, error) {
	j.once.Do(func() {
		j.verifier, j.buildErr = j.build(ctx)
	})
	return j.verifier, j.buildErr
}

// reject configures the built-in 401 response and requests the flow stop. When
// the WWW-Authenticate challenge is enabled it sets the challenge variable;
// hadToken distinguishes an invalid token (error="invalid_token") from a missing
// one (a bare challenge, per RFC 6750).
func (j *jwtValidate) reject(msg *types.Message, hadToken bool) *types.Message {
	msg.Body = map[string]any{"error": "unauthorized"}
	msg.Variables.Set(httpStatusVar, j.rejectStatus)
	if j.wwwAuthenticate {
		msg.Variables.Set(wwwAuthenticateVar, j.challenge(hadToken))
	}
	msg.RequestStop()
	return msg
}

// wwwAuthenticateVar is the variable the challenge is written to; the http source
// emits it when "WWW-Authenticate" is listed in its responseHeaders.
const wwwAuthenticateVar = "WWW-Authenticate"

// challenge builds the WWW-Authenticate: Bearer value, advertising the realm
// (issuer) and the RFC 9728 protected-resource-metadata URL when configured, and
// flagging an invalid token.
func (j *jwtValidate) challenge(hadToken bool) string {
	var parts []string
	if j.issuer != "" {
		parts = append(parts, fmt.Sprintf("realm=%q", j.issuer))
	}
	if j.resourceMetadataURL != "" {
		parts = append(parts, fmt.Sprintf("resource_metadata=%q", j.resourceMetadataURL))
	}
	if hadToken {
		parts = append(parts, `error="invalid_token"`)
	}
	if len(parts) == 0 {
		return "Bearer"
	}
	return "Bearer " + strings.Join(parts, ", ")
}

// bearerToken reads the token from the header variable, stripping a "Bearer "
// prefix (case-insensitive).
func bearerToken(msg *types.Message, header string) string {
	raw, _ := msg.Variables.String(header)
	raw = strings.TrimSpace(raw)
	if len(raw) >= len(bearerPrefix) && strings.EqualFold(raw[:len(bearerPrefix)], bearerPrefix) {
		return strings.TrimSpace(raw[len(bearerPrefix):])
	}
	return raw
}

func orDefaultStr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
