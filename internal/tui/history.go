package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/junnhwan/bond-code/internal/fsx"
)

const maxHistoryPromptBytes = 8000
const maxPromptHistoryEntries = 200

var (
	historySecretPattern = regexp.MustCompile(`(?i)(api[_-]?key|authorization|bearer|password|passwd|secret|token)\s*[:=]`)
	historyBase64Pattern = regexp.MustCompile(`(?i)(data:image/|@image:data:|[A-Za-z0-9+/]{240,}={0,2})`)
)

func shouldSkipPromptHistory(prompt string) bool {
	trimmed := strings.TrimSpace(prompt)
	if trimmed == "" || len(trimmed) > maxHistoryPromptBytes {
		return true
	}
	if historySecretPattern.MatchString(trimmed) || historyBase64Pattern.MatchString(trimmed) {
		return true
	}
	lower := strings.ToLower(trimmed)
	return strings.Contains(lower, "bearer ") ||
		strings.Contains(lower, "authorization:") ||
		strings.Contains(lower, "-----begin ") ||
		strings.Contains(lower, "private key")
}

func loadPromptHistory(path string) []string {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var prompts []string
	if err := json.Unmarshal(data, &prompts); err != nil {
		return nil
	}
	return normalizePromptHistory(prompts)
}

func savePromptHistory(path string, prompts []string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	prompts = normalizePromptHistory(prompts)
	if len(prompts) == 0 {
		return nil
	}
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	data, err := json.MarshalIndent(prompts, "", "  ")
	if err != nil {
		return err
	}
	return fsx.WriteFileAtomic(path, append(data, '\n'), 0o600)
}

func normalizePromptHistory(prompts []string) []string {
	out := make([]string, 0, len(prompts))
	for _, prompt := range prompts {
		prompt = strings.TrimSpace(prompt)
		if shouldSkipPromptHistory(prompt) {
			continue
		}
		if len(out) > 0 && out[len(out)-1] == prompt {
			continue
		}
		out = append(out, prompt)
	}
	if len(out) > maxPromptHistoryEntries {
		out = out[len(out)-maxPromptHistoryEntries:]
	}
	return out
}

func samePromptHistory(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
