package main

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// TrackQuality represents the audio quality analysis metrics for an individual track.
type TrackQuality struct {
	Filename     string    `json:"filename"`
	SizeBytes    int64     `json:"size_bytes"`
	ModTime      time.Time `json:"mod_time"`
	PeakDb       float64   `json:"peak_db"`
	RmsDb        float64   `json:"rms_db"`
	ClippedCount int64     `json:"clipped_count"`
	Score        int32     `json:"score"` // 0 - 50
}

// SoxStats holds parsed metrics from `sox <file> -n stats`.
type SoxStats struct {
	PeakDb       float64
	RmsDb        float64
	ClippedCount int64
}

var (
	clippedRegexes = []*regexp.Regexp{
		regexp.MustCompile(`(?i)clipped\s+(\d+)\s+samples`),
		regexp.MustCompile(`(?i)clipped\s*samples[:\s]+(\d+)`),
		regexp.MustCompile(`(?i)clipped_samples[:\s]+(\d+)`),
		regexp.MustCompile(`(?i)clipped count[:\s]+(\d+)`),
	}
)

// ParseSoxStats extracts Peak level, RMS level, and clipped sample count from sox stats output.
func ParseSoxStats(output string) (*SoxStats, error) {
	stats := &SoxStats{}
	foundPeak := false
	foundRms := false

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "Pk lev dB") {
			fields := strings.Fields(trimmed)
			// Format: Pk lev dB <Overall> [Left] [Right]
			if len(fields) >= 4 {
				val, err := parseDbValue(fields[3])
				if err == nil {
					stats.PeakDb = val
					foundPeak = true
				}
			}
		} else if strings.HasPrefix(trimmed, "RMS lev dB") {
			fields := strings.Fields(trimmed)
			// Format: RMS lev dB <Overall> [Left] [Right]
			if len(fields) >= 4 {
				val, err := parseDbValue(fields[3])
				if err == nil {
					stats.RmsDb = val
					foundRms = true
				}
			}
		}

		for _, re := range clippedRegexes {
			matches := re.FindAllStringSubmatch(line, -1)
			for _, m := range matches {
				if len(m) >= 2 {
					count, err := strconv.ParseInt(m[1], 10, 64)
					if err == nil {
						stats.ClippedCount += count
					}
				}
			}
		}
	}

	if !foundPeak || !foundRms {
		return nil, fmt.Errorf("unable to parse Pk lev dB or RMS lev dB from output")
	}

	return stats, nil
}

func parseDbValue(valStr string) (float64, error) {
	lower := strings.ToLower(valStr)
	if lower == "-inf" {
		return -100.0, nil
	}
	if lower == "inf" || lower == "+inf" {
		return 0.0, nil
	}
	return strconv.ParseFloat(valStr, 64)
}

// CalculateTrackCleanliness computes the 0-50 cleanliness score for given audio stats.
func CalculateTrackCleanliness(stats *SoxStats) int32 {
	if stats == nil {
		return 0
	}

	var score int32 = 50

	// 1. Deduct for severe digital clipping: Pk lev dB >= -0.01 and clipped samples > 0
	if stats.PeakDb >= -0.01 && stats.ClippedCount > 0 {
		deduction := int32(15)
		if stats.ClippedCount > 1000 {
			extra := int32(math.Min(15, float64(stats.ClippedCount/1000)))
			deduction += extra
		}
		score -= deduction
	}

	// 2. Deduct for elevated noise floor relative to dynamic range
	dynamicRange := stats.PeakDb - stats.RmsDb
	if dynamicRange < 10.0 {
		deduction := int32((10.0 - dynamicRange) * 2)
		if deduction < 5 {
			deduction = 5
		}
		score -= deduction
	}

	if score < 0 {
		score = 0
	} else if score > 50 {
		score = 50
	}

	return score
}

// RunSoxStats executes `sox <file> -n stats` and returns the output.
func RunSoxStats(filePath string) (string, error) {
	cmd := exec.Command("sox", filePath, "-n", "stats")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// AnalyzeTrack inspects an audio file and calculates its cleanliness quality score.
func AnalyzeTrack(filePath string) (*TrackQuality, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return &TrackQuality{
			Filename: filePath,
			Score:    0,
		}, nil
	}

	if info.Size() == 0 {
		return &TrackQuality{
			Filename:  filePath,
			SizeBytes: 0,
			ModTime:   info.ModTime(),
			Score:     0,
		}, nil
	}

	out, err := RunSoxStats(filePath)
	if err != nil {
		return &TrackQuality{
			Filename:  filePath,
			SizeBytes: info.Size(),
			ModTime:   info.ModTime(),
			Score:     0,
		}, nil
	}

	stats, err := ParseSoxStats(out)
	if err != nil {
		return &TrackQuality{
			Filename:  filePath,
			SizeBytes: info.Size(),
			ModTime:   info.ModTime(),
			Score:     0,
		}, nil
	}

	score := CalculateTrackCleanliness(stats)

	return &TrackQuality{
		Filename:     filePath,
		SizeBytes:    info.Size(),
		ModTime:      info.ModTime(),
		PeakDb:       stats.PeakDb,
		RmsDb:        stats.RmsDb,
		ClippedCount: stats.ClippedCount,
		Score:        score,
	}, nil
}

// CalculateAggregateCleanliness computes the average track cleanliness across tracks (0 - 50 scale).
func CalculateAggregateCleanliness(tracks []TrackQuality) int32 {
	if len(tracks) == 0 {
		return 0
	}

	var sum int64
	for _, track := range tracks {
		sum += int64(track.Score)
	}

	avg := int32(math.Round(float64(sum) / float64(len(tracks))))
	if avg < 0 {
		avg = 0
	} else if avg > 50 {
		avg = 50
	}
	return avg
}

// CalculateAggregateCleanlinessFromMap computes the average track cleanliness across a track map.
func CalculateAggregateCleanlinessFromMap(tracks map[string]TrackQuality) int32 {
	if len(tracks) == 0 {
		return 0
	}

	var sum int64
	for _, track := range tracks {
		sum += int64(track.Score)
	}

	avg := int32(math.Round(float64(sum) / float64(len(tracks))))
	if avg < 0 {
		avg = 0
	} else if avg > 50 {
		avg = 50
	}
	return avg
}
