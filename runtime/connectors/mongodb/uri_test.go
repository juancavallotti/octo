package mongodb

import "testing"

func TestWithPassword(t *testing.T) {
	tests := []struct {
		name     string
		uri      string
		password string
		want     string
	}{
		{
			// The path every URI-with-inline-credentials config takes: nothing to
			// merge, so nothing is rewritten.
			name: "unset leaves the uri alone",
			uri:  "mongodb://app:inline@db.internal:27017/orders",
			want: "mongodb://app:inline@db.internal:27017/orders",
		},
		{
			name:     "standard scheme",
			uri:      "mongodb://app@db.internal:27017/orders",
			password: "s3cret",
			want:     "mongodb://app:s3cret@db.internal:27017/orders",
		},
		{
			// The SRV form is what Atlas hands out, so it has to work.
			name:     "srv scheme",
			uri:      "mongodb+srv://app@cluster.example.net/?retryWrites=true",
			password: "s3cret",
			want:     "mongodb+srv://app:s3cret@cluster.example.net/?retryWrites=true",
		},
		{
			// net/url escapes it, so a password with URI punctuation needs no
			// hand-encoding in config.
			name:     "escapes punctuation",
			uri:      "mongodb://app@db.internal:27017/orders",
			password: "p@ss/w:rd?",
			want:     "mongodb://app:p%40ss%2Fw%3Ard%3F@db.internal:27017/orders",
		},
		{
			name:     "overrides an embedded password",
			uri:      "mongodb://app:stale@db.internal:27017/orders",
			password: "fresh",
			want:     "mongodb://app:fresh@db.internal:27017/orders",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := withPassword(tt.uri, tt.password, "orders-db")
			if err != nil {
				t.Fatalf("withPassword: %v", err)
			}
			if got != tt.want {
				t.Errorf("withPassword() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWithPasswordErrors(t *testing.T) {
	tests := []struct {
		name string
		uri  string
	}{
		// mongodb://:secret@host authenticates as nobody, so the config is wrong
		// in a way that would otherwise start and look healthy.
		{name: "no username", uri: "mongodb://db.internal:27017/orders"},
		{name: "wrong scheme", uri: "postgres://app@db.internal:5432/orders"},
		// Neither is one of the two schemes, and both would slip past a prefix
		// test — the error should name the config, not come back from the driver.
		{name: "scheme with a suffix", uri: "mongodbx://app@db.internal:27017/orders"},
		{name: "unknown scheme option", uri: "mongodb+foo://app@db.internal:27017/orders"},
		{name: "unparseable", uri: "mongodb://app@ db.internal"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := withPassword(tt.uri, "s3cret", "orders-db"); err == nil {
				t.Errorf("expected an error for %q", tt.uri)
			}
		})
	}
}
