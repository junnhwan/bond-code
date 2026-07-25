package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestConfigExampleCommandPrintsExampleConfigPath(t *testing.T) {
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"config", "example"})

	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "configs/config.example.yaml") {
		t.Fatalf("unexpected config example output %q", out.String())
	}
}
