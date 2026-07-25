package fsx

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteFileAtomic replaces path only after the complete contents have been
// written and synced to a temporary file in the same directory. Keeping the
// temporary file beside the target ensures the final rename stays on one
// filesystem.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	pattern := "." + filepath.Base(path) + ".tmp-*"
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return fmt.Errorf("create temporary file for %s: %w", path, err)
	}
	tmpPath := file.Name()
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
		_ = os.Remove(tmpPath)
	}()

	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("write temporary file for %s: %w", path, err)
	}
	if err := file.Chmod(perm); err != nil {
		return fmt.Errorf("set permissions on temporary file for %s: %w", path, err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync temporary file for %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		closed = true
		return fmt.Errorf("close temporary file for %s: %w", path, err)
	}
	closed = true

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}
