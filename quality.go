package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pbrc "github.com/brotherlogic/recordcollection/proto"
	pb "github.com/brotherlogic/recorder/proto"
)

// CurrentScoringVersion defines the current scoring algorithm version.
const CurrentScoringVersion = 2

// TrackQuality represents the audio quality analysis metrics for an individual track.
type TrackQuality struct {
	Filename       string    `json:"filename"`
	SizeBytes      int64     `json:"size_bytes"`
	ModTime        time.Time `json:"mod_time"`
	PeakDb         float64   `json:"peak_db"`
	RmsDb          float64   `json:"rms_db"`
	ClippedCount   int64     `json:"clipped_count"`
	DynamicRange   float64   `json:"dynamic_range"`
	HasLongSilence bool      `json:"has_long_silence"`
	Score          int32     `json:"score"` // 0 - 50
}

// QualitySummary represents the composite evaluation and per-track breakdown for a release.
type QualitySummary struct {
	ReleaseID         int64                   `json:"release_id"`
	Version           int                     `json:"version"`
	Score             int32                   `json:"score"`              // 0 - 100
	CompletenessScore int32                   `json:"completeness_score"` // 0 - 50
	CleanlinessScore  int32                   `json:"cleanliness_score"`  // 0 - 50
	ExpectedTracks    int                     `json:"expected_tracks"`
	FoundTracks       int                     `json:"found_tracks"`
	Tracks            map[string]TrackQuality `json:"tracks"`
	LastEvaluated     time.Time               `json:"last_evaluated"`
}

// GetQualityFilePath returns the path to quality.json for a given release.
func GetQualityFilePath(saveDir string, releaseID int64) string {
	return filepath.Join(saveDir, fmt.Sprintf("%d", releaseID), "quality.json")
}

// ReadQualitySummary reads and deserializes quality.json for a release.
func ReadQualitySummary(saveDir string, releaseID int64) (*QualitySummary, error) {
	qualityPath := GetQualityFilePath(saveDir, releaseID)
	data, err := os.ReadFile(qualityPath)
	if err != nil {
		return nil, err
	}

	var summary QualitySummary
	if err := json.Unmarshal(data, &summary); err != nil {
		return nil, fmt.Errorf("failed to unmarshal quality summary from %v: %w", qualityPath, err)
	}

	return &summary, nil
}

// WriteQualitySummary serializes and writes quality.json for a release.
func WriteQualitySummary(saveDir string, releaseID int64, summary *QualitySummary) error {
	if summary == nil {
		return fmt.Errorf("summary cannot be nil")
	}

	if releaseID == 0 && summary.ReleaseID != 0 {
		releaseID = summary.ReleaseID
	} else if summary.ReleaseID == 0 && releaseID != 0 {
		summary.ReleaseID = releaseID
	}

	targetPath := GetQualityFilePath(saveDir, releaseID)
	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %v: %w", dir, err)
	}

	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal quality summary: %w", err)
	}

	if err := os.WriteFile(targetPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write %v: %w", targetPath, err)
	}

	return nil
}

// IsCacheValid inspects all .flac files in save_dir/<release_id>/ and compares
// current file sizes and modification timestamps against the summary.
// If files are missing, added, or timestamps/sizes have changed, cache is marked invalid.
func IsCacheValid(saveDir string, releaseID int64, summary *QualitySummary) bool {
	if summary == nil || summary.Tracks == nil {
		return false
	}

	if summary.Version < CurrentScoringVersion {
		return false
	}

	releaseDir := filepath.Join(saveDir, fmt.Sprintf("%d", releaseID))
	entries, err := os.ReadDir(releaseDir)
	if err != nil {
		return false
	}

	flacFiles := make(map[string]os.DirEntry)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(strings.ToLower(entry.Name()), ".flac") {
			flacFiles[entry.Name()] = entry
		}
	}

	if len(flacFiles) != len(summary.Tracks) {
		return false
	}

	if summary.FoundTracks != len(flacFiles) {
		return false
	}

	for name, entry := range flacFiles {
		tq, exists := summary.Tracks[name]
		if !exists {
			for _, track := range summary.Tracks {
				if track.Filename == name || filepath.Base(track.Filename) == name {
					tq = track
					exists = true
					break
				}
			}
		}
		if !exists {
			return false
		}

		info, err := entry.Info()
		if err != nil {
			return false
		}

		if info.Size() != tq.SizeBytes {
			return false
		}

		if !info.ModTime().Equal(tq.ModTime) {
			return false
		}
	}

	return true
}

// GetValidCachedSummary retrieves the cached quality summary if it exists and is valid.
func GetValidCachedSummary(saveDir string, releaseID int64) (*QualitySummary, bool) {
	summary, err := ReadQualitySummary(saveDir, releaseID)
	if err != nil {
		return nil, false
	}

	if !IsCacheValid(saveDir, releaseID, summary) {
		return nil, false
	}

	return summary, true
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
func CalculateTrackCleanliness(stats *SoxStats, hasLongSilence bool) int32 {
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

	// 2. Deduct for low dynamic range / elevated noise floor
	var drDeduction int32
	if stats.PeakDb <= -100.0 || math.IsInf(stats.PeakDb, -1) {
		drDeduction = 20
	} else {
		dynamicRange := stats.PeakDb - stats.RmsDb
		if dynamicRange < 15.0 {
			drDeduction = int32((15.0 - dynamicRange) * 2)
			if drDeduction < 5 {
				drDeduction = 5
			} else if drDeduction > 20 {
				drDeduction = 20
			}
		}
	}
	score -= drDeduction

	// 3. Deduct for long silence
	if hasLongSilence {
		score -= 10
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

	score := CalculateTrackCleanliness(stats, false)

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

// KeyedMutex provides thread-safe per-release concurrency locking.
type KeyedMutex struct {
	locks sync.Map
}

// GetLock returns the *sync.RWMutex for a specific release ID.
func (m *KeyedMutex) GetLock(releaseID int64) *sync.RWMutex {
	val, _ := m.locks.LoadOrStore(releaseID, &sync.RWMutex{})
	return val.(*sync.RWMutex)
}

// QualityServer implements pb.QualityServiceServer.
type QualityServer struct {
	pb.UnimplementedQualityServiceServer
	saveDir  string
	mutex    *KeyedMutex
	rcClient pbrc.RecordCollectionServiceClient
	mu       sync.Mutex
}

// NewQualityServer creates a new QualityServer instance.
func NewQualityServer(saveDir string, rcClient pbrc.RecordCollectionServiceClient) *QualityServer {
	return &QualityServer{
		saveDir:  saveDir,
		mutex:    &KeyedMutex{},
		rcClient: rcClient,
	}
}

func (s *QualityServer) getSaveDir() string {
	if s.saveDir != "" {
		return s.saveDir
	}
	if saveDir != nil {
		return *saveDir
	}
	return ""
}

func (s *QualityServer) getLock(releaseID int64) *sync.RWMutex {
	s.mu.Lock()
	if s.mutex == nil {
		s.mutex = &KeyedMutex{}
	}
	s.mu.Unlock()
	return s.mutex.GetLock(releaseID)
}

// GetQuality handles quality evaluation requests.
func (s *QualityServer) GetQuality(ctx context.Context, req *pb.GetQualityRequest) (*pb.GetQualityResponse, error) {
	if req == nil || req.GetReleaseId() <= 0 {
		return nil, status.Errorf(codes.InvalidArgument, "invalid release id: %d", req.GetReleaseId())
	}

	releaseID := req.GetReleaseId()
	lock := s.getLock(releaseID)
	lock.Lock()
	defer lock.Unlock()

	dir := s.getSaveDir()

	// 1. Check quality.json disk cache; return cached score immediately on cache hit.
	if cached, ok := GetValidCachedSummary(dir, releaseID); ok && cached != nil {
		return &pb.GetQualityResponse{Score: cached.Score}, nil
	}

	// 2. On cache miss or invalidated cache:
	// Compute completeness score (0-50).
	completenessRes, err := EvaluateCompleteness(ctx, s.rcClient, dir, releaseID)
	if err != nil {
		return nil, err
	}

	// Compute cleanliness score (0-50).
	tracksMap := make(map[string]TrackQuality)
	for _, file := range completenessRes.Files {
		tq, err := AnalyzeTrack(file)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to analyze track %s: %v", file, err)
		}
		tq.Filename = filepath.Base(file)
		tracksMap[filepath.Base(file)] = *tq
	}
	cleanlinessScore := CalculateAggregateCleanlinessFromMap(tracksMap)

	// Calculate composite score: TotalScore = CompletenessScore + CleanlinessScore (clamped strictly 0 to 100).
	totalScore := completenessRes.Score + cleanlinessScore
	if totalScore < 0 {
		totalScore = 0
	} else if totalScore > 100 {
		totalScore = 100
	}

	summary := &QualitySummary{
		ReleaseID:         releaseID,
		Version:           CurrentScoringVersion,
		Score:             totalScore,
		CompletenessScore: completenessRes.Score,
		CleanlinessScore:  cleanlinessScore,
		ExpectedTracks:    completenessRes.ExpectedTracks,
		FoundTracks:       completenessRes.FoundTracks,
		Tracks:            tracksMap,
		LastEvaluated:     time.Now().UTC(),
	}

	if err := WriteQualitySummary(dir, releaseID, summary); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to write quality summary: %v", err)
	}

	return &pb.GetQualityResponse{Score: totalScore}, nil
}
