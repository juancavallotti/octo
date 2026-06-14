package types

import "testing"

func TestSettingsDecode(t *testing.T) {
	type config struct {
		Schedule string `json:"schedule"`
		Count    int    `json:"count"`
	}

	settings := Settings{"schedule": "@every 5s", "count": 3}

	var got config
	if err := settings.Decode(&got); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Schedule != "@every 5s" || got.Count != 3 {
		t.Errorf("decoded = %+v, want {@every 5s 3}", got)
	}
}

func TestSettingsDecodeWrongType(t *testing.T) {
	type config struct {
		Count int `json:"count"`
	}
	if err := (Settings{"count": "not-a-number"}).Decode(&config{}); err == nil {
		t.Fatal("expected a decode error for a mistyped setting")
	}
}

func TestSettingsAccessors(t *testing.T) {
	settings := Settings{"name": "orders", "workers": 4, "enabled": true}

	if got, ok := settings.String("name"); !ok || got != "orders" {
		t.Errorf("String = %q, %v", got, ok)
	}
	if got, ok := settings.Int("workers"); !ok || got != 4 {
		t.Errorf("Int = %d, %v", got, ok)
	}
	if got, ok := settings.Bool("enabled"); !ok || !got {
		t.Errorf("Bool = %v, %v", got, ok)
	}
	if _, ok := settings.String("missing"); ok {
		t.Error("String on a missing key should report ok=false")
	}
}
