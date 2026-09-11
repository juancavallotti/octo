// Package user is the iam service's feature module for platform principals and
// the roles granted to them. Identity originates at the OIDC provider; a row is
// created on first sign-in, keyed by the stable `subject`, and email/name are
// kept in sync on later ones. The generated `id` is the durable handle other
// tables reference, so it survives the provider changing an account's email.
//
// There is one way in: POST /auth, which verifies the provider's token.
package user

import (
	"time"
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
	Roles []Role
}

// HasRole reports whether u holds r.
func (u User) HasRole(r Role) bool {
	for _, held := range u.Roles {
		if held == r {
			return true
		}
	}
	return false
}
