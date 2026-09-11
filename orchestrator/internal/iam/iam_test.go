package iam

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMintMachineSendsTheCallerTokenAndTheDeployment(t *testing.T) {
	var (
		gotPath   string
		gotBearer string
		gotBody   map[string]string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotBearer = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "machine.token"})
	}))
	defer srv.Close()

	token, err := New(srv.URL).MintMachine(context.Background(), "caller.token", "dep-1", "")
	if err != nil {
		t.Fatalf("MintMachine: %v", err)
	}
	if token != "machine.token" {
		t.Errorf("token = %q, want the minted one", token)
	}
	if gotPath != "/auth/machine" {
		t.Errorf("path = %q, want /auth/machine", gotPath)
	}
	if gotBearer != "Bearer caller.token" {
		t.Errorf("Authorization = %q, want the caller's own token", gotBearer)
	}
	if gotBody["deployment"] != "dep-1" {
		t.Errorf("deployment = %q, want dep-1", gotBody["deployment"])
	}
}

// A trailing slash on the address must not double up in the path.
func TestMintMachineTrimsTheAddress(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "t"})
	}))
	defer srv.Close()

	if _, err := New(srv.URL+"/").MintMachine(context.Background(), "c", "dep-1", ""); err != nil {
		t.Fatalf("MintMachine: %v", err)
	}
	if gotPath != "/auth/machine" {
		t.Errorf("path = %q, want /auth/machine", gotPath)
	}
}

// iam's own wording says whether the caller may not deploy or was not recognised,
// which are the two an operator has to tell apart.
func TestMintMachineReportsWhatIAMSaid(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "lending an identity to a deployment requires platform:operator or platform:admin",
		})
	}))
	defer srv.Close()

	_, err := New(srv.URL).MintMachine(context.Background(), "c", "dep-1", "")
	if err == nil {
		t.Fatal("MintMachine succeeded against a 403")
	}
	if !strings.Contains(err.Error(), "platform:operator") {
		t.Errorf("error = %v, want it to carry iam's reason", err)
	}
}

// Nothing to mint on the authority of is refused here rather than sent, so a
// request with no credential never reaches iam as an anonymous one.
func TestMintMachineRefusesWithoutACallerToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("iam was called with no caller token")
	}))
	defer srv.Close()

	if _, err := New(srv.URL).MintMachine(context.Background(), "  ", "dep-1", ""); err == nil {
		t.Error("MintMachine accepted an empty caller token")
	}
}

func TestUnconfiguredClientMintsNothing(t *testing.T) {
	c := New("")
	if c.Configured() {
		t.Error("Configured() is true with no address")
	}
	if _, err := c.MintMachine(context.Background(), "c", "dep-1", ""); err == nil {
		t.Error("MintMachine succeeded with no address")
	}
}

// A reply that parses but carries no token is a failure, not an empty identity —
// a deployment would otherwise be created silently unable to authenticate.
func TestMintMachineRefusesAnEmptyToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"token": ""})
	}))
	defer srv.Close()

	if _, err := New(srv.URL).MintMachine(context.Background(), "c", "dep-1", ""); err == nil {
		t.Error("MintMachine accepted a reply with no token")
	}
}
