package builtin

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/junnhwan/bond-code/internal/tool"
)

type SearchTextInput struct {
	Path    string `json:"path"`
	Pattern string `json:"pattern"`
}

type searchTextTool struct{}

func NewSearchTextTool() tool.Tool { return searchTextTool{} }

func (searchTextTool) Name() string { return tool.SearchText }
func (searchTextTool) Description() string {
	return "Search regular text files under a directory when locating symbols or phrases. Does not mutate files. Output is matching path:line snippets."
}
func (searchTextTool) Schema() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":    map[string]any{"type": "string"},
			"pattern": map[string]any{"type": "string"},
		},
		"required": []string{"path", "pattern"},
	}
}
func (searchTextTool) Risk(json.RawMessage) tool.RiskLevel { return tool.RiskLow }
func (t searchTextTool) Execute(ctx context.Context, raw json.RawMessage) (*tool.Result, error) {
	var input SearchTextInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, err
	}
	if input.Path == "" {
		return nil, fmt.Errorf("path is required")
	}
	if input.Pattern == "" {
		return nil, fmt.Errorf("pattern is required")
	}

	var matches []string
	err := filepath.WalkDir(input.Path, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if shouldSkipDir(entry.Name()) && path != input.Path {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		fileMatches, err := searchFile(path, input.Pattern)
		if err != nil {
			return nil
		}
		matches = append(matches, fileMatches...)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return tool.Success(t.Name(), fmt.Sprintf("found %d matches", len(matches)), strings.Join(matches, "\n")), nil
}

func shouldSkipDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", "target", "dist":
		return true
	default:
		return false
	}
}

func searchFile(path string, pattern string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var matches []string
	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		if strings.Contains(line, pattern) {
			matches = append(matches, fmt.Sprintf("%s:%d: %s", path, lineNo, line))
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return matches, nil
}
