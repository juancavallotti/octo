package k8s

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/services/servicestest"
)

// multiKeyServer is a fuller stand-in for the orchestrator store than kvServer:
// many keys, real version rules, and a version-checked delete. The contract suite
// needs all three, and kvServer deliberately holds one key because the tests
// beside it are about request shaping rather than storage.
type multiKeyServer struct {
	mu   sync.Mutex
	rows map[string]kvRow
}

type kvRow struct {
	value   []byte
	version int64
}

func newMultiKeyServer() *multiKeyServer {
	return &multiKeyServer{rows: map[string]kvRow{}}
}

func (s *multiKeyServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, exists := s.rows[r.URL.Path]

	switch r.Method {
	case http.MethodGet:
		if !exists {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set(headerVersion, strconv.FormatInt(row.version, 10))
		_, _ = w.Write(row.value)
	case http.MethodPut:
		expected := parseVersion(r.Header.Get(headerVersion))
		if (expected == 0 && exists) || (expected != 0 && (!exists || row.version != expected)) {
			w.WriteHeader(http.StatusConflict)
			return
		}
		body, _ := io.ReadAll(r.Body)
		row.version++
		row.value = body
		s.rows[r.URL.Path] = row
		w.Header().Set(headerVersion, strconv.FormatInt(row.version, 10))
		w.WriteHeader(http.StatusOK)
	case http.MethodDelete:
		expected := parseVersion(r.Header.Get(headerVersion))
		if !exists {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if expected != 0 && row.version != expected {
			w.WriteHeader(http.StatusConflict)
			return
		}
		delete(s.rows, r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}
}

// The shared contract, run against the k8s module's orchestrator-backed store.
func TestKVContract(t *testing.T) {
	ts := httptest.NewServer(newMultiKeyServer())
	t.Cleanup(ts.Close)
	servicestest.KVContract(t, core.NamespaceUser, newHTTPStore(ts.URL, "dep-123", ""))
}

// And against its coordination-Lease claims.
func TestLeasesContract(t *testing.T) {
	l, _ := newTestLeases(t, newFixedClock())
	servicestest.LeasesContract(t, l)
}
