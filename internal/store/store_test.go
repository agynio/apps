package store

import "testing"

// A nil slice binds as SQL NULL and the permissions column is NOT NULL, so an
// app created without permissions failed the insert outright.
func TestNonNilStrings(t *testing.T) {
	if got := nonNilStrings(nil); got == nil || len(got) != 0 {
		t.Fatalf("expected an empty non-nil slice, got %#v", got)
	}
	values := []string{"thread:create"}
	if got := nonNilStrings(values); len(got) != 1 || got[0] != values[0] {
		t.Fatalf("expected the values to pass through, got %#v", got)
	}
}
