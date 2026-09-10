// Command iam is the platform's identity service. It owns two things that were
// previously spread across the orchestrator and the platform's Auth.js layer:
//
//   - Platform users and the roles granted to them. Roles used to be whatever an
//     id-token claim said they were, which meant no role could be granted without
//     editing the identity provider. Here they are rows.
//   - The exchange of an identity provider's sign-in token for an internal
//     platform token. POST /auth verifies the presented OIDC token, resolves the
//     caller to an octo user with roles, and mints a JWT signed by this service.
//     Any internal service can then verify that token by itself, from the JWKS
//     published at /.well-known/jwks.json — which is what lets each service gain
//     an authorization boundary without every one of them learning OIDC.
//
// It performs no authentication of its own on the management routes and is
// ClusterIP in the chart, like the orchestrator and the observability service. It
// publishes no OpenAPI description, deliberately: it is not a surface to expose or
// to hand to an agent as a tool.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/juancavallotti/octo/iam/internal/auth"
	"github.com/juancavallotti/octo/iam/internal/authz"
	"github.com/juancavallotti/octo/iam/internal/db"
	httpx "github.com/juancavallotti/octo/iam/internal/http"
	"github.com/juancavallotti/octo/iam/internal/signing"
	"github.com/juancavallotti/octo/iam/internal/user"
)

const (
	// defaultPort follows the orchestrator (8090), the observability service
	// (8091) and the embedding server (8092).
	defaultPort     = "8093"
	shutdownTimeout = 10 * time.Second
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	if err := run(); err != nil {
		slog.Error("iam stopped with error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	port := envOr("PORT", defaultPort)
	dsn := os.Getenv("DATABASE_URL")

	// Root context cancelled on SIGINT/SIGTERM so k8s pod termination drains
	// cleanly rather than killing in-flight requests.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var database *db.DB
	if dsn == "" {
		// The service still serves /healthz without a database, which keeps it
		// useful for liveness probes before Postgres is reachable. Everything else
		// needs storage — users, roles and the signing keyset all live in it — so
		// those routes stay unregistered rather than answering with an error.
		slog.Warn("DATABASE_URL is not set; only /healthz will serve")
	} else {
		d, err := db.New(ctx, dsn)
		if err != nil {
			return err
		}
		defer d.Close()
		database = d
		slog.Info("connected to database pool")
	}

	srv, err := newServer(database)
	if err != nil {
		return err
	}
	httpServer := httpx.NewServer(":"+port, srv)

	errCh := make(chan error, 1)
	go func() {
		slog.Info("iam listening", "addr", httpServer.Addr, "db", database != nil)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("shutdown signal received, draining")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	}
}

// newServer wires the routes. database may be nil when DATABASE_URL is unset,
// which leaves every route but the liveness probe unregistered.
func newServer(database *db.DB) (http.Handler, error) {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", healthz)

	if database == nil {
		return mux, nil
	}

	userSvc := user.NewService(user.NewRepo(database.Pool()))
	userHandler := user.NewHandler(userSvc)

	// The one route that cannot ask for a token, because it is how a local run
	// gets a user without an identity provider to get a token from.
	userHandler.RegisterOpen(mux)
	slog.Info("open user routes registered", "endpoints", "POST /users/bootstrap")

	// The signing keyset. It needs no configuration beyond the issuer it stamps:
	// the keypair is generated on demand, stored, shared by every replica through
	// the database, and rotated by whichever request first finds the current one
	// retired. There is deliberately nothing here for an operator to hold.
	//
	// An unset IAM_ISSUER leaves the keyset unwired rather than stopping startup.
	// The service still serves its liveness probe and the user and role routes,
	// which is the shape the orchestrator already takes when a dependency it does
	// not need for everything is missing — and it means a misconfigured install
	// answers questions about itself rather than crash-looping.
	signingCfg, err := signingConfig()
	if err != nil {
		return nil, err
	}
	// The cipher that seals stored private keys. A malformed key stops startup; an
	// absent one leaves signing off, which the ErrInvalidConfig branch below
	// reports along with every other reason the keyset cannot be built.
	cipher, err := newCipher(os.Getenv("KV_ENCRYPTION_KEY"))
	if err != nil {
		return nil, err
	}
	signingSvc, err := newSigningService(database, cipher, signingCfg)
	if err != nil {
		if !errors.Is(err, signing.ErrInvalidConfig) {
			return nil, err
		}
		slog.Warn("token signing is disabled", "reason", err)
		// The exchange is registered anyway, with nothing behind it, so POST /auth
		// answers 503 naming what is missing rather than 404.
		auth.NewHandler(nil).Register(mux)
		return mux, nil
	}
	signing.NewHandler(signingSvc).Register(mux)

	// The management routes, mounted only now that there is something to check a
	// token with. Registered after the keyset rather than beside the bootstrap
	// above, and that ordering is the point: an install with no keyset cannot
	// authenticate anybody, and the alternative to not serving these would be
	// serving the user directory and the role grants to whoever asked.
	userHandler.Register(mux,
		authz.Require(signingSvc),
		authz.Require(signingSvc, string(user.RoleAdmin)))
	slog.Info("user management routes registered",
		"endpoints", "GET /roles, POST/GET /users, GET/PUT/DELETE /users/{id}, "+
			"PUT/DELETE /users/{id}/roles/{role}")

	slog.Info("signing routes registered",
		"issuer", signingSvc.Issuer(), "audience", signingSvc.Audience(),
		"tokenTtl", signingSvc.TokenTTL(),
		"endpoints", "GET /.well-known/jwks.json, GET /.well-known/openid-configuration")

	// Read here rather than inside newAuthService, because a malformed duration is
	// a typo in the chart and the only thing that builder can do with an error is
	// disable itself — which would leave the service healthy, answering 503 on
	// sign-in, with the reason in a log line nobody is reading yet.
	grace, err := refreshGrace()
	if err != nil {
		return nil, err
	}

	// The exchange itself, which needs the identity provider on top of everything
	// above. A nil service is the "no provider configured" state; see the comment
	// on auth.Handler for why the route is registered either way.
	auth.NewHandler(newAuthService(userSvc, signingSvc, grace)).Register(mux)
	slog.Info("auth routes registered",
		"oidcIssuer", os.Getenv("OIDC_ISSUER"),
		"endpoints", "POST /auth, POST /auth/refresh")

	return mux, nil
}

// newAuthService builds the token exchange, or a nil one when no identity
// provider is configured — which is how `task dev` runs today, and is a supported
// way to run rather than an error.
//
// The two settings share their names with the platform's own OIDC configuration
// on purpose: one install has one identity provider, and giving iam a second pair
// of variables would be a way for the two halves to end up pointed at different
// ones.
//
// IAM_ACCEPTED_AUDIENCES widens what the exchange will take beyond the editor's
// own client id, because one install presents more than one face to the provider:
// the `/mcp` resource identifier is the other one. It is named for the platform
// rather than for MCP — this service has no business knowing what MCP is, only
// which audiences are this install.
func newAuthService(users *user.Service, signer *signing.Service, grace time.Duration) *auth.Service {
	issuer, clientID := os.Getenv("OIDC_ISSUER"), os.Getenv("OIDC_CLIENT_ID")
	if issuer == "" || clientID == "" {
		// Named individually, because the exchange needs both and either one
		// missing disables it. A single "not configured" would leave the operator
		// to guess which of the two values did not arrive.
		slog.Warn("the token exchange is disabled; POST /auth will report it as unavailable",
			"oidcIssuer", issuer != "", "oidcClientId", clientID != "")
		return nil
	}
	audiences := acceptedAudiences(clientID, os.Getenv("IAM_ACCEPTED_AUDIENCES"))
	svc, err := auth.NewService(auth.NewVerifier(issuer, audiences), users, signer, grace)
	if err != nil {
		// Unreachable given the guard above, and reported rather than ignored so it
		// cannot become a silent nil if the constructor grows another requirement.
		slog.Error("the token exchange could not be built", "error", err)
		return nil
	}
	return svc
}

// acceptedAudiences is the client id plus whatever else this install answers for,
// comma-separated. The client id is always in the set and never has to be
// repeated: forgetting it would break sign-in, which is the one thing the
// exchange must never be one typo away from.
func acceptedAudiences(clientID, extra string) []string {
	audiences := []string{clientID}
	for _, aud := range strings.Split(extra, ",") {
		if aud = strings.TrimSpace(aud); aud != "" && aud != clientID {
			audiences = append(audiences, aud)
		}
	}
	return audiences
}

// healthz answers the liveness probe. It reports as soon as the process is
// serving, with no dependency on the database — which is the point, since it is
// what a probe uses to decide whether to restart the pod while Postgres is still
// coming up.
func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok"))
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
