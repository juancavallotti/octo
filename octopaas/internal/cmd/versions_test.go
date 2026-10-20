package cmd

import (
	"testing"
	"time"

	"github.com/juancavallotti/octo/octopaas/internal/client"
)

// The tag a deploy freezes the draft under follows the integration's own
// scheme when it has one, and never reuses a tag that is taken.
func TestNextTag(t *testing.T) {

	now := time.Date(2026, 10, 19, 21, 1, 5, 0, time.UTC)
	cases := []struct {
		name string
		tags []string
		want string
	}{
		{name: "no versions yet", want: "v0.1.0"},
		{name: "the highest semver tag, patch bumped", tags: []string{"v1.2.0", "v1.10.3", "v1.9.9"}, want: "v1.10.4"},
		{name: "without the v when the integration writes none", tags: []string{"2.0.1"}, want: "2.0.2"},
		{name: "past a bump that is already taken", tags: []string{"v1.0.0", "v1.0.1"}, want: "v1.0.2"},
		{name: "a dated draft when no tag is semver", tags: []string{"spring-release"}, want: "draft-20261019-210105"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {

			var versions []client.Version
			for _, tag := range c.tags {
				versions = append(versions, client.Version{Tag: tag})
			}
			if got := nextTag(versions, now); got != c.want {
				t.Errorf("nextTag(%v) = %q, want %q", c.tags, got, c.want)
			}
		})
	}
}

// A deploy without --version reuses the newest version only when it is still
// the integration: the same YAML, and no edit after it was frozen.
func TestIsCurrent(t *testing.T) {

	frozen := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	v := client.Version{Definition: "flows: []", CreatedAt: frozen}

	if !isCurrent(client.Integration{Definition: "flows: []", LastUpdated: frozen.Add(-time.Minute)}, v) {
		t.Error("an integration untouched since its newest version should deploy that version")
	}
	if isCurrent(client.Integration{Definition: "flows: [x]", LastUpdated: frozen.Add(-time.Minute)}, v) {
		t.Error("a changed definition should be frozen again")
	}
	if isCurrent(client.Integration{Definition: "flows: []", LastUpdated: frozen.Add(time.Minute)}, v) {
		t.Error("an edit after the freeze (a resource, say) should be frozen again")
	}
}
