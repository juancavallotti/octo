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
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/juancavallotti/octo/orchestrator/internal/apikey"
	"github.com/juancavallotti/octo/orchestrator/internal/bus"
	"github.com/juancavallotti/octo/orchestrator/internal/db"
	"github.com/juancavallotti/octo/orchestrator/internal/deployment"
	"github.com/juancavallotti/octo/orchestrator/internal/devrun"
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
	// devRunReapInterval is how often idle dev runs are swept. A minute against an
	// idle timeout measured in tens of minutes: the sweep is a cache read plus a
	// comparison, so its cost is not what sets this, and a coarser tick would only
	// make the timeout less accurate.
	devRunReapInterval = time.Minute
	// devRunReapTimeout bounds one sweep, so a wedged API server cannot leave the
	// reaper's goroutine blocked until the process ends.
	devRunReapTimeout = 30 * time.Second
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
	// Which API publishes per-integration endpoints. Unset is Ingress; an
	// unrecognised value is an error rather than a silent fall back to it.
	//
	// Read first because it decides which of the settings below even apply.
	// Parsing INGRESS_ANNOTATIONS ahead of it meant a malformed Ingress-only
	// value stopped a gateway-mode start, over a setting that mode ignores.
	endpointAPI, err := kube.ParseEndpointAPI(os.Getenv("ENDPOINT_API"))
	if err != nil {
		return kube.Config{}, err
	}
	var extraAnnotations map[string]string
	if endpointAPI == kube.EndpointAPIIngress {
		if extraAnnotations, err = ingressAnnotationsConfig(); err != nil {
			return kube.Config{}, err
		}
	}
	sidecarPort, err := devRunSidecarPort()
	if err != nil {
		return kube.Config{}, err
	}
	namespace := envOr("KUBE_NAMESPACE", defaultNamespace)
	cfg := kube.Config{
		Namespace:    namespace,
		RuntimeImage: envOr("RUNTIME_IMAGE", defaultRuntimeImage),
		// The dev-run images and this orchestrator's own in-cluster address. All three
		// are unset on an install with dev runs off, which is a coherent state rather
		// than a broken one: kube.DevRunsEnabled reads them together, because each one's
		// absence alone would produce a pod that fails rather than a feature that
		// degrades.
		DevRuntimeImage: os.Getenv("DEV_RUNTIME_IMAGE"),
		SidecarImage:    os.Getenv("DEV_SIDECAR_IMAGE"),
		SidecarPort:     sidecarPort,
		OrchestratorURL: os.Getenv("ORCHESTRATOR_URL"),
		BaseDomain:      os.Getenv("BASE_DOMAIN"),
		EndpointAPI:     endpointAPI,
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

// devRunSidecarPort reads DEV_RUN_SIDECAR_PORT, the port a dev run's sidecar serves
// its command API on. Unset returns 0, which the kube client reads as its own default
// — the same number the sidecar binary defaults to, so the two halves agree with
// nothing configured.
//
// A value that is not a usable port stops startup naming the setting, rather than
// being coerced: the failure it would otherwise produce is a dev run that starts,
// reports ready, and then cannot be reloaded, because the Service's sidecar port routes
// to a port nothing listens on.
func devRunSidecarPort() (int32, error) {
	raw := os.Getenv("DEV_RUN_SIDECAR_PORT")
	if raw == "" {
		return 0, nil
	}
	port, err := strconv.Atoi(raw)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("parse DEV_RUN_SIDECAR_PORT: %q is not a port number", raw)
	}
	return int32(port), nil
}

// devRunIdleTimeout reads DEV_RUN_IDLE_TIMEOUT, how long a dev run survives without
// being reloaded. Unset takes the service's own default.
//
// A malformed duration is a startup error rather than a silent fall back, because the
// likely typos ("60", "60min") differ from the intent by a factor nobody would notice
// from behaviour: too short reaps a run somebody is using, too long leaves pods
// running for days, and both look like "the reaper is a bit off".
func devRunIdleTimeout() (time.Duration, error) {
	raw := os.Getenv("DEV_RUN_IDLE_TIMEOUT")
	if raw == "" {
		return devrun.DefaultIdleTimeout, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("parse DEV_RUN_IDLE_TIMEOUT: %w", err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("DEV_RUN_IDLE_TIMEOUT must be positive, got %q", raw)
	}
	return d, nil
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
		// Kubernetes access is built first because of the one dependency cycle in this
		// wiring: the dev-run service reads integrations and their resources, while a
		// write to either has to notify it so a running dev run picks the change up.
		//
		// It is broken by direction rather than with a setter. The dev-run service takes
		// the two *repositories* — it only reads stored state, and both service methods
		// it would otherwise call are pass-throughs — while the services that own the
		// write path take the dev-run service as their notifier. So the graph is acyclic,
		// nothing is configured after construction, and no service holds a reference back
		// to one holding a reference to it.
		kubeClient, err := newKubeClient(ctx, kc)
		if err != nil {
			return nil, err
		}
		integrationRepo := integration.NewRepo(database.Pool())
		resourceRepo := resource.NewRepo(database.Pool())
		devrunSvc, err := newDevRunService(kubeClient, integrationRepo, resourceRepo)
		if err != nil {
			return nil, err
		}

		// The notifier is wired only when dev runs are actually available. Passing a nil
		// *devrun.Service would be a non-nil interface holding a nil pointer, which the
		// "nothing is listening" check could not see.
		var integrationOpts []integration.Option
		var resourceOpts []resource.Option
		if devrunSvc.Enabled() {
			integrationOpts = append(integrationOpts, integration.WithReloadNotifier(devrunSvc))
			resourceOpts = append(resourceOpts, resource.WithReloadNotifier(devrunSvc))
		}

		integrationSvc := integration.NewService(integrationRepo, integrationOpts...)
		integration.NewHandler(integrationSvc).Register(mux)
		slog.Info("integration routes registered",
			"reloadsDevRuns", devrunSvc.Enabled(),
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
		resourceSvc := resource.NewService(resourceRepo, resourceOpts...)
		resource.NewHandler(resourceSvc).Register(mux)
		slog.Info("resource routes registered",
			"reloadsDevRuns", devrunSvc.Enabled(),
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

		// Deployment and dev-run management need both the database and in-cluster
		// Kubernetes access. Outside a cluster (e.g. local `go run`) the client is nil
		// and these routes stay disabled, mirroring how the DB-less case disables the
		// rest.
		if kubeClient != nil {
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

			// Dev runs: the editor's Run button, as a pod of its own rather than a child
			// process of whichever platform replica answered the request.
			if devrunSvc.Enabled() {
				devrun.NewHandler(devrunSvc).Register(mux)
				startDevRunReaper(ctx, devrunSvc)
				slog.Info("devrun routes registered",
					"devRuntimeImage", kc.DevRuntimeImage, "devSidecarImage", kc.SidecarImage,
					"idleTimeout", devrunSvc.IdleTimeout(),
					"endpoints", "POST/GET /devruns, GET/DELETE /devruns/{id}, "+
						"POST /devruns/{id}/reload, GET /devruns/{id}/logs, "+
						"GET /devruns/{id}/bundle, POST /devruns/{id}/expire")
			} else {
				// Named individually, because the feature needs all three and any one
				// missing disables it. A single "dev runs disabled" would leave the operator
				// to guess which of the three values in their chart did not arrive.
				slog.Info("dev runs disabled",
					"devRuntimeImage", kc.DevRuntimeImage != "",
					"devSidecarImage", kc.SidecarImage != "",
					"orchestratorUrl", kc.OrchestratorURL != "" || kc.RuntimeServices.OrchestratorURL != "")
			}
		}
	} else {
		slog.Warn("DATABASE_URL not set; integration, folder and deployment routes disabled")
	}

	return mux, nil
}

// newKubeClient builds the Kubernetes client, or reports a nil one when this
// orchestrator is not running inside a cluster.
//
// The two failures here are deliberately different in kind. Not being in a cluster (a
// local `go run`) is a legitimate way to run the orchestrator, so it warns and the
// cluster features stay off. A cluster that cannot serve the endpoints this install is
// configured for — gateway mode without the Gateway API CRDs — is a startup failure,
// because the alternative is discovering it at somebody's first exposed deploy.
func newKubeClient(ctx context.Context, kc kube.Config) (*kube.Client, error) {
	client, err := kube.New(kc)
	if err != nil {
		slog.Warn("kubernetes access unavailable; deployment and dev-run routes disabled", "error", err)
		return nil, nil //nolint:nilnil // a nil client means the feature is off, not an error
	}
	if err := client.Preflight(ctx); err != nil {
		return nil, err
	}
	return client, nil
}

// newDevRunService builds the dev-run service, or a nil one when dev runs are not
// configured (no cluster, or the images and orchestrator URL a dev pod needs are
// unset). A nil service reports Enabled() == false, so the caller registers nothing.
//
// DEV_RUN_HASH_SECRET is required as soon as dev runs are otherwise configured, and its
// absence stops startup naming it — the same posture newKVCipher takes for
// KV_ENCRYPTION_KEY, for the same class of reason. Every dev run's identity and, more
// to the point, its public hostname are HMACs keyed on this secret. Unkeyed they would
// be pure functions of a user id and an integration id, which is to say that the
// hostname is the only thing guarding a publicly reachable dev run and it would be
// derivable by anyone who could guess two ids. Falling back silently is not an option
// worth having.
//
// It takes the repositories rather than the integration and resource services, which is
// what keeps this wiring acyclic; see the comment at the call site.
func newDevRunService(
	cluster *kube.Client, integrations *integration.Repo, resources *resource.Repo,
) (*devrun.Service, error) {
	if cluster == nil || !cluster.DevRunsEnabled() {
		return nil, nil //nolint:nilnil // a nil service means dev runs are off, not an error
	}
	hashSecret := os.Getenv("DEV_RUN_HASH_SECRET")
	if hashSecret == "" {
		return nil, errors.New("DEV_RUN_HASH_SECRET must be set when dev runs are configured: " +
			"it keys the derivation of every dev run's identity and public hostname, and an " +
			"unkeyed hash would make every dev-run hostname predictable")
	}
	idleTimeout, err := devRunIdleTimeout()
	if err != nil {
		return nil, err
	}
	// The secret's bytes are used as an HMAC key, which has no length or encoding
	// requirement — so it is taken as written rather than base64-decoded like
	// KV_ENCRYPTION_KEY, whose cipher needs exactly 32 bytes.
	return devrun.NewService(cluster, integrations, resources, []byte(hashSecret),
		devrun.WithIdleTimeout(idleTimeout)), nil
}

// startDevRunReaper sweeps idle dev runs until ctx ends.
//
// On the root context, so the sweep stops when the process drains rather than outliving
// it. Every replica runs its own, and that needs no coordination: a reap deletes an
// object whose name is derived, so two replicas collecting the same idle run produce one
// delete and one NotFound, which DeleteDevRun already ignores.
func startDevRunReaper(ctx context.Context, svc *devrun.Service) {
	go func() {
		ticker := time.NewTicker(devRunReapInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sweep, cancel := context.WithTimeout(ctx, devRunReapTimeout)
				reaped, err := svc.ReapIdle(sweep)
				cancel()
				if err != nil {
					slog.Error("dev run reap", "error", err)
				}
				if reaped > 0 {
					slog.Info("idle dev runs reaped", "count", reaped)
				}
			}
		}
	}()
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
