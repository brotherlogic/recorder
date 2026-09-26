package proto

import (
	"context"
	"testing"
)

type mockQualityServer struct {
	QualityServiceServer
}

func (m *mockQualityServer) GetQuality(ctx context.Context, req *GetQualityRequest) (*GetQualityResponse, error) {
	return &GetQualityResponse{Score: int32(req.GetReleaseId() % 100)}, nil
}

func TestQualityProtoTypes(t *testing.T) {
	req := &GetQualityRequest{ReleaseId: 85}
	if req.GetReleaseId() != 85 {
		t.Fatalf("expected release id 85, got %d", req.GetReleaseId())
	}

	res := &GetQualityResponse{Score: 90}
	if res.GetScore() != 90 {
		t.Fatalf("expected score 90, got %d", res.GetScore())
	}

	var server QualityServiceServer = &mockQualityServer{}
	resp, err := server.GetQuality(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.GetScore() != 85 {
		t.Fatalf("expected score 85, got %d", resp.GetScore())
	}
}

func TestDiskQualityProtoTypes(t *testing.T) {
	dq := &DiskQuality{
		Disk:        1,
		BestRipDate: "2026-09-25",
		Score:       85,
	}
	if dq.GetDisk() != 1 || dq.GetBestRipDate() != "2026-09-25" || dq.GetScore() != 85 {
		t.Fatalf("unexpected DiskQuality values: %+v", dq)
	}

	res := &GetQualityResponse{
		Score:         85,
		DiskQualities: []*DiskQuality{dq},
	}
	if len(res.GetDiskQualities()) != 1 {
		t.Fatalf("expected 1 disk quality, got %d", len(res.GetDiskQualities()))
	}
	if res.GetDiskQualities()[0].GetDisk() != 1 {
		t.Fatalf("expected disk 1, got %d", res.GetDiskQualities()[0].GetDisk())
	}
}
