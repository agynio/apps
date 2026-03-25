package store

import (
	"testing"

	"github.com/google/uuid"
)

func TestNormalizePageSize(t *testing.T) {
	if normalizePageSize(0) != defaultListPageSize {
		t.Fatalf("expected default page size for zero")
	}
	if normalizePageSize(-5) != defaultListPageSize {
		t.Fatalf("expected default page size for negative")
	}
	if normalizePageSize(maxListPageSize+10) != maxListPageSize {
		t.Fatalf("expected max page size clamp")
	}
	if normalizePageSize(25) != 25 {
		t.Fatalf("expected page size 25")
	}
}

func TestPageTokenRoundTrip(t *testing.T) {
	appID := uuid.New()
	token := encodePageToken(appID)
	parsed, err := decodePageToken(token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parsed != appID {
		t.Fatalf("expected %s, got %s", appID.String(), parsed.String())
	}
}

func TestDecodePageTokenInvalid(t *testing.T) {
	if _, err := decodePageToken(""); err == nil {
		t.Fatalf("expected error for empty token")
	}
	if _, err := decodePageToken("not-base64"); err == nil {
		t.Fatalf("expected error for invalid token")
	}
}
