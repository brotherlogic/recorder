package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func FindBestSoxParams(inputFile string, expectedTracks int) (string, string, error) {
	durations := []string{"0.5", "0.8", "1.0", "1.1", "1.2", "1.3", "1.4", "1.5", "1.8", "2.0", "2.5", "3.0", "4.0", "5.0"}
	thresholds := []string{"0.05%", "0.1%", "0.15%", "0.2%", "0.25%", "0.3%", "0.4%", "0.5%", "0.75%", "1%", "1.5%", "2%", "5%", "10%", "15%"}

	if expectedTracks <= 0 {
		return "0.5", "1%", nil // Default parameters
	}

	bestDuration := ""
	bestThreshold := ""
	minDiff := -1

	for _, duration := range durations {
		for _, threshold := range thresholds {
			tmpDir, err := os.MkdirTemp("", "sox_test")
			if err != nil {
				return "", "", err
			}

			strippedFile := filepath.Base(inputFile)
			strippedFile = strippedFile[:len(strippedFile)-4]
			outPattern := filepath.Join(tmpDir, strippedFile+"_track_.wav")
			
			cmd := exec.Command("sox", inputFile, outPattern, "silence", "1", duration, threshold, "1", duration, threshold, ":", "newfile", ":", "restart")
			cmd.Run()

			totalTracks := 0
			matches, _ := filepath.Glob(filepath.Join(tmpDir, strippedFile+"_track_*.wav"))
			for _, match := range matches {
				info, err := os.Stat(match)
				if err == nil && info.Size() > 10000 {
					totalTracks++
				}
			}
			os.RemoveAll(tmpDir)

			diff := abs(totalTracks - expectedTracks)
			if diff == 0 {
				return duration, threshold, nil
			}

			if minDiff == -1 || diff < minDiff {
				minDiff = diff
				bestDuration = duration
				bestThreshold = threshold
			}
		}
	}

	if bestDuration != "" {
		return bestDuration, bestThreshold, nil
	}

	return "", "", fmt.Errorf("could not find any parameters")
}
