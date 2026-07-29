package database

import (
	"testing"
	"time"
)

func TestResolvePoolSettingsPostgresDefaults(t *testing.T) {
	maxOpen, maxIdle, lifetime, err := resolvePoolSettings(connectorSettings{Driver: "postgres"})
	if err != nil {
		t.Fatalf("resolvePoolSettings: %v", err)
	}
	if maxOpen != defaultPostgresMaxOpenConns {
		t.Errorf("maxOpen = %d, want %d", maxOpen, defaultPostgresMaxOpenConns)
	}
	if maxIdle != defaultPostgresMaxIdleConns {
		t.Errorf("maxIdle = %d, want %d", maxIdle, defaultPostgresMaxIdleConns)
	}
	if lifetime != defaultPostgresConnMaxLifetime {
		t.Errorf("lifetime = %v, want %v", lifetime, defaultPostgresConnMaxLifetime)
	}
}

func TestResolvePoolSettingsSQLiteDefaults(t *testing.T) {
	maxOpen, maxIdle, lifetime, err := resolvePoolSettings(connectorSettings{Driver: "sqlite"})
	if err != nil {
		t.Fatalf("resolvePoolSettings: %v", err)
	}
	if maxOpen != defaultSQLiteMaxOpenConns {
		t.Errorf("maxOpen = %d, want %d", maxOpen, defaultSQLiteMaxOpenConns)
	}
	if maxIdle != defaultSQLiteMaxIdleConns {
		t.Errorf("maxIdle = %d, want %d", maxIdle, defaultSQLiteMaxIdleConns)
	}
	if lifetime != defaultSQLiteConnMaxLifetime {
		t.Errorf("lifetime = %v, want %v", lifetime, defaultSQLiteConnMaxLifetime)
	}
}

func TestResolvePoolSettingsRespectsConfigured(t *testing.T) {
	cases := []struct {
		name   string
		driver string
	}{
		{"postgres", "postgres"},
		{"sqlite", "sqlite"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			set := connectorSettings{
				Driver:          tc.driver,
				MaxOpenConns:    7,
				MaxIdleConns:    3,
				ConnMaxLifetime: "5m",
			}
			maxOpen, maxIdle, lifetime, err := resolvePoolSettings(set)
			if err != nil {
				t.Fatalf("resolvePoolSettings: %v", err)
			}
			if maxOpen != 7 {
				t.Errorf("maxOpen = %d, want 7 (configured value must win)", maxOpen)
			}
			if maxIdle != 3 {
				t.Errorf("maxIdle = %d, want 3 (configured value must win)", maxIdle)
			}
			if lifetime != 5*time.Minute {
				t.Errorf("lifetime = %v, want 5m (configured value must win)", lifetime)
			}
		})
	}
}

func TestResolvePoolSettingsRejectsBadLifetime(t *testing.T) {
	_, _, _, err := resolvePoolSettings(connectorSettings{Driver: "postgres", ConnMaxLifetime: "forever"})
	if err == nil {
		t.Fatal("expected an error for an unparseable connMaxLifetime")
	}
}

// TestResolvePoolSettingsRejectsNegativeLifetime confirms a negative
// connMaxLifetime is rejected rather than silently accepted: time.ParseDuration
// parses signed durations, but sql.DB.SetConnMaxLifetime treats <= 0 as
// unlimited, so a negative value would otherwise bypass the driver's default
// without any error.
func TestResolvePoolSettingsRejectsNegativeLifetime(t *testing.T) {
	_, _, _, err := resolvePoolSettings(connectorSettings{Driver: "postgres", ConnMaxLifetime: "-5m"})
	if err == nil {
		t.Fatal("expected an error for a negative connMaxLifetime")
	}
}

// TestStartAppliesDefaultPoolSettings confirms the resolved defaults actually
// reach the *sql.DB, not just resolvePoolSettings' return values. sql.DBStats
// only exposes a getter for MaxOpenConnections — there is no public accessor
// for the configured idle-conns cap or connection lifetime, so those two stay
// covered at the resolvePoolSettings unit-test level above.
func TestStartAppliesDefaultPoolSettings(t *testing.T) {
	c := startSQLite(t)

	db, err := c.DB()
	if err != nil {
		t.Fatalf("DB: %v", err)
	}
	if got := db.Stats().MaxOpenConnections; got != defaultSQLiteMaxOpenConns {
		t.Errorf("MaxOpenConnections = %d, want %d", got, defaultSQLiteMaxOpenConns)
	}
}
