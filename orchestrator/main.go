// Command orchestrator is a small HTTP API that sits alongside the editor and
// runtime in the local k3d dev cluster. This first iteration is intentionally
// minimal: a health check and a read of the db_version row seeded into
// site_settings by the schema Job. It exists so the cluster has a Go service
// wired to Postgres that we can grow real orchestration responsibilities into.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/juancavallotti/eip-go/orchestrator/internal/db"
	"github.com/juancavallotti/eip-go/orchestrator/internal/deployment"
	"github.com/juancavallotti/eip-go/orchestrator/internal/folder"
	httpx "github.com/juancavallotti/eip-go/orchestrator/internal/http"
	"github.com/juancavallotti/eip-go/orchestrator/internal/integration"
	"github.com/juancavallotti/eip-go/orchestrator/internal/kube"
)

const (
	defaultPort = "8090"
	// defaultNamespace and defaultRuntimeImage configure where and from what
	// image integration pods are deployed; both are overridable via env.
	defaultNamespace    = "octo-dev"
	defaultRuntimeImage = "octo-runtime:dev"
	// defaultClusterIssuer is the cert-manager ClusterIssuer used for external
	// per-integration TLS. defaultBaseDomain is empty: external endpoints stay
	// disabled until BASE_DOMAIN is set.
	defaultClusterIssuer = "letsencrypt-prod"
	shutdownTimeout      = 10 * time.Second
	dbQueryTimeout       = 5 * time.Second
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	if err := run(); err != nil {
		slog.Error("orchestrator stopped with error", "error", err)
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
		// useful for liveness probes before Postgres is reachable.
		slog.Warn("DATABASE_URL is not set; /db-version will report the DB as unavailable")
	} else {
		d, err := db.New(ctx, dsn)
		if err != nil {
			return err
		}
		defer d.Close()
		database = d
		slog.Info("connected to database pool")
	}

	srv := newServer(ctx, database, kubeConfig{
		namespace:     envOr("KUBE_NAMESPACE", defaultNamespace),
		runtimeImage:  envOr("RUNTIME_IMAGE", defaultRuntimeImage),
		baseDomain:    os.Getenv("BASE_DOMAIN"),
		clusterIssuer: envOr("CLUSTER_ISSUER", defaultClusterIssuer),
	})
	httpServer := httpx.NewServer(":"+port, srv)

	errCh := make(chan error, 1)
	go func() {
		slog.Info("orchestrator listening", "addr", httpServer.Addr,
			"db", database != nil)
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

// kubeConfig groups the deployment-management settings sourced from the
// environment: where and from what image integration pods run, and the parent
// domain + ClusterIssuer for optional per-integration external endpoints.
type kubeConfig struct {
	namespace     string
	runtimeImage  string
	baseDomain    string
	clusterIssuer string
}

// newServer wires the routes. database may be nil when DATABASE_URL is unset.
// kube configures deployment management, which is enabled only when both a
// database and in-cluster Kubernetes access are present. ctx bounds the lifetime
// of background work started here (the deployment status informers).
func newServer(ctx context.Context, database *db.DB, kc kubeConfig) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("GET /db-version", func(w http.ResponseWriter, r *http.Request) {
		if database == nil {
			http.Error(w, "database not configured", http.StatusServiceUnavailable)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), dbQueryTimeout)
		defer cancel()

		// site_settings.value is jsonb; scan it straight into raw JSON and pass it
		// through unmodified so callers see exactly what was seeded.
		var value json.RawMessage
		err := database.Pool().QueryRow(ctx,
			"SELECT value FROM site_settings WHERE key = $1", "db_version",
		).Scan(&value)
		if err != nil {
			slog.Error("db-version query failed", "error", err)
			http.Error(w, "failed to read db_version", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(value)
	})

	if database != nil {
		integrationSvc := integration.NewService(integration.NewRepo(database.Pool()))
		integration.NewHandler(integrationSvc).Register(mux)
		slog.Info("integration routes registered",
			"endpoints", "POST/GET /integrations, GET/PUT/DELETE /integrations/{id}")

		folderSvc := folder.NewService(folder.NewRepo(database.Pool()))
		folder.NewHandler(folderSvc).Register(mux)
		slog.Info("folder routes registered",
			"endpoints", "POST/GET /folders, GET/PUT/DELETE /folders/{id}, "+
				"GET /folders/{id}/integrations, PUT/DELETE /folders/{id}/integrations/{integrationId}")

		// Deployment management needs both the database and in-cluster Kubernetes
		// access. Outside a cluster (e.g. local `go run`) kube.New fails and the
		// routes stay disabled, mirroring how the DB-less case disables the rest.
		if kubeClient, err := kube.New(kc.namespace, kc.runtimeImage, kc.baseDomain, kc.clusterIssuer); err != nil {
			slog.Warn("kubernetes access unavailable; deployment routes disabled", "error", err)
		} else {
			deploymentSvc := deployment.NewService(
				deployment.NewRepo(database.Pool()), integrationSvc, kubeClient)
			// Watch the cluster and push status changes to SSE subscribers; the
			// informers also back the status read path, so list/stream reads hit a
			// local cache rather than the API server.
			hub := deployment.NewHub()
			kubeClient.StartInformers(ctx, hub.Notify)
			deployment.NewHandler(deploymentSvc, hub).Register(mux)
			slog.Info("deployment routes registered",
				"namespace", kubeClient.Namespace(), "runtimeImage", kc.runtimeImage,
				"baseDomain", kc.baseDomain, "externalEndpoints", kubeClient.ExternalEnabled(),
				"endpoints", "POST/GET /integrations/{id}/deployments, "+
					"GET /integrations/{id}/deployments/events (SSE), GET/DELETE /deployments/{id}")
		}
	} else {
		slog.Warn("DATABASE_URL not set; integration, folder and deployment routes disabled")
	}

	return mux
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
