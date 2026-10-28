package httpapi

import "testing"

func TestMissingColumns(t *testing.T) {
	if m := missingColumns([]string{"id", "company_id", "name", "email", "notes", "created_at"}); len(m) != 0 {
		t.Fatalf("healthy schema should have no missing columns, got %v", m)
	}
	m := missingColumns([]string{"id", "company_id", "name", "notes", "created_at"})
	if len(m) != 1 || m[0] != "email" {
		t.Fatalf("renamed email field should be detected, got %v", m)
	}
}
