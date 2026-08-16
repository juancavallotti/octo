// Package file provides a connector that owns a directory and the two blocks a
// flow reaches files through. The connector holds the root; file-read and
// file-write address files by a path relative to it, and a path that leaves the
// root is refused. Confining reads and writes to a declared directory is the
// point of the connector: the blocks take their path from a CEL expression, so
// the path is frequently chosen by whoever is calling the flow.
package file

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// connectorType is the name a flow declares this connector under, and the prefix
// its blocks are named with.
const connectorType = "file"

// newFileMode is the permission applied to files file-write creates, and
// newDirMode to the parent directories it creates with createDirs. Owner
// read/write only: a flow's files can carry anything the message did.
const (
	newFileMode = 0o600
	newDirMode  = 0o700
)

// init is this module's manifest: the one place that says what importing this
// package puts into the runtime, in a deterministic order. Each block's own
// registration lives beside the block as a registerX function called from here.
func init() {
	registerConnector()
	registerRead()
	registerWrite()
}

func registerConnector() {
	core.MustRegisterConnector(connectorType, func() core.Connector {
		return &Connector{}
	})

	// Package-level editor defaults: the connector and its two blocks share the
	// Storage & Cache palette group unless they set their own icon.
	core.RegisterExtension(core.ExtensionMeta{Group: "Storage & Cache", Icon: "FileText"})

	core.RegisterConnectorMeta(core.ConnectorMeta{
		Type:     connectorType,
		Label:    "File",
		Settings: reflect.TypeFor[connectorSettings](),
	})
}

// connectorSettings configures the directory the connector owns. There is no
// default root: where a flow may read and write is the one thing worth stating
// out loud, and inheriting the process working directory would make it depend on
// how the runtime happened to be launched.
type connectorSettings struct {
	// Directory every path is resolved under. A path that escapes it is refused.
	Root string `json:"root" octo:"label=Root directory,required"`
	// Create missing parent directories when writing.
	CreateDirs bool `json:"createDirs" octo:"label=Create directories,default=false"`
}

// Connector owns a directory that its blocks read and write within. root is
// absolute so the containment check is between absolute paths.
type Connector struct {
	root       string
	createDirs bool
}

// Start resolves and checks the root so a missing or misspelled directory fails
// the deployment rather than every message that reaches a block.
func (c *Connector) Start(_ context.Context, config types.ConnectorConfig) error {
	var set connectorSettings
	if err := config.Settings.Decode(&set); err != nil {
		return err
	}
	if set.Root == "" {
		return errors.New("file connector requires a root directory")
	}

	// A relative root is not merely untidy, it is wrong: resolve folds "./" away,
	// so a root of "." would no longer prefix the paths joined onto it and every
	// path would read as an escape.
	root, err := filepath.Abs(set.Root)
	if err != nil {
		return fmt.Errorf("resolve file root %q: %w", set.Root, err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("file root %q: %w", set.Root, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("file root %q is not a directory", set.Root)
	}

	c.root = root
	c.createDirs = set.CreateDirs
	return nil
}

// Stop releases nothing: the connector holds no handle between messages, since
// each read and write opens the root for the length of that one operation.
func (c *Connector) Stop(context.Context) error { return nil }

// ReadFile returns the contents of path, resolved under the connector's root.
//
// The read goes through os.Root rather than os.ReadFile because resolve's answer
// is lexical, and a lexical answer is not the whole containment question: if
// "<root>/link" is a symlink elsewhere, "link/secret" is a path under the root
// that names a file outside it. os.Root resolves each component against the
// opened root directory and refuses the ones that leave, so the escape is decided
// by the kernel on the path actually walked, not by the string.
func (c *Connector) ReadFile(path string) ([]byte, error) {
	rel, err := c.resolve(path)
	if err != nil {
		return nil, err
	}
	root, err := c.openRoot()
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()

	data, err := root.ReadFile(rel)
	if err != nil {
		return nil, fmt.Errorf("read file %q: %w", path, err)
	}
	return data, nil
}

// WriteFile writes data to path, resolved under the connector's root, replacing
// any file already there. With createDirs set, missing parent directories are
// created first; without it, writing into one that does not exist is an error,
// so a typo in a path does not quietly build a tree nobody asked for.
func (c *Connector) WriteFile(path string, data []byte) error {
	rel, err := c.resolve(path)
	if err != nil {
		return err
	}
	root, err := c.openRoot()
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()

	if c.createDirs {
		if dir := filepath.Dir(rel); dir != "." {
			if err := root.MkdirAll(dir, newDirMode); err != nil {
				return fmt.Errorf("create directory for %q: %w", path, err)
			}
		}
	}
	if err := root.WriteFile(rel, data, newFileMode); err != nil {
		return fmt.Errorf("write file %q: %w", path, err)
	}
	return nil
}

// openRoot opens the root directory for one operation. It is opened per call
// rather than held for the connector's life so a root that is replaced or
// remounted between messages is picked up, and so nothing has to be closed on
// shutdown.
func (c *Connector) openRoot() (*os.Root, error) {
	if c.root == "" {
		return nil, errors.New("file connector not started")
	}
	root, err := os.OpenRoot(c.root)
	if err != nil {
		return nil, fmt.Errorf("open file root %q: %w", c.root, err)
	}
	return root, nil
}

// resolve maps a path to a cleaned path relative to the root, rejecting any that
// escapes it. Containment is decided by filepath.Rel: a relative result starting
// with ".." left the root, anything else did not.
//
// Rel rather than a "root + separator" prefix test, on the joined-but-unclamped
// path, for two reasons. A root that is itself a filesystem root has no such
// prefix — "/" plus a separator is "//", which prefixes none of its own children.
// And pre-cleaning the path as an absolute one ("/" + path) silently rewrites
// "../secret" to "<root>/secret" instead of refusing it, which is safe but
// answers a different question than the one asked: the guard could then never
// fire, and the path would resolve somewhere the caller did not name. A ".." that
// stays inside the root is still fine — Join folds it away here, so os.Root never
// has to walk through a directory the path only mentioned on its way back out.
func (c *Connector) resolve(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("file path is empty")
	}
	// An absolute path is refused rather than silently re-rooted. Join would
	// otherwise treat "/etc/passwd" as relative and hand back "<root>/etc/passwd",
	// which is contained — so the guard below would never fire — but is not the
	// file the caller named. Saying so is the honest answer, and it stops a write
	// with createDirs from mirroring a whole system path inside the root.
	if filepath.IsAbs(path) {
		return "", fmt.Errorf("file path %q must be relative to the connector root", path)
	}
	full := filepath.Join(c.root, path)
	rel, err := filepath.Rel(c.root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("file path %q escapes the connector root", path)
	}
	return rel, nil
}

// resolveConnector binds a block to its file connector by name.
func resolveConnector(name string, deps core.BlockDeps) (*Connector, error) {
	if name == "" {
		return nil, errors.New("a file connector name is required")
	}
	if deps.Connector == nil {
		return nil, fmt.Errorf("file connector %q requested but no connectors are available", name)
	}
	connector, ok := deps.Connector(name)
	if !ok {
		return nil, fmt.Errorf("file connector %q is not configured", name)
	}
	provider, ok := connector.(*Connector)
	if !ok {
		return nil, fmt.Errorf("connector %q is not a file connector", name)
	}
	return provider, nil
}
