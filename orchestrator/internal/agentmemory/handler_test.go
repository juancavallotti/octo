package agentmemory

import (
	"net/http/httptest"
	"testing"
)

// TestRuntimeUserComesFromThePathOrTheQuery pins the two places a runtime write
// can name the person it is for.
//
// The user-memory routes put the person in the path, because there the person is
// the resource. The thread routes cannot: a conversation is addressed by its
// thread key, and a second identifier in the address would let one conversation
// exist at two URLs. So the thread routes carry it as a query parameter — and for
// a while carried it nowhere at all, which stored every conversation attributed
// to nobody while the platform listed conversations by that attribution.
func TestRuntimeUserComesFromThePathOrTheQuery(t *testing.T) {
	t.Run("query", func(t *testing.T) {
		r := httptest.NewRequest("POST", "/turns?userId=user%2Falice", nil)
		if got := runtimeUser(r); got != "user/alice" {
			t.Errorf("want the decoded user, got %q", got)
		}
	})

	t.Run("absent", func(t *testing.T) {
		r := httptest.NewRequest("POST", "/turns", nil)
		if got := runtimeUser(r); got != "" {
			t.Errorf("an agent serving nobody names nobody, got %q", got)
		}
	})

	// The path wins where there is one, so the user-memory routes keep behaving
	// exactly as they did — a query parameter cannot redirect a write at one
	// person's memories into another person's.
	t.Run("path wins", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/memories?userId=mallory", nil)
		r.SetPathValue("userId", "alice")
		if got := runtimeUser(r); got != "alice" {
			t.Errorf("the addressed person should win, got %q", got)
		}
	})
}
