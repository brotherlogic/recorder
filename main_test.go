package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCleanupRetainedFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test_retained")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	s := &Server{}

	// Create a fresh file
	freshFile := filepath.Join(tmpDir, "fresh.wav")
	if err := os.WriteFile(freshFile, []byte("fresh"), 0644); err != nil {
		t.Fatalf("Failed to create fresh file: %v", err)
	}

	// Create an old file
	oldFile := filepath.Join(tmpDir, "old.wav")
	if err := os.WriteFile(oldFile, []byte("old"), 0644); err != nil {
		t.Fatalf("Failed to create old file: %v", err)
	}
	// Backdate it to 25 hours ago
	oldTime := time.Now().Add(-25 * time.Hour)
	if err := os.Chtimes(oldFile, oldTime, oldTime); err != nil {
		t.Fatalf("Failed to backdate file: %v", err)
	}

	// Run cleanup
	s.cleanupRetainedFiles(tmpDir)

	// Verify fresh file exists
	if _, err := os.Stat(freshFile); os.IsNotExist(err) {
		t.Errorf("Fresh file was incorrectly deleted")
	}

	// Verify old file is gone
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Errorf("Old file was not deleted")
	}
}
