package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
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
const CurrentScoringVersion = 3

// DiskQualitySummary represents the quality score and best rip date for an individual disk.
type DiskQualitySummary struct {
	Disk        int32  `json:"disk"`
	BestRipDate string `json:"best_rip_date"`
	Score       int32  `json:"score"`
}

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
	Disks             []DiskQualitySummary    `json:"disks"`
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

// GetTrackDuration returns the duration of an audio file in seconds using soxi or sox --info.
func GetTrackDuration(filePath string) (float64, error) {
	cmd := exec.Command("soxi", "-D", filePath)
	out, err := cmd.Output()
	if err != nil {
		cmd = exec.Command("sox", "--i", "-D", filePath)
		out, err = cmd.Output()
		if err != nil {
			return 0, err
		}
	}
	return strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
}

// DetectLongSilence detects whether an audio track contains continuous silence > 5.0 seconds below -50 dB.
func DetectLongSilence(filePath string) (bool, error) {
	// 1. Check track duration: if total duration is < 5.0 seconds, return false, nil.
	totalDuration, err := GetTrackDuration(filePath)
	if err != nil {
		log.Printf("Warning: failed to get track duration for %s: %v", filePath, err)
		return false, nil
	}
	if totalDuration < 5.0 {
		return false, nil
	}

	// 2. Check pure silence: if stats.PeakDb <= -100 or -inf, return true, nil.
	out, err := RunSoxStats(filePath)
	if err != nil {
		log.Printf("Warning: RunSoxStats failed in DetectLongSilence for %s: %v", filePath, err)
		return false, nil
	}
	stats, err := ParseSoxStats(out)
	if err != nil {
		log.Printf("Warning: ParseSoxStats failed in DetectLongSilence for %s: %v", filePath, err)
		return false, nil
	}
	if stats.PeakDb <= -100.0 || math.IsInf(stats.PeakDb, -1) {
		return true, nil
	}

	// 3. Run SoX silence trimming command in an isolated temporary directory:
	// sox <filePath> <outPattern> silence 1 5.0 -50d 1 5.0 -50d : newfile : restart
	tmpDir, err := os.MkdirTemp("", "silence_detect_*")
	if err != nil {
		return false, fmt.Errorf("failed to create temp dir for silence detection: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	outPattern := filepath.Join(tmpDir, "segment.wav")
	cmd := exec.Command("sox", filePath, outPattern, "silence", "1", "0.1", "-50d", "1", "5.0", "-50d", ":", "newfile", ":", "restart")
	if soxOut, err := cmd.CombinedOutput(); err != nil {
		log.Printf("Warning: SoX silence command failed for %s: %v (output: %s)", filePath, err, string(soxOut))
		return false, nil
	}

	// Detect if continuous silence periods > 5.0s exist below -50 dB
	// (e.g., from generated segment counts or trimmed duration).
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		log.Printf("Warning: failed to read temp dir %s: %v", tmpDir, err)
		return false, nil
	}

	if len(entries) > 1 {
		return true, nil
	}

	if len(entries) == 1 {
		segPath := filepath.Join(tmpDir, entries[0].Name())
		segDuration, err := GetTrackDuration(segPath)
		if err == nil && (totalDuration-segDuration) >= 5.0 {
			return true, nil
		}
	}

	return false, nil
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

	hasLongSilence, err := DetectLongSilence(filePath)
	if err != nil {
		log.Printf("Warning: DetectLongSilence failed for %s: %v", filePath, err)
		hasLongSilence = false
	}

	var dynamicRange float64
	if stats.PeakDb <= -100.0 || math.IsInf(stats.PeakDb, -1) {
		dynamicRange = 0.0
	} else {
		dynamicRange = stats.PeakDb - stats.RmsDb
	}

	score := CalculateTrackCleanliness(stats, hasLongSilence)

	return &TrackQuality{
		Filename:       filePath,
		SizeBytes:      info.Size(),
		ModTime:        info.ModTime(),
		PeakDb:         stats.PeakDb,
		RmsDb:          stats.RmsDb,
		ClippedCount:   stats.ClippedCount,
		DynamicRange:   dynamicRange,
		HasLongSilence: hasLongSilence,
		Score:          score,
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

// EvaluateRunCompletenessAndCleanliness evaluates the completeness and cleanliness scores for a specific run.
func EvaluateRunCompletenessAndCleanliness(tracks []string, expectedTracks int) (completenessScore int32, cleanlinessScore int32, totalScore int32, trackMap map[string]TrackQuality, err error) {
	completenessScore, err = CalculateCompletenessScore(len(tracks), expectedTracks)
	if err != nil {
		return 0, 0, 0, nil, err
	}

	trackMap = make(map[string]TrackQuality, len(tracks))
	for _, trackPath := range tracks {
		tq, err := AnalyzeTrack(trackPath)
		if err != nil {
			return 0, 0, 0, nil, err
		}
		trackMap[trackPath] = *tq
	}

	cleanlinessScore = CalculateAggregateCleanlinessFromMap(trackMap)
	totalScore = completenessScore + cleanlinessScore
	if totalScore < 0 {
		totalScore = 0
	} else if totalScore > 100 {
		totalScore = 100
	}

	return completenessScore, cleanlinessScore, totalScore, trackMap, nil
}

// isLaterDate returns true if date1 is more recent than date2.
func isLaterDate(date1, date2 string) bool {
	layouts := []string{"2006-01-02-02", "2006-01-02", time.RFC3339}
	var t1, t2 time.Time
	for _, layout := range layouts {
		if t1.IsZero() {
			if parsed, err := time.Parse(layout, date1); err == nil {
				t1 = parsed
			}
		}
		if t2.IsZero() {
			if parsed, err := time.Parse(layout, date2); err == nil {
				t2 = parsed
			}
		}
	}
	if !t1.IsZero() && !t2.IsZero() && !t1.Equal(t2) {
		return t1.After(t2)
	}
	return date1 > date2
}

// EvaluateDiskRuns evaluates all runs for a single disk and returns the winning DiskQualitySummary and associated tracks.
func EvaluateDiskRuns(disk int32, diskRuns map[string][]string, expectedTracks int) (*DiskQualitySummary, map[string]TrackQuality, error) {
	if len(diskRuns) == 0 {
		return &DiskQualitySummary{
			Disk:        disk,
			BestRipDate: "",
			Score:       0,
		}, make(map[string]TrackQuality), nil
	}

	runDates := make([]string, 0, len(diskRuns))
	for date := range diskRuns {
		runDates = append(runDates, date)
	}
	sort.Strings(runDates)

	var (
		hasWinner     bool
		winningDate   string
		winningScore  int32
		winningTracks map[string]TrackQuality
	)

	for _, date := range runDates {
		trackFiles := diskRuns[date]
		_, _, totalScore, trackMap, err := EvaluateRunCompletenessAndCleanliness(trackFiles, expectedTracks)
		if err != nil {
			return nil, nil, err
		}

		if !hasWinner {
			hasWinner = true
			winningDate = date
			winningScore = totalScore
			winningTracks = trackMap
		} else if totalScore > winningScore {
			winningDate = date
			winningScore = totalScore
			winningTracks = trackMap
		} else if totalScore == winningScore {
			if isLaterDate(date, winningDate) {
				winningDate = date
				winningScore = totalScore
				winningTracks = trackMap
			}
		}
	}

	return &DiskQualitySummary{
		Disk:        disk,
		BestRipDate: winningDate,
		Score:       winningScore,
	}, winningTracks, nil
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
		var diskQualities []*pb.DiskQuality
		for _, d := range cached.Disks {
			diskQualities = append(diskQualities, &pb.DiskQuality{
				Disk:        d.Disk,
				BestRipDate: d.BestRipDate,
				Score:       d.Score,
			})
		}
		return &pb.GetQualityResponse{
			Score:         cached.Score,
			DiskQualities: diskQualities,
		}, nil
	}

	// 2. On cache miss or invalidated cache:
	// Fetch metadata from recordcollection (to obtain FormatQuantity and per-disk expected track counts via getExpectedTracks).
	rel, err := GetReleaseMetadata(ctx, s.rcClient, releaseID)
	if err != nil {
		return nil, err
	}

	// Scan files and parse into disk/run groups via GroupTracksByDiskAndRun.
	files, err := ScanFlacFiles(dir, releaseID)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, status.Errorf(codes.NotFound, "no FLAC files found for release %d", releaseID)
	}

	diskRuns, err := GroupTracksByDiskAndRun(files, releaseID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to group tracks: %v", err)
	}

	formatQuantity := rel.GetFormatQuantity()
	if formatQuantity <= 0 {
		formatQuantity = 1
	}

	var diskSummaries []DiskQualitySummary
	var diskQualities []*pb.DiskQuality
	combinedTracks := make(map[string]TrackQuality)

	for d := int32(1); d <= formatQuantity; d++ {
		expectedTracks := getExpectedTracks(rel, d)
		runs, hasRuns := diskRuns[d]
		if hasRuns && len(runs) > 0 {
			dSummary, trackMap, err := EvaluateDiskRuns(d, runs, expectedTracks)
			if err != nil {
				return nil, err
			}
			diskSummaries = append(diskSummaries, *dSummary)
			diskQualities = append(diskQualities, &pb.DiskQuality{
				Disk:        dSummary.Disk,
				BestRipDate: dSummary.BestRipDate,
				Score:       dSummary.Score,
			})
			for k, v := range trackMap {
				base := filepath.Base(k)
				v.Filename = base
				combinedTracks[base] = v
			}
		} else {
			dSummary := DiskQualitySummary{
				Disk:        d,
				BestRipDate: "",
				Score:       0,
			}
			diskSummaries = append(diskSummaries, dSummary)
			diskQualities = append(diskQualities, &pb.DiskQuality{
				Disk:        d,
				BestRipDate: "",
				Score:       0,
			})
		}
	}

	// Composite release score:
	// For multi-disk releases: min(disk_1, ..., disk_N). If any expected disk is missing, composite score is 0.
	// For single-disk releases: winning score of Disk 1.
	var totalScore int32
	if len(diskSummaries) > 0 {
		totalScore = diskSummaries[0].Score
		if formatQuantity > 1 {
			for _, ds := range diskSummaries[1:] {
				if ds.Score < totalScore {
					totalScore = ds.Score
				}
			}
		}
	}
	if totalScore < 0 {
		totalScore = 0
	} else if totalScore > 100 {
		totalScore = 100
	}

	summary := &QualitySummary{
		ReleaseID:      releaseID,
		Version:        CurrentScoringVersion,
		Score:          totalScore,
		ExpectedTracks: CalculateTotalExpectedTracks(rel),
		FoundTracks:    len(files),
		Disks:          diskSummaries,
		Tracks:         combinedTracks,
		LastEvaluated:  time.Now().UTC(),
	}

	if err := WriteQualitySummary(dir, releaseID, summary); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to write quality summary: %v", err)
	}

	return &pb.GetQualityResponse{
		Score:         totalScore,
		DiskQualities: diskQualities,
	}, nil
}
