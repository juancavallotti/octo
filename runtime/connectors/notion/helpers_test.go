package notion

import (
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// verifyToken is the verification token blockDeps configures the connector with,
// so block tests can sign payloads the verify block will accept.
const verifyToken = "0123456789abcdef0123456789abcdef"

// blockDeps starts a notion connector and returns BlockDeps that resolve it under
// the name "notion". baseURL is empty for blocks that never call the API (verify,
// event); API-calling block tests point it at a stub server.
func blockDeps(t *testing.T, baseURL string) core.BlockDeps {
	t.Helper()
	conn := startConnector(t, map[string]any{
		"token":             "ntn-test",
		"verificationToken": verifyToken,
		"apiBaseURL":        baseURL,
	})
	return core.BlockDeps{Connector: func(name string) (core.Connector, bool) {
		if name == "notion" {
			return conn, true
		}
		return nil, false
	}}
}

// depsFor returns BlockDeps resolving conn under the name "notion".
func depsFor(conn *Connector) core.BlockDeps {
	return core.BlockDeps{Connector: func(name string) (core.Connector, bool) {
		if name == "notion" {
			return conn, true
		}
		return nil, false
	}}
}

func blockMessage(t *testing.T, body any) *types.Message {
	t.Helper()
	msg, err := types.NewMessage("")
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	msg.Body = body
	return msg
}
