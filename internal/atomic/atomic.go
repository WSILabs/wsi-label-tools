// Package atomic provides atomic file writes via temp-file + rename.
package atomic

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// WriteAtomic writes to target by first writing to a sibling temp file,
// syncing (if fsync is true), then renaming over target. On any error from
// write, the temp file is removed and target is left untouched.
func WriteAtomic(target string, write func(w *os.File) error, fsync bool) error {
	dir := filepath.Dir(target)
	base := filepath.Base(target)
	tmpName := fmt.Sprintf(".%s.tmp-%d-%d", base, os.Getpid(), time.Now().UnixNano())
	tmpPath := filepath.Join(dir, tmpName)

	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}

	cleanup := func() {
		_ = f.Close()
		_ = os.Remove(tmpPath)
	}

	if err := write(f); err != nil {
		cleanup()
		return err
	}
	if fsync {
		if err := f.Sync(); err != nil {
			cleanup()
			return fmt.Errorf("fsync temp: %w", err)
		}
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpPath, target); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
