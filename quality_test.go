package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pbgd "github.com/brotherlogic/godiscogs/proto"
	pbrc "github.com/brotherlogic/recordcollection/proto"
	pb "github.com/brotherlogic/recorder/proto"
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
		Version:           CurrentScoringVersion,
		Score:             85,
		CompletenessScore: 45,
		CleanlinessScore:  40,
		ExpectedTracks:    2,
		FoundTracks:       2,
		LastEvaluated:     now,
		Tracks: map[string]TrackQuality{
			"track1.flac": {
				Filename:       "track1.flac",
				SizeBytes:      1024,
				ModTime:        now,
				PeakDb:         -1.5,
				RmsDb:          -14.2,
				ClippedCount:   0,
				DynamicRange:   12.7,
				HasLongSilence: false,
				Score:          40,
			},
			"track2.flac": {
				Filename:       "track2.flac",
				SizeBytes:      2048,
				ModTime:        now,
				PeakDb:         -2.0,
				RmsDb:          -15.0,
				ClippedCount:   1,
				DynamicRange:   13.0,
				HasLongSilence: true,
				Score:          38,
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
	if loaded.Version != original.Version {
		t.Errorf("Version mismatch: got %v, want %v", loaded.Version, original.Version)
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
		if loadedTrack.DynamicRange != origTrack.DynamicRange {
			t.Errorf("track %v DynamicRange mismatch: got %v, want %v", k, loadedTrack.DynamicRange, origTrack.DynamicRange)
		}
		if loadedTrack.HasLongSilence != origTrack.HasLongSilence {
			t.Errorf("track %v HasLongSilence mismatch: got %v, want %v", k, loadedTrack.HasLongSilence, origTrack.HasLongSilence)
		}
		if loadedTrack.Score != origTrack.Score {
			t.Errorf("track %v Score mismatch: got %v, want %v", k, loadedTrack.Score, origTrack.Score)
		}
	}
}

func TestQualitySummaryCacheVersioning(t *testing.T) {
	tempDir, releaseID, summary := setupTestReleaseFiles(t)
	defer os.RemoveAll(tempDir)

	// Verify IsCacheValid returns false when summary.Version is 0
	summary.Version = 0
	if IsCacheValid(tempDir, releaseID, summary) {
		t.Errorf("expected IsCacheValid to return false when summary.Version is 0, got true")
	}

	// Verify IsCacheValid returns false when summary.Version is 1
	summary.Version = 1
	if IsCacheValid(tempDir, releaseID, summary) {
		t.Errorf("expected IsCacheValid to return false when summary.Version is 1, got true")
	}

	// Verify IsCacheValid returns false when summary.Version is 2
	summary.Version = 2
	if IsCacheValid(tempDir, releaseID, summary) {
		t.Errorf("expected IsCacheValid to return false when summary.Version is 2, got true")
	}

	// Verify IsCacheValid returns true when summary.Version == CurrentScoringVersion and file modification times/sizes match
	summary.Version = CurrentScoringVersion
	if !IsCacheValid(tempDir, releaseID, summary) {
		t.Errorf("expected IsCacheValid to return true when summary.Version is CurrentScoringVersion (%d), got false", CurrentScoringVersion)
	}
}

func TestQualitySummaryV3Serialization(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "quality_test_v3_ser")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	releaseID := int64(12345)
	now := time.Now().UTC().Truncate(time.Millisecond)

	original := &QualitySummary{
		ReleaseID:         releaseID,
		Version:           3,
		Score:             85,
		CompletenessScore: 45,
		CleanlinessScore:  40,
		ExpectedTracks:    2,
		FoundTracks:       2,
		LastEvaluated:     now,
		Disks: []DiskQualitySummary{
			{
				Disk:        1,
				BestRipDate: "2026-09-25",
				Score:       85,
			},
			{
				Disk:        2,
				BestRipDate: "2026-09-26",
				Score:       90,
			},
		},
		Tracks: map[string]TrackQuality{
			"track1.flac": {
				Filename:       "track1.flac",
				SizeBytes:      1024,
				ModTime:        now,
				PeakDb:         -1.5,
				RmsDb:          -14.2,
				ClippedCount:   0,
				DynamicRange:   12.7,
				HasLongSilence: false,
				Score:          40,
			},
			"track2.flac": {
				Filename:       "track2.flac",
				SizeBytes:      2048,
				ModTime:        now,
				PeakDb:         -2.0,
				RmsDb:          -15.0,
				ClippedCount:   1,
				DynamicRange:   13.0,
				HasLongSilence: true,
				Score:          38,
			},
		},
	}

	err = WriteQualitySummary(tempDir, releaseID, original)
	if err != nil {
		t.Fatalf("WriteQualitySummary failed: %v", err)
	}

	loaded, err := ReadQualitySummary(tempDir, releaseID)
	if err != nil {
		t.Fatalf("ReadQualitySummary failed: %v", err)
	}

	if loaded == nil {
		t.Fatalf("loaded summary is nil")
	}

	if loaded.Version != 3 {
		t.Errorf("Version mismatch: got %v, want 3", loaded.Version)
	}

	if len(loaded.Disks) != len(original.Disks) {
		t.Fatalf("Disks length mismatch: got %v, want %v", len(loaded.Disks), len(original.Disks))
	}

	for i, origDisk := range original.Disks {
		loadedDisk := loaded.Disks[i]
		if loadedDisk.Disk != origDisk.Disk {
			t.Errorf("Disk[%d].Disk mismatch: got %v, want %v", i, loadedDisk.Disk, origDisk.Disk)
		}
		if loadedDisk.BestRipDate != origDisk.BestRipDate {
			t.Errorf("Disk[%d].BestRipDate mismatch: got %v, want %v", i, loadedDisk.BestRipDate, origDisk.BestRipDate)
		}
		if loadedDisk.Score != origDisk.Score {
			t.Errorf("Disk[%d].Score mismatch: got %v, want %v", i, loadedDisk.Score, origDisk.Score)
		}
	}
}

func TestCacheVersionInvalidation(t *testing.T) {
	tempDir, releaseID, summary := setupTestReleaseFiles(t)
	defer os.RemoveAll(tempDir)

	// Verify versions 0, 1, and 2 are invalidated when CurrentScoringVersion is 3
	for _, v := range []int{0, 1, 2} {
		summary.Version = v
		if IsCacheValid(tempDir, releaseID, summary) {
			t.Errorf("expected IsCacheValid to return false for legacy version %d, got true", v)
		}
	}

	// Verify version 3 returns true when file stats match
	summary.Version = 3
	if !IsCacheValid(tempDir, releaseID, summary) {
		t.Errorf("expected IsCacheValid to return true for version 3 when file stats match, got false")
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
		Version:           CurrentScoringVersion,
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

const cleanSoxStatsOutput = `
             Overall     Left      Right
DC offset   0.000000  0.000000  0.000000
Min level  -0.704996 -0.704996 -0.704996
Max level   0.704996  0.704996  0.704996
Pk lev dB      -3.04     -3.04     -3.04
RMS lev dB    -18.50    -18.50    -18.50
RMS Pk dB     -18.00    -18.00    -18.00
RMS Tr dB     -19.00    -19.00    -19.00
Crest factor       -      1.41      1.41
Flat factor     0.00      0.00      0.00
Pk count         198       198       198
Bit-depth      32/32     32/32     32/32
Num samples    44.1k
Length s       1.000
Scale max   1.000000
Window s       0.050
`

const clippedSoxStatsOutput = `
sox WARN vol: vol clipped 46000 samples; decrease volume?
             Overall     Left      Right
DC offset   0.000002  0.000002  0.000002
Min level  -1.000000 -1.000000 -1.000000
Max level   1.000000  1.000000  1.000000
Pk lev dB       0.00      0.00      0.00
RMS lev dB     -0.24     -0.24     -0.24
RMS Pk dB      -0.24     -0.24     -0.24
RMS Tr dB      -0.27     -0.27     -0.27
Crest factor       -      1.03      1.03
Flat factor     0.34      0.34      0.34
Pk count       20.2k     20.2k     20.2k
Bit-depth      32/32     32/32     32/32
Num samples    44.1k
Length s       1.000
Scale max   1.000000
Window s       0.050
`

const abnormalNoiseFloorStatsOutput = `
             Overall     Left      Right
DC offset   0.000000  0.000000  0.000000
Min level  -0.704996 -0.704996 -0.704996
Max level   0.704996  0.704996  0.704996
Pk lev dB      -2.00     -2.00     -2.00
RMS lev dB     -5.50     -5.50     -5.50
RMS Pk dB      -5.00     -5.00     -5.00
RMS Tr dB      -6.00     -6.00     -6.00
Crest factor       -      1.10      1.10
Flat factor     0.00      0.00      0.00
Pk count         198       198       198
Bit-depth      32/32     32/32     32/32
Num samples    44.1k
Length s       1.000
Scale max   1.000000
Window s       0.050
`

func TestParseSoxStats(t *testing.T) {
	// Clean audio parsing
	stats, err := ParseSoxStats(cleanSoxStatsOutput)
	if err != nil {
		t.Fatalf("unexpected error parsing clean stats: %v", err)
	}
	if stats == nil {
		t.Fatalf("expected non-nil stats")
	}
	if stats.PeakDb != -3.04 {
		t.Errorf("expected PeakDb -3.04, got %v", stats.PeakDb)
	}
	if stats.RmsDb != -18.50 {
		t.Errorf("expected RmsDb -18.50, got %v", stats.RmsDb)
	}
	if stats.ClippedCount != 0 {
		t.Errorf("expected ClippedCount 0, got %v", stats.ClippedCount)
	}

	// Clipped audio parsing
	clippedStats, err := ParseSoxStats(clippedSoxStatsOutput)
	if err != nil {
		t.Fatalf("unexpected error parsing clipped stats: %v", err)
	}
	if clippedStats.PeakDb != 0.00 {
		t.Errorf("expected PeakDb 0.00, got %v", clippedStats.PeakDb)
	}
	if clippedStats.RmsDb != -0.24 {
		t.Errorf("expected RmsDb -0.24, got %v", clippedStats.RmsDb)
	}
	if clippedStats.ClippedCount != 46000 {
		t.Errorf("expected ClippedCount 46000, got %v", clippedStats.ClippedCount)
	}
}

func TestScoringCleanAudio(t *testing.T) {
	stats := &SoxStats{
		PeakDb:       -3.0,
		RmsDb:        -18.0,
		ClippedCount: 0,
	}
	score := CalculateTrackCleanliness(stats, false)
	if score < 45 || score > 50 {
		t.Errorf("expected high score (~50) for clean audio, got %v", score)
	}
}

func TestScoringClippedAudio(t *testing.T) {
	cleanStats := &SoxStats{
		PeakDb:       -2.0,
		RmsDb:        -18.0,
		ClippedCount: 0,
	}
	cleanScore := CalculateTrackCleanliness(cleanStats, false)

	clippedStats := &SoxStats{
		PeakDb:       0.0,
		RmsDb:        -18.0,
		ClippedCount: 5000,
	}
	clippedScore := CalculateTrackCleanliness(clippedStats, false)

	if clippedScore >= cleanScore {
		t.Errorf("expected clipped audio score (%v) to be less than clean score (%v)", clippedScore, cleanScore)
	}
	if cleanScore-clippedScore < 10 {
		t.Errorf("expected significant deduction for severe clipping, deduction was %v", cleanScore-clippedScore)
	}
}

func TestScoringElevatedNoiseFloor(t *testing.T) {
	cleanStats := &SoxStats{
		PeakDb:       -2.0,
		RmsDb:        -20.0, // Dynamic range 18dB
		ClippedCount: 0,
	}
	cleanScore := CalculateTrackCleanliness(cleanStats, false)

	noisyStats := &SoxStats{
		PeakDb:       -2.0,
		RmsDb:        -5.0, // Dynamic range only 3dB (elevated noise floor)
		ClippedCount: 0,
	}
	noisyScore := CalculateTrackCleanliness(noisyStats, false)

	if noisyScore >= cleanScore {
		t.Errorf("expected noisy audio score (%v) to be less than clean score (%v)", noisyScore, cleanScore)
	}
	if cleanScore-noisyScore < 5 {
		t.Errorf("expected deduction for elevated noise floor, deduction was %v", cleanScore-noisyScore)
	}
}

func TestAnalyzeTrackZeroByteFile(t *testing.T) {
	tmpDir := t.TempDir()
	emptyFile := filepath.Join(tmpDir, "empty.flac")
	err := os.WriteFile(emptyFile, []byte{}, 0644)
	if err != nil {
		t.Fatalf("failed to create empty file: %v", err)
	}

	res, err := AnalyzeTrack(emptyFile)
	if err != nil {
		t.Fatalf("unexpected error analyzing empty file: %v", err)
	}
	if res.Score != 0 {
		t.Errorf("expected score 0 for 0-byte file, got %v", res.Score)
	}
}

func TestAnalyzeTrackCorruptedFile(t *testing.T) {
	tmpDir := t.TempDir()
	corruptedFile := filepath.Join(tmpDir, "corrupted.flac")
	err := os.WriteFile(corruptedFile, []byte("NOT_A_VALID_FLAC_FILE_HEADER_GARBAGE"), 0644)
	if err != nil {
		t.Fatalf("failed to create corrupted file: %v", err)
	}

	res, err := AnalyzeTrack(corruptedFile)
	if err != nil {
		t.Fatalf("unexpected error analyzing corrupted file: %v", err)
	}
	if res.Score != 0 {
		t.Errorf("expected score 0 for corrupted file, got %v", res.Score)
	}
}

func TestCalculateAggregateCleanliness(t *testing.T) {
	tracks := []TrackQuality{
		{Score: 50},
		{Score: 40},
		{Score: 30},
	}
	agg := CalculateAggregateCleanliness(tracks)
	if agg != 40 {
		t.Errorf("expected average score of 40, got %v", agg)
	}

	emptyTracks := []TrackQuality{}
	if CalculateAggregateCleanliness(emptyTracks) != 0 {
		t.Errorf("expected 0 for empty tracklist")
	}
}

func TestCalculateAggregateCleanlinessFromMap(t *testing.T) {
	trackMap := map[string]TrackQuality{
		"track1.flac": {Score: 48},
		"track2.flac": {Score: 42},
	}
	agg := CalculateAggregateCleanlinessFromMap(trackMap)
	if agg != 45 {
		t.Errorf("expected average score of 45, got %v", agg)
	}

	emptyMap := map[string]TrackQuality{}
	if CalculateAggregateCleanlinessFromMap(emptyMap) != 0 {
		t.Errorf("expected 0 for empty track map")
	}
}

func TestAnalyzeTrackRealAudio(t *testing.T) {
	tmpDir := t.TempDir()

	// Clean audio (with healthy dynamic range)
	cleanFile := filepath.Join(tmpDir, "clean.wav")
	cmd := exec.Command("sox", "-n", "-r", "44100", "-c", "2", cleanFile, "synth", "0.1", "sine", "1000", "pad", "0", "1.0", "vol", "-3dB")
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to create clean audio file: %v", err)
	}

	cleanRes, err := AnalyzeTrack(cleanFile)
	if err != nil {
		t.Fatalf("unexpected error analyzing clean audio: %v", err)
	}
	if cleanRes.Score < 45 || cleanRes.Score > 50 {
		t.Errorf("expected clean audio score between 45 and 50, got %v", cleanRes.Score)
	}

	// Clipped audio
	clippedFile := filepath.Join(tmpDir, "clipped.wav")
	cmdClipped := exec.Command("sox", "-n", "-r", "44100", "-c", "2", clippedFile, "synth", "0.2", "sine", "1000", "vol", "10.0")
	if err := cmdClipped.Run(); err != nil {
		t.Fatalf("failed to create clipped audio file: %v", err)
	}

	clippedRes, err := AnalyzeTrack(clippedFile)
	if err != nil {
		t.Fatalf("unexpected error analyzing clipped audio: %v", err)
	}
	if clippedRes.Score >= cleanRes.Score {
		t.Errorf("expected clipped audio score (%v) < clean audio score (%v)", clippedRes.Score, cleanRes.Score)
	}

	// Non-existent file
	missingRes, err := AnalyzeTrack(filepath.Join(tmpDir, "missing.flac"))
	if err != nil {
		t.Fatalf("unexpected error analyzing missing file: %v", err)
	}
	if missingRes.Score != 0 {
		t.Errorf("expected score 0 for missing file, got %v", missingRes.Score)
	}
}

type mockRecordCollectionClient struct {
	pbrc.RecordCollectionServiceClient
	getRecordFunc func(ctx context.Context, in *pbrc.GetRecordRequest, opts ...grpc.CallOption) (*pbrc.GetRecordResponse, error)
}

func (m *mockRecordCollectionClient) GetRecord(ctx context.Context, in *pbrc.GetRecordRequest, opts ...grpc.CallOption) (*pbrc.GetRecordResponse, error) {
	if m.getRecordFunc != nil {
		return m.getRecordFunc(ctx, in, opts...)
	}
	return nil, status.Errorf(codes.NotFound, "not found")
}

func TestKeyedMutex(t *testing.T) {
	km := &KeyedMutex{}
	lock1 := km.GetLock(123)
	if lock1 == nil {
		t.Fatalf("expected non-nil lock for release 123")
	}

	lock1Again := km.GetLock(123)
	if lock1 != lock1Again {
		t.Fatalf("expected identical lock instance for the same release ID")
	}

	lock2 := km.GetLock(456)
	if lock2 == nil {
		t.Fatalf("expected non-nil lock for release 456")
	}
	if lock1 == lock2 {
		t.Fatalf("expected distinct lock instances for different release IDs")
	}
}

func TestQualityServerGetQualityCacheHit(t *testing.T) {
	tempDir := t.TempDir()
	releaseID := int64(1001)

	// Create dummy FLAC file
	relDir := filepath.Join(tempDir, fmt.Sprintf("%d", releaseID))
	if err := os.MkdirAll(relDir, 0755); err != nil {
		t.Fatalf("failed to create release dir: %v", err)
	}
	flacPath := filepath.Join(relDir, "1001_track_001.flac")
	if err := os.WriteFile(flacPath, []byte("fake flac data"), 0644); err != nil {
		t.Fatalf("failed to write flac: %v", err)
	}
	info, err := os.Stat(flacPath)
	if err != nil {
		t.Fatalf("failed to stat flac: %v", err)
	}

	summary := &QualitySummary{
		ReleaseID:         releaseID,
		Version:           CurrentScoringVersion,
		Score:             92,
		CompletenessScore: 50,
		CleanlinessScore:  42,
		ExpectedTracks:    1,
		FoundTracks:       1,
		LastEvaluated:     time.Now().UTC(),
		Tracks: map[string]TrackQuality{
			"1001_track_001.flac": {
				Filename:  "1001_track_001.flac",
				SizeBytes: info.Size(),
				ModTime:   info.ModTime(),
				Score:     42,
			},
		},
	}
	if err := WriteQualitySummary(tempDir, releaseID, summary); err != nil {
		t.Fatalf("failed to write summary: %v", err)
	}

	// rcClient should not be called on cache hit
	mockClient := &mockRecordCollectionClient{
		getRecordFunc: func(ctx context.Context, in *pbrc.GetRecordRequest, opts ...grpc.CallOption) (*pbrc.GetRecordResponse, error) {
			t.Fatalf("rcClient.GetRecord should not be invoked on cache hit")
			return nil, status.Errorf(codes.Internal, "unexpected call")
		},
	}

	server := NewQualityServer(tempDir, mockClient)
	resp, err := server.GetQuality(context.Background(), &pb.GetQualityRequest{ReleaseId: releaseID})
	if err != nil {
		t.Fatalf("GetQuality returned unexpected error: %v", err)
	}
	if resp.GetScore() != 92 {
		t.Errorf("expected cached score 92, got %d", resp.GetScore())
	}
}

func TestQualityServerGetQualityEvaluation(t *testing.T) {
	tempDir := t.TempDir()
	releaseID := int64(2002)

	relDir := filepath.Join(tempDir, fmt.Sprintf("%d", releaseID))
	if err := os.MkdirAll(relDir, 0755); err != nil {
		t.Fatalf("failed to create release dir: %v", err)
	}

	// Create real FLAC audio file using sox
	wavFile := filepath.Join(relDir, "temp.wav")
	cmd := exec.Command("sox", "-n", "-r", "44100", "-c", "2", wavFile, "synth", "0.1", "sine", "1000", "vol", "-3dB")
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to generate wav: %v", err)
	}
	flacPath := filepath.Join(relDir, "2002-2026-09-25_track_001.flac")
	cmdFlac := exec.Command("flac", "--best", wavFile, "-o", flacPath)
	if err := cmdFlac.Run(); err != nil {
		t.Fatalf("failed to convert flac: %v", err)
	}
	os.Remove(wavFile)

	mockClient := &mockRecordCollectionClient{
		getRecordFunc: func(ctx context.Context, in *pbrc.GetRecordRequest, opts ...grpc.CallOption) (*pbrc.GetRecordResponse, error) {
			if in.GetReleaseId() != int32(releaseID) {
				return nil, status.Errorf(codes.NotFound, "wrong release id")
			}
			return &pbrc.GetRecordResponse{
				Record: &pbrc.Record{
					Release: &pbgd.Release{
						Id:             int32(releaseID),
						FormatQuantity: 1,
						Tracklist: []*pbgd.Track{
							{Title: "Track 1", Position: "A1"},
						},
					},
				},
			}, nil
		},
	}

	server := NewQualityServer(tempDir, mockClient)
	resp, err := server.GetQuality(context.Background(), &pb.GetQualityRequest{ReleaseId: releaseID})
	if err != nil {
		t.Fatalf("GetQuality evaluation failed: %v", err)
	}
	if resp.GetScore() <= 0 || resp.GetScore() > 100 {
		t.Errorf("expected score between 1 and 100, got %d", resp.GetScore())
	}

	// Verify quality.json was persisted
	savedSummary, err := ReadQualitySummary(tempDir, releaseID)
	if err != nil {
		t.Fatalf("failed to read persisted quality.json: %v", err)
	}
	if savedSummary.Version != CurrentScoringVersion {
		t.Errorf("saved version mismatch: got %d, want %d", savedSummary.Version, CurrentScoringVersion)
	}
	if savedSummary.Score != resp.GetScore() {
		t.Errorf("saved score mismatch: got %d, want %d", savedSummary.Score, resp.GetScore())
	}
	if savedSummary.ExpectedTracks != 1 || savedSummary.FoundTracks != 1 {
		t.Errorf("unexpected tracks count in summary: expected=%d found=%d", savedSummary.ExpectedTracks, savedSummary.FoundTracks)
	}
}

func TestQualityServerGetQualityConcurrencySameRelease(t *testing.T) {
	tempDir := t.TempDir()
	releaseID := int64(3003)

	relDir := filepath.Join(tempDir, fmt.Sprintf("%d", releaseID))
	os.MkdirAll(relDir, 0755)
	flacPath := filepath.Join(relDir, "3003_track_001.flac")
	os.WriteFile(flacPath, []byte("data"), 0644)

	var activeCalls int32
	var maxConcurrentCalls int32

	mockClient := &mockRecordCollectionClient{
		getRecordFunc: func(ctx context.Context, in *pbrc.GetRecordRequest, opts ...grpc.CallOption) (*pbrc.GetRecordResponse, error) {
			curr := atomic.AddInt32(&activeCalls, 1)
			defer atomic.AddInt32(&activeCalls, -1)

			for {
				max := atomic.LoadInt32(&maxConcurrentCalls)
				if curr <= max {
					break
				}
				if atomic.CompareAndSwapInt32(&maxConcurrentCalls, max, curr) {
					break
				}
			}

			time.Sleep(50 * time.Millisecond)

			return &pbrc.GetRecordResponse{
				Record: &pbrc.Record{
					Release: &pbgd.Release{
						Id:             int32(releaseID),
						FormatQuantity: 1,
						Tracklist: []*pbgd.Track{
							{Title: "Track 1", Position: "1"},
						},
					},
				},
			}, nil
		},
	}

	server := NewQualityServer(tempDir, mockClient)

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = server.GetQuality(context.Background(), &pb.GetQualityRequest{ReleaseId: releaseID})
		}()
	}
	wg.Wait()

	if maxConcurrentCalls > 1 {
		t.Errorf("expected sequential execution for same release_id, but max concurrency was %d", maxConcurrentCalls)
	}
}

func TestQualityServerGetQualityConcurrencyDistinctReleases(t *testing.T) {
	tempDir := t.TempDir()
	release1 := int64(4001)
	release2 := int64(4002)

	for _, rel := range []int64{release1, release2} {
		relDir := filepath.Join(tempDir, fmt.Sprintf("%d", rel))
		os.MkdirAll(relDir, 0755)
		os.WriteFile(filepath.Join(relDir, fmt.Sprintf("%d_track_001.flac", rel)), []byte("data"), 0644)
	}

	startedRelease1 := make(chan struct{})
	release1Block := make(chan struct{})

	mockClient := &mockRecordCollectionClient{
		getRecordFunc: func(ctx context.Context, in *pbrc.GetRecordRequest, opts ...grpc.CallOption) (*pbrc.GetRecordResponse, error) {
			if in.GetReleaseId() == int32(release1) {
				close(startedRelease1)
				<-release1Block
			}
			return &pbrc.GetRecordResponse{
				Record: &pbrc.Record{
					Release: &pbgd.Release{
						Id:             in.GetReleaseId(),
						FormatQuantity: 1,
						Tracklist: []*pbgd.Track{
							{Title: "Track 1", Position: "1"},
						},
					},
				},
			}, nil
		},
	}

	server := NewQualityServer(tempDir, mockClient)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = server.GetQuality(context.Background(), &pb.GetQualityRequest{ReleaseId: release1})
	}()

	select {
	case <-startedRelease1:
	case <-time.After(2 * time.Second):
		t.Fatalf("release 1 did not reach recordcollection client")
	}

	// Call release2 while release1 is blocked inside GetQuality
	done2 := make(chan struct{})
	go func() {
		_, _ = server.GetQuality(context.Background(), &pb.GetQualityRequest{ReleaseId: release2})
		close(done2)
	}()

	select {
	case <-done2:
		// release2 completed concurrently while release1 was blocked!
	case <-time.After(2 * time.Second):
		t.Fatalf("release 2 was blocked by release 1; distinct releases must execute concurrently")
	}

	close(release1Block)
	wg.Wait()
}

func TestQualityServerGetQualityErrors(t *testing.T) {
	tempDir := t.TempDir()

	mockClient := &mockRecordCollectionClient{
		getRecordFunc: func(ctx context.Context, in *pbrc.GetRecordRequest, opts ...grpc.CallOption) (*pbrc.GetRecordResponse, error) {
			if in.GetReleaseId() == 9999 {
				return nil, status.Errorf(codes.NotFound, "release 9999 not found")
			}
			return &pbrc.GetRecordResponse{
				Record: &pbrc.Record{
					Release: &pbgd.Release{
						Id:             in.GetReleaseId(),
						FormatQuantity: 1,
						Tracklist: []*pbgd.Track{
							{Title: "Track 1", Position: "1"},
						},
					},
				},
			}, nil
		},
	}

	server := NewQualityServer(tempDir, mockClient)

	// 1. Invalid release ID
	_, err := server.GetQuality(context.Background(), &pb.GetQualityRequest{ReleaseId: 0})
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument for releaseId=0, got %v", err)
	}

	_, err = server.GetQuality(context.Background(), &pb.GetQualityRequest{ReleaseId: -5})
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument for releaseId=-5, got %v", err)
	}

	// 2. Release not found in recordcollection
	_, err = server.GetQuality(context.Background(), &pb.GetQualityRequest{ReleaseId: 9999})
	if status.Code(err) != codes.NotFound {
		t.Errorf("expected NotFound when release not in recordcollection, got %v", err)
	}

	// 3. No FLAC tracks found on disk
	releaseID := int64(8888)
	// Directory empty
	_, err = server.GetQuality(context.Background(), &pb.GetQualityRequest{ReleaseId: releaseID})
	if status.Code(err) != codes.NotFound {
		t.Errorf("expected NotFound when no FLAC files found on disk, got %v", err)
	}
}

func TestCalculateTrackCleanliness(t *testing.T) {
	// Nil stats returns 0
	if score := CalculateTrackCleanliness(nil, false); score != 0 {
		t.Errorf("expected score 0 for nil stats, got %d", score)
	}

	t.Run("DynamicRangeDeductions", func(t *testing.T) {
		tests := []struct {
			name           string
			peakDb         float64
			rmsDb          float64
			expectedScore  int32
			expectedDeduct int32
		}{
			{
				name:           "dynamicRange >= 15.0 dB (no deduction)",
				peakDb:         -1.0,
				rmsDb:          -16.0, // DR = 15.0
				expectedScore:  50,
				expectedDeduct: 0,
			},
			{
				name:           "dynamicRange > 15.0 dB (no deduction)",
				peakDb:         -1.0,
				rmsDb:          -20.0, // DR = 19.0
				expectedScore:  50,
				expectedDeduct: 0,
			},
			{
				name:           "dynamicRange == 14.0 dB (5 pt minimum deduction)",
				peakDb:         -1.0,
				rmsDb:          -15.0, // DR = 14.0, (15-14)*2 = 2 clamped to 5
				expectedScore:  45,
				expectedDeduct: 5,
			},
			{
				name:           "dynamicRange == 10.0 dB (10 pt deduction)",
				peakDb:         -1.0,
				rmsDb:          -11.0, // DR = 10.0, (15-10)*2 = 10
				expectedScore:  40,
				expectedDeduct: 10,
			},
			{
				name:           "dynamicRange == 4.0 dB (20 pt maximum deduction)",
				peakDb:         -1.0,
				rmsDb:          -5.0, // DR = 4.0, (15-4)*2 = 22 clamped to 20
				expectedScore:  30,
				expectedDeduct: 20,
			},
			{
				name:           "dynamicRange <= 0.0 dB (20 pt maximum deduction)",
				peakDb:         -1.0,
				rmsDb:          -1.0, // DR = 0.0, (15-0)*2 = 30 clamped to 20
				expectedScore:  30,
				expectedDeduct: 20,
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				stats := &SoxStats{
					PeakDb:       tc.peakDb,
					RmsDb:        tc.rmsDb,
					ClippedCount: 0,
				}
				score := CalculateTrackCleanliness(stats, false)
				if score != tc.expectedScore {
					t.Errorf("expected score %d (deduction %d), got %d", tc.expectedScore, tc.expectedDeduct, score)
				}
			})
		}
	})

	t.Run("SilencePenalty", func(t *testing.T) {
		statsClean := &SoxStats{
			PeakDb:       -1.0,
			RmsDb:        -17.0, // DR = 16.0, no DR deduction
			ClippedCount: 0,
		}

		// hasLongSilence == false: 0 deduction
		scoreNoSilence := CalculateTrackCleanliness(statsClean, false)
		if scoreNoSilence != 50 {
			t.Errorf("expected score 50 when hasLongSilence is false, got %d", scoreNoSilence)
		}

		// hasLongSilence == true: deducts 10 points
		scoreSilence := CalculateTrackCleanliness(statsClean, true)
		if scoreSilence != 40 {
			t.Errorf("expected score 40 when hasLongSilence is true, got %d", scoreSilence)
		}
	})

	t.Run("PureSilenceTrackPenalty", func(t *testing.T) {
		// Pure silence (PeakDb <= -100 or -inf)
		// Combines max dynamic range deduction (20) and silence penalty (10)
		// Base: 50 - 20 (max DR) - 10 (silence) = 20
		statsPureSilence1 := &SoxStats{
			PeakDb:       -100.0,
			RmsDb:        -100.0,
			ClippedCount: 0,
		}
		score1 := CalculateTrackCleanliness(statsPureSilence1, true)
		if score1 != 20 {
			t.Errorf("expected score 20 for pure silence with long silence, got %d", score1)
		}

		statsPureSilenceInf := &SoxStats{
			PeakDb:       math.Inf(-1),
			RmsDb:        math.Inf(-1),
			ClippedCount: 0,
		}
		scoreInf := CalculateTrackCleanliness(statsPureSilenceInf, true)
		if scoreInf != 20 {
			t.Errorf("expected score 20 for -inf pure silence with long silence, got %d", scoreInf)
		}

		// Pure silence without long silence flag: 50 - 20 = 30
		scoreNoSilenceFlag := CalculateTrackCleanliness(statsPureSilence1, false)
		if scoreNoSilenceFlag != 30 {
			t.Errorf("expected score 30 for pure silence without long silence flag, got %d", scoreNoSilenceFlag)
		}
	})

	t.Run("ScoreClamping", func(t *testing.T) {
		// Test lower bound clamping to 0:
		// Base: 50
		// Severe clipping: -15 (Pk >= -0.01 && ClippedCount > 0), -15 (ClippedCount >= 15000)
		// Dynamic range deduction: -20
		// Long silence: -10
		// Total deductions: 15 + 15 + 20 + 10 = 60 => score -10, clamped to 0
		statsSevere := &SoxStats{
			PeakDb:       0.0,
			RmsDb:        0.0,
			ClippedCount: 20000,
		}
		scoreClampedZero := CalculateTrackCleanliness(statsSevere, true)
		if scoreClampedZero != 0 {
			t.Errorf("expected clamped score 0, got %d", scoreClampedZero)
		}

		// Test upper bound clamping to 50:
		statsClean := &SoxStats{
			PeakDb:       -1.0,
			RmsDb:        -20.0,
			ClippedCount: 0,
		}
		scoreClampedMax := CalculateTrackCleanliness(statsClean, false)
		if scoreClampedMax != 50 {
			t.Errorf("expected clamped score 50, got %d", scoreClampedMax)
		}
	})
}

func TestDetectLongSilence(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Track with a 6-second silence interval: triggers true
	t1 := filepath.Join(tmpDir, "tone1.wav")
	s6 := filepath.Join(tmpDir, "silence6.wav")
	t2 := filepath.Join(tmpDir, "tone2.wav")
	if err := exec.Command("sox", "-n", "-r", "44100", "-c", "2", t1, "synth", "6.0", "sine", "1000", "vol", "-6dB").Run(); err != nil {
		t.Fatalf("failed to create tone1: %v", err)
	}
	if err := exec.Command("sox", "-n", "-r", "44100", "-c", "2", s6, "trim", "0", "6.0").Run(); err != nil {
		t.Fatalf("failed to create silence6: %v", err)
	}
	if err := exec.Command("sox", "-n", "-r", "44100", "-c", "2", t2, "synth", "6.0", "sine", "1000", "vol", "-6dB").Run(); err != nil {
		t.Fatalf("failed to create tone2: %v", err)
	}
	trackWith6sSilence := filepath.Join(tmpDir, "track_6s_silence.flac")
	if err := exec.Command("sox", t1, s6, t2, trackWith6sSilence).Run(); err != nil {
		t.Fatalf("failed to combine track with 6s silence: %v", err)
	}

	hasLongSilence, err := DetectLongSilence(trackWith6sSilence)
	if err != nil {
		t.Fatalf("unexpected error from DetectLongSilence on 6s silence track: %v", err)
	}
	if !hasLongSilence {
		t.Errorf("expected DetectLongSilence to return true for 6-second silence interval, got false")
	}

	// 2. Track with a 3-second silence interval: triggers false
	s3 := filepath.Join(tmpDir, "silence3.wav")
	if err := exec.Command("sox", "-n", "-r", "44100", "-c", "2", s3, "trim", "0", "3.0").Run(); err != nil {
		t.Fatalf("failed to create silence3: %v", err)
	}
	trackWith3sSilence := filepath.Join(tmpDir, "track_3s_silence.flac")
	if err := exec.Command("sox", t1, s3, t2, trackWith3sSilence).Run(); err != nil {
		t.Fatalf("failed to combine track with 3s silence: %v", err)
	}

	hasLongSilence3s, err := DetectLongSilence(trackWith3sSilence)
	if err != nil {
		t.Fatalf("unexpected error from DetectLongSilence on 3s silence track: %v", err)
	}
	if hasLongSilence3s {
		t.Errorf("expected DetectLongSilence to return false for 3-second silence interval, got true")
	}

	// 3. Track under 5 seconds total duration: triggers false
	trackShort := filepath.Join(tmpDir, "track_under_5s.flac")
	if err := exec.Command("sox", "-n", "-r", "44100", "-c", "2", trackShort, "synth", "4.0", "sine", "1000", "vol", "-6dB").Run(); err != nil {
		t.Fatalf("failed to create short track: %v", err)
	}

	hasLongSilenceShort, err := DetectLongSilence(trackShort)
	if err != nil {
		t.Fatalf("unexpected error from DetectLongSilence on track under 5s: %v", err)
	}
	if hasLongSilenceShort {
		t.Errorf("expected DetectLongSilence to return false for track under 5s, got true")
	}

	// 4. Pure silence track: triggers true
	trackPureSilence := filepath.Join(tmpDir, "track_pure_silence.flac")
	if err := exec.Command("sox", "-n", "-r", "44100", "-c", "2", trackPureSilence, "trim", "0", "8.0").Run(); err != nil {
		t.Fatalf("failed to create pure silence track: %v", err)
	}

	hasLongSilencePure, err := DetectLongSilence(trackPureSilence)
	if err != nil {
		t.Fatalf("unexpected error from DetectLongSilence on pure silence track: %v", err)
	}
	if !hasLongSilencePure {
		t.Errorf("expected DetectLongSilence to return true for pure silence track, got false")
	}
}

func TestAnalyzeTrack(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Verifies correct population of DynamicRange, HasLongSilence, and Score for clean audio without silence
	cleanFile := filepath.Join(tmpDir, "clean.flac")
	if err := exec.Command("sox", "-n", "-r", "44100", "-c", "2", cleanFile, "synth", "0.1", "sine", "1000", "pad", "0", "1.5", "repeat", "5", "vol", "-3dB").Run(); err != nil {
		t.Fatalf("failed to create clean audio: %v", err)
	}

	tqClean, err := AnalyzeTrack(cleanFile)
	if err != nil {
		t.Fatalf("unexpected error analyzing clean track: %v", err)
	}
	if tqClean.HasLongSilence {
		t.Errorf("expected HasLongSilence=false for clean track, got true")
	}
	if tqClean.DynamicRange < 0 {
		t.Errorf("expected DynamicRange >= 0, got %v", tqClean.DynamicRange)
	}
	if tqClean.Score < 45 || tqClean.Score > 50 {
		t.Errorf("expected Score between 45 and 50 for clean track, got %d", tqClean.Score)
	}

	// 2. Verifies correct population of DynamicRange, HasLongSilence, and Score for track with 6-second silence
	s6 := filepath.Join(tmpDir, "s6.wav")
	exec.Command("sox", "-n", "-r", "44100", "-c", "2", s6, "trim", "0", "6.0").Run()
	silenceFile := filepath.Join(tmpDir, "silence.flac")
	if err := exec.Command("sox", cleanFile, s6, cleanFile, silenceFile).Run(); err != nil {
		t.Fatalf("failed to create silence track: %v", err)
	}

	tqSilence, err := AnalyzeTrack(silenceFile)
	if err != nil {
		t.Fatalf("unexpected error analyzing silence track: %v", err)
	}
	if !tqSilence.HasLongSilence {
		t.Errorf("expected HasLongSilence=true for track with 6s silence, got false")
	}
	if tqSilence.Score > tqClean.Score-10 {
		t.Errorf("expected Score with long silence to be penalized by at least 10 pts (clean=%d, silence=%d)", tqClean.Score, tqSilence.Score)
	}

	// 3. Verifies fault tolerance on missing or corrupted FLAC files
	missingFile := filepath.Join(tmpDir, "nonexistent.flac")
	tqMissing, err := AnalyzeTrack(missingFile)
	if err != nil {
		t.Fatalf("unexpected error on missing file: %v", err)
	}
	if tqMissing.Score != 0 {
		t.Errorf("expected Score=0 for missing file, got %d", tqMissing.Score)
	}

	corruptFile := filepath.Join(tmpDir, "corrupted.flac")
	if err := os.WriteFile(corruptFile, []byte("NOT_A_VALID_FLAC_DATA"), 0644); err != nil {
		t.Fatalf("failed to write corrupted file: %v", err)
	}
	tqCorrupt, err := AnalyzeTrack(corruptFile)
	if err != nil {
		t.Fatalf("unexpected error on corrupted file: %v", err)
	}
	if tqCorrupt.Score != 0 {
		t.Errorf("expected Score=0 for corrupted file, got %d", tqCorrupt.Score)
	}
}

func TestRunAwareDiskScoring(t *testing.T) {
	tmpDir := t.TempDir()

	// Create clean tracks for Run 2
	cleanTrack1 := filepath.Join(tmpDir, "123_1-2026-02-01_clean_track_1.flac")
	if err := exec.Command("sox", "-n", "-r", "44100", "-c", "2", cleanTrack1, "synth", "0.1", "sine", "1000", "pad", "0", "1.5", "repeat", "5", "vol", "-3dB").Run(); err != nil {
		t.Fatalf("failed to create clean audio 1: %v", err)
	}
	cleanTrack2 := filepath.Join(tmpDir, "123_1-2026-02-01_clean_track_2.flac")
	if err := exec.Command("sox", "-n", "-r", "44100", "-c", "2", cleanTrack2, "synth", "0.1", "sine", "1000", "pad", "0", "1.5", "repeat", "5", "vol", "-3dB").Run(); err != nil {
		t.Fatalf("failed to create clean audio 2: %v", err)
	}

	// Create corrupt track for Run 1
	corruptTrack := filepath.Join(tmpDir, "123_1-2026-01-01_bad_track_1.flac")
	if err := os.WriteFile(corruptTrack, []byte("NOT_A_VALID_FLAC"), 0644); err != nil {
		t.Fatalf("failed to create corrupt track: %v", err)
	}

	// Run 1: 1 corrupt track out of 2 expected tracks (completeness: 25, cleanliness: 0 -> total: 25)
	// Run 2: 2 clean tracks out of 2 expected tracks (completeness: 50, cleanliness: ~45-50 -> total: ~95-100)
	diskRuns := map[string][]string{
		"2026-01-01": {corruptTrack},
		"2026-02-01": {cleanTrack1, cleanTrack2},
	}

	summary, trackMap, _, err := EvaluateDiskRuns(1, diskRuns, 2)
	if err != nil {
		t.Fatalf("unexpected error evaluating disk runs: %v", err)
	}

	if summary == nil {
		t.Fatalf("expected non-nil summary")
	}
	if summary.Disk != 1 {
		t.Errorf("expected disk 1, got %d", summary.Disk)
	}
	if summary.BestRipDate != "2026-02-01" {
		t.Errorf("expected BestRipDate 2026-02-01, got %s", summary.BestRipDate)
	}
	if summary.Score < 90 {
		t.Errorf("expected winning score >= 90, got %d", summary.Score)
	}
	if len(trackMap) != 2 {
		t.Errorf("expected 2 tracks in winning trackMap, got %d", len(trackMap))
	}
}

func TestRunTieBreaking(t *testing.T) {
	tmpDir := t.TempDir()

	track1 := filepath.Join(tmpDir, "123_1-2026-01-01_track_1.flac")
	if err := exec.Command("sox", "-n", "-r", "44100", "-c", "2", track1, "synth", "0.1", "sine", "1000", "pad", "0", "1.5", "repeat", "5", "vol", "-3dB").Run(); err != nil {
		t.Fatalf("failed to create audio: %v", err)
	}

	track2 := filepath.Join(tmpDir, "123_1-2026-02-01_track_1.flac")
	data, err := os.ReadFile(track1)
	if err != nil {
		t.Fatalf("failed to read track1: %v", err)
	}
	if err := os.WriteFile(track2, data, 0644); err != nil {
		t.Fatalf("failed to write track2: %v", err)
	}

	// Given Run 1 (2026-01-01, equal score) and Run 2 (2026-02-01, equal score)
	diskRuns := map[string][]string{
		"2026-01-01": {track1},
		"2026-02-01": {track2},
	}

	summary, trackMap, _, err := EvaluateDiskRuns(1, diskRuns, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summary.BestRipDate != "2026-02-01" {
		t.Errorf("expected winning run 2026-02-01 (most recent date), got %s", summary.BestRipDate)
	}
	if len(trackMap) != 1 {
		t.Errorf("expected 1 track in winning trackMap, got %d", len(trackMap))
	}
}

func TestEvaluateDiskRunsEmpty(t *testing.T) {
	summary, trackMap, _, err := EvaluateDiskRuns(2, map[string][]string{}, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summary.Disk != 2 {
		t.Errorf("expected disk 2, got %d", summary.Disk)
	}
	if summary.BestRipDate != "" {
		t.Errorf("expected empty BestRipDate, got %s", summary.BestRipDate)
	}
	if summary.Score != 0 {
		t.Errorf("expected score 0, got %d", summary.Score)
	}
	if len(trackMap) != 0 {
		t.Errorf("expected empty trackMap, got %v", trackMap)
	}
}

func TestEvaluateRunCompletenessAndCleanliness(t *testing.T) {
	// Test error when expectedTracks <= 0
	_, _, _, _, err := EvaluateRunCompletenessAndCleanliness([]string{"dummy.flac"}, 0)
	if err == nil {
		t.Errorf("expected error for expectedTracks <= 0, got nil")
	}

	// Test empty tracks returns error from CalculateCompletenessScore
	_, _, _, _, err = EvaluateRunCompletenessAndCleanliness([]string{}, 2)
	if err == nil {
		t.Errorf("expected error for empty tracks, got nil")
	}
}

func TestMultiDiskBottleneck(t *testing.T) {
	tempDir := t.TempDir()
	releaseID := int64(7001)

	relDir := filepath.Join(tempDir, fmt.Sprintf("%d", releaseID))
	if err := os.MkdirAll(relDir, 0755); err != nil {
		t.Fatalf("failed to create release dir: %v", err)
	}

	// Disk 1 track (expected 1 track -> 50 completeness + clean audio -> ~90-100 score)
	disk1Track := filepath.Join(relDir, "7001_1-2026-09-25_track_01.flac")
	if err := exec.Command("sox", "-n", "-r", "44100", "-c", "2", disk1Track, "synth", "0.1", "sine", "1000", "pad", "0", "1.5", "repeat", "5", "vol", "-3dB").Run(); err != nil {
		t.Fatalf("failed to create disk 1 audio: %v", err)
	}

	// Disk 2 corrupt track: 1 corrupt track out of 2 expected tracks -> completeness 25, cleanliness 0 -> score 25
	disk2Track := filepath.Join(relDir, "7001_2-2026-09-25_track_01.flac")
	if err := os.WriteFile(disk2Track, []byte("NOT_A_VALID_FLAC_AUDIO"), 0644); err != nil {
		t.Fatalf("failed to create disk 2 corrupt file: %v", err)
	}

	mockClient := &mockRecordCollectionClient{
		getRecordFunc: func(ctx context.Context, in *pbrc.GetRecordRequest, opts ...grpc.CallOption) (*pbrc.GetRecordResponse, error) {
			return &pbrc.GetRecordResponse{
				Record: &pbrc.Record{
					Release: &pbgd.Release{
						Id:             int32(releaseID),
						FormatQuantity: 2,
						Tracklist: []*pbgd.Track{
							{Title: "Disk 1 Track 1", Position: "1-1"},
							{Title: "Disk 2 Track 1", Position: "2-1"},
							{Title: "Disk 2 Track 2", Position: "2-2"},
						},
					},
				},
			}, nil
		},
	}

	server := NewQualityServer(tempDir, mockClient)
	resp, err := server.GetQuality(context.Background(), &pb.GetQualityRequest{ReleaseId: releaseID})
	if err != nil {
		t.Fatalf("GetQuality failed: %v", err)
	}

	if len(resp.GetDiskQualities()) != 2 {
		t.Fatalf("expected 2 disk qualities, got %d", len(resp.GetDiskQualities()))
	}

	d1 := resp.GetDiskQualities()[0]
	d2 := resp.GetDiskQualities()[1]

	if d1.GetDisk() != 1 || d2.GetDisk() != 2 {
		t.Errorf("unexpected disk numbers: d1=%d, d2=%d", d1.GetDisk(), d2.GetDisk())
	}

	if d1.GetScore() <= d2.GetScore() {
		t.Errorf("expected disk 1 score (%d) > disk 2 score (%d)", d1.GetScore(), d2.GetScore())
	}

	// Bottleneck composite score must equal min(disk1, disk2)
	expectedScore := d2.GetScore()
	if resp.GetScore() != expectedScore {
		t.Errorf("expected bottleneck release score %d, got %d", expectedScore, resp.GetScore())
	}
}

func TestMissingDiskHandling(t *testing.T) {
	tempDir := t.TempDir()
	releaseID := int64(7002)

	relDir := filepath.Join(tempDir, fmt.Sprintf("%d", releaseID))
	if err := os.MkdirAll(relDir, 0755); err != nil {
		t.Fatalf("failed to create release dir: %v", err)
	}

	// 2-disk release with only Disk 1 recorded
	disk1Track := filepath.Join(relDir, "7002_1-2026-09-25_track_01.flac")
	if err := exec.Command("sox", "-n", "-r", "44100", "-c", "2", disk1Track, "synth", "0.1", "sine", "1000", "pad", "0", "1.5", "repeat", "5", "vol", "-3dB").Run(); err != nil {
		t.Fatalf("failed to create disk 1 audio: %v", err)
	}

	mockClient := &mockRecordCollectionClient{
		getRecordFunc: func(ctx context.Context, in *pbrc.GetRecordRequest, opts ...grpc.CallOption) (*pbrc.GetRecordResponse, error) {
			return &pbrc.GetRecordResponse{
				Record: &pbrc.Record{
					Release: &pbgd.Release{
						Id:             int32(releaseID),
						FormatQuantity: 2,
						Tracklist: []*pbgd.Track{
							{Title: "Disk 1 Track 1", Position: "1-1"},
							{Title: "Disk 2 Track 1", Position: "2-1"},
						},
					},
				},
			}, nil
		},
	}

	server := NewQualityServer(tempDir, mockClient)
	resp, err := server.GetQuality(context.Background(), &pb.GetQualityRequest{ReleaseId: releaseID})
	if err != nil {
		t.Fatalf("GetQuality failed: %v", err)
	}

	// Overall score must be 0 because Disk 2 is missing
	if resp.GetScore() != 0 {
		t.Errorf("expected overall score 0 for missing disk release, got %d", resp.GetScore())
	}

	if len(resp.GetDiskQualities()) != 2 {
		t.Fatalf("expected 2 disk qualities, got %d", len(resp.GetDiskQualities()))
	}

	d1 := resp.GetDiskQualities()[0]
	d2 := resp.GetDiskQualities()[1]

	if d1.GetDisk() != 1 || d1.GetScore() <= 0 {
		t.Errorf("expected disk 1 to have score > 0, got disk=%d score=%d", d1.GetDisk(), d1.GetScore())
	}

	if d2.GetDisk() != 2 || d2.GetScore() != 0 || d2.GetBestRipDate() != "" {
		t.Errorf("expected disk 2 to have score 0 and empty rip date, got disk=%d score=%d date=%q", d2.GetDisk(), d2.GetScore(), d2.GetBestRipDate())
	}
}

func TestSingleDiskNormalization(t *testing.T) {
	tempDir := t.TempDir()
	releaseID := int64(7003)

	relDir := filepath.Join(tempDir, fmt.Sprintf("%d", releaseID))
	if err := os.MkdirAll(relDir, 0755); err != nil {
		t.Fatalf("failed to create release dir: %v", err)
	}

	// Single-disk release with disk 0 in filename
	disk0Track := filepath.Join(relDir, "7003_0-2026-09-25_track_01.flac")
	if err := exec.Command("sox", "-n", "-r", "44100", "-c", "2", disk0Track, "synth", "0.1", "sine", "1000", "pad", "0", "1.5", "repeat", "5", "vol", "-3dB").Run(); err != nil {
		t.Fatalf("failed to create disk 0 audio: %v", err)
	}

	mockClient := &mockRecordCollectionClient{
		getRecordFunc: func(ctx context.Context, in *pbrc.GetRecordRequest, opts ...grpc.CallOption) (*pbrc.GetRecordResponse, error) {
			return &pbrc.GetRecordResponse{
				Record: &pbrc.Record{
					Release: &pbgd.Release{
						Id:             int32(releaseID),
						FormatQuantity: 1,
						Tracklist: []*pbgd.Track{
							{Title: "Track 1", Position: "1"},
						},
					},
				},
			}, nil
		},
	}

	server := NewQualityServer(tempDir, mockClient)
	resp, err := server.GetQuality(context.Background(), &pb.GetQualityRequest{ReleaseId: releaseID})
	if err != nil {
		t.Fatalf("GetQuality failed: %v", err)
	}

	if len(resp.GetDiskQualities()) != 1 {
		t.Fatalf("expected 1 disk quality, got %d", len(resp.GetDiskQualities()))
	}

	dq := resp.GetDiskQualities()[0]
	if dq.GetDisk() != 1 {
		t.Errorf("expected disk number normalized to 1, got %d", dq.GetDisk())
	}
	if dq.GetScore() <= 0 {
		t.Errorf("expected score > 0, got %d", dq.GetScore())
	}
	if resp.GetScore() != dq.GetScore() {
		t.Errorf("expected overall score %d matching disk 1 score, got %d", dq.GetScore(), resp.GetScore())
	}
}

func TestGetQualityCacheHitWithDisks(t *testing.T) {
	tempDir := t.TempDir()
	releaseID := int64(7004)

	relDir := filepath.Join(tempDir, fmt.Sprintf("%d", releaseID))
	if err := os.MkdirAll(relDir, 0755); err != nil {
		t.Fatalf("failed to create release dir: %v", err)
	}

	flac1 := filepath.Join(relDir, "7004_1-2026-09-25_track_01.flac")
	if err := os.WriteFile(flac1, []byte("audio1"), 0644); err != nil {
		t.Fatalf("failed to write flac1: %v", err)
	}
	flac2 := filepath.Join(relDir, "7004_2-2026-09-25_track_01.flac")
	if err := os.WriteFile(flac2, []byte("audio2"), 0644); err != nil {
		t.Fatalf("failed to write flac2: %v", err)
	}

	info1, _ := os.Stat(flac1)
	info2, _ := os.Stat(flac2)

	summary := &QualitySummary{
		ReleaseID:         releaseID,
		Version:           CurrentScoringVersion,
		Score:             75,
		CompletenessScore: 40,
		CleanlinessScore:  35,
		ExpectedTracks:    2,
		FoundTracks:       2,
		LastEvaluated:     time.Now().UTC(),
		Disks: []DiskQualitySummary{
			{
				Disk:        1,
				BestRipDate: "2026-09-20",
				Score:       90,
			},
			{
				Disk:        2,
				BestRipDate: "2026-09-25",
				Score:       75,
			},
		},
		Tracks: map[string]TrackQuality{
			"7004_1-2026-09-25_track_01.flac": {
				Filename:  "7004_1-2026-09-25_track_01.flac",
				SizeBytes: info1.Size(),
				ModTime:   info1.ModTime(),
				Score:     45,
			},
			"7004_2-2026-09-25_track_01.flac": {
				Filename:  "7004_2-2026-09-25_track_01.flac",
				SizeBytes: info2.Size(),
				ModTime:   info2.ModTime(),
				Score:     30,
			},
		},
	}

	if err := WriteQualitySummary(tempDir, releaseID, summary); err != nil {
		t.Fatalf("failed to write summary: %v", err)
	}

	mockClient := &mockRecordCollectionClient{
		getRecordFunc: func(ctx context.Context, in *pbrc.GetRecordRequest, opts ...grpc.CallOption) (*pbrc.GetRecordResponse, error) {
			t.Fatalf("rcClient should not be dialed on cache hit")
			return nil, status.Errorf(codes.Internal, "unexpected call")
		},
	}

	server := NewQualityServer(tempDir, mockClient)
	resp, err := server.GetQuality(context.Background(), &pb.GetQualityRequest{ReleaseId: releaseID})
	if err != nil {
		t.Fatalf("GetQuality failed: %v", err)
	}

	if resp.GetScore() != 75 {
		t.Errorf("expected score 75, got %d", resp.GetScore())
	}

	if len(resp.GetDiskQualities()) != 2 {
		t.Fatalf("expected 2 disk qualities from cache, got %d", len(resp.GetDiskQualities()))
	}

	if resp.GetDiskQualities()[0].GetDisk() != 1 || resp.GetDiskQualities()[0].GetScore() != 90 || resp.GetDiskQualities()[0].GetBestRipDate() != "2026-09-20" {
		t.Errorf("unexpected disk 1 from cache: %v", resp.GetDiskQualities()[0])
	}
	if resp.GetDiskQualities()[1].GetDisk() != 2 || resp.GetDiskQualities()[1].GetScore() != 75 || resp.GetDiskQualities()[1].GetBestRipDate() != "2026-09-25" {
		t.Errorf("unexpected disk 2 from cache: %v", resp.GetDiskQualities()[1])
	}
}



