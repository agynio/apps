package server

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc/metadata"
)

func TestIdentityFromMetadata(t *testing.T) {
	_, err := identityFromMetadata(context.Background())
	if err == nil {
		t.Fatalf("expected error for missing metadata")
	}

	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(identityMetadata, ""))
	_, err = identityFromMetadata(ctx)
	if err == nil {
		t.Fatalf("expected error for empty metadata value")
	}

	ctx = metadata.NewIncomingContext(context.Background(), metadata.Pairs(identityMetadata, "not-a-uuid"))
	_, err = identityFromMetadata(ctx)
	if err == nil {
		t.Fatalf("expected error for invalid uuid")
	}

	identityID := uuid.New()
	ctx = metadata.NewIncomingContext(context.Background(), metadata.Pairs(identityMetadata, identityID.String()))
	parsed, err := identityFromMetadata(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parsed != identityID {
		t.Fatalf("expected %s, got %s", identityID.String(), parsed.String())
	}
}

func TestParseUUID(t *testing.T) {
	_, err := parseUUID("")
	if err == nil {
		t.Fatalf("expected error for empty uuid")
	}

	_, err = parseUUID("not-a-uuid")
	if err == nil {
		t.Fatalf("expected error for invalid uuid")
	}

	identityID := uuid.New()
	parsed, err := parseUUID(identityID.String())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parsed != identityID {
		t.Fatalf("expected %s, got %s", identityID.String(), parsed.String())
	}
}
