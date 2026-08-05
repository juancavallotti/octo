package mongodb

import (
	"context"
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/types"
)

func TestStartRequiresURI(t *testing.T) {
	c := &Connector{}
	cfg := types.ConnectorConfig{Name: "orders-db", Type: connectorType, Settings: types.Settings{}}
	if err := c.Start(context.Background(), cfg); err == nil {
		t.Error("expected an error when uri is missing")
	}
}

func TestStartRejectsBadSettings(t *testing.T) {
	tests := []struct {
		name     string
		settings types.Settings
	}{
		{
			name:     "unknown extended json mode",
			settings: types.Settings{"uri": "mongodb://db.internal:27017", "extendedJson": "loose"},
		},
		{
			name:     "unparseable timeout",
			settings: types.Settings{"uri": "mongodb://db.internal:27017", "timeout": "soon"},
		},
		{
			name:     "negative pool size",
			settings: types.Settings{"uri": "mongodb://db.internal:27017", "maxPoolSize": -1},
		},
		{
			// A password with nobody to attach it to; caught before any dial.
			name:     "password without a username",
			settings: types.Settings{"uri": "mongodb://db.internal:27017", "password": "s3cret"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Connector{}
			cfg := types.ConnectorConfig{Name: "orders-db", Type: connectorType, Settings: tt.settings}
			if err := c.Start(context.Background(), cfg); err == nil {
				t.Error("expected a startup error")
			}
		})
	}
}

// A connector that never started must not hand a block a nil client to
// dereference, and must not fail a second Stop.
func TestCollectionWithoutStart(t *testing.T) {
	c := &Connector{}
	if _, err := c.Collection("orders", "line-items"); err == nil {
		t.Error("expected an error from a connector that was never started")
	}
}

func TestStopIsIdempotent(t *testing.T) {
	c := &Connector{}
	if err := c.Stop(context.Background()); err != nil {
		t.Errorf("Stop on a connector that never started: %v", err)
	}
	if err := c.Stop(context.Background()); err != nil {
		t.Errorf("second Stop: %v", err)
	}
}

func TestResolveExtendedJSON(t *testing.T) {
	tests := []struct {
		name    string
		mode    string
		want    bool
		wantErr bool
	}{
		{name: "unset is relaxed", mode: "", want: false},
		{name: "relaxed", mode: extendedJSONRelaxed, want: false},
		{name: "canonical", mode: extendedJSONCanonical, want: true},
		{name: "unknown", mode: "extended", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveExtendedJSON(tt.mode)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error for %q", tt.mode)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveExtendedJSON: %v", err)
			}
			if got != tt.want {
				t.Errorf("resolveExtendedJSON(%q) = %v, want %v", tt.mode, got, tt.want)
			}
		})
	}
}

func TestResolveClientOptionsDefaults(t *testing.T) {
	opts, err := resolveClientOptions("mongodb://db.internal:27017", connectorSettings{})
	if err != nil {
		t.Fatalf("resolveClientOptions: %v", err)
	}
	if opts.MaxPoolSize == nil || *opts.MaxPoolSize != defaultMaxPoolSize {
		t.Errorf("MaxPoolSize = %v, want %d", opts.MaxPoolSize, defaultMaxPoolSize)
	}
	if opts.Timeout == nil || *opts.Timeout != defaultTimeout {
		t.Errorf("Timeout = %v, want %s", opts.Timeout, defaultTimeout)
	}
	// Left to the driver: opening idle connections eagerly, and recycling healthy
	// ones, are both things the deployment is better placed to decide.
	if opts.MinPoolSize != nil {
		t.Errorf("MinPoolSize = %v, want unset", *opts.MinPoolSize)
	}
	if opts.MaxConnIdleTime != nil {
		t.Errorf("MaxConnIdleTime = %v, want unset", *opts.MaxConnIdleTime)
	}
	if opts.AppName == nil || *opts.AppName != appName {
		t.Errorf("AppName = %v, want %q", opts.AppName, appName)
	}
}

func TestResolveClientOptionsRespectsConfigured(t *testing.T) {
	set := connectorSettings{
		MaxPoolSize:     50,
		MinPoolSize:     5,
		MaxConnIdleTime: "5m",
		Timeout:         "10s",
	}
	opts, err := resolveClientOptions("mongodb://db.internal:27017", set)
	if err != nil {
		t.Fatalf("resolveClientOptions: %v", err)
	}
	if opts.MaxPoolSize == nil || *opts.MaxPoolSize != 50 {
		t.Errorf("MaxPoolSize = %v, want 50", opts.MaxPoolSize)
	}
	if opts.MinPoolSize == nil || *opts.MinPoolSize != 5 {
		t.Errorf("MinPoolSize = %v, want 5", opts.MinPoolSize)
	}
	if opts.MaxConnIdleTime == nil || *opts.MaxConnIdleTime != 5*time.Minute {
		t.Errorf("MaxConnIdleTime = %v, want 5m", opts.MaxConnIdleTime)
	}
	if opts.Timeout == nil || *opts.Timeout != 10*time.Second {
		t.Errorf("Timeout = %v, want 10s", opts.Timeout)
	}
}

// A connection string that already tunes the pool keeps its own number: this
// package's default fills a gap, it does not overrule a deliberate choice.
func TestResolveClientOptionsLeavesURITuningAlone(t *testing.T) {
	opts, err := resolveClientOptions("mongodb://db.internal:27017/?maxPoolSize=50", connectorSettings{})
	if err != nil {
		t.Fatalf("resolveClientOptions: %v", err)
	}
	if opts.MaxPoolSize == nil || *opts.MaxPoolSize != 50 {
		t.Errorf("MaxPoolSize = %v, want the uri's 50", opts.MaxPoolSize)
	}
}

// ...but a setting does, so there is one place an operator can override
// whatever the connection string says.
func TestResolveClientOptionsSettingBeatsURI(t *testing.T) {
	set := connectorSettings{MaxPoolSize: 8}
	opts, err := resolveClientOptions("mongodb://db.internal:27017/?maxPoolSize=50", set)
	if err != nil {
		t.Fatalf("resolveClientOptions: %v", err)
	}
	if opts.MaxPoolSize == nil || *opts.MaxPoolSize != 8 {
		t.Errorf("MaxPoolSize = %v, want the setting's 8", opts.MaxPoolSize)
	}
}

func TestResolveClientOptionsRejectsBadValues(t *testing.T) {
	tests := []struct {
		name string
		set  connectorSettings
	}{
		{name: "negative max pool", set: connectorSettings{MaxPoolSize: -1}},
		{name: "negative min pool", set: connectorSettings{MinPoolSize: -1}},
		{name: "unparseable idle time", set: connectorSettings{MaxConnIdleTime: "forever"}},
		// The driver reads a negative duration as "no limit", so accepting one
		// would silently bypass the default the author meant to change.
		{name: "negative timeout", set: connectorSettings{Timeout: "-1s"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := resolveClientOptions("mongodb://db.internal:27017", tt.set); err == nil {
				t.Error("expected a config error")
			}
		})
	}
}

// verifyOnStart off is what lets a config with no server behind it be built --
// a flow test, or a deployment that would rather ride out a failover than
// refuse to start during one. The client is opened either way; only the ping is
// skipped, so nothing about the connector's later behaviour changes.
func TestStartWithoutVerification(t *testing.T) {
	off := false
	c := &Connector{}
	cfg := types.ConnectorConfig{
		Name: "orders-db",
		Type: connectorType,
		Settings: types.Settings{
			// A port nothing listens on: reaching it would be the failure this
			// test asserts does not happen.
			"uri":           "mongodb://127.0.0.1:9/?serverSelectionTimeoutMS=200",
			"database":      "orders",
			"verifyOnStart": off,
		},
	}
	if err := c.Start(context.Background(), cfg); err != nil {
		t.Fatalf("Start with verifyOnStart off: %v", err)
	}
	t.Cleanup(func() { _ = c.Stop(context.Background()) })

	// The connector is live enough to hand out handles; the block that uses one
	// is what will discover the server is not there.
	if _, err := c.Collection("", "orders"); err != nil {
		t.Errorf("Collection: %v", err)
	}
}

// The default is still to fail fast: a typo in a URI should not wait for
// traffic to surface.
func TestStartVerifiesByDefault(t *testing.T) {
	c := &Connector{}
	cfg := types.ConnectorConfig{
		Name: "orders-db",
		Type: connectorType,
		Settings: types.Settings{
			"uri": "mongodb://127.0.0.1:9/?serverSelectionTimeoutMS=200",
		},
	}
	if err := c.Start(context.Background(), cfg); err == nil {
		_ = c.Stop(context.Background())
		t.Error("expected an unreachable server to fail at startup")
	}
}
