package runtimeversion

import "testing"

func TestImageTag(t *testing.T) {
	cases := []struct {
		image string
		want  string
	}{
		{"octo-runtime:v0.8.8", "v0.8.8"},
		{"ghcr.io/juancavallotti/octo-runtime:0.8.8", "0.8.8"},
		{"registry:5000/octo/runtime:dev", "dev"},
		{"registry:5000/octo/runtime", ""},
		{"octo-runtime", ""},
		{"", ""},
		{"octo-runtime@sha256:abcd", ""},
		{"ghcr.io/octo/runtime:0.8.8@sha256:abcd", ""},
		{"sha256:ed08b693c518be5d6995e2e2edd6bb8ab42972a34a2c375cd7e7a85aecf8e210", ""},
		{"sha512:ed08b693", ""},
	}
	for _, tc := range cases {
		if got := ImageTag(tc.image); got != tc.want {
			t.Errorf("ImageTag(%q) = %q, want %q", tc.image, got, tc.want)
		}
	}
}

func TestCurrent(t *testing.T) {
	cases := []struct {
		version, image, want string
	}{
		{"0.9.0", "reg/octo-runtime:latest", "0.9.0"},
		{"0.9.0", "reg/octo-runtime@sha256:abcd", "0.9.0"},
		{"", "reg/octo-runtime:0.8.8", "0.8.8"},
		{"", "reg/octo-runtime@sha256:abcd", ""},
	}
	for _, tc := range cases {
		if got := Current(tc.version, tc.image); got != tc.want {
			t.Errorf("Current(%q, %q) = %q, want %q", tc.version, tc.image, got, tc.want)
		}
	}
}

func TestBehind(t *testing.T) {
	cases := []struct {
		deployed, current string
		want              bool
	}{
		{"0.8.5", "0.8.8", true},
		{"0.9.0", "0.10.0", true},
		{"v1.2.3", "1.3.0", true},
		{"0.8", "0.8.1", true},
		{"0.8.8", "0.8.8", false},
		{"0.8", "0.8.0", false},
		// Ahead is not behind.
		{"0.8.9", "0.8.8", false},
		{"1.0.0", "0.9.9", false},
		// Incomparable tags and unknowns never claim an upgrade.
		{"dev", "0.8.8", false},
		{"0.8.5", "main", false},
		{"0.9.0-rc1", "0.9.0", false},
		{"", "0.8.8", false},
		{"0.8.5", "", false},
	}
	for _, tc := range cases {
		if got := Behind(tc.deployed, tc.current); got != tc.want {
			t.Errorf("Behind(%q, %q) = %v, want %v", tc.deployed, tc.current, got, tc.want)
		}
	}
}
