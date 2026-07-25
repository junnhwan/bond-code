package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoaderIndexesDirectorySkillsOnly(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "debugging", "debug systematically", "when_to_use: after a test fails", "# Body\nReproduce first.")
	if err := os.WriteFile(filepath.Join(root, "orphan.md"), []byte("# no"), 0o600); err != nil {
		t.Fatal(err)
	}

	loader := NewLoaderFromRoot(root)
	index, err := loader.Index()
	if err != nil {
		t.Fatal(err)
	}
	if len(index) != 1 || index[0].Name != "debugging" {
		t.Fatalf("unexpected index %#v", index)
	}
	if index[0].WhenToUse != "after a test fails" {
		t.Fatalf("when_to_use = %q", index[0].WhenToUse)
	}
	if index[0].Body != "" {
		t.Fatal("index entries must not carry body")
	}

	skill, err := loader.Load("debugging")
	if err != nil {
		t.Fatal(err)
	}
	if skill.Body == "" || !strings.Contains(skill.Body, "Reproduce first") {
		t.Fatalf("unexpected skill body %#v", skill.Body)
	}
	if skill.Description != "debug systematically" {
		t.Fatalf("description = %q", skill.Description)
	}
}

func TestLoaderProjectOverridesUserByName(t *testing.T) {
	user := t.TempDir()
	project := t.TempDir()
	writeSkill(t, user, "commit", "user commit skill", "", "user body")
	writeSkill(t, project, "commit", "project commit skill", "", "project body")

	loader := NewLoader(Paths{UserDir: user, ProjectDir: project})
	index, err := loader.Index()
	if err != nil {
		t.Fatal(err)
	}
	if len(index) != 1 || index[0].Description != "project commit skill" {
		t.Fatalf("expected project override, got %#v", index)
	}
	s, err := loader.Load("commit")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s.Body, "project body") {
		t.Fatalf("body = %q", s.Body)
	}
	if s.Source != SourceProject {
		t.Fatalf("source = %s", s.Source)
	}
}

func TestLoaderHidesDisableModelInvocationFromIndex(t *testing.T) {
	root := t.TempDir()
	writeSkillRaw(t, root, "secret", "---\ndescription: hush\ndisable-model-invocation: true\n---\nbody\n")
	loader := NewLoaderFromRoot(root)
	index, err := loader.Index()
	if err != nil {
		t.Fatal(err)
	}
	if len(index) != 0 {
		t.Fatalf("model index should omit disable-model-invocation, got %#v", index)
	}
	all, err := loader.IndexAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || !all[0].DisableModelInvocation {
		t.Fatalf("IndexAll = %#v", all)
	}
	_, _, err = loader.Expand("secret", "")
	if err == nil {
		t.Fatal("expected expand to reject disable-model-invocation")
	}
}

func TestLoaderReturnsStableErrorForMissingSkill(t *testing.T) {
	loader := NewLoaderFromRoot(t.TempDir())
	_, err := loader.Load("missing")
	if err == nil {
		t.Fatal("expected missing skill error")
	}
}

func writeSkill(t *testing.T, root, name, description, extraFront, body string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("name: " + name + "\n")
	b.WriteString("description: " + description + "\n")
	if extraFront != "" {
		b.WriteString(extraFront)
		if !strings.HasSuffix(extraFront, "\n") {
			b.WriteByte('\n')
		}
	}
	b.WriteString("---\n")
	b.WriteString(body)
	if !strings.HasSuffix(body, "\n") {
		b.WriteByte('\n')
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeSkillRaw(t *testing.T, root, name, content string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
