package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/junnhwan/bond-code/internal/fsx"
)

const (
	EntrypointName     = "MEMORY.md"
	MaxEntrypointLines = 200
	MaxEntrypointBytes = 25_000
	MaxTopicFiles      = 200
)

// MemoryStore is a Claude Code-style file-based auto-memory directory.
//
// Layout: {baseDir}/memory/MEMORY.md (index) + topic *.md files with frontmatter.
type MemoryStore struct {
	dir string
	mu  sync.RWMutex

	// modelWrote marks that the main agent saved memory this turn; the
	// background extractor skips when true (CC hasMemoryWritesSince).
	modelWrote bool
}

// NewMemoryStore creates (if needed) {baseDir}/memory and returns a store.
func NewMemoryStore(baseDir string) (*MemoryStore, error) {
	dir := filepath.Join(baseDir, "memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &MemoryStore{dir: dir}, nil
}

func (s *MemoryStore) Dir() string {
	if s == nil {
		return ""
	}
	return s.dir
}

func (s *MemoryStore) EntrypointPath() string {
	return filepath.Join(s.dir, EntrypointName)
}

// MarkModelWrite records that the main agent wrote memory this turn.
func (s *MemoryStore) MarkModelWrite() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.modelWrote = true
	s.mu.Unlock()
}

// ConsumeModelWrite returns whether the main agent wrote memory since the last
// consume, and clears the flag. Used by the background extractor to skip.
func (s *MemoryStore) ConsumeModelWrite() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.modelWrote
	s.modelWrote = false
	return v
}

// GetMemoryContext returns the truncated MEMORY.md index (empty if missing).
// maxChars bounds the returned string when provided.
func (s *MemoryStore) GetMemoryContext(maxChars ...int) (string, error) {
	if s == nil {
		return "", nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	content, err := s.readIndexLocked()
	if err != nil {
		return "", err
	}
	content = truncateEntrypoint(content)
	if len(maxChars) > 0 && maxChars[0] > 0 && len(content) > maxChars[0] {
		return content[:maxChars[0]], nil
	}
	return content, nil
}

func (s *MemoryStore) readIndexLocked() (string, error) {
	data, err := os.ReadFile(s.EntrypointPath())
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func truncateEntrypoint(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	lines := strings.Split(trimmed, "\n")
	lineCount := len(lines)
	byteCount := len(trimmed)
	lineTrunc := lineCount > MaxEntrypointLines
	byteTrunc := byteCount > MaxEntrypointBytes
	if !lineTrunc && !byteTrunc {
		return trimmed
	}
	out := trimmed
	if lineTrunc {
		out = strings.Join(lines[:MaxEntrypointLines], "\n")
	}
	if len(out) > MaxEntrypointBytes {
		cut := strings.LastIndex(out[:MaxEntrypointBytes], "\n")
		if cut <= 0 {
			cut = MaxEntrypointBytes
		}
		out = out[:cut]
	}
	reason := fmt.Sprintf("%d lines / %d bytes", lineCount, byteCount)
	return out + "\n\n> WARNING: MEMORY.md is " + reason + " (limits: " +
		fmt.Sprintf("%d lines / %d bytes", MaxEntrypointLines, MaxEntrypointBytes) +
		"). Only part of it was loaded. Keep index entries to one line under ~150 chars; move detail into topic files."
}

// Scan returns topic-file headers (excludes MEMORY.md), newest first, capped.
func (s *MemoryStore) Scan() ([]MemoryHeader, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.scanLocked()
}

func (s *MemoryStore) scanLocked() ([]MemoryHeader, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var headers []MemoryHeader
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".md") || name == EntrypointName {
			continue
		}
		path := filepath.Join(s.dir, name)
		info, err := e.Info()
		if err != nil {
			continue
		}
		// Read only the head for frontmatter.
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		// Cap read cost: frontmatter is at the top; still parse full small files.
		text := string(data)
		if len(text) > 8_000 {
			text = text[:8_000]
		}
		meta, _ := parseFrontmatter(text)
		h := MemoryHeader{
			Filename:    name,
			FilePath:    path,
			MtimeMs:     info.ModTime().UnixMilli(),
			Name:        meta["name"],
			Description: meta["description"],
		}
		if t, ok := ParseType(meta["type"]); ok {
			h.Type = t
		}
		headers = append(headers, h)
	}
	sort.Slice(headers, func(i, j int) bool {
		return headers[i].MtimeMs > headers[j].MtimeMs
	})
	if len(headers) > MaxTopicFiles {
		headers = headers[:MaxTopicFiles]
	}
	return headers, nil
}

// Count returns the number of topic memory files.
func (s *MemoryStore) Count() (int, error) {
	headers, err := s.Scan()
	if err != nil {
		return 0, err
	}
	return len(headers), nil
}

// Read loads one topic file by filename (basename).
func (s *MemoryStore) Read(filename string) (*MemoryFile, error) {
	if s == nil {
		return nil, fmt.Errorf("memory store is nil")
	}
	filename = filepath.Base(strings.TrimSpace(filename))
	if filename == "" || filename == EntrypointName || filename == "." || filename == ".." {
		return nil, fmt.Errorf("invalid memory filename %q", filename)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.readLocked(filename)
}

func (s *MemoryStore) readLocked(filename string) (*MemoryFile, error) {
	path := filepath.Join(s.dir, filename)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	meta, body := parseFrontmatter(string(data))
	f := &MemoryFile{
		Filename:    filename,
		FilePath:    path,
		MtimeMs:     info.ModTime().UnixMilli(),
		Name:        meta["name"],
		Description: meta["description"],
		Body:        body,
	}
	if t, ok := ParseType(meta["type"]); ok {
		f.Type = t
	}
	return f, nil
}

// List returns full topic files, newest first.
func (s *MemoryStore) List() ([]MemoryFile, error) {
	headers, err := s.Scan()
	if err != nil {
		return nil, err
	}
	out := make([]MemoryFile, 0, len(headers))
	for _, h := range headers {
		f, err := s.Read(h.Filename)
		if err != nil {
			continue
		}
		out = append(out, *f)
	}
	return out, nil
}

// Save writes (or overwrites) a topic file and upserts its MEMORY.md index line.
func (s *MemoryStore) Save(file MemoryFile) error {
	if s == nil {
		return fmt.Errorf("memory store is nil")
	}
	file.Name = strings.TrimSpace(file.Name)
	file.Description = strings.TrimSpace(file.Description)
	file.Body = strings.TrimSpace(file.Body)
	if file.Body == "" {
		return fmt.Errorf("memory content is required")
	}
	if _, ok := ParseType(string(file.Type)); !ok {
		return fmt.Errorf("invalid memory type %q (want user|feedback|project|reference)", file.Type)
	}
	if file.Name == "" {
		file.Name = "memory"
	}
	if file.Description == "" {
		// description drives relevance selection — require something usable.
		file.Description = truncateRunes(file.Body, 120)
	}
	if file.Filename == "" {
		file.Filename = deriveFilename(file.Type, file.Name)
	}
	file.Filename = sanitizeFilename(file.Filename)
	if file.Filename == EntrypointName {
		return fmt.Errorf("cannot use MEMORY.md as a topic filename")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.dir, file.Filename)
	content := formatTopicFile(file.Name, file.Description, file.Type, file.Body)
	if err := fsx.WriteFileAtomic(path, []byte(content), 0o644); err != nil {
		return err
	}
	if err := s.upsertIndexLocked(file.Name, file.Filename, file.Description); err != nil {
		return err
	}
	return nil
}

// Delete removes a topic file and its index line.
func (s *MemoryStore) Delete(filename string) error {
	if s == nil {
		return fmt.Errorf("memory store is nil")
	}
	filename = sanitizeFilename(filename)
	if filename == "" || filename == EntrypointName {
		return fmt.Errorf("invalid memory filename")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.dir, filename)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return s.removeIndexLineLocked(filename)
}

// RebuildIndex rewrites MEMORY.md from current topic frontmatter (CC dream
// index maintenance / /memory compact).
func (s *MemoryStore) RebuildIndex() (string, error) {
	if s == nil {
		return "", fmt.Errorf("memory store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	headers, err := s.scanLocked()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("# Memory index\n\n")
	for _, h := range headers {
		name := h.Name
		if name == "" {
			name = strings.TrimSuffix(h.Filename, ".md")
		}
		desc := h.Description
		if desc == "" {
			desc = name
		}
		b.WriteString(indexLine(name, h.Filename, desc))
		b.WriteString("\n")
	}
	content := b.String()
	if err := fsx.WriteFileAtomic(s.EntrypointPath(), []byte(content), 0o644); err != nil {
		return "", err
	}
	return strings.TrimSpace(content), nil
}

func (s *MemoryStore) upsertIndexLocked(name, filename, description string) error {
	raw, err := s.readIndexLocked()
	if err != nil {
		return err
	}
	line := indexLine(name, filename, description)
	lines := splitNonEmptyLines(raw)
	replaced := false
	needle := "(" + filename + ")"
	for i, existing := range lines {
		if strings.Contains(existing, needle) {
			lines[i] = line
			replaced = true
			break
		}
	}
	if !replaced {
		// Drop a pure title heading-only file content; keep user notes above.
		if strings.TrimSpace(raw) == "" {
			lines = []string{"# Memory index", line}
		} else {
			lines = append(lines, line)
		}
	}
	// Ensure heading exists.
	out := strings.Join(ensureIndexHeading(lines), "\n") + "\n"
	return fsx.WriteFileAtomic(s.EntrypointPath(), []byte(out), 0o644)
}

func (s *MemoryStore) removeIndexLineLocked(filename string) error {
	raw, err := s.readIndexLocked()
	if err != nil {
		return err
	}
	if raw == "" {
		return nil
	}
	needle := "(" + filename + ")"
	var kept []string
	for _, line := range strings.Split(raw, "\n") {
		if strings.Contains(line, needle) {
			continue
		}
		kept = append(kept, line)
	}
	out := strings.TrimRight(strings.Join(kept, "\n"), "\n") + "\n"
	return fsx.WriteFileAtomic(s.EntrypointPath(), []byte(out), 0o644)
}

func ensureIndexHeading(lines []string) []string {
	for _, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "#") {
			return lines
		}
	}
	return append([]string{"# Memory index"}, lines...)
}

func splitNonEmptyLines(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		out = append(out, line)
	}
	return out
}

// Search ranks topic files by keyword overlap on name/description/body/type.
func (s *MemoryStore) Search(opts SearchOptions) ([]MemoryFile, error) {
	files, err := s.List()
	if err != nil {
		return nil, err
	}
	if opts.Type != "" {
		filtered := files[:0]
		for _, f := range files {
			if f.Type == opts.Type {
				filtered = append(filtered, f)
			}
		}
		files = filtered
	}
	ranked := rankFiles(files, opts.Query)
	if opts.Limit > 0 && len(ranked) > opts.Limit {
		ranked = ranked[:opts.Limit]
	}
	if opts.MaxChars > 0 {
		ranked = boundFilesByChars(ranked, opts.MaxChars)
	}
	return ranked, nil
}

func rankFiles(files []MemoryFile, query string) []MemoryFile {
	terms := tokenize(query)
	type scored struct {
		f     MemoryFile
		score int
	}
	var scoredItems []scored
	for _, f := range files {
		score := 0
		hay := strings.ToLower(strings.Join([]string{
			f.Name, f.Description, f.Body, string(f.Type), f.Filename,
		}, " "))
		if len(terms) == 0 {
			// No query: recency order already from List.
			scoredItems = append(scoredItems, scored{f: f, score: int(f.MtimeMs / 1000)})
			continue
		}
		for _, term := range terms {
			if strings.Contains(hay, term) {
				score += 2
			}
			if strings.Contains(strings.ToLower(f.Description), term) {
				score += 3
			}
			if strings.Contains(strings.ToLower(f.Name), term) {
				score += 2
			}
		}
		if score == 0 {
			continue
		}
		scoredItems = append(scoredItems, scored{f: f, score: score})
	}
	sort.Slice(scoredItems, func(i, j int) bool {
		if scoredItems[i].score == scoredItems[j].score {
			return scoredItems[i].f.MtimeMs > scoredItems[j].f.MtimeMs
		}
		return scoredItems[i].score > scoredItems[j].score
	})
	out := make([]MemoryFile, 0, len(scoredItems))
	for _, s := range scoredItems {
		out = append(out, s.f)
	}
	return out
}

func boundFilesByChars(files []MemoryFile, maxChars int) []MemoryFile {
	if maxChars <= 0 {
		return files
	}
	var total int
	var out []MemoryFile
	for _, f := range files {
		cost := len(f.Body) + len(f.Description) + len(f.Name)
		if total > 0 && total+cost > maxChars {
			break
		}
		out = append(out, f)
		total += cost
	}
	return out
}

func tokenize(query string) []string {
	fields := strings.Fields(strings.ToLower(query))
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.Trim(field, ".,;:!?()[]{}\"'")
		if field != "" {
			out = append(out, field)
		}
	}
	return out
}

var nonFileChar = regexp.MustCompile(`[^a-z0-9_\-]+`)

func deriveFilename(typ MemoryType, name string) string {
	base := strings.ToLower(strings.TrimSpace(name))
	base = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		if r == ' ' || r == '-' || r == '_' {
			return '_'
		}
		return -1
	}, base)
	base = nonFileChar.ReplaceAllString(base, "_")
	base = strings.Trim(base, "_")
	if base == "" {
		base = "note"
	}
	if typ != "" && !strings.HasPrefix(base, string(typ)+"_") {
		base = string(typ) + "_" + base
	}
	if len(base) > 60 {
		base = base[:60]
	}
	return base + ".md"
}

func sanitizeFilename(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, "\\", "")
	name = strings.ReplaceAll(name, "/", "")
	if name == "" {
		return ""
	}
	if !strings.HasSuffix(strings.ToLower(name), ".md") {
		name += ".md"
	}
	return name
}

func truncateRunes(s string, n int) string {
	s = strings.TrimSpace(s)
	if n <= 0 || len(s) <= n {
		return s
	}
	// byte-approximate is fine for description hooks
	if n > 3 && len(s) > n {
		return s[:n-3] + "..."
	}
	return s[:n]
}
