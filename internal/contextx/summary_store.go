package contextx

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/junnhwan/bond-code/internal/fsx"
)

type SummaryStore struct {
	dataDir   string
	sessionID string
}

func NewSummaryStore(dataDir, sessionID string) *SummaryStore {
	return &SummaryStore{dataDir: dataDir, sessionID: sessionID}
}

func (s *SummaryStore) Load() (*SummaryArtifact, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(s.path())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var artifact SummaryArtifact
	if err := json.Unmarshal(data, &artifact); err != nil {
		return nil, err
	}
	return &artifact, nil
}

func (s *SummaryStore) Save(artifact SummaryArtifact) error {
	if err := s.validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path()), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return err
	}
	return fsx.WriteFileAtomic(s.path(), data, 0o600)
}

func (s *SummaryStore) path() string {
	return filepath.Join(s.dataDir, "context-summaries", s.sessionID+".json")
}

func (s *SummaryStore) validate() error {
	if s.sessionID == "" || strings.ContainsAny(s.sessionID, `/\`) || s.sessionID == "." || s.sessionID == ".." {
		return fmt.Errorf("invalid session id %q", s.sessionID)
	}
	return nil
}
