package integration

import (
	"context"
	"fmt"
	"strings"

	"github.com/juancavallotti/octo/orchestrator/internal/projectfile"
)

// maxPathLen bounds a config file's path; the column is unconstrained varchar
// so the limit is enforced here rather than by the database.
const maxPathLen = 512

// ListFiles returns an integration's config files ordered by path.
func (s *Service) ListFiles(ctx context.Context, integrationID string) ([]ConfigFile, error) {
	return s.repo.ConfigFiles(ctx, integrationID)
}

// GetFile returns one of an integration's config files, or ErrFileNotFound.
func (s *Service) GetFile(ctx context.Context, integrationID, path string) (ConfigFile, error) {
	if err := validatePath(path); err != nil {
		return ConfigFile{}, err
	}
	files, err := s.repo.ConfigFiles(ctx, integrationID)
	if err != nil {
		return ConfigFile{}, err
	}
	for _, f := range files {
		if f.Path == path {
			return f, nil
		}
	}
	return ConfigFile{}, fmt.Errorf("%w: %q", ErrFileNotFound, path)
}

// PutFile writes one config file, creating it when path is new. actorID is the
// editing user's id, or "" when unknown.
//
// The write is refused with ErrFilesConflict when the project would no longer
// merge into one config, because the merged definition is what every read of
// the integration returns: storing files that cannot merge would leave an
// integration nobody can open to fix.
func (s *Service) PutFile(ctx context.Context, integrationID, path, content, actorID string) (ConfigFile, error) {
	if err := validatePath(path); err != nil {
		return ConfigFile{}, err
	}
	current, err := s.repo.ConfigFiles(ctx, integrationID)
	if err != nil {
		return ConfigFile{}, err
	}
	if err := ensureMerges(withFile(current, path, content)); err != nil {
		return ConfigFile{}, err
	}
	written, err := s.repo.PutConfigFile(ctx, integrationID, path, content, actorID)
	if err != nil {
		return ConfigFile{}, err
	}
	s.notifyChanged(ctx, integrationID)
	return written, nil
}

// DeleteFile removes one config file. Removing a file can only remove merge
// conflicts, never add one, so it needs no merge check.
func (s *Service) DeleteFile(ctx context.Context, integrationID, path, actorID string) error {
	if err := validatePath(path); err != nil {
		return err
	}
	if err := s.repo.DeleteConfigFile(ctx, integrationID, path, actorID); err != nil {
		return err
	}
	s.notifyChanged(ctx, integrationID)
	return nil
}

// validatePath accepts only a path the runtime would load as config: a root-level,
// non-test YAML file. A backslash is refused too, since an exported bundle carries
// the path as a zip entry name, where it would read as a separator.
func validatePath(path string) error {
	if len(path) > maxPathLen {
		return fmt.Errorf("%w: path must be at most %d characters", ErrInvalid, maxPathLen)
	}
	if strings.ContainsRune(path, '\\') {
		return fmt.Errorf("%w: path %q contains a backslash", ErrInvalid, path)
	}
	if role := projectfile.Classify(path); role != projectfile.RoleConfig {
		return fmt.Errorf("%w: %q would be a %s file, not config — a config file is a .yaml or .yml "+
			"at the project root that does not end in _test", ErrInvalid, path, role)
	}
	return nil
}

// withFile returns the project's files as they would be with content at path.
func withFile(current []ConfigFile, path, content string) []projectfile.File {
	ret := make([]projectfile.File, 0, len(current)+1)
	for _, f := range current {
		if f.Path != path {
			ret = append(ret, projectfile.File{Path: f.Path, Content: f.Content})
		}
	}
	return append(ret, projectfile.File{Path: path, Content: content})
}

// ensureMerges refuses a set of config files that does not fold into one config.
func ensureMerges(files []projectfile.File) error {
	if _, err := projectfile.Merge(files); err != nil {
		return fmt.Errorf("%w: %w", ErrFilesConflict, err)
	}
	return nil
}
