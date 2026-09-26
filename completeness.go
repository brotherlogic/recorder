package main

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"

	"github.com/brotherlogic/goserver/utils"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pbgd "github.com/brotherlogic/godiscogs/proto"
	pbrc "github.com/brotherlogic/recordcollection/proto"
)

// CompletenessResult contains the evaluated completeness metrics for a release.
type CompletenessResult struct {
	Score          int32
	ExpectedTracks int
	FoundTracks    int
	Files          []string
}

// CalculateTotalExpectedTracks calculates the total number of expected tracks across all discs of a release.
func CalculateTotalExpectedTracks(release *pbgd.Release) int {
	if release == nil {
		return 0
	}
	if release.GetFormatQuantity() <= 1 {
		return len(release.GetTracklist())
	}
	total := 0
	for disk := int32(1); disk <= release.GetFormatQuantity(); disk++ {
		total += getExpectedTracks(release, disk)
	}
	if total == 0 {
		return len(release.GetTracklist())
	}
	return total
}

// GetReleaseMetadata queries recordcollection for the release protobuf.
func GetReleaseMetadata(ctx context.Context, client pbrc.RecordCollectionServiceClient, releaseID int64) (*pbgd.Release, error) {
	if client == nil {
		conn, err := utils.LFDialServer(ctx, "recordcollection")
		if err != nil {
			return nil, status.Errorf(codes.Unavailable, "failed to dial recordcollection: %v", err)
		}
		defer conn.Close()
		client = pbrc.NewRecordCollectionServiceClient(conn)
	}

	res, err := client.GetRecord(ctx, &pbrc.GetRecordRequest{ReleaseId: int32(releaseID)})
	if err != nil {
		st, ok := status.FromError(err)
		if ok {
			switch st.Code() {
			case codes.NotFound:
				return nil, status.Errorf(codes.NotFound, "release %d not found in recordcollection: %v", releaseID, err)
			case codes.Unavailable, codes.DeadlineExceeded:
				return nil, status.Errorf(codes.Unavailable, "recordcollection service unavailable: %v", err)
			default:
				return nil, status.Errorf(codes.Unavailable, "recordcollection error: %v", err)
			}
		}
		return nil, status.Errorf(codes.Unavailable, "recordcollection error: %v", err)
	}

	if res == nil || res.GetRecord() == nil || res.GetRecord().GetRelease() == nil {
		return nil, status.Errorf(codes.NotFound, "release %d has no metadata", releaseID)
	}

	return res.GetRecord().GetRelease(), nil
}

// GetTotalExpectedTracks queries the recordcollection service for release metadata and calculates total expected tracks across all discs.
func GetTotalExpectedTracks(ctx context.Context, client pbrc.RecordCollectionServiceClient, releaseID int64) (int, error) {
	rel, err := GetReleaseMetadata(ctx, client, releaseID)
	if err != nil {
		return 0, err
	}

	expected := CalculateTotalExpectedTracks(rel)
	if expected <= 0 {
		return 0, status.Errorf(codes.NotFound, "release %d has no expected tracks", releaseID)
	}

	return expected, nil
}

// ScanFlacFiles scans baseDir/<release_id>/ matching finalized *track*.flac files.
func ScanFlacFiles(baseDir string, releaseID int64) ([]string, error) {
	if baseDir == "" {
		baseDir = *saveDir
	}
	releaseDir := filepath.Join(baseDir, strconv.FormatInt(releaseID, 10))
	matches, err := filepath.Glob(filepath.Join(releaseDir, "*track*.flac"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	return matches, nil
}

// CalculateCompletenessScore computes the completeness score on a 0-50 scale.
func CalculateCompletenessScore(found, expected int) (int32, error) {
	if found == 0 {
		return 0, status.Errorf(codes.NotFound, "no FLAC tracks found (found: 0)")
	}
	if expected <= 0 {
		return 0, status.Errorf(codes.NotFound, "invalid expected tracks count: %d", expected)
	}
	missing := expected - found
	switch {
	case missing == 0:
		return 50, nil
	case missing == 1:
		return 25, nil
	case missing == 2:
		return 10, nil
	case missing == 3:
		return 5, nil
	case missing >= 4:
		return 0, nil
	default:
		score := int32(math.Max(0, 50-10*float64(found-expected)))
		return score, nil
	}
}

// EvaluateCompleteness queries recordcollection, scans directory, and computes completeness score.
func EvaluateCompleteness(ctx context.Context, client pbrc.RecordCollectionServiceClient, baseDir string, releaseID int64) (*CompletenessResult, error) {
	expected, err := GetTotalExpectedTracks(ctx, client, releaseID)
	if err != nil {
		return nil, err
	}

	files, err := ScanFlacFiles(baseDir, releaseID)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, status.Errorf(codes.NotFound, "no FLAC files found for release %d", releaseID)
	}

	score, err := CalculateCompletenessScore(len(files), expected)
	if err != nil {
		return nil, err
	}

	return &CompletenessResult{
		Score:          score,
		ExpectedTracks: expected,
		FoundTracks:    len(files),
		Files:          files,
	}, nil
}

// TrackFileInfo contains parsed details from a track audio filename.
type TrackFileInfo struct {
	Path      string
	Filename  string
	ReleaseID int64
	Disk      int32
	Date      string
	TrackNum  int
}

var trackFilenameRegex = regexp.MustCompile(`^(?P<release>\d+)(?:_(?P<disk>\d+))?-(?P<date>\d{4}-\d{2}-\d{2}(?:-\d+)?).*_track_(?P<track>\d+)\.flac$`)

// ParseTrackFilename parses a track filename and extracts release, disk, date, and track number.
func ParseTrackFilename(filename string, releaseID int64) (*TrackFileInfo, error) {
	base := filepath.Base(filename)
	matches := trackFilenameRegex.FindStringSubmatch(base)
	if matches == nil {
		return nil, fmt.Errorf("filename %q does not match expected pattern", base)
	}

	relIdx := trackFilenameRegex.SubexpIndex("release")
	diskIdx := trackFilenameRegex.SubexpIndex("disk")
	dateIdx := trackFilenameRegex.SubexpIndex("date")
	trackIdx := trackFilenameRegex.SubexpIndex("track")

	parsedRelease, err := strconv.ParseInt(matches[relIdx], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse release ID: %w", err)
	}
	if releaseID > 0 && parsedRelease != releaseID {
		return nil, fmt.Errorf("release ID mismatch: expected %d, got %d", releaseID, parsedRelease)
	}

	var disk int32 = 1
	if matches[diskIdx] != "" {
		d, err := strconv.ParseInt(matches[diskIdx], 10, 32)
		if err != nil {
			return nil, fmt.Errorf("failed to parse disk number: %w", err)
		}
		if d > 0 {
			disk = int32(d)
		}
	}

	trackNum, err := strconv.Atoi(matches[trackIdx])
	if err != nil {
		return nil, fmt.Errorf("failed to parse track number: %w", err)
	}

	return &TrackFileInfo{
		Path:      filename,
		Filename:  base,
		ReleaseID: parsedRelease,
		Disk:      disk,
		Date:      matches[dateIdx],
		TrackNum:  trackNum,
	}, nil
}

// GroupTracksByDiskAndRun groups audio file paths by disk and recording run identifier.
func GroupTracksByDiskAndRun(files []string, releaseID int64) (map[int32]map[string][]string, error) {
	result := make(map[int32]map[string][]string)
	for _, file := range files {
		info, err := ParseTrackFilename(file, releaseID)
		if err != nil {
			// Non-matching or malformed filenames are skipped gracefully
			continue
		}
		if _, ok := result[info.Disk]; !ok {
			result[info.Disk] = make(map[string][]string)
		}
		result[info.Disk][info.Date] = append(result[info.Disk][info.Date], file)
	}
	for _, runs := range result {
		for run := range runs {
			sort.Strings(runs[run])
		}
	}
	return result, nil
}

