package memory

import "strings"

// Closed four-type taxonomy, matching Claude Code memdir/memoryTypes.ts.
// Only content not derivable from the current project state belongs here.
type MemoryType string

const (
	TypeUser      MemoryType = "user"
	TypeFeedback  MemoryType = "feedback"
	TypeProject   MemoryType = "project"
	TypeReference MemoryType = "reference"
)

var AllTypes = []MemoryType{TypeUser, TypeFeedback, TypeProject, TypeReference}

func ParseType(raw string) (MemoryType, bool) {
	t := MemoryType(strings.ToLower(strings.TrimSpace(raw)))
	switch t {
	case TypeUser, TypeFeedback, TypeProject, TypeReference:
		return t, true
	default:
		return "", false
	}
}

// MemoryHeader is the lightweight frontmatter scan result used for selection
// and manifests (CC memoryScan.MemoryHeader).
type MemoryHeader struct {
	Filename    string
	FilePath    string
	MtimeMs     int64
	Name        string
	Description string
	Type        MemoryType
}

// MemoryFile is a full topic memory on disk.
type MemoryFile struct {
	Filename    string
	FilePath    string
	MtimeMs     int64
	Name        string
	Description string
	Type        MemoryType
	Body        string
}

// SaveArgs is the model-facing memory_save payload.
type SaveArgs struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"`
	// Filename is optional; when empty, derived from name/type.
	Filename string `json:"filename,omitempty"`
}

// SearchArgs is the model-facing memory_search payload.
type SearchArgs struct {
	Query    string `json:"query"`
	Type     string `json:"type"`
	Limit    int    `json:"limit"`
	MaxChars int    `json:"max_chars"`
}

// SearchOptions controls store-level search/selection.
type SearchOptions struct {
	Query    string
	Type     MemoryType
	Limit    int
	MaxChars int
}
