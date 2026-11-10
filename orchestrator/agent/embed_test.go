package agent

import (
	"os"
	"path/filepath"
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
	definition := rawConfig(t)
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

// The dolphin suites live beside the app. Shipping one would put a test file in
// every install's integration.
func TestNoTestSuiteIsBundled(t *testing.T) {
	suites, err := filepath.Glob("*_test.yaml")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(suites) == 0 {
		t.Fatal("no suites found beside the app, so this test proves nothing")
	}
	for _, suite := range suites {
		if _, err := bundleFS.ReadFile(suite); err == nil {
			t.Errorf("%s is embedded; it should not be", suite)
		}
	}
}

// Every config file beside the app has to ship. The embed list is written out by
// hand to keep the suites out, so a flow file added to the folder runs under
// `octo run --config orchestrator/agent` and in every dolphin suite, and is then
// silently missing from every install.
func TestEveryConfigFileIsBundled(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	bundled := map[string]bool{}
	for _, name := range configFiles {
		bundled[name] = true
	}
	found := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, "_test.yaml") {
			continue
		}
		found++
		if !bundled[name] {
			t.Errorf("%s is a config file of the app but not in configFiles", name)
		}
	}
	if found != len(configFiles) {
		t.Errorf("configFiles names %d files, the folder holds %d", len(configFiles), found)
	}
	if _, err := Files(); err != nil {
		t.Errorf("Files: %v — a name in configFiles is not embedded", err)
	}
}

// rawConfig is every config file as written, concatenated. The line-scanning tests
// read it rather than the merged definition, which is re-encoded YAML.
func rawConfig(t *testing.T) string {
	t.Helper()
	files, err := Files()
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	var b strings.Builder
	for _, f := range files {
		b.WriteString(f.Content)
		b.WriteString("\n")
	}
	return b.String()
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

// Every skill a block loads has to name an alias the templates block declares.
//
// The test above proves each `- resource: skills/x.md` is bundled; it says
// nothing about the `as:` beneath it, or about the `resource: x` that a skill
// slot uses to reach it. Those are two different names and only the second is
// what an agent asks for.
//
// It exists because they came apart. A new template was inserted between an
// existing `- resource:` and its `as:`, which left one file with no alias and
// gave its name to the other — so a skill slot named an alias that did not exist
// while every load of the stolen name returned the wrong text. Nothing failed:
// the file was still bundled, the config still parsed, the agent still ran and
// still loaded "a" skill. It was found by review, which is not a repeatable way
// to find it.
func TestEverySkillSlotNamesADeclaredAlias(t *testing.T) {
	definition := rawConfig(t)

	// The aliases the templates block declares: an `as:` is only one when it
	// follows a `- resource:` line, which is exactly the adjacency that broke.
	declared := map[string]bool{}
	previousWasResource := false
	for _, line := range strings.Split(definition, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "- resource: skills/"):
			previousWasResource = true
		case previousWasResource && strings.HasPrefix(trimmed, "as: "):
			declared[strings.TrimSpace(strings.TrimPrefix(trimmed, "as: "))] = true
			previousWasResource = false
		default:
			previousWasResource = false
		}
	}
	if len(declared) == 0 {
		t.Fatal("no template aliases found, so this test proves nothing")
	}

	// Every bundled skill file must have got one. A file with no alias is
	// unreachable, which is the other half of what went wrong.
	skills, err := Skills()
	if err != nil {
		t.Fatalf("Skills: %v", err)
	}
	if len(declared) != len(skills) {
		t.Errorf("%d skills bundled but %d aliases declared — one of them has no `as:`",
			len(skills), len(declared))
	}

	// And every slot that asks for one must name an alias that exists.
	for _, line := range strings.Split(definition, "\n") {
		trimmed := strings.TrimSpace(line)
		const marker = "resource: "
		if !strings.HasPrefix(trimmed, marker) || strings.HasPrefix(trimmed, "- resource:") {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(trimmed, marker))
		if strings.Contains(name, "/") || strings.Contains(name, ".") {
			continue // a path, handled by the bundling test above
		}
		if !declared[name] {
			t.Errorf("a skill slot names resource %q, which no template declares", name)
		}
	}
}
