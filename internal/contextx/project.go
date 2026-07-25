package contextx

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

type ProjectSummary struct {
	Root       string
	Language   string
	GoModule   string
	HasGit     bool
	DirtyFiles []string
	KeyFiles   []string
}

func InspectProject(root string) (*ProjectSummary, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	summary := &ProjectSummary{Root: absRoot}

	if exists(filepath.Join(absRoot, ".git")) {
		summary.HasGit = true
	}
	goModPath := filepath.Join(absRoot, "go.mod")
	if exists(goModPath) {
		summary.Language = "go"
		summary.KeyFiles = append(summary.KeyFiles, "go.mod")
		module, err := parseGoModule(goModPath)
		if err != nil {
			return nil, err
		}
		summary.GoModule = module
	}

	for _, dir := range []string{"cmd", "internal", "pkg", "api"} {
		if exists(filepath.Join(absRoot, dir)) {
			summary.KeyFiles = append(summary.KeyFiles, dir+"/")
		}
	}
	return summary, nil
}

func parseGoModule(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module ")), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
