package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// LocalStorage writes output files to a local directory.
type LocalStorage struct {
	baseDir string
}

// NewLocalStorage creates a LocalStorage rooted at baseDir.
func NewLocalStorage(baseDir string) *LocalStorage {
	return &LocalStorage{baseDir: baseDir}
}

// WriteParquet writes data to baseDir/jobID/dataType.parquet.
// It creates intermediate directories as needed.
// Returns the absolute file path that was written.
func (s *LocalStorage) WriteParquet(ctx context.Context, jobID, dataType string, data []byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	dir := filepath.Join(s.baseDir, jobID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("local storage: creating directory %s: %w", dir, err)
	}

	path := filepath.Join(dir, dataType+".parquet")
	if err := os.WriteFile(path, data, 0o640); err != nil {
		return "", fmt.Errorf("local storage: writing parquet %s: %w", path, err)
	}
	return path, nil
}

// WriteJSON writes data to baseDir/jobID/dataType.json.
// It creates intermediate directories as needed.
// Returns the absolute file path that was written.
func (s *LocalStorage) WriteJSON(ctx context.Context, jobID, dataType string, data []byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	dir := filepath.Join(s.baseDir, jobID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("local storage: creating directory %s: %w", dir, err)
	}

	path := filepath.Join(dir, dataType+".json")
	if err := os.WriteFile(path, data, 0o640); err != nil {
		return "", fmt.Errorf("local storage: writing json %s: %w", path, err)
	}
	return path, nil
}
