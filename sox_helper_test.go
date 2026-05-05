package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func FindSoxParams(t *testing.T, dataDir string, prefix string, expectedTracks int) (string, string, error) {
	durations := []string{"0.5", "0.8", "1.0", "1.1", "1.2", "1.3", "1.4", "1.5", "1.8", "2.0", "2.5", "3.0", "4.0", "5.0"}
	thresholds := []string{"0.05%", "0.1%", "0.15%", "0.2%", "0.25%", "0.3%", "0.4%", "0.5%", "0.75%", "1%", "1.5%", "2%", "5%", "10%", "15%"}

	files, err := filepath.Glob(filepath.Join(dataDir, prefix+"*.wav"))
	if err != nil {
		return "", "", err
	}
	if len(files) == 0 {
		return "", "", fmt.Errorf("no files found for prefix %v in %v", prefix, dataDir)
	}

	for _, duration := range durations {
		for _, threshold := range thresholds {
			t.Logf("Testing duration=%v, threshold=%v", duration, threshold)
			
			tmpDir, err := os.MkdirTemp("", "sox_test_output")
			if err != nil {
				return "", "", err
			}
			defer os.RemoveAll(tmpDir)

			totalTracks := 0
			for _, file := range files {
				strippedFile := filepath.Base(file)
				strippedFile = strippedFile[:len(strippedFile)-4]
				
				// Run sox to split tracks
				// Command from main.go: sox <input> <output> silence 1 <dur> <thresh> 1 <dur> <thresh> : newfile : restart
				outPattern := filepath.Join(tmpDir, strippedFile+"_track_.wav")
				cmd := exec.Command("sox", file, outPattern, "silence", "1", duration, threshold, "1", duration, threshold, ":", "newfile", ":", "restart")
				err := cmd.Run()
				if err != nil {
					t.Logf("Sox failed for %v with %v/%v: %v", file, duration, threshold, err)
					continue
				}

				// Count generated tracks
				matches, err := filepath.Glob(filepath.Join(tmpDir, strippedFile+"_track_*.wav"))
				if err != nil {
					return "", "", err
				}
				for _, match := range matches {
					info, err := os.Stat(match)
					if err == nil && info.Size() > 10000 {
						totalTracks++
					}
				}
			}

			t.Logf("Result for %v/%v: %v tracks", duration, threshold, totalTracks)
			if totalTracks == expectedTracks {
				return duration, threshold, nil
			}
		}
	}

	return "", "", fmt.Errorf("could not find parameters for prefix %v to get %v tracks", prefix, expectedTracks)
}

func TestFindSoxParams1144284(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping sox parameter search in short mode")
	}
	
	duration, threshold, err := FindSoxParams(t, "testing", "1144284", 10)
	if err != nil {
		t.Fatalf("Failed to find params for 1144284: %v", err)
	}
	t.Logf("Success for 1144284: duration=%v, threshold=%v", duration, threshold)
}

func TestFindSoxParams22650623(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping sox parameter search in short mode")
	}

	duration, threshold, err := FindSoxParams(t, "testing", "22650623", 2)
	if err != nil {
		t.Fatalf("Failed to find params for 22650623: %v", err)
	}
	t.Logf("Success for 22650623: duration=%v, threshold=%v", duration, threshold)
}
