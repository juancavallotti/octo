package kube

import (
	"context"
	"log/slog"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	"github.com/juancavallotti/octo/orchestrator/internal/environment/current"
)

// unresolvedNamespace is what ns answers for an environment it has no namespace
// for. A dot is not allowed in a namespace name, so every call made with it is
// refused by the API server — a failure, never a write into the release
// namespace on behalf of an environment that is not the default.
const unresolvedNamespace = "unresolved.environment"

// namespaceRegistry maps each environment other than the default to the
// namespace it runs in. The environment service fills it at startup and keeps
// it current as environments are created and deleted.
type namespaceRegistry struct {
	mu     sync.RWMutex
	byName map[string]string
	// resolve looks up an environment the registry has not heard of — one
	// another replica created — and is nil until the environment service is
	// wired.
	resolve func(ctx context.Context, environment string) (string, bool, error)
}

func (r *namespaceRegistry) get(environment string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ns, ok := r.byName[environment]
	return ns, ok
}

func (r *namespaceRegistry) set(environment, namespace string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byName == nil {
		r.byName = map[string]string{}
	}
	r.byName[environment] = namespace
}

func (r *namespaceRegistry) forget(environment string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ns, ok := r.byName[environment]
	delete(r.byName, environment)
	return ns, ok
}

func (r *namespaceRegistry) names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.byName))
	for name := range r.byName {
		out = append(out, name)
	}
	return out
}

func (r *namespaceRegistry) all() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.byName))
	for _, ns := range r.byName {
		out = append(out, ns)
	}
	return out
}

// ns is the namespace of the environment ctx acts in: the release namespace for
// the default, the registered one for any other.
func (c *Client) ns(ctx context.Context) string {
	environment := current.From(ctx)
	if environment == "" {
		return c.namespace
	}
	if ns, ok := c.environmentNamespaces.get(environment); ok {
		return ns
	}
	// Another replica created it since this one started. Asked once, then
	// registered — which also starts watching it — so the next call is a map read.
	c.environmentNamespaces.mu.RLock()
	resolve := c.environmentNamespaces.resolve
	c.environmentNamespaces.mu.RUnlock()
	if resolve != nil {
		ns, ok, err := resolve(ctx, environment)
		if err == nil && ok {
			c.RegisterEnvironment(environment, ns)
			return ns
		}
		if err != nil {
			slog.ErrorContext(ctx, "kube: resolve the environment's namespace", "environment", environment, "error", err)
		}
	}
	slog.ErrorContext(ctx, "kube: no namespace is registered for the environment", "environment", environment)
	return unresolvedNamespace
}

// ResolveEnvironmentsWith sets how a namespace is found for an environment this
// client has not registered.
func (c *Client) ResolveEnvironmentsWith(resolve func(ctx context.Context, environment string) (string, bool, error)) {
	c.environmentNamespaces.mu.Lock()
	defer c.environmentNamespaces.mu.Unlock()
	c.environmentNamespaces.resolve = resolve
}

// RegisterEnvironment records namespace as environment's and, once informers
// are running, starts watching it.
func (c *Client) RegisterEnvironment(environment, namespace string) {
	// An environment re-created under another namespace moves: the old one is no
	// longer its, and a sweep reading it would find nothing there.
	if old, ok := c.environmentNamespaces.get(environment); ok && old != namespace {
		c.informers.unwatch(old)
	}
	c.environmentNamespaces.set(environment, namespace)
	c.informers.watch(c, namespace)
}

// RegisteredEnvironments names every environment this client has a namespace for.
func (c *Client) RegisteredEnvironments() []string {
	return c.environmentNamespaces.names()
}

// ForgetEnvironment stops watching environment's namespace and forgets it.
func (c *Client) ForgetEnvironment(environment string) {
	if ns, ok := c.environmentNamespaces.forget(environment); ok {
		c.informers.unwatch(ns)
	}
}

// watched is the informer-backed read path for one namespace.
type watched struct {
	deps   corelisterDeployments
	pods   corelisters.PodNamespaceLister
	synced func() bool
	stop   context.CancelFunc
}

// informerSet holds a watched entry per namespace. Before StartInformers there
// is no context to run them under, so watch only records nothing; StartInformers
// then starts every namespace registered so far.
type informerSet struct {
	mu       sync.RWMutex
	ctx      context.Context
	onChange func(integrationID string)
	byNS     map[string]*watched
}

// cache returns the synced informer cache for the namespace ctx acts in, or nil
// when there is none yet — the caller then reads the API server directly.
func (c *Client) cache(ctx context.Context) *watched {
	// Resolved before the lock: resolving an environment this replica has not
	// seen registers it, which starts watching it under the same lock, and a
	// read lock cannot be upgraded.
	ns := c.ns(ctx)
	c.informers.mu.RLock()
	w := c.informers.byNS[ns]
	c.informers.mu.RUnlock()
	if w == nil || !w.synced() {
		return nil
	}
	return w
}

// StartInformers begins watching the orchestrator-managed Deployments and Pods in
// the release namespace and every registered environment's, and invokes
// onChange(integrationID) whenever one changes, so the caller can push live
// updates. It also wires the lister-backed read path. The informers run until
// ctx is cancelled; an environment registered later is watched from then on.
func (c *Client) StartInformers(ctx context.Context, onChange func(integrationID string)) {
	c.informers.mu.Lock()
	c.informers.ctx = ctx
	c.informers.onChange = onChange
	c.informers.mu.Unlock()
	c.informers.watch(c, c.namespace)
	for _, ns := range c.environmentNamespaces.all() {
		c.informers.watch(c, ns)
	}
}

func (s *informerSet) watch(c *Client, namespace string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx == nil {
		return
	}
	if s.byNS == nil {
		s.byNS = map[string]*watched{}
	}
	if _, ok := s.byNS[namespace]; ok {
		return
	}
	ctx, stop := context.WithCancel(s.ctx)
	factory := informers.NewSharedInformerFactoryWithOptions(
		c.clientset, informerResync,
		informers.WithNamespace(namespace),
		informers.WithTweakListOptions(func(o *metav1.ListOptions) {
			o.LabelSelector = labelManagedBy + "=" + managedByValue
		}),
	)
	depInformer := factory.Apps().V1().Deployments()
	podInformer := factory.Core().V1().Pods()
	w := &watched{
		deps: depInformer.Lister().Deployments(namespace),
		pods: podInformer.Lister().Pods(namespace),
		stop: stop,
	}
	onChange := s.onChange
	handler := cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj any) { notifyIntegration(obj, onChange) },
		UpdateFunc: func(_, obj any) { notifyIntegration(obj, onChange) },
		DeleteFunc: func(obj any) { notifyIntegration(obj, onChange) },
	}
	// AddEventHandler can only fail before the informer starts; ignore the
	// registration handle since handlers live for the informer's lifetime.
	_, _ = depInformer.Informer().AddEventHandler(handler)
	_, _ = podInformer.Informer().AddEventHandler(handler)
	factory.Start(ctx.Done())
	w.synced = func() bool {
		return depInformer.Informer().HasSynced() && podInformer.Informer().HasSynced()
	}
	s.byNS[namespace] = w
}

func (s *informerSet) unwatch(namespace string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if w, ok := s.byNS[namespace]; ok {
		w.stop()
		delete(s.byNS, namespace)
	}
}

// allSynced reports whether informers were started and every watched
// namespace's caches have loaded.
func (s *informerSet) allSynced() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.byNS) == 0 {
		return false
	}
	for _, w := range s.byNS {
		if !w.synced() {
			return false
		}
	}
	return true
}

// WaitForCacheSync blocks until every watched namespace's informer caches have
// loaded, or ctx ends.
//
// It exists so the reconciler starts from loaded caches where it can. It does not
// guard against acting on an empty cache: every read here, the reconciler's
// listing included, treats an unsynced cache as a reason to read the API server
// directly, which answers authoritatively or fails.
//
// False means the caches never synced: informers were not started, or ctx ended
// first. Both mean the same thing to a caller — do not act.
func (c *Client) WaitForCacheSync(ctx context.Context) bool {
	// A short poll rather than cache.WaitForCacheSync, which takes a stop channel
	// and would need one derived from ctx. The wait happens once, at startup, and
	// half a second of latency on it costs nothing.
	c.informers.mu.RLock()
	started := c.informers.ctx != nil
	c.informers.mu.RUnlock()
	if !started {
		return false
	}
	ticker := time.NewTicker(cacheSyncPoll)
	defer ticker.Stop()
	for {
		if c.informers.allSynced() {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
}
