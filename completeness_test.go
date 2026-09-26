package main

import (
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCalculateCompletenessScore(t *testing.T) {
	tests := []struct {
		name          string
		found         int
		expected      int
		expectedScore int32
		expectErrCode *codes.Code
	}{
		// Error cases
		{
			name:          "Zero found tracks",
			found:         0,
			expected:      5,
			expectedScore: 0,
			expectErrCode: codePtr(codes.NotFound),
		},
		{
			name:          "Zero expected tracks",
			found:         5,
			expected:      0,
			expectedScore: 0,
			expectErrCode: codePtr(codes.NotFound),
		},
		{
			name:          "Negative expected tracks",
			found:         5,
			expected:      -1,
			expectedScore: 0,
			expectErrCode: codePtr(codes.NotFound),
		},
		// Single track edge case
		{
			name:          "Single track match",
			found:         1,
			expected:      1,
			expectedScore: 50,
			expectErrCode: nil,
		},
		// Discrete step curve cases (missing tracks)
		{
			name:          "0 missing tracks",
			found:         10,
			expected:      10,
			expectedScore: 50,
			expectErrCode: nil,
		},
		{
			name:          "1 missing track",
			found:         9,
			expected:      10,
			expectedScore: 25,
			expectErrCode: nil,
		},
		{
			name:          "2 missing tracks",
			found:         8,
			expected:      10,
			expectedScore: 10,
			expectErrCode: nil,
		},
		{
			name:          "3 missing tracks",
			found:         7,
			expected:      10,
			expectedScore: 5,
			expectErrCode: nil,
		},
		{
			name:          "4 missing tracks",
			found:         6,
			expected:      10,
			expectedScore: 0,
			expectErrCode: nil,
		},
		{
			name:          "5 missing tracks",
			found:         5,
			expected:      10,
			expectedScore: 0,
			expectErrCode: nil,
		},
		// Extra track cases
		{
			name:          "1 extra track",
			found:         11,
			expected:      10,
			expectedScore: 40,
			expectErrCode: nil,
		},
		{
			name:          "2 extra tracks",
			found:         12,
			expected:      10,
			expectedScore: 30,
			expectErrCode: nil,
		},
		{
			name:          "5 extra tracks",
			found:         15,
			expected:      10,
			expectedScore: 0,
			expectErrCode: nil,
		},
		{
			name:          "6 extra tracks floored",
			found:         16,
			expected:      10,
			expectedScore: 0,
			expectErrCode: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			score, err := CalculateCompletenessScore(tc.found, tc.expected)
			if tc.expectErrCode != nil {
				if err == nil {
					t.Fatalf("expected error code %v, got nil error", *tc.expectErrCode)
				}
				st, ok := status.FromError(err)
				if !ok {
					t.Fatalf("expected grpc status error, got: %v", err)
				}
				if st.Code() != *tc.expectErrCode {
					t.Fatalf("expected error code %v, got %v", *tc.expectErrCode, st.Code())
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if score != tc.expectedScore {
					t.Fatalf("score mismatch for found=%d, expected=%d: got %d, want %d", tc.found, tc.expected, score, tc.expectedScore)
				}
			}
		})
	}
}

func codePtr(c codes.Code) *codes.Code {
	return &c
}

func TestParseTrackFilename(t *testing.T) {
	tests := []struct {
		name          string
		filename      string
		releaseID     int64
		wantReleaseID int64
		wantDisk      int32
		wantDate      string
		wantTrackNum  int
		wantFilename  string
		wantPath      string
		wantErr       bool
	}{
		{
			name:          "Single-disk naming",
			filename:      "123-2026-09-25-something_track_01.flac",
			releaseID:     123,
			wantReleaseID: 123,
			wantDisk:      1,
			wantDate:      "2026-09-25",
			wantTrackNum:  1,
			wantFilename:  "123-2026-09-25-something_track_01.flac",
			wantPath:      "123-2026-09-25-something_track_01.flac",
			wantErr:       false,
		},
		{
			name:          "Multi-disk naming",
			filename:      "123_2-2026-09-25-something_track_04.flac",
			releaseID:     123,
			wantReleaseID: 123,
			wantDisk:      2,
			wantDate:      "2026-09-25",
			wantTrackNum:  4,
			wantFilename:  "123_2-2026-09-25-something_track_04.flac",
			wantPath:      "123_2-2026-09-25-something_track_04.flac",
			wantErr:       false,
		},
		{
			name:          "Session suffix",
			filename:      "123_1-2026-09-25-01-something_track_02.flac",
			releaseID:     123,
			wantReleaseID: 123,
			wantDisk:      1,
			wantDate:      "2026-09-25-01",
			wantTrackNum:  2,
			wantFilename:  "123_1-2026-09-25-01-something_track_02.flac",
			wantPath:      "123_1-2026-09-25-01-something_track_02.flac",
			wantErr:       false,
		},
		{
			name:          "Disk 0 normalization",
			filename:      "123_0-2026-09-25-something_track_01.flac",
			releaseID:     123,
			wantReleaseID: 123,
			wantDisk:      1,
			wantDate:      "2026-09-25",
			wantTrackNum:  1,
			wantFilename:  "123_0-2026-09-25-something_track_01.flac",
			wantPath:      "123_0-2026-09-25-something_track_01.flac",
			wantErr:       false,
		},
		{
			name:          "Full directory path handling",
			filename:      "/data/music/123/123_2-2026-09-25-title_track_05.flac",
			releaseID:     123,
			wantReleaseID: 123,
			wantDisk:      2,
			wantDate:      "2026-09-25",
			wantTrackNum:  5,
			wantFilename:  "123_2-2026-09-25-title_track_05.flac",
			wantPath:      "/data/music/123/123_2-2026-09-25-title_track_05.flac",
			wantErr:       false,
		},
		{
			name:      "Release ID mismatch",
			filename:  "123-2026-09-25-something_track_01.flac",
			releaseID: 456,
			wantErr:   true,
		},
		{
			name:      "Invalid filename non-flac",
			filename:  "123-2026-09-25-something_track_01.wav",
			releaseID: 123,
			wantErr:   true,
		},
		{
			name:      "Invalid filename missing track",
			filename:  "123-2026-09-25-something.flac",
			releaseID: 123,
			wantErr:   true,
		},
		{
			name:      "Invalid filename malformed date",
			filename:  "123-bad-date_track_01.flac",
			releaseID: 123,
			wantErr:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseTrackFilename(tc.filename, tc.releaseID)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseTrackFilename(%q, %d) expected error, got nil", tc.filename, tc.releaseID)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseTrackFilename(%q, %d) unexpected error: %v", tc.filename, tc.releaseID, err)
			}
			if got.ReleaseID != tc.wantReleaseID {
				t.Errorf("ReleaseID = %d, want %d", got.ReleaseID, tc.wantReleaseID)
			}
			if got.Disk != tc.wantDisk {
				t.Errorf("Disk = %d, want %d", got.Disk, tc.wantDisk)
			}
			if got.Date != tc.wantDate {
				t.Errorf("Date = %q, want %q", got.Date, tc.wantDate)
			}
			if got.TrackNum != tc.wantTrackNum {
				t.Errorf("TrackNum = %d, want %d", got.TrackNum, tc.wantTrackNum)
			}
			if got.Filename != tc.wantFilename {
				t.Errorf("Filename = %q, want %q", got.Filename, tc.wantFilename)
			}
			if got.Path != tc.wantPath {
				t.Errorf("Path = %q, want %q", got.Path, tc.wantPath)
			}
		})
	}
}

func TestGroupTracksByDiskAndRun(t *testing.T) {
	files := []string{
		"/path/123-2026-09-25-album_track_01.flac",
		"/path/123-2026-09-25-album_track_02.flac",
		"/path/123-2026-09-26-album_track_01.flac",
		"/path/123_2-2026-09-25-album_track_01.flac",
		"/path/123_2-2026-09-25-album_track_02.flac",
		"/path/stray_file.txt",
		"/path/123-corrupt_file.flac",
	}

	grouped, err := GroupTracksByDiskAndRun(files, 123)
	if err != nil {
		t.Fatalf("unexpected error from GroupTracksByDiskAndRun: %v", err)
	}

	// Verify Disk 1
	disk1Runs, ok := grouped[1]
	if !ok {
		t.Fatalf("expected Disk 1 in grouped results")
	}
	if len(disk1Runs["2026-09-25"]) != 2 {
		t.Errorf("Disk 1 run 2026-09-25: expected 2 tracks, got %d", len(disk1Runs["2026-09-25"]))
	}
	if len(disk1Runs["2026-09-26"]) != 1 {
		t.Errorf("Disk 1 run 2026-09-26: expected 1 track, got %d", len(disk1Runs["2026-09-26"]))
	}

	// Verify Disk 2
	disk2Runs, ok := grouped[2]
	if !ok {
		t.Fatalf("expected Disk 2 in grouped results")
	}
	if len(disk2Runs["2026-09-25"]) != 2 {
		t.Errorf("Disk 2 run 2026-09-25: expected 2 tracks, got %d", len(disk2Runs["2026-09-25"]))
	}

	// Verify stray/corrupt files were omitted
	for disk, runs := range grouped {
		for run, tracks := range runs {
			for _, track := range tracks {
				if track == "/path/stray_file.txt" || track == "/path/123-corrupt_file.flac" {
					t.Errorf("unparseable file included in disk %d, run %s: %s", disk, run, track)
				}
			}
		}
	}
}

