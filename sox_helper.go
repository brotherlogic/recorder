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

	// Create a downsampled version of the input file to a temporary file
	// to make parameter search dramatically faster.
	// Downsampling to mono, 8000Hz, 16-bit preserves silence characteristics while minimizing data size.
	downsampleDir, err := os.MkdirTemp("", "sox_downsample")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(downsampleDir)

	downsampledFile := filepath.Join(downsampleDir, "downsampled.wav")
	
	searchFile := inputFile

	inputInfo, err := os.Stat(inputFile)
	if err == nil && inputInfo.Size() > 50*1024*1024 {
		downsampleCmd := exec.Command("sox", inputFile, "-r", "8000", "-c", "1", "-b", "16", downsampledFile)
		if err := downsampleCmd.Run(); err == nil {
			searchFile = downsampledFile
		}
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

			strippedFile := filepath.Base(searchFile)
			strippedFile = strippedFile[:len(strippedFile)-4]
			outPattern := filepath.Join(tmpDir, strippedFile+"_track_.wav")
			
			cmd := exec.Command("sox", searchFile, outPattern, "silence", "1", duration, threshold, "1", duration, threshold, ":", "newfile", ":", "restart")
			cmd.Run()

			totalTracks := 0
			matches, _ := filepath.Glob(filepath.Join(tmpDir, strippedFile+"_track_*.wav"))
			for _, match := range matches {
				info, err := os.Stat(match)
				// The downsampled file size is much smaller, but even a 5-second track
				// at 8000Hz 16-bit mono is 8000 * 2 * 5 = 80,000 bytes.
				// Thus, 10000 bytes remains an extremely safe size threshold to exclude tiny noise artifacts.
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
