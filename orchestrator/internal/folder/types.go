// Package folder is the orchestrator feature module for organizing integrations
// in a folder tree. It owns the integration_idx_structure table (the folders,
// which nest via a self-referencing parent) and integration_folder_members (the
// single-folder membership of each integration). It exposes tree CRUD and
// membership operations through a repository, a validating service and an HTTP
// handler.
package folder

// Folder is a node in the organization tree. ParentID is nil for a root folder;
// IDs are UUIDs in canonical text form. Children is populated only when a tree is
// assembled (see Service.Tree); the repository returns folders with it empty.
type Folder struct {
	ID       string
	ParentID *string
	Name     string
	Children []Folder
}
