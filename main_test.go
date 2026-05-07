package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pbgd "github.com/brotherlogic/godiscogs/proto"
)

func TestCleanupRetainedFiles(t *testing.T) {
	s := &Server{}
	dir, err := os.MkdirTemp("", "test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	oldFile := filepath.Join(dir, "old.wav")
	err = os.WriteFile(oldFile, []byte("test"), 0644)
	if err != nil {
		t.Fatalf("Failed to create old file: %v", err)
	}

	// Backdate the file to 48 hours ago
	oldTime := time.Now().Add(-48 * time.Hour)
	os.Chtimes(oldFile, oldTime, oldTime)

	s.cleanupRetainedFiles(dir)

	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Errorf("Old file was not deleted")
	}
}

func TestGetDiskFromPosition(t *testing.T) {
	tests := []struct {
		pos      string
		expected int
	}{
		{"A1", 1},
		{"B1", 1},
		{"C1", 2},
		{"D1", 2},
		{"Z1", 13},
		{"AA1", 14},
		{"AB1", 14},
		{"AC1", 15},
		{"AD1", 15},
	}

	for _, tc := range tests {
		got := getDiskFromPosition(tc.pos)
		if got != tc.expected {
			t.Errorf("getDiskFromPosition(%v) = %v, want %v", tc.pos, got, tc.expected)
		}
	}
}

func TestGetExpectedTracks(t *testing.T) {
	tests := []struct {
		name     string
		release  *pbgd.Release
		disk     int32
		expected int
	}{
		{
			name: "Single disk CD",
			release: &pbgd.Release{
				FormatQuantity: 1,
				Tracklist: []*pbgd.Track{
					{Position: "1"},
					{Position: "2"},
				},
			},
			disk:     1,
			expected: 2,
		},
		{
			name: "Multi disk CD",
			release: &pbgd.Release{
				FormatQuantity: 2,
				Tracklist: []*pbgd.Track{
					{Position: "1-1"},
					{Position: "1-2"},
					{Position: "2-1"},
					{Position: "2-2"},
					{Position: "2-3"},
				},
			},
			disk:     2,
			expected: 3,
		},
		{
			name: "Release 19347250 Disk 5",
			release: &pbgd.Release{
				FormatQuantity: 19,
				Tracklist: []*pbgd.Track{
					{Position: "A1"}, {Position: "A2"}, {Position: "A3"}, {Position: "A4"}, {Position: "A5"}, {Position: "A6"}, {Position: "A7"}, {Position: "A8"}, {Position: "A9"}, {Position: "A10"}, {Position: "A11"}, {Position: "A12"}, {Position: "A13"}, {Position: "A14"}, {Position: "A15"},
					{Position: "B16"}, {Position: "B17"}, {Position: "B18"}, {Position: "B19"}, {Position: "B20"}, {Position: "B21"}, {Position: "B22"}, {Position: "B23"},
					{Position: "C1"}, {Position: "C2"}, {Position: "C3"}, {Position: "C4"}, {Position: "C5"}, {Position: "C6"}, {Position: "C7"}, {Position: "C8"}, {Position: "C9"},
					{Position: "D10"}, {Position: "D11"}, {Position: "D12"}, {Position: "D13"}, {Position: "D14"}, {Position: "D15"}, {Position: "D16"}, {Position: "D17"}, {Position: "D18"}, {Position: "D19"}, {Position: "D20"}, {Position: "D21"}, {Position: "D22"},
					{Position: "E1"}, {Position: "E2"}, {Position: "E3"}, {Position: "E4"}, {Position: "E5"}, {Position: "E6"}, {Position: "E7"}, {Position: "E8"}, {Position: "E9"}, {Position: "E10"}, {Position: "E11"}, {Position: "E12"},
					{Position: "F13"}, {Position: "F14"}, {Position: "F15"}, {Position: "F16"}, {Position: "F17"}, {Position: "F18"}, {Position: "F19"}, {Position: "F20"}, {Position: "F21"},
					{Position: "G22"}, {Position: "G23"}, {Position: "G24"}, {Position: "G25"}, {Position: "G26"}, {Position: "G27"}, {Position: "G28"}, {Position: "G29"}, {Position: "G30"}, {Position: "G31"}, {Position: "G32"},
					{Position: "H33"}, {Position: "H34"}, {Position: "H35"}, {Position: "H36"}, {Position: "H37"}, {Position: "H38"}, {Position: "H39"},
					{Position: "I1"}, {Position: "I2"}, {Position: "I3"}, {Position: "I4"}, {Position: "I5"}, {Position: "I6"}, {Position: "I7"}, {Position: "I8"}, {Position: "I9"}, {Position: "I10"}, {Position: "I11"}, {Position: "I12"}, {Position: "I13"}, {Position: "I14"},
					{Position: "J15"}, {Position: "J16"}, {Position: "J17"}, {Position: "J18"}, {Position: "J19"}, {Position: "J20"}, {Position: "J21"}, {Position: "J22"},
					{Position: "K23"}, {Position: "K24"}, {Position: "K25"},
					{Position: "L26"}, {Position: "L27"}, {Position: "L28"},
				},
			},
			disk:     5,
			expected: 22, // I(14) + J(8)
		},
		{
			name: "Release 19347250 Disk 6",
			release: &pbgd.Release{
				FormatQuantity: 19,
				Tracklist: []*pbgd.Track{
					{Position: "A1"}, {Position: "A2"}, {Position: "A3"}, {Position: "A4"}, {Position: "A5"}, {Position: "A6"}, {Position: "A7"}, {Position: "A8"}, {Position: "A9"}, {Position: "A10"}, {Position: "A11"}, {Position: "A12"}, {Position: "A13"}, {Position: "A14"}, {Position: "A15"},
					{Position: "B16"}, {Position: "B17"}, {Position: "B18"}, {Position: "B19"}, {Position: "B20"}, {Position: "B21"}, {Position: "B22"}, {Position: "B23"},
					{Position: "C1"}, {Position: "C2"}, {Position: "C3"}, {Position: "C4"}, {Position: "C5"}, {Position: "C6"}, {Position: "C7"}, {Position: "C8"}, {Position: "C9"},
					{Position: "D10"}, {Position: "D11"}, {Position: "D12"}, {Position: "D13"}, {Position: "D14"}, {Position: "D15"}, {Position: "D16"}, {Position: "D17"}, {Position: "D18"}, {Position: "D19"}, {Position: "D20"}, {Position: "D21"}, {Position: "D22"},
					{Position: "E1"}, {Position: "E2"}, {Position: "E3"}, {Position: "E4"}, {Position: "E5"}, {Position: "E6"}, {Position: "E7"}, {Position: "E8"}, {Position: "E9"}, {Position: "E10"}, {Position: "E11"}, {Position: "E12"},
					{Position: "F13"}, {Position: "F14"}, {Position: "F15"}, {Position: "F16"}, {Position: "F17"}, {Position: "F18"}, {Position: "F19"}, {Position: "F20"}, {Position: "F21"},
					{Position: "G22"}, {Position: "G23"}, {Position: "G24"}, {Position: "G25"}, {Position: "G26"}, {Position: "G27"}, {Position: "G28"}, {Position: "G29"}, {Position: "G30"}, {Position: "G31"}, {Position: "G32"},
					{Position: "H33"}, {Position: "H34"}, {Position: "H35"}, {Position: "H36"}, {Position: "H37"}, {Position: "H38"}, {Position: "H39"},
					{Position: "I1"}, {Position: "I2"}, {Position: "I3"}, {Position: "I4"}, {Position: "I5"}, {Position: "I6"}, {Position: "I7"}, {Position: "I8"}, {Position: "I9"}, {Position: "I10"}, {Position: "I11"}, {Position: "I12"}, {Position: "I13"}, {Position: "I14"},
					{Position: "J15"}, {Position: "J16"}, {Position: "J17"}, {Position: "J18"}, {Position: "J19"}, {Position: "J20"}, {Position: "J21"}, {Position: "J22"},
					{Position: "K23"}, {Position: "K24"}, {Position: "K25"},
					{Position: "L26"}, {Position: "L27"}, {Position: "L28"},
				},
			},
			disk:     6,
			expected: 6, // K(3) + L(3)
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := getExpectedTracks(tc.release, tc.disk)
			if got != tc.expected {
				t.Errorf("getExpectedTracks() = %v, want %v", got, tc.expected)
			}
		})
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
					{Position: "A1"}, {Position: "A2"}, {Position: "A3"}, {Position: "A4"}, {Position: "A5"}, {Position: "A6"}, {Position: "A7"}, {Position: "A8"}, {Position: "A9"}, {Position: "A10"}, {Position: "A11"}, {Position: "A12"}, {Position: "A13"}, {Position: "A14"}, {Position: "A15"},
					{Position: "B16"}, {Position: "B17"}, {Position: "B18"}, {Position: "B19"}, {Position: "B20"}, {Position: "B21"}, {Position: "B22"}, {Position: "B23"},
					{Position: "C1"}, {Position: "C2"}, {Position: "C3"}, {Position: "C4"}, {Position: "C5"}, {Position: "C6"}, {Position: "C7"}, {Position: "C8"}, {Position: "C9"},
					{Position: "D10"}, {Position: "D11"}, {Position: "D12"}, {Position: "D13"}, {Position: "D14"}, {Position: "D15"}, {Position: "D16"}, {Position: "D17"}, {Position: "D18"}, {Position: "D19"}, {Position: "D20"}, {Position: "D21"}, {Position: "D22"},
					{Position: "E1"}, {Position: "E2"}, {Position: "E3"}, {Position: "E4"}, {Position: "E5"}, {Position: "E6"}, {Position: "E7"}, {Position: "E8"}, {Position: "E9"}, {Position: "E10"}, {Position: "E11"}, {Position: "E12"},
					{Position: "F13"}, {Position: "F14"}, {Position: "F15"}, {Position: "F16"}, {Position: "F17"}, {Position: "F18"}, {Position: "F19"}, {Position: "F20"}, {Position: "F21"},
					{Position: "G22"}, {Position: "G23"}, {Position: "G24"}, {Position: "G25"}, {Position: "G26"}, {Position: "G27"}, {Position: "G28"}, {Position: "G29"}, {Position: "G30"}, {Position: "G31"}, {Position: "G32"},
					{Position: "H33"}, {Position: "H34"}, {Position: "H35"}, {Position: "H36"}, {Position: "H37"}, {Position: "H38"}, {Position: "H39"},
					{Position: "I1"}, {Position: "I2"}, // Disk 5 starts here
				},
			},
			disk:     5,
			expected: 84, // 23+22+21+18
		},
		{
			name: "Release 19347250 Disk 6",
			release: &pbgd.Release{
				Tracklist: []*pbgd.Track{
					{Position: "A1"}, {Position: "A2"}, {Position: "A3"}, {Position: "A4"}, {Position: "A5"}, {Position: "A6"}, {Position: "A7"}, {Position: "A8"}, {Position: "A9"}, {Position: "A10"}, {Position: "A11"}, {Position: "A12"}, {Position: "A13"}, {Position: "A14"}, {Position: "A15"},
					{Position: "B16"}, {Position: "B17"}, {Position: "B18"}, {Position: "B19"}, {Position: "B20"}, {Position: "B21"}, {Position: "B22"}, {Position: "B23"},
					{Position: "C1"}, {Position: "C2"}, {Position: "C3"}, {Position: "C4"}, {Position: "C5"}, {Position: "C6"}, {Position: "C7"}, {Position: "C8"}, {Position: "C9"},
					{Position: "D10"}, {Position: "D11"}, {Position: "D12"}, {Position: "D13"}, {Position: "D14"}, {Position: "D15"}, {Position: "D16"}, {Position: "D17"}, {Position: "D18"}, {Position: "D19"}, {Position: "D20"}, {Position: "D21"}, {Position: "D22"},
					{Position: "E1"}, {Position: "E2"}, {Position: "E3"}, {Position: "E4"}, {Position: "E5"}, {Position: "E6"}, {Position: "E7"}, {Position: "E8"}, {Position: "E9"}, {Position: "E10"}, {Position: "E11"}, {Position: "E12"},
					{Position: "F13"}, {Position: "F14"}, {Position: "F15"}, {Position: "F16"}, {Position: "F17"}, {Position: "F18"}, {Position: "F19"}, {Position: "F20"}, {Position: "F21"},
					{Position: "G22"}, {Position: "G23"}, {Position: "G24"}, {Position: "G25"}, {Position: "G26"}, {Position: "G27"}, {Position: "G28"}, {Position: "G29"}, {Position: "G30"}, {Position: "G31"}, {Position: "G32"},
					{Position: "H33"}, {Position: "H34"}, {Position: "H35"}, {Position: "H36"}, {Position: "H37"}, {Position: "H38"}, {Position: "H39"},
					{Position: "I1"}, {Position: "I2"}, {Position: "I3"}, {Position: "I4"}, {Position: "I5"}, {Position: "I6"}, {Position: "I7"}, {Position: "I8"}, {Position: "I9"}, {Position: "I10"}, {Position: "I11"}, {Position: "I12"}, {Position: "I13"}, {Position: "I14"},
					{Position: "J15"}, {Position: "J16"}, {Position: "J17"}, {Position: "J18"}, {Position: "J19"}, {Position: "J20"}, {Position: "J21"}, {Position: "J22"},
					{Position: "K23"}, {Position: "K24"}, // Disk 6 starts here
				},
			},
			disk:     6,
			expected: 106, // 84 + 22
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

func TestDownloadImageSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("fake-image-data"))
	}))
	defer server.Close()

	tmpFile, err := downloadImage(server.URL)
	if err != nil {
		t.Fatalf("downloadImage failed: %v", err)
	}
	defer os.Remove(tmpFile)

	content, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}

	if string(content) != "fake-image-data" {
		t.Errorf("expected content 'fake-image-data', got %v", string(content))
	}
}

func TestDownloadImageRetry(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("retried-image-data"))
	}))
	defer server.Close()

	tmpFile, err := downloadImage(server.URL)
	if err != nil {
		t.Fatalf("downloadImage failed: %v", err)
	}
	defer os.Remove(tmpFile)

	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %v", attempts)
	}

	content, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}

	if string(content) != "retried-image-data" {
		t.Errorf("expected content 'retried-image-data', got %v", string(content))
	}
}

func TestDownloadImageFail(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	_, err := downloadImage(server.URL)
	if err == nil {
		t.Errorf("expected error from downloadImage, got nil")
	}

	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %v", attempts)
	}
}

func TestConvertToFlac(t *testing.T) {
	s := &Server{}
	dir, err := os.MkdirTemp("", "flac_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	// Create a dummy wav file
	wavFile := filepath.Join(dir, "track1.wav")
	cmd := exec.Command("sox", "-n", "-r", "44100", "-c", "2", wavFile, "trim", "0.0", "0.1")
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to create dummy wav: %v", err)
	}

	release := &pbgd.Release{
		Artists: []*pbgd.Artist{{Name: "Release Artist"}},
		Tracklist: []*pbgd.Track{
			{Title: "Track 1"},
		},
	}

	err = s.convertToFlac([]string{wavFile}, 1, release, 0, dir)
	if err != nil {
		t.Fatalf("convertToFlac failed: %v", err)
	}

	flacFile := filepath.Join(dir, "track1.flac")
	if _, err := os.Stat(flacFile); os.IsNotExist(err) {
		t.Fatalf("flac file was not created")
	}

	// Verify tags using metaflac
	tagsCmd := exec.Command("metaflac", "--show-tag=TITLE", "--show-tag=ARTIST", flacFile)
	output, err := tagsCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("metaflac failed: %v -> %v", err, string(output))
	}

	outStr := string(output)
	if !strings.Contains(outStr, "TITLE=Track 1") {
		t.Errorf("expected TITLE=Track 1 in output, got %v", outStr)
	}
	if !strings.Contains(outStr, "ARTIST=Release Artist") {
		t.Errorf("expected ARTIST=Release Artist in output, got %v", outStr)
	}
}

func TestConvertToFlacImageFail(t *testing.T) {
	s := &Server{}
	dir, err := os.MkdirTemp("", "flac_test_imgfail")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	wavFile := filepath.Join(dir, "track1.wav")
	cmd := exec.Command("sox", "-n", "-r", "44100", "-c", "2", wavFile, "trim", "0.0", "0.1")
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to create dummy wav: %v", err)
	}

	release := &pbgd.Release{
		Artists: []*pbgd.Artist{{Name: "Release Artist"}},
		Images:  []*pbgd.Image{{Uri: "http://example.com/bad-image.jpg"}},
		Tracklist: []*pbgd.Track{
			{Title: "Track 1"},
		},
	}

	// Mock image server that fails
	imgServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer imgServer.Close()
	release.Images[0].Uri = imgServer.URL

	err = s.convertToFlac([]string{wavFile}, 1, release, 0, dir)
	if err != nil {
		t.Fatalf("convertToFlac failed even when image download failed: %v", err)
	}

	flacFile := filepath.Join(dir, "track1.flac")
	if _, err := os.Stat(flacFile); os.IsNotExist(err) {
		t.Fatalf("flac file was not created")
	}

	// Verify tags still applied
	tagsCmd := exec.Command("metaflac", "--show-tag=TITLE", flacFile)
	output, _ := tagsCmd.CombinedOutput()
	if !strings.Contains(string(output), "TITLE=Track 1") {
		t.Errorf("expected TITLE tag to be applied even if image fails")
	}
}


func TestConvertToFlacNoMatch(t *testing.T) {
	s := &Server{}
	dir, err := os.MkdirTemp("", "flac_test_nomatch")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	wavFile := filepath.Join(dir, "track1.wav")
	cmd := exec.Command("sox", "-n", "-r", "44100", "-c", "2", wavFile, "trim", "0.0", "0.1")
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to create dummy wav: %v", err)
	}

	release := &pbgd.Release{
		Tracklist: []*pbgd.Track{{Title: "Track 1"}},
	}

	// Case where len(splitFiles) != expectedTracks
	err = s.convertToFlac([]string{wavFile}, 2, release, 0, dir)
	if err != nil {
		t.Fatalf("convertToFlac failed: %v", err)
	}

	flacFile := filepath.Join(dir, "track1.flac")
	if _, err := os.Stat(flacFile); os.IsNotExist(err) {
		t.Fatalf("flac file was not created")
	}

	tagsCmd := exec.Command("metaflac", "--show-tag=TITLE", flacFile)
	output, _ := tagsCmd.CombinedOutput()
	if strings.Contains(string(output), "TITLE=Track 1") {
		t.Errorf("did not expect TITLE tag when track counts do not match")
	}
}


