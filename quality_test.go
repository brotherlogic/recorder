package main

import (
	"context"
	"fmt"
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
	score := CalculateTrackCleanliness(stats)
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
	cleanScore := CalculateTrackCleanliness(cleanStats)

	clippedStats := &SoxStats{
		PeakDb:       0.0,
		RmsDb:        -18.0,
		ClippedCount: 5000,
	}
	clippedScore := CalculateTrackCleanliness(clippedStats)

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
	cleanScore := CalculateTrackCleanliness(cleanStats)

	noisyStats := &SoxStats{
		PeakDb:       -2.0,
		RmsDb:        -5.0, // Dynamic range only 3dB (elevated noise floor)
		ClippedCount: 0,
	}
	noisyScore := CalculateTrackCleanliness(noisyStats)

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
	flacPath := filepath.Join(relDir, "2002_track_001.flac")
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
