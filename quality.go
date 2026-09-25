package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type TrackQuality struct {
	Filename     string    `json:"filename"`
	SizeBytes    int64     `json:"size_bytes"`
	ModTime      time.Time `json:"mod_time"`
	PeakDb       float64   `json:"peak_db"`
	RmsDb        float64   `json:"rms_db"`
	ClippedCount int64     `json:"clipped_count"`
	Score        int32     `json:"score"` // 0 - 50
}

type QualitySummary struct {
	ReleaseID         int64                   `json:"release_id"`
	Score             int32                   `json:"score"` // 0 - 100
	CompletenessScore int32                   `json:"completeness_score"` // 0 - 50
	CleanlinessScore  int32                   `json:"cleanliness_score"`  // 0 - 50
	ExpectedTracks    int                     `json:"expected_tracks"`
	FoundTracks       int                     `json:"found_tracks"`
	Tracks            map[string]TrackQuality `json:"tracks"`
	LastEvaluated     time.Time               `json:"last_evaluated"`
}

// GetQualityFilePath returns the path to quality.json for a given release.
func GetQualityFilePath(saveDir string, releaseID int64) string {
	return filepath.Join(saveDir, fmt.Sprintf("%d", releaseID), "quality.json")
}

// ReadQualitySummary reads and deserializes quality.json for a release.
func ReadQualitySummary(saveDir string, releaseID int64) (*QualitySummary, error) {
	qualityPath := GetQualityFilePath(saveDir, releaseID)
	data, err := os.ReadFile(qualityPath)
	if err != nil {
		return nil, err
	}

	var summary QualitySummary
	if err := json.Unmarshal(data, &summary); err != nil {
		return nil, fmt.Errorf("failed to unmarshal quality summary from %v: %w", qualityPath, err)
	}

	return &summary, nil
}

// WriteQualitySummary serializes and writes quality.json for a release.
func WriteQualitySummary(saveDir string, releaseID int64, summary *QualitySummary) error {
	if summary == nil {
		return fmt.Errorf("summary cannot be nil")
	}

	if releaseID == 0 && summary.ReleaseID != 0 {
		releaseID = summary.ReleaseID
	} else if summary.ReleaseID == 0 && releaseID != 0 {
		summary.ReleaseID = releaseID
	}

	targetPath := GetQualityFilePath(saveDir, releaseID)
	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %v: %w", dir, err)
	}

	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal quality summary: %w", err)
	}

	if err := os.WriteFile(targetPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write %v: %w", targetPath, err)
	}

	return nil
}

// IsCacheValid inspects all .flac files in save_dir/<release_id>/ and compares
// current file sizes and modification timestamps against the summary.
// If files are missing, added, or timestamps/sizes have changed, cache is marked invalid.
func IsCacheValid(saveDir string, releaseID int64, summary *QualitySummary) bool {
	if summary == nil || summary.Tracks == nil {
		return false
	}

	releaseDir := filepath.Join(saveDir, fmt.Sprintf("%d", releaseID))
	entries, err := os.ReadDir(releaseDir)
	if err != nil {
		return false
	}

	flacFiles := make(map[string]os.DirEntry)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(strings.ToLower(entry.Name()), ".flac") {
			flacFiles[entry.Name()] = entry
		}
	}

	if len(flacFiles) != len(summary.Tracks) {
		return false
	}

	if summary.FoundTracks != len(flacFiles) {
		return false
	}

	for name, entry := range flacFiles {
		tq, exists := summary.Tracks[name]
		if !exists {
			for _, track := range summary.Tracks {
				if track.Filename == name || filepath.Base(track.Filename) == name {
					tq = track
					exists = true
					break
				}
			}
		}
		if !exists {
			return false
		}

		info, err := entry.Info()
		if err != nil {
			return false
		}

		if info.Size() != tq.SizeBytes {
			return false
		}

		if !info.ModTime().Equal(tq.ModTime) {
			return false
		}
	}

	return true
}

// GetValidCachedSummary retrieves the cached quality summary if it exists and is valid.
func GetValidCachedSummary(saveDir string, releaseID int64) (*QualitySummary, bool) {
	summary, err := ReadQualitySummary(saveDir, releaseID)
	if err != nil {
		return nil, false
	}

	if !IsCacheValid(saveDir, releaseID, summary) {
		return nil, false
	}

	return summary, true
}
