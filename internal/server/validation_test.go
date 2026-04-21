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

func TestValidateSlug(t *testing.T) {
	tests := []struct {
		name    string
		slug    string
		wantErr bool
	}{
		{
			name:    "valid",
			slug:    "demo-app",
			wantErr: false,
		},
		{
			name:    "empty",
			slug:    "",
			wantErr: true,
		},
		{
			name:    "too long",
			slug:    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			wantErr: true,
		},
		{
			name:    "invalid pattern",
			slug:    "Demo-App",
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateSlug(test.slug)
			if test.wantErr && err == nil {
				t.Fatalf("expected error")
			}
			if !test.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateName(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{
			name:    "valid",
			value:   "Demo App",
			wantErr: false,
		},
		{
			name:    "empty",
			value:   "",
			wantErr: true,
		},
		{
			name:    "whitespace",
			value:   "  ",
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateName(test.value)
			if test.wantErr && err == nil {
				t.Fatalf("expected error")
			}
			if !test.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateAuditLogMessage(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{
			name:    "valid",
			value:   "Installation started",
			wantErr: false,
		},
		{
			name:    "empty",
			value:   "",
			wantErr: true,
		},
		{
			name:    "whitespace",
			value:   "  ",
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateAuditLogMessage(test.value)
			if test.wantErr && err == nil {
				t.Fatalf("expected error")
			}
			if !test.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
