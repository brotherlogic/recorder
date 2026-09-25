package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestQualitySummarySerialization(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "quality_test_ser")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	releaseID := int64(12345)
	now := time.Now().UTC().Truncate(time.Millisecond)

	original := &QualitySummary{
		ReleaseID:         releaseID,
		Score:             85,
		CompletenessScore: 45,
		CleanlinessScore:  40,
		ExpectedTracks:    2,
		FoundTracks:       2,
		LastEvaluated:     now,
		Tracks: map[string]TrackQuality{
			"track1.flac": {
				Filename:     "track1.flac",
				SizeBytes:    1024,
				ModTime:      now,
				PeakDb:       -1.5,
				RmsDb:        -14.2,
				ClippedCount: 0,
				Score:        40,
			},
			"track2.flac": {
				Filename:     "track2.flac",
				SizeBytes:    2048,
				ModTime:      now,
				PeakDb:       -2.0,
				RmsDb:        -15.0,
				ClippedCount: 1,
				Score:        38,
			},
		},
	}

	err = WriteQualitySummary(tempDir, releaseID, original)
	if err != nil {
		t.Fatalf("WriteQualitySummary failed: %v", err)
	}

	expectedPath := filepath.Join(tempDir, "12345", "quality.json")
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Fatalf("expected quality.json at %v, but not found", expectedPath)
	}

	loaded, err := ReadQualitySummary(tempDir, releaseID)
	if err != nil {
		t.Fatalf("ReadQualitySummary failed: %v", err)
	}

	if loaded == nil {
		t.Fatalf("loaded summary is nil")
	}

	if loaded.ReleaseID != original.ReleaseID {
		t.Errorf("ReleaseID mismatch: got %v, want %v", loaded.ReleaseID, original.ReleaseID)
	}
	if loaded.Score != original.Score {
		t.Errorf("Score mismatch: got %v, want %v", loaded.Score, original.Score)
	}
	if loaded.CompletenessScore != original.CompletenessScore {
		t.Errorf("CompletenessScore mismatch: got %v, want %v", loaded.CompletenessScore, original.CompletenessScore)
	}
	if loaded.CleanlinessScore != original.CleanlinessScore {
		t.Errorf("CleanlinessScore mismatch: got %v, want %v", loaded.CleanlinessScore, original.CleanlinessScore)
	}
	if loaded.ExpectedTracks != original.ExpectedTracks {
		t.Errorf("ExpectedTracks mismatch: got %v, want %v", loaded.ExpectedTracks, original.ExpectedTracks)
	}
	if loaded.FoundTracks != original.FoundTracks {
		t.Errorf("FoundTracks mismatch: got %v, want %v", loaded.FoundTracks, original.FoundTracks)
	}
	if len(loaded.Tracks) != len(original.Tracks) {
		t.Fatalf("Tracks len mismatch: got %v, want %v", len(loaded.Tracks), len(original.Tracks))
	}

	for k, origTrack := range original.Tracks {
		loadedTrack, ok := loaded.Tracks[k]
		if !ok {
			t.Errorf("missing track %v in loaded summary", k)
			continue
		}
		if loadedTrack.Filename != origTrack.Filename {
			t.Errorf("track %v Filename mismatch: got %v, want %v", k, loadedTrack.Filename, origTrack.Filename)
		}
		if loadedTrack.SizeBytes != origTrack.SizeBytes {
			t.Errorf("track %v SizeBytes mismatch: got %v, want %v", k, loadedTrack.SizeBytes, origTrack.SizeBytes)
		}
		if !loadedTrack.ModTime.Equal(origTrack.ModTime) {
			t.Errorf("track %v ModTime mismatch: got %v, want %v", k, loadedTrack.ModTime, origTrack.ModTime)
		}
		if loadedTrack.PeakDb != origTrack.PeakDb {
			t.Errorf("track %v PeakDb mismatch: got %v, want %v", k, loadedTrack.PeakDb, origTrack.PeakDb)
		}
		if loadedTrack.RmsDb != origTrack.RmsDb {
			t.Errorf("track %v RmsDb mismatch: got %v, want %v", k, loadedTrack.RmsDb, origTrack.RmsDb)
		}
		if loadedTrack.ClippedCount != origTrack.ClippedCount {
			t.Errorf("track %v ClippedCount mismatch: got %v, want %v", k, loadedTrack.ClippedCount, origTrack.ClippedCount)
		}
		if loadedTrack.Score != origTrack.Score {
			t.Errorf("track %v Score mismatch: got %v, want %v", k, loadedTrack.Score, origTrack.Score)
		}
	}
}

func setupTestReleaseFiles(t *testing.T) (string, int64, *QualitySummary) {
	tempDir, err := os.MkdirTemp("", "quality_cache_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	releaseID := int64(98765)
	releaseDir := filepath.Join(tempDir, "98765")
	if err := os.MkdirAll(releaseDir, 0755); err != nil {
		t.Fatalf("failed to create release dir: %v", err)
	}

	f1 := filepath.Join(releaseDir, "track1.flac")
	f2 := filepath.Join(releaseDir, "track2.flac")

	if err := os.WriteFile(f1, []byte("audio-content-track-1"), 0644); err != nil {
		t.Fatalf("failed to write track1.flac: %v", err)
	}
	if err := os.WriteFile(f2, []byte("audio-content-track-2-longer"), 0644); err != nil {
		t.Fatalf("failed to write track2.flac: %v", err)
	}

	info1, err := os.Stat(f1)
	if err != nil {
		t.Fatalf("stat f1: %v", err)
	}
	info2, err := os.Stat(f2)
	if err != nil {
		t.Fatalf("stat f2: %v", err)
	}

	summary := &QualitySummary{
		ReleaseID:         releaseID,
		Score:             90,
		CompletenessScore: 50,
		CleanlinessScore:  40,
		ExpectedTracks:    2,
		FoundTracks:       2,
		LastEvaluated:     time.Now().UTC(),
		Tracks: map[string]TrackQuality{
			"track1.flac": {
				Filename:     "track1.flac",
				SizeBytes:    info1.Size(),
				ModTime:      info1.ModTime(),
				PeakDb:       -1.0,
				RmsDb:        -14.0,
				ClippedCount: 0,
				Score:        40,
			},
			"track2.flac": {
				Filename:     "track2.flac",
				SizeBytes:    info2.Size(),
				ModTime:      info2.ModTime(),
				PeakDb:       -1.2,
				RmsDb:        -14.5,
				ClippedCount: 0,
				Score:        40,
			},
		},
	}

	if err := WriteQualitySummary(tempDir, releaseID, summary); err != nil {
		t.Fatalf("failed to write initial quality summary: %v", err)
	}

	return tempDir, releaseID, summary
}

func TestCacheHitReturnsValidCachedSummary(t *testing.T) {
	tempDir, releaseID, summary := setupTestReleaseFiles(t)
	defer os.RemoveAll(tempDir)

	if !IsCacheValid(tempDir, releaseID, summary) {
		t.Errorf("expected cache to be valid, but IsCacheValid returned false")
	}

	cached, valid := GetValidCachedSummary(tempDir, releaseID)
	if !valid || cached == nil {
		t.Errorf("expected GetValidCachedSummary to return valid summary, got valid=%v, cached=%v", valid, cached)
	}
}

func TestTouchingFlacFileInvalidatesCache(t *testing.T) {
	tempDir, releaseID, summary := setupTestReleaseFiles(t)
	defer os.RemoveAll(tempDir)

	f1 := filepath.Join(tempDir, "98765", "track1.flac")
	newTime := time.Now().Add(5 * time.Minute)
	if err := os.Chtimes(f1, newTime, newTime); err != nil {
		t.Fatalf("failed to touch file: %v", err)
	}

	if IsCacheValid(tempDir, releaseID, summary) {
		t.Errorf("expected touched file to invalidate cache, but IsCacheValid returned true")
	}

	_, valid := GetValidCachedSummary(tempDir, releaseID)
	if valid {
		t.Errorf("expected GetValidCachedSummary to return valid=false after touching file")
	}
}

func TestModifyingFlacFileInvalidatesCache(t *testing.T) {
	tempDir, releaseID, summary := setupTestReleaseFiles(t)
	defer os.RemoveAll(tempDir)

	f1 := filepath.Join(tempDir, "98765", "track1.flac")
	f, err := os.OpenFile(f1, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("failed to open file for append: %v", err)
	}
	if _, err := f.Write([]byte("-more-bytes")); err != nil {
		f.Close()
		t.Fatalf("failed to write to file: %v", err)
	}
	f.Close()

	if IsCacheValid(tempDir, releaseID, summary) {
		t.Errorf("expected modified file size to invalidate cache, but IsCacheValid returned true")
	}
}

func TestAddingNewFlacFileInvalidatesCache(t *testing.T) {
	tempDir, releaseID, summary := setupTestReleaseFiles(t)
	defer os.RemoveAll(tempDir)

	f3 := filepath.Join(tempDir, "98765", "track3.flac")
	if err := os.WriteFile(f3, []byte("track-3-audio"), 0644); err != nil {
		t.Fatalf("failed to create new track: %v", err)
	}

	if IsCacheValid(tempDir, releaseID, summary) {
		t.Errorf("expected newly added FLAC file to invalidate cache, but IsCacheValid returned true")
	}

	_, valid := GetValidCachedSummary(tempDir, releaseID)
	if valid {
		t.Errorf("expected GetValidCachedSummary to return valid=false after adding file")
	}
}

func TestRemovingFlacFileInvalidatesCache(t *testing.T) {
	tempDir, releaseID, summary := setupTestReleaseFiles(t)
	defer os.RemoveAll(tempDir)

	f2 := filepath.Join(tempDir, "98765", "track2.flac")
	if err := os.Remove(f2); err != nil {
		t.Fatalf("failed to remove track2: %v", err)
	}

	if IsCacheValid(tempDir, releaseID, summary) {
		t.Errorf("expected removed FLAC file to invalidate cache, but IsCacheValid returned true")
	}
}

func TestMissingSummaryFile(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "quality_missing_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	releaseID := int64(11111)
	summary, err := ReadQualitySummary(tempDir, releaseID)
	if err == nil {
		t.Errorf("expected error reading missing quality.json, got nil (summary: %v)", summary)
	}

	cached, valid := GetValidCachedSummary(tempDir, releaseID)
	if valid || cached != nil {
		t.Errorf("expected valid=false, cached=nil for missing summary, got valid=%v, cached=%v", valid, cached)
	}
}
