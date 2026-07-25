package custom

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/junnhwan/bond-code/internal/command"
)

func TestSplitFrontmatter(t *testing.T) {
	cases := []struct {
		name     string
		content  string
		wantName string
		wantDesc string
		wantBody string
	}{
		{
			name:     "with frontmatter",
			content:  "---\nname: greet\ndescription: Say hi\n---\nHello $ARGUMENTS!",
			wantName: "greet",
			wantDesc: "Say hi",
			wantBody: "Hello $ARGUMENTS!",
		},
		{
			name:     "no frontmatter uses whole body",
			content:  "Just a body with $1",
			wantBody: "Just a body with $1",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			name, desc, body := splitFrontmatter(tc.content)
			if name != tc.wantName || desc != tc.wantDesc || body != tc.wantBody {
				t.Errorf("got name=%q desc=%q body=%q, want %q/%q/%q", name, desc, body, tc.wantName, tc.wantDesc, tc.wantBody)
			}
		})
	}
}

func TestSubstituteArgs(t *testing.T) {
	out := SubstituteArgs("Review $1 and $2 (all: $ARGUMENTS)", []string{"a.go", "b.go"})
	want := "Review a.go and b.go (all: a.go b.go)"
	if out != want {
		t.Errorf("SubstituteArgs = %q, want %q", out, want)
	}
}

func TestLoadRegistersProjectLevelCommand(t *testing.T) {
	dir := t.TempDir()
	prevWD, _ := os.Getwd()
	defer os.Chdir(prevWD)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	cmdDir := filepath.Join(dir, ".bondcode", "commands")
	if err := os.MkdirAll(cmdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cmdDir, "greet.md"), []byte("---\nname: greet\ndescription: hi\n---\nHello $ARGUMENTS"), 0o644); err != nil {
		t.Fatal(err)
	}

	registry := command.NewRegistry()
	if err := Load(registry); err != nil {
		t.Fatal(err)
	}
	cmd, ok := registry.Get("greet")
	if !ok {
		t.Fatal("expected the greet custom command to be registered")
	}
	if cmd.PromptTemplate == "" {
		t.Fatal("custom command must carry a prompt template, not a Run handler")
	}
	if cmd.Description != "hi" {
		t.Errorf("description = %q, want %q", cmd.Description, "hi")
	}
}
