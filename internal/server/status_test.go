package server

import (
	"errors"
	"testing"

	"github.com/agynio/apps/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestToStatusError(t *testing.T) {
	statusErr := toStatusError(store.NotFound("app"))
	if status.Code(statusErr) != codes.NotFound {
		t.Fatalf("expected not found, got %v", status.Code(statusErr))
	}

	statusErr = toStatusError(store.AlreadyExists("app"))
	if status.Code(statusErr) != codes.AlreadyExists {
		t.Fatalf("expected already exists, got %v", status.Code(statusErr))
	}

	statusErr = toStatusError(errors.New("boom"))
	if status.Code(statusErr) != codes.Internal {
		t.Fatalf("expected internal, got %v", status.Code(statusErr))
	}
}
