package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func FindSharedSoxParams(t *testing.T, dataDir string, targets map[string]int) (string, string, error) {
	durations := []string{"0.5", "0.8", "1.0", "1.1", "1.2", "1.3", "1.4", "1.5", "1.8", "2.0", "2.5", "3.0", "4.0", "5.0"}
	thresholds := []string{"0.05%", "0.1%", "0.15%", "0.2%", "0.25%", "0.3%", "0.4%", "0.5%", "0.75%", "1%", "1.5%", "2%", "5%", "10%", "15%"}

	bestDuration := ""
	bestThreshold := ""
	minTotalDiff := -1

	for _, duration := range durations {
		for _, threshold := range thresholds {
			t.Logf("Testing shared parameters: duration=%v, threshold=%v", duration, threshold)
			totalDiff := 0
			
			for prefix, expectedTracks := range targets {
				files, err := filepath.Glob(filepath.Join(dataDir, prefix+"*.wav"))
				if err != nil || len(files) == 0 {
					return "", "", fmt.Errorf("error finding files for %v: %v", prefix, err)
				}

				tmpDir, err := os.MkdirTemp("", "sox_shared_test")
				if err != nil {
					return "", "", err
				}
				
				totalTracks := 0
				for _, file := range files {
					strippedFile := filepath.Base(file)
					strippedFile = strippedFile[:len(strippedFile)-4]
					
					outPattern := filepath.Join(tmpDir, strippedFile+"_track_.wav")
					cmd := exec.Command("sox", file, outPattern, "silence", "1", duration, threshold, "1", duration, threshold, ":", "newfile", ":", "restart")
					cmd.Run()

					matches, _ := filepath.Glob(filepath.Join(tmpDir, strippedFile+"_track_*.wav"))
					for _, match := range matches {
						info, err := os.Stat(match)
						if err == nil && info.Size() > 10000 {
							totalTracks++
						}
					}
				}
				os.RemoveAll(tmpDir)

				diff := abs(totalTracks - expectedTracks)
				totalDiff += diff
				if diff != 0 {
					t.Logf("  Failed for %v: got %v tracks, expected %v (diff %v)", prefix, totalTracks, expectedTracks, diff)
				} else {
				    t.Logf("  Matched for %v: got %v tracks", prefix, expectedTracks)
				}
			}

			if totalDiff == 0 {
				return duration, threshold, nil
			}

			if minTotalDiff == -1 || totalDiff < minTotalDiff {
				minTotalDiff = totalDiff
				bestDuration = duration
				bestThreshold = threshold
				t.Logf("  New best match: total diff %v", totalDiff)
			}
		}
	}

	if bestDuration != "" {
		t.Logf("Returning closest match: duration=%v, threshold=%v (total diff %v)", bestDuration, bestThreshold, minTotalDiff)
		return bestDuration, bestThreshold, nil
	}

	return "", "", fmt.Errorf("could not find any parameters")
}

func TestFindSharedSoxParams(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping sox parameter search in short mode")
	}
	
	targets := map[string]int{
		"1144284": 10,
		"22650623": 2,
	}
	
	duration, threshold, err := FindSharedSoxParams(t, "testing", targets)
	if err != nil {
		t.Fatalf("Failed to find shared params: %v", err)
	}
	t.Logf("SUCCESS: Found shared parameters: duration=%v, threshold=%v", duration, threshold)
}