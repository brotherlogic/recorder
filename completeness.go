package main

import (
	"context"
	"math"
	"path/filepath"
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

// GetTotalExpectedTracks queries the recordcollection service for release metadata and calculates total expected tracks across all discs.
func GetTotalExpectedTracks(ctx context.Context, client pbrc.RecordCollectionServiceClient, releaseID int64) (int, error) {
	if client == nil {
		conn, err := utils.LFDialServer(ctx, "recordcollection")
		if err != nil {
			return 0, status.Errorf(codes.Unavailable, "failed to dial recordcollection: %v", err)
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
				return 0, status.Errorf(codes.NotFound, "release %d not found in recordcollection: %v", releaseID, err)
			case codes.Unavailable, codes.DeadlineExceeded:
				return 0, status.Errorf(codes.Unavailable, "recordcollection service unavailable: %v", err)
			default:
				return 0, status.Errorf(codes.Unavailable, "recordcollection error: %v", err)
			}
		}
		return 0, status.Errorf(codes.Unavailable, "recordcollection error: %v", err)
	}

	if res == nil || res.GetRecord() == nil || res.GetRecord().GetRelease() == nil {
		return 0, status.Errorf(codes.NotFound, "release %d has no metadata", releaseID)
	}

	expected := CalculateTotalExpectedTracks(res.GetRecord().GetRelease())
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
	if found == expected {
		return 50, nil
	}
	if found < expected {
		score := int32(50.0 * float64(found) / float64(expected))
		return score, nil
	}
	score := int32(math.Max(0, 50.0-float64(found-expected)*10))
	return score, nil
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
