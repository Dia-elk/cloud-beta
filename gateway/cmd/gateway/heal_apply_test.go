package main

import "testing"

func TestApplyableColumns(t *testing.T) {
	// A real missing-column proposal is applyable.
	cols, ok := applyableColumns("add_missing_columns", "email,notes")
	if !ok || len(cols) != 2 || cols[0] != "email" || cols[1] != "notes" {
		t.Fatalf("add_missing_columns email,notes should be applyable, got %v, %v", cols, ok)
	}
	// Simulated proposals are NEVER applyable, even for a whitelisted field.
	if _, ok := applyableColumns("simulated", "email"); ok {
		t.Fatal("simulated kind must not be applyable")
	}
	// Unknown kinds are preview-only.
	if _, ok := applyableColumns("rename_field", "email"); ok {
		t.Fatal("unknown kind must not be applyable")
	}
	if _, ok := applyableColumns("", "email"); ok {
		t.Fatal("empty kind must not be applyable")
	}
	// Unknown columns are not applyable.
	if _, ok := applyableColumns("add_missing_columns", "email,evil_col"); ok {
		t.Fatal("unknown column must not be applyable")
	}
	if _, ok := applyableColumns("add_missing_columns", "email; DROP TABLE clients"); ok {
		t.Fatal("injected column text must not be applyable")
	}
	// "id" is a primary key and is never applyable, even if a proposal names it.
	if _, ok := applyableColumns("add_missing_columns", "id"); ok {
		t.Fatal("id must never be applyable")
	}
	if _, ok := applyableColumns("add_missing_columns", "email,id"); ok {
		t.Fatal("a proposal mixing a whitelisted column with id must not be applyable")
	}
	// Empty column list is not applyable.
	if _, ok := applyableColumns("add_missing_columns", ""); ok {
		t.Fatal("empty column list must not be applyable")
	}
	// Duplicates collapse to one apply, in order.
	cols, ok = applyableColumns("add_missing_columns", "notes,email,notes")
	if !ok || len(cols) != 2 || cols[0] != "notes" || cols[1] != "email" {
		t.Fatalf("duplicates should dedupe in order, got %v, %v", cols, ok)
	}
}

func TestHealWhitelistDefinitions(t *testing.T) {
	// The whitelist must be exactly the known clients schema types —
	// nothing else may ever reach an ALTER TABLE from this path.
	want := map[string]string{
		"email":      "TEXT NOT NULL DEFAULT ''",
		"notes":      "TEXT NOT NULL DEFAULT ''",
		"name":       "TEXT NOT NULL DEFAULT ''",
		"company_id": "TEXT NOT NULL DEFAULT ''",
		"created_at": "TIMESTAMPTZ NOT NULL DEFAULT now()",
	}
	if len(healColumnDefinitions) != len(want) {
		t.Fatalf("whitelist has %d entries, want %d: %v", len(healColumnDefinitions), len(want), healColumnDefinitions)
	}
	for col, def := range want {
		if healColumnDefinitions[col] != def {
			t.Fatalf("whitelist %q = %q, want %q", col, healColumnDefinitions[col], def)
		}
	}
	if _, ok := healColumnDefinitions["id"]; ok {
		t.Fatal("whitelist must not contain id")
	}
}

func TestIsTemplateRepo(t *testing.T) {
	for _, repo := range []string{"", "sample-app", "template:client-tracker"} {
		if !isTemplateRepo(repo) {
			t.Fatalf("template repo %q should be deployable", repo)
		}
	}
	for _, repo := range []string{"git:github.com/acme/app", "https://github.com/acme/app", "http://example.com/app.git", "acme/app", "my-app/repo", "git@github.com:acme/app"} {
		if isTemplateRepo(repo) {
			t.Fatalf("non-template repo %q must not deploy in MVP v1", repo)
		}
	}
}
