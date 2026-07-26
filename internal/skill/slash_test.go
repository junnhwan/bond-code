package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandForUserAllowsDisableModelInvocation(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "handoff")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw := "---\nname: handoff\ndescription: hand off\ndisable-model-invocation: true\n---\nDo handoff.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	loader := NewLoaderFromRoot(root)
	if _, _, err := loader.Expand("handoff", ""); err == nil {
		t.Fatal("model Expand must reject user-only skills")
	}
	content, s, err := loader.ExpandForUser("handoff", "next session")
	if err != nil {
		t.Fatal(err)
	}
	if s == nil || s.Name != "handoff" {
		t.Fatalf("skill = %#v", s)
	}
	if !strings.Contains(content, "Do handoff.") {
		t.Fatalf("content = %q", content)
	}
}

func TestExpandForUserRejectsModelOnly(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "secret")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw := "---\nname: secret\ndescription: hush\nuser-invocable: false\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	loader := NewLoaderFromRoot(root)
	_, s, err := loader.ExpandForUser("secret", "")
	if err == nil {
		t.Fatal("expected model-only reject")
	}
	if s == nil || s.SlashInvocable() {
		t.Fatalf("expected non-slash skill, got %#v", s)
	}
}

func TestFormatIndexSummarizesModelVsUserOnly(t *testing.T) {
	skills := []Skill{
		{Name: "a", Description: "model ok", UserInvocable: true},
		{Name: "b", Description: "user only", DisableModelInvocation: true, UserInvocable: true},
	}
	out := FormatIndex(skills)
	for _, want := range []string{
		"2 skills",
		"1 model-invocable",
		"1 user-only",
		"type /<name>",
		"[user-only]",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("FormatIndex missing %q:\n%s", want, out)
		}
	}
}

func TestUserInvocableDefaultsTrueWithoutFrontmatter(t *testing.T) {
	fm, _ := parseFrontmatter("# just a body\n")
	if !fm.UserInvocable {
		t.Fatal("skills without frontmatter must default user-invocable true")
	}
}
