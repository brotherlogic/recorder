package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	pbgd "github.com/brotherlogic/godiscogs/proto"
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

func TestGetTrackOffset(t *testing.T) {
	tests := []struct {
		name     string
		release  *pbgd.Release
		disk     int32
		expected int
	}{
		{
			name: "Single disk CD",
			release: &pbgd.Release{
				Tracklist: []*pbgd.Track{
					{Position: "1"},
					{Position: "2"},
				},
			},
			disk:     1,
			expected: 0,
		},
		{
			name: "Multi disk CD",
			release: &pbgd.Release{
				Tracklist: []*pbgd.Track{
					{Position: "1-1"},
					{Position: "1-2"},
					{Position: "2-1"},
					{Position: "2-2"},
				},
			},
			disk:     2,
			expected: 2,
		},
		{
			name: "Double Vinyl",
			release: &pbgd.Release{
				Tracklist: []*pbgd.Track{
					{Position: "A1"},
					{Position: "A2"},
					{Position: "B1"},
					{Position: "B2"},
					{Position: "C1"},
					{Position: "C2"},
					{Position: "D1"},
					{Position: "D2"},
				},
			},
			disk:     2, // Sides C & D
			expected: 4,
		},
		{
			name: "Release 19347250 Disk 5",
			release: &pbgd.Release{
				Tracklist: []*pbgd.Track{
					{Position: "1-1"}, {Position: "1-2"}, {Position: "1-3"}, {Position: "1-4"},
					{Position: "2-1"}, {Position: "2-2"}, {Position: "2-3"}, {Position: "2-4"},
					{Position: "3-1"}, {Position: "3-2"}, {Position: "3-3"}, {Position: "3-4"},
					{Position: "4-1"}, {Position: "4-2"}, {Position: "4-3"}, {Position: "4-4"}, {Position: "4-5"}, {Position: "4-6"},
					{Position: "5-1"}, {Position: "5-2"},
				},
			},
			disk:     5,
			expected: 18,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := getTrackOffset(tc.release, tc.disk)
			if got != tc.expected {
				t.Errorf("getTrackOffset() = %v, want %v", got, tc.expected)
			}
		})
	}
}
