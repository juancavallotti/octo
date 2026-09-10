// Package user is the iam service's feature module for platform principals and
// the roles granted to them. Identity originates at the OIDC provider; iam
// bootstraps a row on first sign-in (keyed by the stable `subject`) and keeps
// email/name in sync on later logins. The generated `id` is the durable handle
// every other table references — api_keys, integrations.created_by — so it
// survives the identity provider changing an account's email.
//
// It shares the `users` table with the orchestrator's own user module, which
// still serves POST /users/bootstrap while the platform is pointed at it. Both
// write the same idempotent upsert keyed on `subject`, so the two paths converge
// on one row. The orchestrator's copy goes away in the change that moves the
// platform onto POST /auth.
//
// The module follows the same repository/service/handler shape as the
// orchestrator's feature modules.
package user

import (
	"time"

	"github.com/juancavallotti/octo/iam/internal/role"
)

// User is a platform principal. IDs are UUIDs in canonical text form; Subject is
// the OIDC `sub`.
type User struct {
	ID          string
	Subject     string
	Email       string
	Name        string
	CreatedAt   time.Time
	LastLoginAt time.Time
	// Roles is what this user has been granted. It is populated by the reads that
	// join user_roles and is empty — not nil-versus-empty meaningful — for a user
	// who has been granted nothing.
	Roles []role.Role
}

// HasRole reports whether u holds r.
func (u User) HasRole(r role.Role) bool {
	for _, held := range u.Roles {
		if held == r {
			return true
		}
	}
	return false
}
