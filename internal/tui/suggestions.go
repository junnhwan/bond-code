package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/junnhwan/bond-code/internal/command"
)

// Suggestion represents a single suggestion item
type Suggestion struct {
	Name        string // Command name (without /)
	Description string // Command description
	Prefix      string
	// Source labels where the command comes from: "builtin" (default) or
	// "custom" (.bondcode/commands/*.md prompt templates). Shown as a dim badge
	// in the completion list so users can tell project-defined commands apart.
	Source string
}

// SuggestionList manages the list of suggestions and selection
type SuggestionList struct {
	commandItems    []Suggestion
	items           []Suggestion
	selected        int // Currently selected index (-1 means no selection)
	visible         bool
	mode            string
	filter          string
	initialSelected int
}

// NewSuggestionList creates a new suggestion list from the canonical builtin
// surface plus discoverable custom prompt-template commands from the registry.
func NewSuggestionList(registry *command.Registry) *SuggestionList {
	descriptors := command.DiscoverableSurfaceDescriptors()
	initialSelected := firstConfiguredSurfaceIndex(registry, descriptors)
	items := make([]Suggestion, 0, len(descriptors))
	seen := make(map[string]struct{}, len(descriptors))
	for _, descriptor := range descriptors {
		items = append(items, Suggestion{
			Name:        descriptor.Name,
			Description: descriptor.Description,
			Prefix:      "/",
			Source:      "builtin",
		})
		seen[descriptor.Name] = struct{}{}
	}

	if registry != nil {
		for _, cmd := range registry.List() {
			if strings.TrimSpace(cmd.PromptTemplate) == "" {
				continue
			}
			if _, reserved := command.LookupSurfaceDescriptor(cmd.Name); reserved {
				continue
			}
			if _, duplicate := seen[cmd.Name]; duplicate {
				continue
			}
			descriptor := command.CustomSurfaceDescriptor(cmd.Name, cmd.Description)
			items = append(items, Suggestion{
				Name:        descriptor.Name,
				Description: descriptor.Description,
				Prefix:      "/",
				Source:      "custom",
			})
			seen[descriptor.Name] = struct{}{}
		}
	}

	return &SuggestionList{
		commandItems:    items,
		items:           items,
		selected:        -1,
		visible:         false,
		mode:            "command",
		initialSelected: initialSelected,
	}
}

func firstConfiguredSurfaceIndex(registry *command.Registry, descriptors []command.SurfaceDescriptor) int {
	if registry == nil {
		return 0
	}
	for i, descriptor := range descriptors {
		cmd, ok := registry.Get(descriptor.Name)
		if ok && strings.TrimSpace(cmd.PromptTemplate) == "" {
			return i
		}
	}
	return 0
}

// Show displays the suggestion list with filtered items
func (s *SuggestionList) Show(filter string) {
	s.showItems("command", filter, s.commandItems)
}

func (s *SuggestionList) ShowFiles(filter string, items []Suggestion) {
	for i := range items {
		items[i].Prefix = "@"
	}
	s.showItems("file", filter, items)
}

func (s *SuggestionList) showItems(mode string, filter string, items []Suggestion) {
	wasVisible := s.visible
	modeChanged := s.mode != mode
	filterChanged := s.filter != filter
	s.visible = true
	s.mode = mode
	s.filter = filter
	s.items = items

	// Get the current filtered list (temporarily set visible for GetVisible to work)
	visible := s.getFiltered(filter)
	if len(visible) == 0 {
		s.selected = -1
		return
	}

	// Clamp selection to valid range. Partial registries prefer their first
	// configured canonical command without changing the policy order.
	if !wasVisible {
		s.selected = 0
		if mode == "command" && filter == "" && s.initialSelected < len(visible) {
			s.selected = s.initialSelected
		}
		return
	}
	if modeChanged || filterChanged || s.selected < 0 || s.selected >= len(visible) {
		s.selected = 0
	}
}

// getFiltered returns fuzzy-matched suggestions ranked by score (internal
// helper). A substring match is required; earlier / word-boundary hits rank
// higher. With no filter, all items are returned in their original order.
func (s *SuggestionList) getFiltered(filter string) []Suggestion {
	if filter == "" {
		return s.items
	}
	type scored struct {
		item  Suggestion
		score int
	}
	matches := make([]scored, 0, len(s.items))
	for _, item := range s.items {
		sc := fuzzyScore(item.Name, filter)
		if sc < 0 {
			continue
		}
		matches = append(matches, scored{item, sc})
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		return matches[i].item.Name < matches[j].item.Name
	})
	out := make([]Suggestion, len(matches))
	for i, m := range matches {
		out[i] = m.item
	}
	return out
}

// Hide hides the suggestion list
func (s *SuggestionList) Hide() {
	s.visible = false
	s.selected = -1
	s.filter = ""
}

// IsVisible returns whether the suggestion list is currently visible
func (s *SuggestionList) IsVisible() bool {
	return s.visible
}

// GetVisible returns filtered suggestions based on the input
func (s *SuggestionList) GetVisible(filter string) []Suggestion {
	if !s.visible {
		return nil
	}
	return s.getFiltered(filter)
}

// GetSelected returns the currently selected suggestion, or empty string if none
func (s *SuggestionList) GetSelected(filter string) string {
	if !s.visible || s.selected < 0 {
		return ""
	}

	visible := s.GetVisible(filter)
	if s.selected >= len(visible) {
		return ""
	}

	return visible[s.selected].Name
}

func (s *SuggestionList) GetSelectedCompletion(filter string) string {
	selected := s.GetSelected(filter)
	if selected == "" {
		return ""
	}
	return s.CurrentPrefix() + selected + " "
}

func (s *SuggestionList) CurrentPrefix() string {
	if !s.visible {
		return "/"
	}
	if s.mode == "file" {
		return "@"
	}
	return "/"
}

// SelectNext moves selection down
func (s *SuggestionList) SelectNext(filter string) {
	if !s.visible {
		return
	}

	visible := s.GetVisible(filter)
	if len(visible) == 0 {
		s.selected = -1
		return
	}

	s.selected++
	if s.selected >= len(visible) {
		s.selected = 0 // Wrap around
	}
}

// SelectPrev moves selection up
func (s *SuggestionList) SelectPrev(filter string) {
	if !s.visible {
		return
	}

	visible := s.GetVisible(filter)
	if len(visible) == 0 {
		s.selected = -1
		return
	}

	s.selected--
	if s.selected < 0 {
		s.selected = len(visible) - 1 // Wrap around
	}
}

// GetSelectedIndex returns the currently selected index (-1 if none)
func (s *SuggestionList) GetSelectedIndex() int {
	return s.selected
}

// FileMentionSuggestions lists project files for @-completion, preferring `git
// ls-files` (respects .gitignore, fast) and falling back to a directory walk.
// Results are fuzzy-ranked against the filter and capped so completion stays
// responsive in large repos.
func FileMentionSuggestions(root string, filter string) []Suggestion {
	root = strings.TrimSpace(root)
	if root == "" {
		root = "."
	}
	filter = strings.TrimPrefix(strings.TrimSpace(filter), "@")

	files := listProjectFiles(root)

	type scored struct {
		path  string
		score int
	}
	matches := make([]scored, 0, len(files))
	for _, f := range files {
		f = strings.TrimSpace(filepath.ToSlash(f))
		if f == "" {
			continue
		}
		sc := 0
		if filter != "" {
			sc = fuzzyScore(f, filter)
			if sc < 0 {
				continue
			}
		}
		matches = append(matches, scored{f, sc})
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		return matches[i].path < matches[j].path
	})

	const limit = 50
	out := make([]Suggestion, 0, len(matches))
	for i := range matches {
		if len(out) >= limit {
			break
		}
		out = append(out, Suggestion{Name: matches[i].path, Prefix: "@"})
	}
	return out
}

// listProjectFiles returns project-relative file paths. It prefers `git
// ls-files` (respects .gitignore, very fast) and falls back to a directory walk
// that skips VCS / build caches when the project is not a git repo.
func listProjectFiles(root string) []string {
	if out, err := exec.Command("git", "-C", root, "ls-files").Output(); err == nil {
		raw := strings.Split(strings.TrimSpace(string(out)), "\n")
		files := make([]string, 0, len(raw))
		for _, f := range raw {
			if f = strings.TrimSpace(f); f != "" {
				files = append(files, f)
			}
		}
		return files
	}

	skipDirs := map[string]bool{
		".git": true, "node_modules": true, "vendor": true,
		".idea": true, ".vscode": true, "dist": true, "build": true, "target": true,
	}
	var files []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		first := rel
		if idx := strings.Index(rel, "/"); idx >= 0 {
			first = rel[:idx]
		}
		if d.IsDir() {
			if skipDirs[first] || skipDirs[rel] {
				return filepath.SkipDir
			}
			return nil
		}
		files = append(files, rel)
		return nil
	})
	return files
}

// fuzzyScore rates how well s matches query as a case-insensitive fuzzy
// substring: -1 when there is no match, otherwise higher is better (earlier
// match + word-boundary / prefix bonus). Lightweight, no third-party deps;
// shared by file-mention and slash-command completion.
func fuzzyScore(s, query string) int {
	s = strings.ToLower(s)
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return 0
	}
	idx := strings.Index(s, query)
	if idx < 0 {
		return -1
	}
	score := 100 - idx
	if idx == 0 || isFuzzyWordSep(s[idx-1]) {
		score += 15
	}
	return score
}

func isFuzzyWordSep(b byte) bool {
	switch b {
	case '/', '_', '-', '.', ' ':
		return true
	}
	return false
}
