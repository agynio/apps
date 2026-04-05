package server

import "testing"

func TestValidatePermissions(t *testing.T) {
	tests := []struct {
		name        string
		permissions []string
		wantErr     bool
	}{
		{
			name:        "valid",
			permissions: []string{"thread:create", "thread:write"},
			wantErr:     false,
		},
		{
			name:        "unknown",
			permissions: []string{"unknown"},
			wantErr:     true,
		},
		{
			name:        "duplicate",
			permissions: []string{"thread:create", "thread:create"},
			wantErr:     true,
		},
		{
			name:        "empty",
			permissions: nil,
			wantErr:     false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validatePermissions(test.permissions)
			if test.wantErr && err == nil {
				t.Fatalf("expected error")
			}
			if !test.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
