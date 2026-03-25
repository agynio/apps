package server

import (
	"encoding/hex"
	"testing"
)

func TestNewServiceToken(t *testing.T) {
	token, hash, err := newServiceToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(token) != 64 {
		t.Fatalf("expected token length 64, got %d", len(token))
	}
	if _, err := hex.DecodeString(token); err != nil {
		t.Fatalf("token was not hex: %v", err)
	}
	if hash != hashServiceToken(token) {
		t.Fatalf("hash did not match token")
	}

	secondToken, secondHash, err := newServiceToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token == secondToken {
		t.Fatalf("expected tokens to differ")
	}
	if hash == secondHash {
		t.Fatalf("expected hashes to differ")
	}
}
