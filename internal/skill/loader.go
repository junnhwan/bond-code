package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Loader discovers skills from BondCode-owned directories only:
//   - user:    ~/.bondcode/skills/<name>/SKILL.md  (or $BONDCODE_HOME/skills)
//   - project: <projectRoot>/.bondcode/skills/<name>/SKILL.md
//   - root:    optional skills.root (config) extra dir
//
// Only the directory form name/SKILL.md is supported (Claude Code skills layout).
// Project skills override user skills with the same name; config root overrides both.
type Loader struct {
	userDir       string
	projectDir    string
	extraDirs     []string
	maxChars      int
	listingBudget int
}

// Paths configures skill discovery roots.
type Paths struct {
	UserDir       string
	ProjectDir    string
	ExtraDirs     []string
	MaxChars      int
	ListingBudget int
}

// NewLoader builds a multi-source loader. Empty dirs are skipped.
func NewLoader(paths Paths) *Loader {
	extra := make([]string, 0, len(paths.ExtraDirs))
	for _, d := range paths.ExtraDirs {
		d = strings.TrimSpace(d)
		if d != "" {
			extra = append(extra, d)
		}
	}
	maxChars := paths.MaxChars
	if maxChars <= 0 {
		maxChars = 12000
	}
	budget := paths.ListingBudget
	if budget <= 0 {
		budget = DefaultListingBudget
	}
	return &Loader{
		userDir:       strings.TrimSpace(paths.UserDir),
		projectDir:    strings.TrimSpace(paths.ProjectDir),
		extraDirs:     extra,
		maxChars:      maxChars,
		listingBudget: budget,
	}
}

// NewLoaderFromRoot is a convenience for tests: a single root as SourceRoot.
func NewLoaderFromRoot(root string) *Loader {
	return NewLoader(Paths{ExtraDirs: []string{root}})
}

func (l *Loader) MaxChars() int {
	if l == nil || l.maxChars <= 0 {
		return 12000
	}
	return l.maxChars
}

func (l *Loader) ListingBudget() int {
	if l == nil || l.listingBudget <= 0 {
		return DefaultListingBudget
	}
	return l.listingBudget
}

// Roots returns configured discovery directories (for status / CLI).
func (l *Loader) Roots() []string {
	if l == nil {
		return nil
	}
	var out []string
	if l.userDir != "" {
		out = append(out, l.userDir)
	}
	if l.projectDir != "" {
		out = append(out, l.projectDir)
	}
	out = append(out, l.extraDirs...)
	return out
}

// Root returns the primary display root (project, else user, else first extra).
func (l *Loader) Root() string {
	if l == nil {
		return ""
	}
	if l.projectDir != "" {
		return l.projectDir
	}
	if l.userDir != "" {
		return l.userDir
	}
	if len(l.extraDirs) > 0 {
		return l.extraDirs[0]
	}
	return ""
}

// Index lists model-facing skill metadata (no body). Sorted by name.
func (l *Loader) Index() ([]Skill, error) {
	all, err := l.IndexAll()
	if err != nil {
		return nil, err
	}
	out := make([]Skill, 0, len(all))
	for _, s := range all {
		if s.ModelInvocable() {
			out = append(out, s)
		}
	}
	return out, nil
}

// IndexAll lists every discovered skill including non-model-invocable ones.
func (l *Loader) IndexAll() ([]Skill, error) {
	if l == nil {
		return nil, nil
	}
	byName := map[string]Skill{}
	loadDir := func(dir string, source Source) error {
		if dir == "" {
			return nil
		}
		skills, err := loadSkillsFromDir(dir, source)
		if err != nil {
			return err
		}
		for _, s := range skills {
			byName[s.Name] = s
		}
		return nil
	}
	if err := loadDir(l.userDir, SourceUser); err != nil {
		return nil, err
	}
	if err := loadDir(l.projectDir, SourceProject); err != nil {
		return nil, err
	}
	for _, dir := range l.extraDirs {
		if err := loadDir(dir, SourceRoot); err != nil {
			return nil, err
		}
	}
	out := make([]Skill, 0, len(byName))
	for _, s := range byName {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Load returns the full skill (with Body) by name.
func (l *Loader) Load(name string) (*Skill, error) {
	name = strings.TrimSpace(name)
	name = strings.TrimPrefix(name, "/")
	if name == "" {
		return nil, fmt.Errorf("skill name is required")
	}
	index, err := l.IndexAll()
	if err != nil {
		return nil, err
	}
	for _, item := range index {
		if item.Name != name {
			continue
		}
		full, err := readSkillFile(item.Path, item.Source)
		if err != nil {
			return nil, err
		}
		full.Name = item.Name
		return &full, nil
	}
	return nil, fmt.Errorf("skill %q not found", name)
}

// Expand loads a skill and builds the inline prompt body (base dir + args).
// Used by the model-facing skill tool; rejects disable-model-invocation.
func (l *Loader) Expand(name, args string) (string, *Skill, error) {
	s, err := l.Load(name)
	if err != nil {
		return "", nil, err
	}
	if s.DisableModelInvocation {
		return "", s, fmt.Errorf("skill %q cannot be invoked by the model (disable-model-invocation)", name)
	}
	return l.expandLoaded(s, args)
}

// ExpandForUser loads a skill for a user slash command (/name).
// User-only skills (disable-model-invocation) are allowed; model-only
// (user-invocable: false) are rejected — matching Claude Code.
func (l *Loader) ExpandForUser(name, args string) (string, *Skill, error) {
	s, err := l.Load(name)
	if err != nil {
		return "", nil, err
	}
	if !s.SlashInvocable() {
		return "", s, fmt.Errorf("skill %q is model-only (user-invocable: false)", name)
	}
	return l.expandLoaded(s, args)
}

func (l *Loader) expandLoaded(s *Skill, args string) (string, *Skill, error) {
	content := ExpandContent(*s, args)
	if max := l.MaxChars(); max > 0 && len(content) > max {
		content = content[:max] + "\n[skill content truncated]"
	}
	return content, s, nil
}

func loadSkillsFromDir(basePath string, source Source) ([]Skill, error) {
	entries, err := os.ReadDir(basePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Skill
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillDir := filepath.Join(basePath, entry.Name())
		skillPath := filepath.Join(skillDir, "SKILL.md")
		info, err := os.Stat(skillPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		raw, err := os.ReadFile(skillPath)
		if err != nil {
			return nil, err
		}
		fm, _ := parseFrontmatter(string(raw))
		name := entry.Name()
		s := Skill{
			Name:                   name,
			Description:            fm.Description,
			WhenToUse:              fm.WhenToUse,
			ArgumentHint:           fm.ArgumentHint,
			AllowedTools:           append([]string(nil), fm.AllowedTools...),
			DisableModelInvocation: fm.DisableModelInvocation,
			UserInvocable:          fm.UserInvocable,
			Source:                 source,
			Dir:                    skillDir,
			Path:                   skillPath,
			ContentLength:          int(info.Size()),
		}
		if s.Description == "" {
			s.Description = "Skill"
		}
		out = append(out, s)
	}
	return out, nil
}

func readSkillFile(path string, source Source) (Skill, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, err
	}
	fm, body := parseFrontmatter(string(raw))
	dir := filepath.Dir(path)
	name := filepath.Base(dir)
	return Skill{
		Name:                   name,
		Description:            fm.Description,
		WhenToUse:              fm.WhenToUse,
		ArgumentHint:           fm.ArgumentHint,
		AllowedTools:           append([]string(nil), fm.AllowedTools...),
		DisableModelInvocation: fm.DisableModelInvocation,
		UserInvocable:          fm.UserInvocable,
		Source:                 source,
		Dir:                    dir,
		Path:                   path,
		Body:                   body,
		ContentLength:          len(raw),
	}, nil
}
