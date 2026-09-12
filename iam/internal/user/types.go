// Package user is the iam service's feature module for platform principals and
// the roles granted to them.
//
// Two keys, and the difference between them is the provisioning story. An
// administrator provisions somebody by **address**, because that is the only
// thing they know about a colleague who has never been here. The OIDC
// **subject** is discovered: it is empty until the first sign-in writes it onto
// the row waiting for that address, and from then on it is what every sign-in
// keys on — so the provider changing somebody's address is a refresh rather
// than a new person. The generated id is the durable handle other tables
// reference and outlives both.
//
// There is one way in: POST /auth, which verifies the provider's token.
package user

import (
	"time"
)

// User is a platform principal. IDs are UUIDs in canonical text form.
type User struct {
	ID string
	// Subject is the OIDC `sub`, empty for somebody provisioned who has not
	// signed in yet.
	Subject   string
	Email     string
	Name      string
	CreatedAt time.Time
	// LastLoginAt is nil for somebody who has never arrived, which is a different
	// thing from having arrived when the row was written.
	LastLoginAt *time.Time
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
