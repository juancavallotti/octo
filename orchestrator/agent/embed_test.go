package agent

import (
	"strings"
	"testing"

	"github.com/juancavallotti/octo/orchestrator/internal/version"
)

func TestDefinitionIsTheApp(t *testing.T) {
	definition, err := Definition()
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	for _, want := range []string{"service:", "name: dr-octo", "ai-agent", "${LLM_CONNECTOR_TYPE}"} {
		if !strings.Contains(definition, want) {
			t.Errorf("the embedded definition does not contain %q", want)
		}
	}
}

// Every skill the definition declares has to be in the bundle, or the agent
// installs and then fails to build in the cluster — the one failure this package
// can prevent and the pod cannot.
func TestEverySkillTheDefinitionDeclaresIsBundled(t *testing.T) {
	definition, err := Definition()
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	skills, err := Skills()
	if err != nil {
		t.Fatalf("Skills: %v", err)
	}

	for _, line := range strings.Split(definition, "\n") {
		line = strings.TrimSpace(line)
		const marker = "- resource: "
		if !strings.HasPrefix(line, marker) {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(line, marker))
		if _, ok := skills[name]; !ok {
			t.Errorf("the definition declares resource %q, which is not in the bundle", name)
		}
	}
	if len(skills) == 0 {
		t.Fatal("no skills bundled")
	}
}

// The dolphin suite lives beside the app. Shipping it would put a test file in
// every install's integration.
func TestTheTestSuiteIsNotBundled(t *testing.T) {
	if _, err := bundleFS.ReadFile("config_test.yaml"); err == nil {
		t.Error("config_test.yaml is embedded; it should not be")
	}
}

func TestDigestIsStable(t *testing.T) {
	first, err := Digest()
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	second, err := Digest()
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	if first != second {
		t.Errorf("digest is not stable: %s then %s", first, second)
	}
	if len(first) != 64 {
		t.Errorf("digest = %q, want a hex sha256", first)
	}
}

func TestTagNamesTheRelease(t *testing.T) {
	if tag := Tag(); tag != "v"+version.Version {
		t.Errorf("Tag = %q, want the octo release %q", tag, "v"+version.Version)
	}
}

func TestBuildTagStandsBesideTheRelease(t *testing.T) {
	tag := BuildTag("abcdef0123456789abcdef")
	if want := Tag() + "-abcdef012345"; tag != want {
		t.Errorf("BuildTag = %q, want %q", tag, want)
	}
}

// Version tags allow only [A-Za-z0-9._-] and at most 64 characters, so neither
// tag the installer mints can be rejected by the snapshot layer.
func TestTagsAreAcceptableVersionTags(t *testing.T) {
	for _, tag := range []string{Tag(), BuildTag("abcdef0123456789abcdef")} {
		for _, r := range tag {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			default:
				t.Errorf("tag %q contains %q, which a version tag may not", tag, r)
			}
		}
		if len(tag) > 64 {
			t.Errorf("tag %q is longer than a version tag may be", tag)
		}
	}
}

func TestIsSkillRecognisesTheBundlesOwnResources(t *testing.T) {
	skills, err := Skills()
	if err != nil {
		t.Fatalf("Skills: %v", err)
	}
	for name := range skills {
		if !IsSkill(name) {
			t.Errorf("IsSkill(%q) = false, want true for a bundled skill", name)
		}
	}
	if IsSkill("templates/user-added.tmpl") {
		t.Error("IsSkill matched a resource the bundle does not own")
	}
}
