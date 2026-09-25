package main

import (
	"context"
	"fmt"
	"os"
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
