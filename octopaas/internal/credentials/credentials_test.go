package credentials

import (
	"os"
	"testing"
	"time"
)

// A session survives a round trip, and the file holding it is yours alone.
func TestSaveIsPrivateAndRoundTrips(t *testing.T) {

	home := t.TempDir()
	s, err := Load(home)
	if err != nil {
		t.Fatalf("loading with no file should be an empty store: %v", err)
	}
	expiry := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	s.Sessions["local"] = Session{Issuer: "https://idp.example.com", AccessToken: "at", RefreshToken: "rt", Expiry: expiry}
	s.Clients["https://idp.example.com"] = Client{ClientID: "c1", RedirectURI: "http://localhost:8976/callback"}
	if err := s.Save(home); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(Path(home))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("credentials.yaml should be 0600, is %o", info.Mode().Perm())
	}

	back, err := Load(home)
	if err != nil {
		t.Fatal(err)
	}
	got := back.Sessions["local"]
	if got.RefreshToken != "rt" || !got.Expiry.Equal(expiry) {
		t.Errorf("the session should come back as saved, got %+v", got)
	}
	if back.Clients["https://idp.example.com"].ClientID != "c1" {
		t.Errorf("the registered client should come back, got %+v", back.Clients)
	}
}
