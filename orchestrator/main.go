// Command orchestrator is a small HTTP API that sits alongside the editor and
// runtime in the local k3d dev cluster. This first iteration is intentionally
// minimal: a health check and a read of the db_version row seeded into
// site_settings by the schema Job. It exists so the cluster has a Go service
// wired to Postgres that we can grow real orchestration responsibilities into.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/juancavallotti/octo/orchestrator/internal/apikey"
	"github.com/juancavallotti/octo/orchestrator/internal/bus"
	"github.com/juancavallotti/octo/orchestrator/internal/db"
	"github.com/juancavallotti/octo/orchestrator/internal/deployment"
	"github.com/juancavallotti/octo/orchestrator/internal/folder"
	httpx "github.com/juancavallotti/octo/orchestrator/internal/http"
	"github.com/juancavallotti/octo/orchestrator/internal/integration"
	"github.com/juancavallotti/octo/orchestrator/internal/kube"
	"github.com/juancavallotti/octo/orchestrator/internal/kv"
	"github.com/juancavallotti/octo/orchestrator/internal/resource"
	"github.com/juancavallotti/octo/orchestrator/internal/secret"
	"github.com/juancavallotti/octo/orchestrator/internal/snapshot"
	"github.com/juancavallotti/octo/orchestrator/internal/user"
)

const (
	defaultPort = "8090"
	// defaultNamespace and defaultRuntimeImage configure where and from what
	// image integration pods are deployed; both are overridable via env.
	defaultNamespace    = "octo-dev"
	defaultRuntimeImage = "octo-runtime:dev"
	shutdownTimeout     = 10 * time.Second
	dbQueryTimeout      = 5 * time.Second
	// deploymentSnapshotTimeout bounds the DB + cluster work to compute one
	// deployment snapshot published from the informer callback.
	deploymentSnapshotTimeout = 15 * time.Second
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

	kubeCfg, err := kubeConfig()
	if err != nil {
		return err
	}

	srv, err := newServer(ctx, database, kubeCfg)
	if err != nil {
		return err
	}
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

// kubeConfig reads the cluster facts the deployment path needs from the
// environment. Everything it rejects is rejected here, before a client exists,
// so a misconfigured install stops at startup naming the setting rather than
// producing deployments whose endpoints route nowhere.
func kubeConfig() (kube.Config, error) {
	extraAnnotations, err := ingressAnnotationsConfig()
	if err != nil {
		return kube.Config{}, err
	}
	// Which API publishes per-integration endpoints. Unset is Ingress; an
	// unrecognised value is an error rather than a silent fall back to it.
	endpointAPI, err := kube.ParseEndpointAPI(os.Getenv("ENDPOINT_API"))
	if err != nil {
		return kube.Config{}, err
	}
	namespace := envOr("KUBE_NAMESPACE", defaultNamespace)
	cfg := kube.Config{
		Namespace:    namespace,
		RuntimeImage: envOr("RUNTIME_IMAGE", defaultRuntimeImage),
		BaseDomain:   os.Getenv("BASE_DOMAIN"),
		EndpointAPI:  endpointAPI,
		// Empty ("") means no TLS annotation and no per-host cert: letsencrypt-prod
		// only exists because this project's own k3s bootstrap creates it, so it must
		// not be assumed as a default on an arbitrary cluster.
		ClusterIssuer:     os.Getenv("CLUSTER_ISSUER"),
		WildcardTLSSecret: os.Getenv("WILDCARD_TLS_SECRET"),
		// Empty omits IngressClassName, letting the cluster's default IngressClass
		// (if any) claim the per-integration Ingress.
		IngressClass:     os.Getenv("INGRESS_CLASS"),
		ExtraAnnotations: extraAnnotations,
		// The Gateway per-integration HTTPRoutes attach to, in gateway mode. The
		// namespace defaults to our own, which is the single-namespace install; a
		// Gateway run by whoever owns ingress lives elsewhere and says so.
		Gateway: kube.GatewayRef{
			Name:        os.Getenv("GATEWAY_NAME"),
			Namespace:   envOr("GATEWAY_NAMESPACE", namespace),
			SectionName: os.Getenv("GATEWAY_SECTION_NAME"),
		},
		ImagePullSecrets: imagePullSecretsConfig(),
		RuntimeServices:  runtimeServicesConfig(),
	}
	if err := cfg.Validate(); err != nil {
		return kube.Config{}, err
	}
	return cfg, nil
}

// ingressAnnotationsConfig parses INGRESS_ANNOTATIONS, a JSON object of extra
// annotations merged onto every per-integration Ingress (e.g. controller-specific
// body-size or timeout annotations). Unset means none; malformed JSON is a
// startup error rather than a silently-ignored one. Ignored in gateway mode,
// where the route carries no controller configuration at all.
func ingressAnnotationsConfig() (map[string]string, error) {
	raw := os.Getenv("INGRESS_ANNOTATIONS")
	if raw == "" {
		return nil, nil
	}
	var ann map[string]string
	if err := json.Unmarshal([]byte(raw), &ann); err != nil {
		return nil, fmt.Errorf("parse INGRESS_ANNOTATIONS: %w", err)
	}
	return ann, nil
}

// imagePullSecretsConfig parses RUNTIME_IMAGE_PULL_SECRETS, a comma-separated
// list of Secret names authenticating the pull of the runtime image on the pods
// this orchestrator deploys. Unset means none, which is the public-image case.
//
// Comma-separated rather than JSON, unlike INGRESS_ANNOTATIONS above, because
// these are names and not a map — the chart joins the same list it renders into
// its own workloads' imagePullSecrets, and a name containing a comma is not a
// legal Kubernetes object name. Blanks are dropped so a trailing comma, or the
// empty string the chart emits for an empty list, does not become a Secret named
// "" that the kubelet reports as missing on every pod.
func imagePullSecretsConfig() []string {
	raw := os.Getenv("RUNTIME_IMAGE_PULL_SECRETS")
	if raw == "" {
		return nil
	}
	var names []string
	for _, name := range strings.Split(raw, ",") {
		if name = strings.TrimSpace(name); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// runtimeServicesConfig reads the runtime-services env injected into deployed
// runtime pods. The orchestrator URL is the linchpin: without it the runtime has
// no KV endpoint, so an empty URL disables injection entirely (Module left empty)
// and the runtime falls back to its standalone default. With a URL set, the module
// defaults to k8s (Lease-based leader election + orchestrator KV).
func runtimeServicesConfig() kube.RuntimeServices {
	orchestratorURL := os.Getenv("ORCHESTRATOR_URL")
	if orchestratorURL == "" {
		return kube.RuntimeServices{}
	}
	return kube.RuntimeServices{
		Module:          envOr("RUNTIME_SERVICES_MODULE", "k8s"),
		OrchestratorURL: orchestratorURL,
		ServiceAccount:  os.Getenv("RUNTIME_SERVICE_ACCOUNT"),
		NATSURL:         os.Getenv("NATS_URL"),
	}
}

// newServer wires the routes. database may be nil when DATABASE_URL is unset.
// kc configures deployment management, which is enabled only when both a
// database and in-cluster Kubernetes access are present. ctx bounds the lifetime
// of background work started here (the deployment status informers).
func newServer(ctx context.Context, database *db.DB, kc kube.Config) (http.Handler, error) {
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

		// Version tags. Snapshotting needs only the database (not Kubernetes), so it
		// is registered outside the deployment/kube gate below — tags can be created
		// and managed even where deploys are unavailable.
		snapshotSvc := snapshot.NewService(snapshot.NewRepo(database.Pool()), integrationSvc)
		snapshot.NewHandler(snapshotSvc).Register(mux)
		slog.Info("snapshot routes registered",
			"endpoints", "POST/GET /integrations/{id}/snapshots, DELETE /snapshots/{id}, "+
				"GET /snapshots/{id}/resources, GET /snapshots/{id}/resources/content")

		// Integration resources (env files, templates). CRUD needs only the
		// database, so it is registered outside the deployment/kube gate below. The
		// service is also shared into the deployment service (below) so a Current
		// deploy knows which env vars the working-copy .env files supply.
		resourceSvc := resource.NewService(resource.NewRepo(database.Pool()))
		resource.NewHandler(resourceSvc).Register(mux)
		slog.Info("resource routes registered",
			"endpoints", "POST/GET /integrations/{id}/resources, "+
				"GET/PUT/DELETE /integrations/{id}/resources/{resourceId}")

		// Users and their API keys need only the database (identity comes from the
		// platform's OIDC layer, which bootstraps a user on first sign-in). Registered
		// outside the kube gate so authentication works wherever the DB is reachable.
		user.NewHandler(user.NewService(user.NewRepo(database.Pool()))).Register(mux)
		slog.Info("user routes registered",
			"endpoints", "POST /users/bootstrap, GET /users/{id}")

		apikey.NewHandler(apikey.NewService(apikey.NewRepo(database.Pool()))).Register(mux)
		slog.Info("apikey routes registered",
			"endpoints", "POST/GET /users/{userId}/apikeys, "+
				"DELETE /users/{userId}/apikeys/{id}, POST /apikeys/verify")

		// Deployment-scoped KV store the runtime's k8s services module calls. Values
		// in a secret namespace are encrypted with KV_ENCRYPTION_KEY; without the key,
		// secrets are rejected but plain KV still works.
		cipher, cipherErr := newKVCipher(os.Getenv("KV_ENCRYPTION_KEY"))
		if cipherErr != nil {
			return nil, cipherErr
		}
		kvSvc := kv.NewService(kv.NewRepo(database.Pool()), cipher)
		kv.NewHandler(kvSvc).Register(mux)
		slog.Info("kv routes registered",
			"encryption", cipher != nil,
			"endpoints", "GET/PUT/DELETE /deployments/{id}/kv/{namespace}/{key}")

		// The user-facing object browser the platform UI calls: a JSON facade over
		// the same store, fixed to the "user" namespace, adding the listing the raw
		// KV routes lack.
		kv.NewObjectHandler(kvSvc).Register(mux)
		slog.Info("object routes registered",
			"endpoints", "GET /deployments/{id}/objects, GET/PUT/DELETE /deployments/{id}/objects/{key}")

		// Deployment management needs both the database and in-cluster Kubernetes
		// access. Outside a cluster (e.g. local `go run`) kube.New fails and the
		// routes stay disabled, mirroring how the DB-less case disables the rest.
		if kubeClient, err := kube.New(kc); err != nil {
			slog.Warn("kubernetes access unavailable; deployment routes disabled", "error", err)
		} else {
			// Being in a cluster is not the same as that cluster being able to serve
			// the endpoints this install is configured for: Gateway API is CRDs, and
			// their absence is a startup failure rather than a disabled feature. It is
			// deliberately not the warn-and-continue above — that one covers running
			// outside a cluster at all, which is a legitimate way to run the
			// orchestrator; this one covers a cluster that will never serve what it
			// was asked for, and only says so at the first exposed deploy.
			if err := kubeClient.Preflight(ctx); err != nil {
				return nil, err
			}
			deploymentRepo := deployment.NewRepo(database.Pool())
			deploymentSvc := deployment.NewService(deploymentRepo, integrationSvc, kubeClient,
				deployment.WithStoreCleaner(kvSvc),
				// Enforce tagged deploys: a deploy must reference a snapshot and ships
				// its frozen definition.
				deployment.WithSnapshots(snapshotSvc),
				// Report working-copy .env keys for the Current deploy case.
				deployment.WithResources(resourceSvc))
			// Publish deployment status to NATS for cross-node fan-out; the BFF
			// subscribes and serves the SSE. A noop publisher when NATS_URL is unset
			// (local/standalone) leaves clients on the list-polling fallback.
			publisher, err := bus.NewPublisher(os.Getenv("NATS_URL"))
			if err != nil {
				return nil, err
			}
			context.AfterFunc(ctx, publisher.Close)
			// Watch the cluster; on each change recompute the integration's snapshot
			// (DB + live status) and publish it. The informers also back the status
			// read path, so list reads hit a local cache rather than the API server.
			kubeClient.StartInformers(ctx, func(integrationID string) {
				sctx, cancel := context.WithTimeout(ctx, deploymentSnapshotTimeout)
				defer cancel()
				items, err := deploymentSvc.ListByIntegration(sctx, integrationID)
				if err != nil {
					slog.Error("deployment snapshot", "integrationId", integrationID, "error", err)
					return
				}
				data, err := deployment.MarshalSnapshot(items)
				if err != nil {
					slog.Error("deployment snapshot marshal", "integrationId", integrationID, "error", err)
					return
				}
				publisher.Publish(deployment.DeploymentsSubject(integrationID), data)
			})
			deployment.NewHandler(deploymentSvc).Register(mux)
			slog.Info("deployment routes registered",
				"namespace", kubeClient.Namespace(), "runtimeImage", kc.RuntimeImage,
				"baseDomain", kc.BaseDomain, "externalEndpoints", kubeClient.ExternalEnabled(),
				// Which API publishes those endpoints, and — in gateway mode — what they
				// attach to. Both are answers you otherwise get by reading the chart.
				"endpointApi", kc.EndpointAPI, "gateway", kc.Gateway.Name,
				"nats", os.Getenv("NATS_URL") != "",
				"endpoints", "POST/GET /integrations/{id}/deployments, GET/DELETE /deployments/{id}")

			// Cluster-wide secrets share the kube client (values live in the shared
			// octo-secrets Secret) and the deployment repo (to refuse deleting a
			// secret a deployment still references).
			secretSvc := secret.NewService(secret.NewRepo(database.Pool()), kubeClient, deploymentRepo)
			secret.NewHandler(secretSvc).Register(mux)
			slog.Info("secret routes registered",
				"endpoints", "GET /secrets, PUT/DELETE /secrets/{name}")
		}
	} else {
		slog.Warn("DATABASE_URL not set; integration, folder and deployment routes disabled")
	}

	return mux, nil
}

// newKVCipher builds the secret-namespace encryption cipher from a base64-encoded
// key. An empty key disables encryption (secret-namespace writes are then rejected);
// a malformed key or an invalid key length is a startup error.
func newKVCipher(b64 string) (*kv.Cipher, error) {
	if b64 == "" {
		slog.Warn("KV_ENCRYPTION_KEY not set; KV secret-namespace writes will be rejected")
		return nil, nil //nolint:nilnil // nil cipher means encryption disabled, not an error
	}
	key, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("decode KV_ENCRYPTION_KEY: %w", err)
	}
	return kv.NewCipher(key)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
