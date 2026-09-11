package signing

import "errors"

var (
	// ErrNoKey is returned when the set holds no key that can sign — which, since
	// the service mints one on demand, means only that it could not.
	ErrNoKey = errors.New("no signing key is available")
	// ErrInvalidConfig is returned by NewService when the settings it is given
	// cannot produce a coherent keyset.
	ErrInvalidConfig = errors.New("invalid signing configuration")
	// ErrNotOurToken is returned by Verify for anything this service did not mint,
	// or minted too long ago: a bad signature, another issuer, another audience,
	// or an expiry outside whatever window the caller allowed. One error for all
	// of them on purpose — the distinction matters to a log and not to a caller,
	// who can do nothing differently in any of the cases.
	ErrNotOurToken = errors.New("not a token this service minted")
)

// ErrExpired accompanies ErrNotOurToken when a token failed the expiry check and
// nothing else. It exists so that a caller allowed to renew an expired token —
// only auth.Service.Refresh, and only for a machine token — can tell that apart
// from a token that was never ours. Treating it like any other error refuses the
// token, which is the reading every other caller should take.
var ErrExpired = errors.New("the token has expired")
