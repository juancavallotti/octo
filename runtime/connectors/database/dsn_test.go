package database

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/juancavallotti/octo/types"
)

func TestWithPasswordLeavesDSNAloneWhenUnset(t *testing.T) {
	const dsn = "postgres://app@db.internal:5432/app?sslmode=require"

	got, err := withPassword("postgres", dsn, "", "db")
	if err != nil {
		t.Fatalf("withPassword: %v", err)
	}
	if got != dsn {
		t.Errorf("dsn = %q, want it unchanged (%q)", got, dsn)
	}
}

func TestWithPasswordURLForm(t *testing.T) {
	tests := []struct {
		name     string
		dsn      string
		password string
		want     string
	}{
		//nolint:gosec // G101: test fixtures, not real credentials
		{
			name:     "no password in the dsn",
			dsn:      "postgres://app@db.internal:5432/app?sslmode=require",
			password: "s3cret",
			want:     "postgres://app:s3cret@db.internal:5432/app?sslmode=require",
		},
		//nolint:gosec // G101: test fixtures, not real credentials
		{
			// The escaping net/url does is the reason to parse rather than splice.
			name:     "password needing escaping",
			dsn:      "postgres://app@db.internal:5432/app",
			password: "p@ss/w:rd",
			want:     "postgres://app:p%40ss%2Fw%3Ard@db.internal:5432/app",
		},
		//nolint:gosec // G101: test fixtures, not real credentials
		{
			name:     "dsn already carries a password: the setting wins",
			dsn:      "postgres://app:old@db.internal:5432/app",
			password: "new",
			want:     "postgres://app:new@db.internal:5432/app",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := withPassword("postgres", tt.dsn, tt.password, "db")
			if err != nil {
				t.Fatalf("withPassword: %v", err)
			}
			if got != tt.want {
				t.Errorf("dsn = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWithPasswordKeywordForm(t *testing.T) {
	tests := []struct {
		name     string
		dsn      string
		password string
		want     string
	}{
		{
			name:     "appended",
			dsn:      "host=db.internal user=app dbname=app sslmode=require",
			password: "s3cret",
			want:     "host=db.internal user=app dbname=app sslmode=require password='s3cret'",
		},
		{
			name:     "quoted when it has spaces and quotes",
			dsn:      "host=db.internal user=app",
			password: `a b'c\d`,
			want:     `host=db.internal user=app password='a b\'c\\d'`,
		},
		{
			name:     "replaces the one already in the dsn",
			dsn:      "host=db.internal password=old user=app",
			password: "new",
			want:     "host=db.internal user=app password='new'",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := withPassword("postgres", tt.dsn, tt.password, "db")
			if err != nil {
				t.Fatalf("withPassword: %v", err)
			}
			if got != tt.want {
				t.Errorf("dsn = %q, want %q", got, tt.want)
			}
		})
	}
}

// A password means nothing to sqlite; fail loudly rather than ignore it, so the
// author does not believe a database is protected when it is not.
func TestStartRejectsPasswordForSQLite(t *testing.T) {
	c := &Connector{}
	cfg := types.ConnectorConfig{
		Name: "test-db",
		Type: "database",
		Settings: types.Settings{
			"driver":   "sqlite",
			"dsn":      "file:" + filepath.Join(t.TempDir(), "test.db"),
			"password": "s3cret",
		},
	}

	err := c.Start(context.Background(), cfg)
	if err == nil {
		_ = c.Stop(context.Background())
		t.Fatal("expected an error when a sqlite connector is given a password")
	}
}
