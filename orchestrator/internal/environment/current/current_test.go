package current

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type known map[string]bool

func (k known) Exists(_ context.Context, name string) (bool, error) { return k[name], nil }

type failing struct{}

func (failing) Exists(context.Context, string) (bool, error) { return false, errors.New("down") }

func serve(lookup Lookup, path, header string) (*httptest.ResponseRecorder, string, bool) {
	var seen string
	var explicit bool
	h := Parse(Require(lookup, func(path string) bool { return path == "/healthz" }, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen, explicit = From(r.Context()), Explicit(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})))
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if header != "" {
		req.Header.Set(Header, header)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec, seen, explicit
}

func TestTheHeaderSelectsTheEnvironment(t *testing.T) {
	cases := []struct {
		name, header, want string
		explicit           bool
		status             int
	}{
		{"absent", "", "", false, http.StatusNoContent},
		{"the default by name", "octo", "", true, http.StatusNoContent},
		{"a known environment", "staging", "staging", true, http.StatusNoContent},
		{"padded", " staging ", "staging", true, http.StatusNoContent},
		{"an unknown environment", "nope", "", true, http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec, seen, explicit := serve(known{"staging": true}, "/integrations", tc.header)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d", rec.Code, tc.status)
			}
			if rec.Code == http.StatusNoContent && (seen != tc.want || explicit != tc.explicit) {
				t.Errorf("environment = %q (explicit %v), want %q (explicit %v)", seen, explicit, tc.want, tc.explicit)
			}
		})
	}
}

func TestTheEnvironmentRoutesIgnoreAStaleSelection(t *testing.T) {
	for _, path := range []string{"/environments", "/environments/staging"} {
		if rec, _, _ := serve(known{}, path, "deleted"); rec.Code != http.StatusNoContent {
			t.Errorf("%s with a deleted environment = %d, want it served", path, rec.Code)
		}
	}
}

func TestALookupFailureIsUnavailableNotUnknown(t *testing.T) {
	if rec, _, _ := serve(failing{}, "/integrations", "staging"); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestColumn(t *testing.T) {
	if Column(With(context.Background(), "octo", true)) != nil {
		t.Error("the default is not NULL")
	}
	if c := Column(With(context.Background(), "staging", true)); c == nil || *c != "staging" {
		t.Errorf("Column = %v, want staging", c)
	}
}

type counting struct {
	known
	calls int
}

func (c *counting) Exists(ctx context.Context, name string) (bool, error) {
	c.calls++
	return c.known.Exists(ctx, name)
}

func TestCachedAsksOncePerTTL(t *testing.T) {
	inner := &counting{known: known{"staging": true}}
	now := time.Unix(0, 0)
	c := Cached(inner, time.Minute).(*cached)
	c.now = func() time.Time { return now }

	for range 3 {
		if ok, _ := c.Exists(context.Background(), "staging"); !ok {
			t.Fatal("staging reported missing")
		}
	}
	if inner.calls != 1 {
		t.Errorf("asked %d times within the ttl, want 1", inner.calls)
	}
	now = now.Add(2 * time.Minute)
	_, _ = c.Exists(context.Background(), "staging")
	if inner.calls != 2 {
		t.Errorf("asked %d times after the ttl, want 2", inner.calls)
	}
}

// A name that does not exist is the caller's to choose, so it is not remembered:
// the memory stays bounded by the real environments.
func TestCachedForgetsNamesThatDoNotExist(t *testing.T) {
	c := Cached(&counting{known: known{"staging": true}}, time.Minute).(*cached)
	for _, name := range []string{"a", "b", "c"} {
		if ok, _ := c.Exists(context.Background(), name); ok {
			t.Fatalf("%s reported to exist", name)
		}
	}
	if len(c.answers) != 0 {
		t.Errorf("remembered %d unknown names, want none", len(c.answers))
	}
}

// A route the guard serves without a token must not answer by whether the named
// environment exists, or a caller who has not signed in could probe for them.
func TestAnUncheckedRouteIsServedWithoutALookup(t *testing.T) {
	rec, _, _ := serve(failing{}, "/healthz", "made-up")
	if rec.Code != http.StatusNoContent {
		t.Errorf("an unchecked route with an unknown environment: %d, want it served", rec.Code)
	}
}
