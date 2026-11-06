package snapshot

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/juancavallotti/octo/orchestrator/internal/environment/current"
)

// ErrSameEnvironment is returned when a version is promoted into the environment
// it is already in.
var ErrSameEnvironment = errors.New("the version is already in that environment")

// ErrNameTaken is returned when promoting would create an integration in the
// target environment under a name another integration there already has.
var ErrNameTaken = errors.New("an unrelated integration in that environment has the same name")

// ErrUnknownEnvironment is returned when the target is not an environment.
var ErrUnknownEnvironment = errors.New("unknown environment")

// ErrMayNotBuild is returned when the caller holds no role in the target
// environment that may create integrations and versions there.
var ErrMayNotBuild = errors.New("you may not build integrations in that environment")

// Promotion is what promoting a version produced in the target environment.
type Promotion struct {
	// IntegrationID is the target's integration: the family member already there,
	// or the one this promotion created.
	IntegrationID string
	// Created reports whether this promotion created that integration.
	Created bool
	// Snapshot is the version, under the same tag, in the target.
	Snapshot Summary
	// Environment is the target, "" for the default.
	Environment string
}

// environments answers whether a name is an environment.
type environments interface {
	Exists(ctx context.Context, name string) (bool, error)
}

// WithEnvironments lets versions be promoted, checking targets against e.
func (s *Service) WithEnvironments(e environments) *Service {
	s.environments = e
	return s
}

// Promote copies the version snapshotID, which is in the environment ctx acts
// in, into target under the same tag. mayBuild says whether the caller may
// create integrations and versions in target; the caller's roles live with the
// request, so the handler decides it.
//
// The version lands on the target's member of the integration's family — the
// one promoted from the same root — creating it, with a working copy seeded from
// the version, the first time. The copied version carries the source's flows
// and templates and the target's own env files: env files hold an environment's
// hosts and credentials, so they are never carried from one to another.
func (s *Service) Promote(ctx context.Context, snapshotID, target, actorID string, mayBuild bool) (Promotion, error) {
	target = current.Normalize(target)
	if target == current.From(ctx) {
		return Promotion{}, ErrSameEnvironment
	}
	if s.environments == nil {
		return Promotion{}, ErrUnknownEnvironment
	}
	ok, err := s.environments.Exists(ctx, target)
	if err != nil {
		return Promotion{}, err
	}
	if !ok {
		return Promotion{}, ErrUnknownEnvironment
	}
	if !mayBuild {
		return Promotion{}, ErrMayNotBuild
	}
	return s.repo.Promote(ctx, snapshotID, target, actorID)
}

// Promote performs a promotion in one transaction; see Service.Promote.
func (r *Repo) Promote(ctx context.Context, snapshotID, target, actorID string) (Promotion, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Promotion{}, fmt.Errorf("snapshot repo: promote: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var tag, name, icon, root string
	err = tx.QueryRow(ctx,
		`SELECT s.tag, i.name, i.icon, COALESCE(i.promoted_from, i.id)::text
		   FROM integration_snapshots s JOIN integrations i ON i.id = s.integration_id
		  WHERE s.id = $1`, snapshotID).Scan(&tag, &name, &icon, &root)
	if errors.Is(err, pgx.ErrNoRows) {
		return Promotion{}, ErrNotFound
	}
	if err != nil {
		return Promotion{}, fmt.Errorf("snapshot repo: promote: read the version: %w", err)
	}

	out := Promotion{Environment: target}
	environment := nullable(target)
	err = tx.QueryRow(ctx,
		`SELECT id::text FROM integrations
		  WHERE environment IS NOT DISTINCT FROM $1 AND (id = $2 OR promoted_from = $2)
		  LIMIT 1`, environment, root).Scan(&out.IntegrationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if out.IntegrationID, err = createPromoted(ctx, tx, snapshotID, name, icon, root, environment, actorID); err != nil {
			return Promotion{}, err
		}
		out.Created = true
	case err != nil:
		return Promotion{}, fmt.Errorf("snapshot repo: promote: find the target: %w", err)
	}

	err = tx.QueryRow(ctx,
		`INSERT INTO integration_snapshots (integration_id, tag) VALUES ($1, $2)
		 RETURNING id::text, integration_id::text, tag, created_at`, out.IntegrationID, tag,
	).Scan(&out.Snapshot.ID, &out.Snapshot.IntegrationID, &out.Snapshot.Tag, &out.Snapshot.CreatedAt)
	if err != nil {
		if pgErrorCode(err) == pgUniqueViolation {
			return Promotion{}, ErrTagExists
		}
		return Promotion{}, fmt.Errorf("snapshot repo: promote: create the version: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO integration_file_snapshots (snapshot_id, kind, path, content, role)
		 SELECT $1::uuid, kind, path, content, role FROM integration_file_snapshots
		  WHERE snapshot_id = $2 AND kind <> 'env'
		 UNION ALL
		 SELECT $1::uuid, kind, path, content, role FROM integration_files
		  WHERE integration_id = $3 AND kind = 'env'`,
		out.Snapshot.ID, snapshotID, out.IntegrationID); err != nil {
		return Promotion{}, fmt.Errorf("snapshot repo: promote: freeze the files: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Promotion{}, fmt.Errorf("snapshot repo: promote: commit: %w", err)
	}
	return out, nil
}

// createPromoted creates the family's member in the target environment, its
// working copy seeded from the version so it opens on what was promoted.
func createPromoted(
	ctx context.Context, tx pgx.Tx, snapshotID, name, icon, root string, environment *string, actorID string,
) (string, error) {
	var id string
	err := tx.QueryRow(ctx,
		`INSERT INTO integrations (name, icon, environment, promoted_from, created_by, updated_by)
		 VALUES ($1, $2, $3, $4, NULLIF($5, '')::uuid, NULLIF($5, '')::uuid)
		 RETURNING id::text`, name, icon, environment, root, strings.TrimSpace(actorID)).Scan(&id)
	if err != nil {
		if pgErrorCode(err) == pgUniqueViolation {
			return "", ErrNameTaken
		}
		return "", fmt.Errorf("snapshot repo: promote: create the integration: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO integration_files (integration_id, kind, path, content, role)
		 SELECT $1, kind, path, content, role FROM integration_file_snapshots
		  WHERE snapshot_id = $2 AND kind <> 'env'`, id, snapshotID); err != nil {
		return "", fmt.Errorf("snapshot repo: promote: seed the working copy: %w", err)
	}
	return id, nil
}

func nullable(environment string) *string {
	if environment == "" {
		return nil
	}
	return &environment
}
