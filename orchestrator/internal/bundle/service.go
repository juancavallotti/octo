package bundle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/juancavallotti/octo/orchestrator/internal/integration"
	"github.com/juancavallotti/octo/orchestrator/internal/resource"
	"github.com/juancavallotti/octo/orchestrator/internal/snapshot"
)

const (
	// nameAttempts bounds the search for a free integration name on import. An
	// import that cannot find a free name in this many tries is telling the user
	// something they should answer themselves, by renaming.
	nameAttempts = 50
	// fallbackName names an import whose archive says nothing about what it is and
	// whose caller supplied no name either.
	fallbackName = "Imported integration"
	// rollbackTimeout bounds the undo of a failed import or replace. The undo runs
	// on a context detached from the request's, so a cancelled or timed-out request
	// — one likely cause of the failure being undone — cannot also stop the cleanup.
	rollbackTimeout = 10 * time.Second
)

// integrationService is the slice of the integration module this service needs:
// read one, create one, replace one. Declared here, in the consumer;
// *integration.Service satisfies it.
type integrationService interface {
	Get(ctx context.Context, id string) (integration.Integration, error)
	Create(ctx context.Context, name, definition, actorID string) (integration.Integration, error)
	Update(ctx context.Context, id, name, definition, actorID string) (integration.Integration, error)
	Delete(ctx context.Context, id string) error
}

// resourceService is the slice of the resource module this service needs. The
// writes go through the service rather than the repository so a bundle import is
// validated — and notifies dev runs — exactly like a hand upload.
type resourceService interface {
	ListByIntegration(ctx context.Context, integrationID string) ([]resource.Resource, error)
	Create(ctx context.Context, integrationID, kind, name, content string) (resource.Resource, error)
	Update(ctx context.Context, integrationID, id, kind, name, content string) (resource.Resource, error)
	Delete(ctx context.Context, integrationID, id string) error
}

// snapshotService is the slice of the snapshot module this service needs: a tag
// and the resources frozen under it.
type snapshotService interface {
	Get(ctx context.Context, id string) (snapshot.Snapshot, error)
	ListResources(ctx context.Context, snapshotID string) ([]snapshot.Resource, error)
}

// Service assembles and applies whole-integration bundles. It owns no storage of
// its own: a bundle is a view over the integration, resource and snapshot
// modules, which is why this module has a service and a handler but no repository.
type Service struct {
	integrations integrationService
	resources    resourceService
	snapshots    snapshotService

	// replacing serializes Replace per integration, so two replaces of the same
	// integration cannot interleave and have one's rollback undo the other's
	// successful write. Within one process only: this is a guard against a user
	// double-clicking or a client retrying, not a distributed lock, and across
	// replicas concurrent replaces remain last-write-wins like every other write
	// in this API.
	replacing sync.Map // integration id -> *sync.Mutex
}

// NewService returns a Service over the modules a bundle is assembled from.
func NewService(integrations integrationService, resources resourceService, snapshots snapshotService) *Service {
	return &Service{integrations: integrations, resources: resources, snapshots: snapshots}
}

// Export assembles an integration's working copy — its definition plus every live
// resource — into one bundle.
func (s *Service) Export(ctx context.Context, integrationID string) (Bundle, error) {
	it, err := s.integrations.Get(ctx, integrationID)
	if err != nil {
		return Bundle{}, err
	}
	items, err := s.resources.ListByIntegration(ctx, integrationID)
	if err != nil {
		return Bundle{}, err
	}
	out := Bundle{Name: it.Name, Definition: it.Definition}
	for _, r := range items {
		out.Resources = append(out.Resources, File{Kind: r.Kind, Name: r.Name, Content: r.Content})
	}
	return out, nil
}

// ExportSnapshot assembles the bundle a version tag froze: that tag's definition
// and its frozen resources, named after the integration it belongs to.
//
// The integration is read only for its display name — the contents come entirely
// from the snapshot, so an export of a tag does not change when the working copy
// does.
func (s *Service) ExportSnapshot(ctx context.Context, snapshotID string) (Bundle, error) {
	snap, err := s.snapshots.Get(ctx, snapshotID)
	if err != nil {
		return Bundle{}, err
	}
	it, err := s.integrations.Get(ctx, snap.IntegrationID)
	if err != nil {
		return Bundle{}, err
	}
	items, err := s.snapshots.ListResources(ctx, snapshotID)
	if err != nil {
		return Bundle{}, err
	}
	out := Bundle{Name: it.Name, Tag: snap.Tag, Definition: snap.Definition}
	for _, r := range items {
		out.Resources = append(out.Resources, File{Kind: r.Kind, Name: r.Name, Content: r.Content})
	}
	return out, nil
}

// Import creates a new integration from an archive, with its resources.
//
// fallback names the import when the archive does not (a manifest-less zip); the
// handler passes the uploaded filename's stem. A name already in use is suffixed
// rather than rejected: an import is a copy of something that may well already be
// here, and failing the upload over a name is a worse answer than "(2)".
func (s *Service) Import(ctx context.Context, data []byte, fallback, actorID string) (integration.Integration, error) {
	b, err := Read(data)
	if err != nil {
		return integration.Integration{}, err
	}

	created, err := s.createUniquelyNamed(ctx, importName(b.Name, fallback), b.Definition, actorID)
	if err != nil {
		return integration.Integration{}, err
	}
	for _, r := range b.Resources {
		if _, err := s.resources.Create(ctx, created.ID, r.Kind, r.Name, r.Content); err != nil {
			// A half-imported integration is worse than none: it looks like the bundle
			// but silently misses a file the definition refers to. Roll the create back
			// so the user retries a failed import rather than debugging a broken one.
			s.discard(ctx, created.ID)
			return integration.Integration{}, fmt.Errorf("importing resource %q: %w", r.Name, err)
		}
	}
	return created, nil
}

// Replace overwrites an existing integration's definition and resource set from
// an archive, keeping its identity: its id, its name, its folder, its version tags
// and its deployments all stay as they are. The bundle's own name is ignored —
// replacing an integration's contents is not a rename.
//
// The resource set is reconciled by name rather than dropped and re-created, so a
// resource that is in both keeps its id (and anything referring to it), and only
// what the bundle no longer carries is deleted.
func (s *Service) Replace(ctx context.Context, integrationID string, data []byte, actorID string) (integration.Integration, error) {
	b, err := Read(data)
	if err != nil {
		return integration.Integration{}, err
	}

	// Read the archive before taking the lock — parsing is the slow part and does
	// not touch the integration — then hold it for the whole read-modify-restore.
	lock := s.lockFor(integrationID)
	lock.Lock()
	defer lock.Unlock()

	it, err := s.integrations.Get(ctx, integrationID)
	if err != nil {
		return integration.Integration{}, err
	}
	// What is there now, read before anything is written, so a failure part-way
	// through can be put back. The definition and the resources are stored by two
	// different modules and there is no transaction spanning them, so this is the
	// only thing standing between a failed replace and a working copy that is
	// half the bundle and half what it replaced.
	previous, err := s.Export(ctx, integrationID)
	if err != nil {
		return integration.Integration{}, err
	}

	updated, err := s.integrations.Update(ctx, integrationID, it.Name, b.Definition, actorID)
	if err != nil {
		return integration.Integration{}, err
	}
	if err := s.reconcileResources(ctx, integrationID, b.Resources); err != nil {
		s.restore(ctx, integrationID, previous, actorID)
		return integration.Integration{}, err
	}
	return updated, nil
}

// restore puts an integration back to a bundle read from it before a replace
// began. Best-effort and incapable of failing the caller — the replace's own
// error is the one worth reporting — so a failed restore is logged loudly and
// leaves a working copy the user can fix by replacing again.
func (s *Service) restore(ctx context.Context, integrationID string, previous Bundle, actorID string) {
	ctx, cancel := rollbackContext(ctx)
	defer cancel()

	if _, err := s.integrations.Update(ctx, integrationID, previous.Name, previous.Definition, actorID); err != nil {
		slog.Error("rolling back a failed bundle replace: definition",
			"integrationId", integrationID, "error", err)
	}
	if err := s.reconcileResources(ctx, integrationID, previous.Resources); err != nil {
		slog.Error("rolling back a failed bundle replace: resources",
			"integrationId", integrationID, "error", err)
	}
}

// reconcileResources makes an integration's stored resources match the bundle's:
// existing names are updated in place, new ones created, and the rest removed.
func (s *Service) reconcileResources(ctx context.Context, integrationID string, files []File) error {
	existing, err := s.resources.ListByIntegration(ctx, integrationID)
	if err != nil {
		return err
	}
	byName := make(map[string]resource.Resource, len(existing))
	for _, r := range existing {
		byName[r.Name] = r
	}

	for _, f := range files {
		current, ok := byName[f.Name]
		if ok {
			delete(byName, f.Name)
			if _, err := s.resources.Update(ctx, integrationID, current.ID, f.Kind, f.Name, f.Content); err != nil {
				return fmt.Errorf("replacing resource %q: %w", f.Name, err)
			}
			continue
		}
		if _, err := s.resources.Create(ctx, integrationID, f.Kind, f.Name, f.Content); err != nil {
			return fmt.Errorf("adding resource %q: %w", f.Name, err)
		}
	}
	for name, leftover := range byName {
		if err := s.resources.Delete(ctx, integrationID, leftover.ID); err != nil {
			return fmt.Errorf("removing resource %q: %w", name, err)
		}
	}
	return nil
}

// createUniquelyNamed creates the integration, suffixing the name until it is
// free. Only a name conflict is retried; every other failure is the caller's.
func (s *Service) createUniquelyNamed(ctx context.Context, name, definition, actorID string) (integration.Integration, error) {
	candidate := name
	for attempt := 2; ; attempt++ {
		created, err := s.integrations.Create(ctx, candidate, definition, actorID)
		if err == nil {
			return created, nil
		}
		if !errors.Is(err, integration.ErrNameTaken) || attempt > nameAttempts {
			return integration.Integration{}, err
		}
		candidate = fmt.Sprintf("%s (%d)", name, attempt)
	}
}

// discard removes an integration created for an import that then failed. It
// cannot fail the caller — the import error is the one worth reporting — so a
// failed rollback is logged and leaves a record the user can delete.
func (s *Service) discard(ctx context.Context, integrationID string) {
	ctx, cancel := rollbackContext(ctx)
	defer cancel()

	if err := s.integrations.Delete(ctx, integrationID); err != nil {
		slog.Error("rolling back a failed bundle import", "integrationId", integrationID, "error", err)
	}
}

// importName picks the name a new integration takes: what the archive says, else
// what the caller derived from the filename, else a generic one.
func importName(fromBundle, fallback string) string {
	if fromBundle != "" {
		return fromBundle
	}
	if fallback != "" {
		return fallback
	}
	return fallbackName
}

// lockFor returns the mutex guarding one integration's replaces, creating it on
// first use. The map only grows, by one small entry per integration ever
// replaced, which is bounded by the number of integrations.
func (s *Service) lockFor(integrationID string) *sync.Mutex {
	lock, _ := s.replacing.LoadOrStore(integrationID, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

// rollbackContext derives a bounded context for undoing a failed write, detached
// from the request's cancellation. The most likely reason a write failed is that
// the request was cancelled or timed out — and a cleanup that inherits that
// cancellation fails immediately, leaving exactly the partial state it exists to
// remove. It keeps the request's values (tracing, deadlines aside) so the undo is
// still attributable to the request that caused it.
func rollbackContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
}
