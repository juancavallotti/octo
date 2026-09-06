package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/juancavallotti/octo/observability/internal/storagestats"
)

type fakeStorageCollector struct {
	stats storagestats.Stats
}

func (f *fakeStorageCollector) Collect(context.Context) storagestats.Stats { return f.stats }

func doStorage(t *testing.T, svc StorageCollector) *httptest.ResponseRecorder {
	t.Helper()

	mux := http.NewServeMux()
	NewStorageHandler(svc).Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/storage", nil))
	return rec
}

// The report is the answer even when both stores are absent: a non-2xx would make
// the page that renders it indistinguishable from the service being down.
func TestStorageReportsAbsentStoresAs200WithReasons(t *testing.T) {
	rec := doStorage(t, &fakeStorageCollector{stats: storagestats.Stats{
		RedisReason:    "no REDIS_URL is configured for this installation",
		DatabaseReason: "this service is running without a database",
	}})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Redis          *json.RawMessage `json:"redis"`
		Database       *json.RawMessage `json:"database"`
		RedisReason    string           `json:"redisReason"`
		DatabaseReason string           `json:"databaseReason"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Redis != nil || got.Database != nil {
		t.Errorf("absent stores should be null halves, got redis=%s database=%s", got.Redis, got.Database)
	}
	if got.RedisReason == "" || got.DatabaseReason == "" {
		t.Errorf("an absent half needs its reason on the wire, got %+v", got)
	}
}

// The wire names are the page's contract; a renamed field would render as a blank
// tile rather than fail.
func TestStorageReportCarriesBothHalves(t *testing.T) {
	rec := doStorage(t, &fakeStorageCollector{stats: storagestats.Stats{
		Redis:    &storagestats.RedisStats{UsedMemory: 1024, MaxMemory: 4096, HitRate: 0.5},
		Database: &storagestats.DatabaseStats{AcquiredConns: 2, MaxConns: 8, KVTableBytes: 512},
	}})

	var got map[string]map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["redis"]["usedMemoryBytes"] != float64(1024) || got["redis"]["maxMemoryBytes"] != float64(4096) {
		t.Errorf("redis half = %v", got["redis"])
	}
	if got["database"]["acquiredConns"] != float64(2) || got["database"]["kvTableBytes"] != float64(512) {
		t.Errorf("database half = %v", got["database"])
	}
}
