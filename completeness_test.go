package main

import (
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCalculateCompletenessScore(t *testing.T) {
	tests := []struct {
		name          string
		found         int
		expected      int
		expectedScore int32
		expectErrCode *codes.Code
	}{
		// Error cases
		{
			name:          "Zero found tracks",
			found:         0,
			expected:      5,
			expectedScore: 0,
			expectErrCode: codePtr(codes.NotFound),
		},
		{
			name:          "Zero expected tracks",
			found:         5,
			expected:      0,
			expectedScore: 0,
			expectErrCode: codePtr(codes.NotFound),
		},
		{
			name:          "Negative expected tracks",
			found:         5,
			expected:      -1,
			expectedScore: 0,
			expectErrCode: codePtr(codes.NotFound),
		},
		// Single track edge case
		{
			name:          "Single track match",
			found:         1,
			expected:      1,
			expectedScore: 50,
			expectErrCode: nil,
		},
		// Discrete step curve cases (missing tracks)
		{
			name:          "0 missing tracks",
			found:         10,
			expected:      10,
			expectedScore: 50,
			expectErrCode: nil,
		},
		{
			name:          "1 missing track",
			found:         9,
			expected:      10,
			expectedScore: 25,
			expectErrCode: nil,
		},
		{
			name:          "2 missing tracks",
			found:         8,
			expected:      10,
			expectedScore: 10,
			expectErrCode: nil,
		},
		{
			name:          "3 missing tracks",
			found:         7,
			expected:      10,
			expectedScore: 5,
			expectErrCode: nil,
		},
		{
			name:          "4 missing tracks",
			found:         6,
			expected:      10,
			expectedScore: 0,
			expectErrCode: nil,
		},
		{
			name:          "5 missing tracks",
			found:         5,
			expected:      10,
			expectedScore: 0,
			expectErrCode: nil,
		},
		// Extra track cases
		{
			name:          "1 extra track",
			found:         11,
			expected:      10,
			expectedScore: 40,
			expectErrCode: nil,
		},
		{
			name:          "2 extra tracks",
			found:         12,
			expected:      10,
			expectedScore: 30,
			expectErrCode: nil,
		},
		{
			name:          "5 extra tracks",
			found:         15,
			expected:      10,
			expectedScore: 0,
			expectErrCode: nil,
		},
		{
			name:          "6 extra tracks floored",
			found:         16,
			expected:      10,
			expectedScore: 0,
			expectErrCode: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			score, err := CalculateCompletenessScore(tc.found, tc.expected)
			if tc.expectErrCode != nil {
				if err == nil {
					t.Fatalf("expected error code %v, got nil error", *tc.expectErrCode)
				}
				st, ok := status.FromError(err)
				if !ok {
					t.Fatalf("expected grpc status error, got: %v", err)
				}
				if st.Code() != *tc.expectErrCode {
					t.Fatalf("expected error code %v, got %v", *tc.expectErrCode, st.Code())
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if score != tc.expectedScore {
					t.Fatalf("score mismatch for found=%d, expected=%d: got %d, want %d", tc.found, tc.expected, score, tc.expectedScore)
				}
			}
		})
	}
}

func codePtr(c codes.Code) *codes.Code {
	return &c
}
