// Command iam is the identity service. It owns two things:
//
//   - Platform users and the roles granted to them, held as rows rather than as
//     id-token claims, so a role can be granted without editing the identity
//     provider.
//   - The exchange of an identity provider's sign-in token for an internal
//     platform token. POST /auth verifies the presented OIDC token, resolves the
//     caller to an octo user with roles, and mints a JWT signed by this service.
//     Anything holding that token can verify it from the JWKS published at
//     /.well-known/jwks.json, without itself learning OIDC.
//
// It performs no authentication of its own on the management routes and is
// ClusterIP in the chart.
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

	// Root context cancelled on SIGINT/SIGTERM so termination drains rather than
	// killing in-flight requests.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var database *db.DB
	if dsn == "" {
		// /healthz serves without a database, so the liveness probe answers before
		// Postgres is reachable. Everything else needs storage — users, roles and
		// the signing keyset all live in it — so those routes stay unregistered
		// rather than answering with an error.
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

	// The signing keyset needs no configuration beyond the issuer it stamps: the
	// keypair is generated on demand, stored, shared by every replica through the
	// database, and rotated by whichever request first finds the current one
	// retired. An unset IAM_ISSUER leaves the keyset unwired rather than stopping
	// startup, so a misconfigured install answers questions about itself rather
	// than crash-looping.
	signingCfg, err := signingConfig()
	if err != nil {
		return nil, err
	}
	// The cipher that seals stored private keys. A malformed key stops startup; an
	// absent one leaves signing off, which the ErrInvalidConfig branch below
	// reports.
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
	// token with: an install with no keyset cannot authenticate anybody, and
	// serving these without one would hand the user directory and the role grants
	// to whoever asked.
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

	// A nil service is the "no provider configured" state; see auth.Handler for why
	// the route is registered either way.
	auth.NewHandler(newAuthService(userSvc, signingSvc)).Register(mux)
	slog.Info("auth routes registered",
		"oidcIssuer", os.Getenv("OIDC_ISSUER"),
		"endpoints", "POST /auth, POST /auth/refresh, POST /auth/machine")

	return mux, nil
}

// newAuthService builds the token exchange, or a nil one when no identity
// provider is configured, which is a supported way to run rather than an error.
//
// It reads OIDC_ISSUER and OIDC_CLIENT_ID under those shared names because one
// install has one identity provider. IAM_ACCEPTED_AUDIENCES widens what the
// exchange will take beyond the client id, for an install that presents more than
// one audience to the provider.
func newAuthService(users *user.Service, signer *signing.Service) *auth.Service {
	issuer, clientID := os.Getenv("OIDC_ISSUER"), os.Getenv("OIDC_CLIENT_ID")
	if issuer == "" || clientID == "" {
		// Named individually, because either one missing disables the exchange and
		// a single "not configured" would not say which.
		slog.Warn("the token exchange is disabled; POST /auth will report it as unavailable",
			"oidcIssuer", issuer != "", "oidcClientId", clientID != "")
		return nil
	}
	audiences := acceptedAudiences(clientID, os.Getenv("IAM_ACCEPTED_AUDIENCES"))
	svc, err := auth.NewService(auth.NewVerifier(issuer, audiences), users, signer)
	if err != nil {
		// Unreachable given the guard above, and reported rather than ignored so it
		// cannot become a silent nil if the constructor grows a requirement.
		slog.Error("the token exchange could not be built", "error", err)
		return nil
	}
	return svc
}

// acceptedAudiences is the client id plus whatever else this install answers for,
// comma-separated. The client id is always in the set and never has to be
// repeated in extra.
func acceptedAudiences(clientID, extra string) []string {
	audiences := []string{clientID}
	for _, aud := range strings.Split(extra, ",") {
		if aud = strings.TrimSpace(aud); aud != "" && aud != clientID {
			audiences = append(audiences, aud)
		}
	}
	return audiences
}

// healthz answers the liveness probe as soon as the process is serving, with no
// dependency on the database.
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
