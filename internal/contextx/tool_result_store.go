package contextx

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/junnhwan/bond-code/internal/fsx"
)

type ToolResultStore struct {
	dataDir   string
	sessionID string
}

func NewToolResultStore(dataDir, sessionID string) *ToolResultStore {
	return &ToolResultStore{dataDir: dataDir, sessionID: sessionID}
}

func (s *ToolResultStore) Save(toolCallID string, output string) (string, error) {
	sessionID := sanitizePathSegment(s.sessionID)
	if sessionID == "" {
		sessionID = "session"
	}
	fileName := sanitizePathSegment(toolCallID)
	if fileName == "" {
		fileName = "tool-call"
	}
	fileName += ".txt"

	path := filepath.Join(s.dataDir, "tool-results", sessionID, fileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := fsx.WriteFileAtomic(path, []byte(output), 0o600); err != nil {
		return "", err
	}
	return filepath.ToSlash(path), nil
}

func sanitizePathSegment(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "._")
}
