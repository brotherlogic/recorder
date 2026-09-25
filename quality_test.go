package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pbgd "github.com/brotherlogic/godiscogs/proto"
	pbrc "github.com/brotherlogic/recordcollection/proto"
)

type mockRecordCollectionClient struct {
	pbrc.RecordCollectionServiceClient
	getRecordFunc func(ctx context.Context, in *pbrc.GetRecordRequest, opts ...grpc.CallOption) (*pbrc.GetRecordResponse, error)
}

func (m *mockRecordCollectionClient) GetRecord(ctx context.Context, in *pbrc.GetRecordRequest, opts ...grpc.CallOption) (*pbrc.GetRecordResponse, error) {
	if m.getRecordFunc != nil {
		return m.getRecordFunc(ctx, in, opts...)
	}
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func TestCompletenessScoringAlgorithm(t *testing.T) {
	tests := []struct {
		name          string
		found         int
		expected      int
		wantScore     int32
		wantErrCode   codes.Code
		expectingErr  bool
	}{
		{
			name:         "Exact track match (10 of 10)",
			found:        10,
			expected:     10,
			wantScore:    50,
			expectingErr: false,
		},
		{
			name:         "Partial tracks (5 of 10)",
			found:        5,
			expected:     10,
			wantScore:    25,
			expectingErr: false,
		},
		{
			name:         "Partial tracks (3 of 10)",
			found:        3,
			expected:     10,
			wantScore:    15,
			expectingErr: false,
		},
		{
			name:         "Excess tracks (12 of 10)",
			found:        12,
			expected:     10,
			wantScore:    30, // 50 - (12-10)*10 = 30
			expectingErr: false,
		},
		{
			name:         "Excess tracks heavy penalty (16 of 10)",
			found:        16,
			expected:     10,
			wantScore:    0, // max(0, 50 - 6*10) = 0
			expectingErr: false,
		},
		{
			name:         "0 tracks found",
			found:        0,
			expected:     10,
			wantScore:    0,
			wantErrCode:  codes.NotFound,
			expectingErr: true,
		},
		{
			name:         "0 expected tracks",
			found:        5,
			expected:     0,
			wantScore:    0,
			wantErrCode:  codes.NotFound,
			expectingErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			score, err := CalculateCompletenessScore(tc.found, tc.expected)
			if tc.expectingErr {
				if err == nil {
					t.Fatalf("CalculateCompletenessScore(%d, %d) expected error, got nil", tc.found, tc.expected)
				}
				if status.Code(err) != tc.wantErrCode {
					t.Fatalf("CalculateCompletenessScore(%d, %d) status code = %v, want %v", tc.found, tc.expected, status.Code(err), tc.wantErrCode)
				}
			} else {
				if err != nil {
					t.Fatalf("CalculateCompletenessScore(%d, %d) unexpected error: %v", tc.found, tc.expected, err)
				}
				if score != tc.wantScore {
					t.Fatalf("CalculateCompletenessScore(%d, %d) = %d, want %d", tc.found, tc.expected, score, tc.wantScore)
				}
			}
		})
	}
}

func TestScanFlacFiles(t *testing.T) {
	tmpDir := t.TempDir()
	releaseID := int64(12345)
	releaseDir := filepath.Join(tmpDir, fmt.Sprintf("%d", releaseID))
	err := os.MkdirAll(releaseDir, 0755)
	if err != nil {
		t.Fatalf("failed to create release dir: %v", err)
	}

	// Create dummy files
	track1 := filepath.Join(releaseDir, "12345_track_01.flac")
	track2 := filepath.Join(releaseDir, "12345_track_02.flac")
	nonTrack := filepath.Join(releaseDir, "other.txt")
	nonFlac := filepath.Join(releaseDir, "12345_track_03.wav")

	for _, f := range []string{track1, track2, nonTrack, nonFlac} {
		if err := os.WriteFile(f, []byte("dummy audio"), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}
	}

	files, err := ScanFlacFiles(tmpDir, releaseID)
	if err != nil {
		t.Fatalf("ScanFlacFiles returned error: %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("ScanFlacFiles found %d files, want 2", len(files))
	}

	if files[0] != track1 || files[1] != track2 {
		t.Fatalf("ScanFlacFiles returned %v, want [%s, %s]", files, track1, track2)
	}

	// Scan non-existent dir
	emptyFiles, err := ScanFlacFiles(tmpDir, 99999)
	if err != nil {
		t.Fatalf("ScanFlacFiles for non-existent dir returned error: %v", err)
	}
	if len(emptyFiles) != 0 {
		t.Fatalf("ScanFlacFiles for non-existent dir returned %d files, want 0", len(emptyFiles))
	}
}

func TestCalculateTotalExpectedTracks(t *testing.T) {
	// Single disk
	singleDisc := &pbgd.Release{
		FormatQuantity: 1,
		Tracklist: []*pbgd.Track{
			{Title: "Track 1"},
			{Title: "Track 2"},
			{Title: "Track 3"},
		},
	}
	if got := CalculateTotalExpectedTracks(singleDisc); got != 3 {
		t.Errorf("CalculateTotalExpectedTracks(singleDisc) = %d, want 3", got)
	}

	// Multi disk with positions
	multiDisc := &pbgd.Release{
		FormatQuantity: 2,
		Tracklist: []*pbgd.Track{
			{Title: "Disc 1 Track 1", Position: "A1"},
			{Title: "Disc 1 Track 2", Position: "A2"},
			{Title: "Disc 2 Track 1", Position: "C1"},
			{Title: "Disc 2 Track 2", Position: "C2"},
		},
	}
	if got := CalculateTotalExpectedTracks(multiDisc); got != 4 {
		t.Errorf("CalculateTotalExpectedTracks(multiDisc) = %d, want 4", got)
	}

	// Nil release
	if got := CalculateTotalExpectedTracks(nil); got != 0 {
		t.Errorf("CalculateTotalExpectedTracks(nil) = %d, want 0", got)
	}
}

func TestEvaluateCompleteness_ExactMatch(t *testing.T) {
	tmpDir := t.TempDir()
	releaseID := int64(100)
	releaseDir := filepath.Join(tmpDir, fmt.Sprintf("%d", releaseID))
	_ = os.MkdirAll(releaseDir, 0755)

	for i := 1; i <= 10; i++ {
		_ = os.WriteFile(filepath.Join(releaseDir, fmt.Sprintf("track_%02d.flac", i)), []byte("flac"), 0644)
	}

	tracks := make([]*pbgd.Track, 10)
	for i := 0; i < 10; i++ {
		tracks[i] = &pbgd.Track{Title: fmt.Sprintf("Track %d", i+1)}
	}

	client := &mockRecordCollectionClient{
		getRecordFunc: func(ctx context.Context, in *pbrc.GetRecordRequest, opts ...grpc.CallOption) (*pbrc.GetRecordResponse, error) {
			return &pbrc.GetRecordResponse{
				Record: &pbrc.Record{
					Release: &pbgd.Release{
						Id:             int32(releaseID),
						FormatQuantity: 1,
						Tracklist:      tracks,
					},
				},
			}, nil
		},
	}

	res, err := EvaluateCompleteness(context.Background(), client, tmpDir, releaseID)
	if err != nil {
		t.Fatalf("EvaluateCompleteness failed: %v", err)
	}
	if res.Score != 50 {
		t.Errorf("Score = %d, want 50", res.Score)
	}
	if res.ExpectedTracks != 10 {
		t.Errorf("ExpectedTracks = %d, want 10", res.ExpectedTracks)
	}
	if res.FoundTracks != 10 {
		t.Errorf("FoundTracks = %d, want 10", res.FoundTracks)
	}
	if len(res.Files) != 10 {
		t.Errorf("Files count = %d, want 10", len(res.Files))
	}
}

func TestEvaluateCompleteness_PartialTracks(t *testing.T) {
	tmpDir := t.TempDir()
	releaseID := int64(200)
	releaseDir := filepath.Join(tmpDir, fmt.Sprintf("%d", releaseID))
	_ = os.MkdirAll(releaseDir, 0755)

	for i := 1; i <= 5; i++ {
		_ = os.WriteFile(filepath.Join(releaseDir, fmt.Sprintf("track_%02d.flac", i)), []byte("flac"), 0644)
	}

	tracks := make([]*pbgd.Track, 10)
	for i := 0; i < 10; i++ {
		tracks[i] = &pbgd.Track{Title: fmt.Sprintf("Track %d", i+1)}
	}

	client := &mockRecordCollectionClient{
		getRecordFunc: func(ctx context.Context, in *pbrc.GetRecordRequest, opts ...grpc.CallOption) (*pbrc.GetRecordResponse, error) {
			return &pbrc.GetRecordResponse{
				Record: &pbrc.Record{
					Release: &pbgd.Release{
						Id:             int32(releaseID),
						FormatQuantity: 1,
						Tracklist:      tracks,
					},
				},
			}, nil
		},
	}

	res, err := EvaluateCompleteness(context.Background(), client, tmpDir, releaseID)
	if err != nil {
		t.Fatalf("EvaluateCompleteness failed: %v", err)
	}
	if res.Score != 25 {
		t.Errorf("Score = %d, want 25", res.Score)
	}
	if res.ExpectedTracks != 10 {
		t.Errorf("ExpectedTracks = %d, want 10", res.ExpectedTracks)
	}
	if res.FoundTracks != 5 {
		t.Errorf("FoundTracks = %d, want 5", res.FoundTracks)
	}
}

func TestEvaluateCompleteness_ExcessTracks(t *testing.T) {
	tmpDir := t.TempDir()
	releaseID := int64(300)
	releaseDir := filepath.Join(tmpDir, fmt.Sprintf("%d", releaseID))
	_ = os.MkdirAll(releaseDir, 0755)

	for i := 1; i <= 12; i++ {
		_ = os.WriteFile(filepath.Join(releaseDir, fmt.Sprintf("track_%02d.flac", i)), []byte("flac"), 0644)
	}

	tracks := make([]*pbgd.Track, 10)
	for i := 0; i < 10; i++ {
		tracks[i] = &pbgd.Track{Title: fmt.Sprintf("Track %d", i+1)}
	}

	client := &mockRecordCollectionClient{
		getRecordFunc: func(ctx context.Context, in *pbrc.GetRecordRequest, opts ...grpc.CallOption) (*pbrc.GetRecordResponse, error) {
			return &pbrc.GetRecordResponse{
				Record: &pbrc.Record{
					Release: &pbgd.Release{
						Id:             int32(releaseID),
						FormatQuantity: 1,
						Tracklist:      tracks,
					},
				},
			}, nil
		},
	}

	res, err := EvaluateCompleteness(context.Background(), client, tmpDir, releaseID)
	if err != nil {
		t.Fatalf("EvaluateCompleteness failed: %v", err)
	}
	if res.Score != 30 {
		t.Errorf("Score = %d, want 30", res.Score)
	}
	if res.ExpectedTracks != 10 {
		t.Errorf("ExpectedTracks = %d, want 10", res.ExpectedTracks)
	}
	if res.FoundTracks != 12 {
		t.Errorf("FoundTracks = %d, want 12", res.FoundTracks)
	}
}

func TestEvaluateCompleteness_ZeroTracksFound(t *testing.T) {
	tmpDir := t.TempDir()
	releaseID := int64(400)
	// Directory exists but has 0 flac files
	releaseDir := filepath.Join(tmpDir, fmt.Sprintf("%d", releaseID))
	_ = os.MkdirAll(releaseDir, 0755)

	tracks := make([]*pbgd.Track, 10)
	for i := 0; i < 10; i++ {
		tracks[i] = &pbgd.Track{Title: fmt.Sprintf("Track %d", i+1)}
	}

	client := &mockRecordCollectionClient{
		getRecordFunc: func(ctx context.Context, in *pbrc.GetRecordRequest, opts ...grpc.CallOption) (*pbrc.GetRecordResponse, error) {
			return &pbrc.GetRecordResponse{
				Record: &pbrc.Record{
					Release: &pbgd.Release{
						Id:             int32(releaseID),
						FormatQuantity: 1,
						Tracklist:      tracks,
					},
				},
			}, nil
		},
	}

	res, err := EvaluateCompleteness(context.Background(), client, tmpDir, releaseID)
	if err == nil {
		t.Fatalf("EvaluateCompleteness expected error for 0 tracks, got res: %v", res)
	}
	if status.Code(err) != codes.NotFound {
		t.Errorf("status.Code(err) = %v, want NotFound", status.Code(err))
	}
}

func TestEvaluateCompleteness_ReleaseNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	releaseID := int64(500)

	client := &mockRecordCollectionClient{
		getRecordFunc: func(ctx context.Context, in *pbrc.GetRecordRequest, opts ...grpc.CallOption) (*pbrc.GetRecordResponse, error) {
			return nil, status.Errorf(codes.NotFound, "release %d not found in recordcollection", releaseID)
		},
	}

	res, err := EvaluateCompleteness(context.Background(), client, tmpDir, releaseID)
	if err == nil {
		t.Fatalf("EvaluateCompleteness expected error for not found release, got: %v", res)
	}
	if status.Code(err) != codes.NotFound {
		t.Errorf("status.Code(err) = %v, want NotFound", status.Code(err))
	}
}

func TestEvaluateCompleteness_UpstreamUnavailable(t *testing.T) {
	tmpDir := t.TempDir()
	releaseID := int64(600)

	client := &mockRecordCollectionClient{
		getRecordFunc: func(ctx context.Context, in *pbrc.GetRecordRequest, opts ...grpc.CallOption) (*pbrc.GetRecordResponse, error) {
			return nil, status.Errorf(codes.Unavailable, "recordcollection service unavailable")
		},
	}

	res, err := EvaluateCompleteness(context.Background(), client, tmpDir, releaseID)
	if err == nil {
		t.Fatalf("EvaluateCompleteness expected error for unavailable service, got: %v", res)
	}
	if status.Code(err) != codes.Unavailable {
		t.Errorf("status.Code(err) = %v, want Unavailable", status.Code(err))
	}
}

func TestEvaluateCompleteness_DeadlineExceeded(t *testing.T) {
	tmpDir := t.TempDir()
	releaseID := int64(700)

	client := &mockRecordCollectionClient{
		getRecordFunc: func(ctx context.Context, in *pbrc.GetRecordRequest, opts ...grpc.CallOption) (*pbrc.GetRecordResponse, error) {
			return nil, status.Errorf(codes.DeadlineExceeded, "deadline exceeded")
		},
	}

	res, err := EvaluateCompleteness(context.Background(), client, tmpDir, releaseID)
	if err == nil {
		t.Fatalf("EvaluateCompleteness expected error for timeout, got: %v", res)
	}
	if status.Code(err) != codes.Unavailable {
		t.Errorf("status.Code(err) = %v, want Unavailable", status.Code(err))
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
