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
