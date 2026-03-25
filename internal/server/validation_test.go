package server

import "testing"

func TestValidateSlug(t *testing.T) {
	tests := []struct {
		slug    string
		wantErr bool
	}{
		{slug: "app", wantErr: false},
		{slug: "a1", wantErr: false},
		{slug: "my-app", wantErr: false},
		{slug: "a-1", wantErr: false},
		{slug: "", wantErr: true},
		{slug: "App", wantErr: true},
		{slug: "-bad", wantErr: true},
		{slug: "bad-", wantErr: true},
		{slug: "bad_name", wantErr: true},
		{slug: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", wantErr: true},
	}

	for _, test := range tests {
		err := validateSlug(test.slug)
		if test.wantErr && err == nil {
			t.Fatalf("expected error for slug %q", test.slug)
		}
		if !test.wantErr && err != nil {
			t.Fatalf("unexpected error for slug %q: %v", test.slug, err)
		}
	}
}

func TestValidateName(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{name: "Apps", wantErr: false},
		{name: "  Acme App  ", wantErr: false},
		{name: "", wantErr: true},
		{name: "   ", wantErr: true},
	}

	for _, test := range tests {
		err := validateName(test.name)
		if test.wantErr && err == nil {
			t.Fatalf("expected error for name %q", test.name)
		}
		if !test.wantErr && err != nil {
			t.Fatalf("unexpected error for name %q: %v", test.name, err)
		}
	}
}
