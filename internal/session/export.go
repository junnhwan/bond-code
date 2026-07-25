package session

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/junnhwan/bond-code/internal/fsx"
)

func (s *JSONLStore) Export(sessionID, targetPath string) error {
	if err := validateSessionID(sessionID); err != nil {
		return err
	}
	data, err := os.ReadFile(s.path(sessionID))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
		return err
	}
	return fsx.WriteFileAtomic(targetPath, data, 0o600)
}

func (s *JSONLStore) Import(sessionID, sourcePath string) error {
	if err := validateSessionID(sessionID); err != nil {
		return err
	}
	f, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	var out bytes.Buffer
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return err
		}
		event.SessionID = sessionID
		data, err := json.Marshal(event)
		if err != nil {
			return err
		}
		out.Write(data)
		out.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return fsx.WriteFileAtomic(s.path(sessionID), out.Bytes(), 0o600)
}

func (s *JSONLStore) Fork(sourceSessionID, targetSessionID string) error {
	if err := validateSessionID(sourceSessionID); err != nil {
		return err
	}
	return s.Import(targetSessionID, s.path(sourceSessionID))
}
