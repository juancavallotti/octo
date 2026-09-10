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
	"syscall"
	"time"

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

	user.NewHandler(user.NewService(user.NewRepo(database.Pool()))).Register(mux)
	slog.Info("user routes registered",
		"endpoints", "GET /roles, GET /users, GET /users/{id}, "+
			"PUT/DELETE /users/{id}/roles/{role}")

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
	signingSvc, err := signing.NewService(signing.NewRepo(database.Pool()), signingCfg)
	if err != nil {
		if !errors.Is(err, signing.ErrInvalidConfig) {
			return nil, err
		}
		slog.Warn("token signing is disabled; set IAM_ISSUER to enable it", "reason", err)
		return mux, nil
	}
	signing.NewHandler(signingSvc).Register(mux)
	slog.Info("signing routes registered",
		"issuer", signingSvc.Issuer(), "audience", signingSvc.Audience(),
		"tokenTtl", signingSvc.TokenTTL(),
		"endpoints", "GET /.well-known/jwks.json, GET /.well-known/openid-configuration")

	return mux, nil
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
